package moduscfg

import (
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

// CartridgeConfig defines one office assignment in MODUS.
type CartridgeConfig struct {
	Role    string `yaml:"role"`
	Provider string `yaml:"provider,omitempty"`
	Family  string `yaml:"family"`
	Model   string `yaml:"model"`
	Backend string `yaml:"backend"`
}

// MainBrainConfig defines the active sovereign cartridge for MODUS.
type MainBrainConfig = CartridgeConfig

// OfficersConfig holds the standing specialist offices.
type OfficersConfig struct {
	Librarian CartridgeConfig `yaml:"librarian"`
	Coder     CartridgeConfig `yaml:"coder"`
	Inspector CartridgeConfig `yaml:"inspector"`
	Scout     CartridgeConfig `yaml:"scout"`
}

// Config is the persisted ~/.modus/config.yaml shape relevant to runtime.
type Config struct {
	ProjectName string          `yaml:"project_name"`
	ProjectDir  string          `yaml:"project_dir"`
	OS          string          `yaml:"os"`
	Arch        string          `yaml:"arch"`
	CPUs        int             `yaml:"cpus"`
	TrustStage  int             `yaml:"trust_stage"`
	MainBrain   MainBrainConfig `yaml:"main_brain"`
	Officers    OfficersConfig  `yaml:"officers"`
}

type OfficeOption struct {
	Label      string
	Assignment CartridgeConfig
}

type ProviderCatalog struct {
	Provider string
	Family   string
	Backend  string
	Models   []string
}

// DefaultAssignment returns the default staffing for one office.
func DefaultAssignment(role string) CartridgeConfig {
	switch role {
	case "librarian":
		return CartridgeConfig{Role: "librarian", Provider: "ollama", Family: "local", Model: "gemma4:26b", Backend: "ollama"}
	case "coder":
		return CartridgeConfig{Role: "coder", Provider: "ollama", Family: "local", Model: "gemma4:31b", Backend: "ollama"}
	case "inspector":
		return CartridgeConfig{Role: "inspector", Provider: "qwen", Family: "cloud", Model: "qwen-3.6", Backend: "cli"}
	case "scout":
		return CartridgeConfig{Role: "scout", Provider: "google", Family: "cloud", Model: "gemini-2.5-flash", Backend: "api"}
	default:
		return CartridgeConfig{Role: "modus", Provider: "openai", Family: "openai", Model: "chatgpt", Backend: "sdk"}
	}
}

func OfficeDisplayName(role string) string {
	switch role {
	case "main_brain":
		return "Commanding Officer"
	case "librarian":
		return "Librarian"
	case "coder":
		return "Minor Coder"
	case "inspector":
		return "Inspector"
	case "scout":
		return "Scout"
	default:
		return role
	}
}

func ProviderCatalogs() []ProviderCatalog {
	return []ProviderCatalog{
		{Provider: "openai", Family: "openai", Backend: "sdk", Models: []string{"chatgpt", "gpt-5.4", "gpt-5.4-mini", "gpt-5.4-nano"}},
		{Provider: "anthropic", Family: "anthropic", Backend: "api", Models: []string{"claude-sonnet-4-6", "claude-opus-4-6", "claude-haiku-4-5"}},
		{Provider: "google", Family: "cloud", Backend: "api", Models: []string{"gemini-2.5-pro", "gemini-2.5-flash", "gemini-2.5-flash-lite"}},
		{Provider: "qwen", Family: "cloud", Backend: "api", Models: []string{"qwen-plus", "qwen-max", "qwen3-235b-a22b"}},
		{Provider: "moonshot", Family: "cloud", Backend: "api", Models: []string{"kimi-k2.5", "kimi-k2-thinking", "kimi-k2-turbo-preview"}},
		{Provider: "minimax", Family: "cloud", Backend: "api", Models: []string{"MiniMax-M2.5", "MiniMax-M2.5-highspeed", "MiniMax-M2.1"}},
		{Provider: "ollama", Family: "local", Backend: "ollama", Models: []string{"gemma4:26b", "gemma4:31b", "qwen3:14b", "llama3.3:70b"}},
	}
}

func RecommendedAssignments(role string) []OfficeOption {
	var options []OfficeOption
	add := func(label string, assignment CartridgeConfig) {
		assignment = applyDefaultAssignment(assignment, role)
		options = append(options, OfficeOption{Label: label, Assignment: assignment})
	}

	switch role {
	case "main_brain":
		add("OpenAI ChatGPT auth (recommended)", CartridgeConfig{Role: "modus", Provider: "openai", Family: "openai", Model: "chatgpt", Backend: "sdk"})
		add("OpenAI GPT-5.4", CartridgeConfig{Role: "modus", Provider: "openai", Family: "openai", Model: "gpt-5.4", Backend: "sdk"})
		add("Anthropic Claude Sonnet 4.6", CartridgeConfig{Role: "modus", Provider: "anthropic", Family: "anthropic", Model: "claude-sonnet-4-6", Backend: "api"})
		add("Google Gemini 2.5 Pro", CartridgeConfig{Role: "modus", Provider: "google", Family: "cloud", Model: "gemini-2.5-pro", Backend: "api"})
		add("Moonshot Kimi K2.5", CartridgeConfig{Role: "modus", Provider: "moonshot", Family: "cloud", Model: "kimi-k2.5", Backend: "api"})
		add("MiniMax M2.5", CartridgeConfig{Role: "modus", Provider: "minimax", Family: "cloud", Model: "MiniMax-M2.5", Backend: "api"})
	case "librarian":
		add("Local Gemma 26B (recommended)", CartridgeConfig{Role: "librarian", Provider: "ollama", Family: "local", Model: "gemma4:26b", Backend: "ollama"})
		add("OpenAI GPT-5.4 mini", CartridgeConfig{Role: "librarian", Provider: "openai", Family: "openai", Model: "gpt-5.4-mini", Backend: "sdk"})
		add("Google Gemini 2.5 Flash", CartridgeConfig{Role: "librarian", Provider: "google", Family: "cloud", Model: "gemini-2.5-flash", Backend: "api"})
		add("Qwen Plus", CartridgeConfig{Role: "librarian", Provider: "qwen", Family: "cloud", Model: "qwen-plus", Backend: "api"})
		add("Moonshot Kimi K2.5", CartridgeConfig{Role: "librarian", Provider: "moonshot", Family: "cloud", Model: "kimi-k2.5", Backend: "api"})
	case "coder":
		add("Local Gemma 31B (recommended)", CartridgeConfig{Role: "coder", Provider: "ollama", Family: "local", Model: "gemma4:31b", Backend: "ollama"})
		add("OpenAI GPT-5.4 mini", CartridgeConfig{Role: "coder", Provider: "openai", Family: "openai", Model: "gpt-5.4-mini", Backend: "sdk"})
		add("Anthropic Claude Sonnet 4.6", CartridgeConfig{Role: "coder", Provider: "anthropic", Family: "anthropic", Model: "claude-sonnet-4-6", Backend: "api"})
		add("Google Gemini 2.5 Pro", CartridgeConfig{Role: "coder", Provider: "google", Family: "cloud", Model: "gemini-2.5-pro", Backend: "api"})
		add("Qwen 235B", CartridgeConfig{Role: "coder", Provider: "qwen", Family: "cloud", Model: "qwen3-235b-a22b", Backend: "api"})
		add("Moonshot Kimi K2.5", CartridgeConfig{Role: "coder", Provider: "moonshot", Family: "cloud", Model: "kimi-k2.5", Backend: "api"})
		add("MiniMax M2.5", CartridgeConfig{Role: "coder", Provider: "minimax", Family: "cloud", Model: "MiniMax-M2.5", Backend: "api"})
	case "inspector":
		add("Qwen Plus API (recommended)", CartridgeConfig{Role: "inspector", Provider: "qwen", Family: "cloud", Model: "qwen-plus", Backend: "api"})
		add("Qwen CLI", CartridgeConfig{Role: "inspector", Provider: "qwen", Family: "cloud", Model: "qwen-3.6", Backend: "cli"})
		add("OpenAI GPT-5.4", CartridgeConfig{Role: "inspector", Provider: "openai", Family: "openai", Model: "gpt-5.4", Backend: "sdk"})
		add("Anthropic Claude Sonnet 4.6", CartridgeConfig{Role: "inspector", Provider: "anthropic", Family: "anthropic", Model: "claude-sonnet-4-6", Backend: "api"})
		add("Google Gemini 2.5 Flash", CartridgeConfig{Role: "inspector", Provider: "google", Family: "cloud", Model: "gemini-2.5-flash", Backend: "api"})
		add("MiniMax M2.5", CartridgeConfig{Role: "inspector", Provider: "minimax", Family: "cloud", Model: "MiniMax-M2.5", Backend: "api"})
	case "scout":
		add("Google Gemini 2.5 Flash (recommended)", CartridgeConfig{Role: "scout", Provider: "google", Family: "cloud", Model: "gemini-2.5-flash", Backend: "api"})
		add("Google Gemini 2.5 Pro", CartridgeConfig{Role: "scout", Provider: "google", Family: "cloud", Model: "gemini-2.5-pro", Backend: "api"})
		add("OpenAI GPT-5.4 mini", CartridgeConfig{Role: "scout", Provider: "openai", Family: "openai", Model: "gpt-5.4-mini", Backend: "sdk"})
		add("Anthropic Claude Haiku 4.5", CartridgeConfig{Role: "scout", Provider: "anthropic", Family: "anthropic", Model: "claude-haiku-4-5", Backend: "api"})
		add("Qwen Plus", CartridgeConfig{Role: "scout", Provider: "qwen", Family: "cloud", Model: "qwen-plus", Backend: "api"})
		add("Moonshot Kimi K2.5", CartridgeConfig{Role: "scout", Provider: "moonshot", Family: "cloud", Model: "kimi-k2.5", Backend: "api"})
		add("MiniMax M2.1", CartridgeConfig{Role: "scout", Provider: "minimax", Family: "cloud", Model: "MiniMax-M2.1", Backend: "api"})
	}
	return options
}

func ProviderModels(provider string) []string {
	for _, cat := range ProviderCatalogs() {
		if cat.Provider == provider {
			return append([]string(nil), cat.Models...)
		}
	}
	return nil
}

func NormalizeAssignment(role string, cfg CartridgeConfig) CartridgeConfig {
	return applyDefaultAssignment(cfg, role)
}

func FamilyForProvider(provider string) string {
	for _, cat := range ProviderCatalogs() {
		if cat.Provider == provider {
			return cat.Family
		}
	}
	return ""
}

func BackendForProvider(provider string) string {
	for _, cat := range ProviderCatalogs() {
		if cat.Provider == provider {
			return cat.Backend
		}
	}
	return ""
}

func applyDefaultAssignment(cfg CartridgeConfig, role string) CartridgeConfig {
	def := DefaultAssignment(role)
	if cfg.Role == "" {
		cfg.Role = def.Role
	}
	if cfg.Provider == "" {
		cfg.Provider = inferProvider(cfg, def)
	}
	if cfg.Family == "" {
		cfg.Family = def.Family
	}
	if cfg.Model == "" {
		cfg.Model = def.Model
	}
	if cfg.Backend == "" {
		cfg.Backend = def.Backend
	}
	return cfg
}

func inferProvider(cfg CartridgeConfig, def CartridgeConfig) string {
	switch {
	case cfg.Provider != "":
		return cfg.Provider
	case cfg.Model == "chatgpt" || strings.HasPrefix(cfg.Model, "gpt-"):
		return "openai"
	case strings.HasPrefix(cfg.Model, "claude-"):
		return "anthropic"
	case strings.HasPrefix(cfg.Model, "gemini-"):
		return "google"
	case strings.HasPrefix(cfg.Model, "mistral") || strings.HasPrefix(cfg.Model, "devstral"):
		return "mistral"
	case strings.HasPrefix(cfg.Model, "command-"):
		return "cohere"
	case strings.HasPrefix(cfg.Model, "deepseek-"):
		return "deepseek"
	case strings.HasPrefix(strings.ToLower(cfg.Model), "kimi-"):
		return "moonshot"
	case strings.HasPrefix(strings.ToLower(cfg.Model), "minimax-"):
		return "minimax"
	case strings.Contains(cfg.Model, "qwen"):
		return "qwen"
	case cfg.Backend == "ollama":
		return "ollama"
	default:
		return def.Provider
	}
}

// DefaultPath returns ~/.modus/config.yaml.
func DefaultPath() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".modus", "config.yaml")
}

// Load reads the MODUS config file from disk.
func Load(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var cfg Config
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, err
	}
	cfg.MainBrain = applyDefaultAssignment(cfg.MainBrain, "main_brain")
	cfg.Officers.Librarian = applyDefaultAssignment(cfg.Officers.Librarian, "librarian")
	cfg.Officers.Coder = applyDefaultAssignment(cfg.Officers.Coder, "coder")
	cfg.Officers.Inspector = applyDefaultAssignment(cfg.Officers.Inspector, "inspector")
	cfg.Officers.Scout = applyDefaultAssignment(cfg.Officers.Scout, "scout")
	return &cfg, nil
}

// LoadDefault reads ~/.modus/config.yaml if it exists.
func LoadDefault() (*Config, error) {
	return Load(DefaultPath())
}

func Save(path string, cfg *Config) error {
	data, err := yaml.Marshal(cfg)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return err
	}
	return os.WriteFile(path, data, 0644)
}
