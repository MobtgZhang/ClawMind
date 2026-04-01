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
	"github.com/mobtgzhang/clawmind/backend/internal/llm"
	"github.com/mobtgzhang/clawmind/backend/internal/mcpclient"
	"github.com/mobtgzhang/clawmind/backend/internal/memory"
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

	var mem memory.Store
	memBackend := strings.ToLower(strings.TrimSpace(os.Getenv("CLAWMIND_MEMORY_BACKEND")))
	if memBackend == "memory" {
		mem = memory.NewInMemoryStore()
		slog.Info("memory backend", "mode", "in-memory")
	} else {
		sqlMem := memory.NewSQLiteStore(st.DB())
		if k, err := strconv.Atoi(strings.TrimSpace(os.Getenv("CLAWMIND_MEMORY_SEMANTIC_TOP_K"))); err == nil && k > 0 {
			sqlMem.SemanticTopK = k
		}
		emModel := strings.TrimSpace(os.Getenv("CLAWMIND_EMBEDDING_MODEL"))
		if emModel != "" {
			sqlMem.Embed = func(ctx context.Context, text string) ([]float32, error) {
				return client.EmbedText(ctx, resolved.BaseURL, resolved.APIKey, emModel, text)
			}
			slog.Info("memory semantic RAG", "embeddingModel", emModel)
		}
		mem = sqlMem
		slog.Info("memory backend", "mode", "sqlite")
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
	srv := api.NewServer(st, cfgMgr, client, mem, toolsPath, skillsPath, mcpSess)
	slog.Info("skills file", "path", skillsPath)

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
