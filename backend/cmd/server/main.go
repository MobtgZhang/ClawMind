package main

import (
	"context"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/joho/godotenv"
	"github.com/mobtgzhang/clawmind/backend/internal/api"
	"github.com/mobtgzhang/clawmind/backend/internal/clawmindcfg"
	"github.com/mobtgzhang/clawmind/backend/internal/evolution"
	"github.com/mobtgzhang/clawmind/backend/internal/harness"
	"github.com/mobtgzhang/clawmind/backend/internal/llm"
	"github.com/mobtgzhang/clawmind/backend/internal/mcpclient"
	"github.com/mobtgzhang/clawmind/backend/internal/memory"
	"github.com/mobtgzhang/clawmind/backend/internal/plugin"
	"github.com/mobtgzhang/clawmind/backend/internal/store"
)

func main() {
	_ = godotenv.Load()
	_ = godotenv.Load("../.env")

	addr := getenv("LISTEN", ":8080")
	dbPath := getenv("DB_PATH", "./data/clawmind.db")
	if err := os.MkdirAll(filepath.Dir(dbPath), 0o755); err != nil {
		slog.Error("mkdir data", "err", err)
		os.Exit(1)
	}
	st, err := store.Open(dbPath)
	if err != nil {
		slog.Error("store open", "err", err)
		os.Exit(1)
	}
	defer st.Close()

	client := llm.NewClient(llm.Config{
		HTTPClient: llm.DefaultHTTPClient(),
	})

	wd, err := os.Getwd()
	if err != nil {
		slog.Error("getwd", "err", err)
		os.Exit(1)
	}
	clawDir := strings.TrimSpace(os.Getenv("CLAWMIND_DIR"))
	if clawDir == "" {
		if filepath.Base(wd) == "backend" {
			clawDir = filepath.Join(wd, "..", ".clawmind")
		} else {
			clawDir = filepath.Join(wd, ".clawmind")
		}
	}
	clawDir = filepath.Clean(clawDir)
	cfgMgr := clawmindcfg.NewManager(clawDir)
	if err := cfgMgr.EnsureDir(); err != nil {
		slog.Error("clawmind dir", "err", err)
		os.Exit(1)
	}
	slog.Info("config file", "path", cfgMgr.ConfigPath())

	fileCfg, _ := cfgMgr.Load()
	resolved := fileCfg.Resolved()

	// The plugin home is the composition point: every part of the product is a plugin, so boot loads
	// ordered layers and reports what was skipped instead of silently dropping it. The layers are
	// resolved before anything reads config, because a patch layer is operator config and wins over
	// the file.
	pluginHome := strings.TrimSpace(os.Getenv("CLAWMIND_PLUGIN_DIR"))
	if pluginHome == "" {
		pluginHome = filepath.Join(clawDir, "plugins")
	}
	reg := plugin.NewRegistry(pluginHome)
	skipped, err := reg.Load()
	if err != nil {
		slog.Warn("plugin registry", "err", err)
	}
	for _, problem := range skipped {
		slog.Warn("plugin skipped", "problem", problem)
	}
	runtime := plugin.Resolve(reg)
	dispatcher := plugin.NewDispatcher(reg)
	// Hook commands run inside the plugin home. High-risk hooks are refused unless the operator
	// explicitly allows approval, so a plugin cannot quietly shell out.
	dispatcher.SetRunner(plugin.DefaultRunner(pluginHome, nil))
	if runtime.Hooks.AllowShell != nil {
		dispatcher.SetAllowShell(*runtime.Hooks.AllowShell)
	}
	dispatcher.SetLimits(runtime.Hooks.MaxOutputChars, runtime.Hooks.Timeout)
	if runtime.Prompt.AlwaysOnMax > 0 {
		harness.SetAlwaysOnMax(runtime.Prompt.AlwaysOnMax)
	}
	slog.Info("plugin registry", "home", pluginHome, "layers", len(reg.Layers()))

	var mem memory.Store
	memBackend := strings.ToLower(strings.TrimSpace(os.Getenv("CLAWMIND_MEMORY_BACKEND")))
	if runtime.Memory.Backend != "" {
		memBackend = strings.ToLower(runtime.Memory.Backend)
	}
	emModel := strings.TrimSpace(os.Getenv("CLAWMIND_EMBEDDING_MODEL"))
	if runtime.Memory.EmbeddingModel != "" {
		emModel = runtime.Memory.EmbeddingModel
	}
	var embed memory.EmbedFunc
	if emModel != "" {
		embed = func(ctx context.Context, text string) ([]float32, error) {
			return client.EmbedText(ctx, resolved.BaseURL, resolved.APIKey, emModel, text)
		}
		slog.Info("memory semantic RAG", "embeddingModel", emModel)
	}
	retriever := memory.NewRetriever(embed)
	retriever.Budget = memoryBudgetFromEnv(runtime)
	layerCap := memoryLayerCapFromEnv(runtime)
	if memBackend == "memory" {
		memStore := memory.NewInMemoryStore()
		memStore.Cap = layerCap
		mem = memStore
		slog.Info("memory backend", "mode", "in-memory", "caps", layerCap)
	} else {
		sqlMem := memory.NewSQLiteStore(st.DB())
		sqlMem.Embed = embed
		sqlMem.Ret = retriever
		sqlMem.Cap = layerCap
		if err := sqlMem.EnsureSchema(); err != nil {
			slog.Warn("memory schema", "err", err)
		}
		mem = sqlMem
		slog.Info("memory backend", "mode", "sqlite", "maxItems", retriever.Budget.MaxItems, "charBudget", retriever.Budget.CharBudget)
	}

	var mcpSess *mcpclient.Session
	if mcpCmd := strings.TrimSpace(os.Getenv("CLAWMIND_MCP_COMMAND")); mcpCmd != "" {
		mctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		sess, err := mcpclient.Connect(mctx, mcpCmd, splitPipeArgs(os.Getenv("CLAWMIND_MCP_ARGS")), parseEnvPairs(os.Getenv("CLAWMIND_MCP_ENV")))
		cancel()
		if err != nil {
			slog.Warn("mcp connect failed", "err", err)
		} else {
			mcpSess = sess
			slog.Info("mcp connected", "tools", len(sess.Definitions()))
			defer func() { _ = sess.Close() }()
		}
	}

	toolsPath := getenv("TOOLS_PATH", "./config/tools.json")
	skillsPath := filepath.Join(clawDir, "skills.json")
	if runtime.Tools.ToolsPath != "" {
		toolsPath = runtime.Tools.ToolsPath
	}
	if runtime.Tools.SkillsPath != "" {
		skillsPath = runtime.Tools.SkillsPath
	}
	srv := api.NewServer(st, cfgMgr, client, mem, toolsPath, skillsPath, mcpSess)
	slog.Info("skills file", "path", skillsPath)

	srv.Plugins = reg
	srv.Dispatcher = dispatcher
	srv.Runtime = runtime
	srv.Cap = layerCap

	assets := memory.NewAssetRegistry(st.DB())
	if err := assets.EnsureSchema(); err != nil {
		slog.Warn("memory asset registry", "err", err)
	}
	srv.Assets = assets
	srv.Ret = retriever
	srv.Pipeline = memory.NewPipeline(mem, client, memory.DistillConfig{
		BaseURL: resolved.BaseURL,
		APIKey:  resolved.APIKey,
		Model:   resolved.Model,
		Async:   distillAsync(envBool("CLAWMIND_MEMORY_DISTILL_ASYNC", true), runtime.Memory.DistillAsync),
	})
	defer srv.Pipeline.Wait()

	harnessRoot := strings.TrimSpace(os.Getenv("CLAWMIND_HARNESS_DIR"))
	if harnessRoot == "" {
		harnessRoot = filepath.Join(clawDir, "harness")
	}
	srv.HarnessRoot = harnessRoot
	if envBool("CLAWMIND_EVOLUTION", false) {
		mode := getenv("CLAWMIND_EVOLUTION_MODE", "parallel")
		k := envInt("CLAWMIND_EVOLUTION_K", 3)
		srv.Harness = evolution.New(st, client, evolution.Config{
			K:             k,
			Mode:          mode,
			Root:          harnessRoot,
			BaseURL:       resolved.BaseURL,
			APIKey:        resolved.APIKey,
			Model:         resolved.Model,
			Workdir:       wd,
			ProbeExec:     envBool("CLAWMIND_PROBE_EXEC", false),
			Resume:        envBool("CLAWMIND_EVOLUTION_RESUME", true),
			EnforceLedger: envBool("CLAWMIND_EVOLUTION_LEDGER", true),
			SeedSkills:    envBool("CLAWMIND_SEED_SKILLS", true),
		})
		if envBool("CLAWMIND_PROBE_EXEC", false) {
			srv.Harness.SetProbeRunner(evolution.NewExecProbe(wd))
		}
		slog.Info("self-evolution engine", "mode", mode, "k", k, "root", harnessRoot)
	}

	gin.SetMode(gin.ReleaseMode)
	r := gin.New()
	r.Use(gin.Recovery())
	r.Use(gin.Logger())
	r.Use(devCORS())
	srv.Register(r)

	if !strings.HasPrefix(addr, ":") && !strings.Contains(addr, ".") {
		addr = ":" + addr
	}
	slog.Info("listening", "addr", addr)
	if err := r.Run(addr); err != nil {
		slog.Error("server", "err", err)
		os.Exit(1)
	}
}

func splitPipeArgs(s string) []string {
	s = strings.TrimSpace(s)
	if s == "" {
		return nil
	}
	return strings.Split(s, "|")
}

func parseEnvPairs(s string) []string {
	var out []string
	for _, p := range strings.Split(s, ";") {
		p = strings.TrimSpace(p)
		if p != "" {
			out = append(out, p)
		}
	}
	return out
}

func getenv(k, def string) string {
	v := strings.TrimSpace(os.Getenv(k))
	if v == "" {
		return def
	}
	return v
}

func envInt(k string, def int) int {
	v := strings.TrimSpace(os.Getenv(k))
	if v == "" {
		return def
	}
	n, err := strconv.Atoi(v)
	if err != nil || n < 1 {
		return def
	}
	return n
}

func envBool(k string, def bool) bool {
	switch strings.ToLower(strings.TrimSpace(os.Getenv(k))) {
	case "":
		return def
	case "1", "true", "yes", "on":
		return true
	case "0", "false", "no", "off":
		return false
	}
	return def
}

func devCORS() gin.HandlerFunc {
	allowed := map[string]struct{}{
		"http://127.0.0.1:5173": {},
		"http://localhost:5173": {},
	}
	return func(c *gin.Context) {
		o := c.GetHeader("Origin")
		if _, ok := allowed[o]; ok {
			c.Header("Access-Control-Allow-Origin", o)
			c.Header("Access-Control-Allow-Credentials", "true")
		}
		c.Header("Access-Control-Allow-Methods", "GET, POST, PUT, PATCH, DELETE, OPTIONS")
		c.Header("Access-Control-Allow-Headers", "Origin, Content-Type, Accept")
		if c.Request.Method == http.MethodOptions {
			c.AbortWithStatus(http.StatusNoContent)
			return
		}
		c.Next()
	}
}

// memoryBudgetFromEnv reads the retrieval budget. Retrieval is capped by item count, characters
// and time: an uncapped layer eventually pushes the rest of the context out, which is the failure
// mode these caps exist to prevent.
func memoryBudgetFromEnv(layer plugin.Runtime) memory.Budget {
	b := memory.DefaultBudget()
	if n := envInt("CLAWMIND_MEMORY_MAX_ITEMS", 0); n > 0 {
		b.MaxItems = n
	}
	if n := envInt("CLAWMIND_MEMORY_CHAR_BUDGET", 0); n > 0 {
		b.CharBudget = n
	}
	if n := envInt("CLAWMIND_MEMORY_TIMEOUT_MS", 0); n > 0 {
		b.Timeout = time.Duration(n) * time.Millisecond
	}
	// The patch layer is read after the environment so an operator can override the defaults
	// without editing a plugin, while a plugin still wins when it declares the key.
	if layer.Memory.MaxItems > 0 {
		b.MaxItems = layer.Memory.MaxItems
	}
	if layer.Memory.CharBudget > 0 {
		b.CharBudget = layer.Memory.CharBudget
	}
	if layer.Memory.Timeout > 0 && layer.Memory.Timeout < b.Timeout {
		b.Timeout = layer.Memory.Timeout
	}
	return b
}

// memoryLayerCapFromEnv reads the per-layer item limits.
func memoryLayerCapFromEnv(layer plugin.Runtime) memory.LayerCap {
	c := memory.DefaultLayerCap()
	if n := envInt("CLAWMIND_MEMORY_L0_CAP", 0); n > 0 {
		c.L0 = n
	}
	if n := envInt("CLAWMIND_MEMORY_L1_CAP", 0); n > 0 {
		c.L1 = n
	}
	if n := envInt("CLAWMIND_MEMORY_L2_CAP", 0); n > 0 {
		c.L2 = n
	}
	if n := envInt("CLAWMIND_MEMORY_L3_CAP", 0); n > 0 {
		c.L3 = n
	}
	// Caps may only be lowered by a patch layer: a layer that raised one would let a layer grow
	// past the budget the cap exists to protect.
	for name, value := range layer.Memory.Caps {
		if value <= 0 {
			continue
		}
		target := memory.Layer(-1)
		// Layer names are matched case-insensitively: the docs and the census label them L0-L3, so a
		// manifest that writes "L1" must not be silently ignored.
		switch strings.ToLower(name) {
		case "l0":
			target = memory.LayerConversation
		case "l1":
			target = memory.LayerAtom
		case "l2":
			target = memory.LayerScenario
		case "l3":
			target = memory.LayerPersona
		}
		if target < 0 {
			continue
		}
		if current := c.ForLayer(target); value < current {
			c.SetLayer(target, value)
		}
	}
	return c
}

func distillAsync(def bool, layer *bool) bool {
	if layer != nil {
		return *layer
	}
	return def
}
