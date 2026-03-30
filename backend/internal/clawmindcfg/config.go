package clawmindcfg

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync"
)

// Config is persisted as .clawmind/config.json (camelCase JSON).
type Config struct {
	OpenAIBaseURL   string   `json:"openaiBaseUrl"`
	OpenAIAPIKey    string   `json:"openaiApiKey"`
	OpenAIModel     string   `json:"openaiModel"`
	SystemPrompt    string   `json:"systemPrompt"`
	Temperature     float64  `json:"temperature"`
	TopP            float64  `json:"topP"`
	TopK            *int     `json:"topK,omitempty"`
	MaxAgentRounds  int      `json:"maxAgentRounds"`
}

// Resolved merges file config with environment fallbacks for API calls.
type Resolved struct {
	BaseURL        string
	APIKey         string
	Model          string
	SystemPrompt   string
	Temperature    float64
	TopP           float64
	TopK           *int
	MaxAgentRounds int
}

// Manager loads and saves config under a directory (e.g. .clawmind).
type Manager struct {
	dir string
	mu  sync.RWMutex
}

// NewManager creates a manager; dir is the path to the .clawmind directory (not the file).
func NewManager(dir string) *Manager {
	return &Manager{dir: filepath.Clean(dir)}
}

func (m *Manager) Dir() string { return m.dir }

// ConfigPath is the full path to config.json.
func (m *Manager) ConfigPath() string {
	return filepath.Join(m.dir, "config.json")
}

func (m *Manager) filePath() string {
	return filepath.Join(m.dir, "config.json")
}

// EnsureDir creates .clawmind and default config.json if missing.
func (m *Manager) EnsureDir() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if err := os.MkdirAll(m.dir, 0o755); err != nil {
		return err
	}
	path := m.filePath()
	if _, err := os.Stat(path); os.IsNotExist(err) {
		return m.saveUnlocked(Default())
	}
	return nil
}

// Default returns factory defaults.
func Default() Config {
	return Config{
		OpenAIBaseURL:  "https://api.openai.com/v1",
		OpenAIAPIKey:   "",
		OpenAIModel:    "gpt-4o-mini",
		SystemPrompt:   "你是 ClawMind 中的 AI 助手，请诚实、准确、有条理地帮助用户。",
		Temperature:    0.7,
		TopP:           1,
		TopK:           nil,
		MaxAgentRounds: 16,
	}
}

// Load reads config.json; if corrupt or missing, returns Default() after trying to read.
func (m *Manager) Load() (Config, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	path := m.filePath()
	b, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return Default(), nil
		}
		return Config{}, err
	}
	var c Config
	if err := json.Unmarshal(b, &c); err != nil {
		return Config{}, err
	}
	normalizeInPlace(&c)
	return c, nil
}

// fixCommonBaseURLTypo prepends a missing leading "h" when the URL was saved as "ttps://..." or "ttp://...".
func fixCommonBaseURLTypo(s string) string {
	s = strings.TrimSpace(s)
	switch {
	case strings.HasPrefix(s, "ttps://"):
		return "h" + s
	case strings.HasPrefix(s, "ttp://"):
		return "h" + s
	default:
		return s
	}
}

func normalizeInPlace(c *Config) {
	c.OpenAIBaseURL = fixCommonBaseURLTypo(c.OpenAIBaseURL)
	if c.TopK != nil && *c.TopK <= 0 {
		c.TopK = nil
	}
	if c.MaxAgentRounds < 1 {
		c.MaxAgentRounds = 16
	}
	if c.MaxAgentRounds > 256 {
		c.MaxAgentRounds = 256
	}
}

// Save writes the full config (atomic replace).
func (m *Manager) Save(c Config) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	normalizeInPlace(&c)
	return m.saveUnlocked(c)
}

func (m *Manager) saveUnlocked(c Config) error {
	if err := os.MkdirAll(m.dir, 0o755); err != nil {
		return err
	}
	b, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return err
	}
	path := m.filePath()
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, b, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

// Resolved builds runtime LLM parameters (env used when file leaves fields empty).
func (c Config) Resolved() Resolved {
	out := Resolved{
		BaseURL:        fixCommonBaseURLTypo(strings.TrimSpace(c.OpenAIBaseURL)),
		Model:          strings.TrimSpace(c.OpenAIModel),
		SystemPrompt:   strings.TrimSpace(c.SystemPrompt),
		Temperature:    c.Temperature,
		TopP:           c.TopP,
		TopK:           c.TopK,
		MaxAgentRounds: c.MaxAgentRounds,
	}
	if out.BaseURL == "" {
		out.BaseURL = fixCommonBaseURLTypo(getenvDefault("OPENAI_BASE_URL", "https://api.openai.com/v1"))
	}
	if out.Model == "" {
		out.Model = getenvDefault("OPENAI_MODEL", "gpt-4o-mini")
	}
	if out.SystemPrompt == "" {
		out.SystemPrompt = getenvDefault("SYSTEM_PROMPT", "你是 ClawMind 中的 AI 助手，请诚实、准确、有条理地帮助用户。")
	}
	out.APIKey = strings.TrimSpace(c.OpenAIAPIKey)
	if out.APIKey == "" {
		out.APIKey = strings.TrimSpace(os.Getenv("OPENAI_API_KEY"))
	}
	if out.MaxAgentRounds < 1 {
		out.MaxAgentRounds = 1
	}
	if out.MaxAgentRounds > 256 {
		out.MaxAgentRounds = 256
	}
	return out
}

func getenvDefault(k, def string) string {
	v := strings.TrimSpace(os.Getenv(k))
	if v == "" {
		return def
	}
	return v
}
