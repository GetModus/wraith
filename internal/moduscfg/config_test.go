package moduscfg

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadMainBrainConfig(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	content := `project_name: "modus"
trust_stage: 2
main_brain:
  role: "modus"
  provider: "openai"
  family: "openai"
  model: "chatgpt"
  backend: "sdk"
`
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("load config: %v", err)
	}

	if cfg.MainBrain.Role != "modus" {
		t.Fatalf("role = %q, want modus", cfg.MainBrain.Role)
	}
	if cfg.MainBrain.Model != "chatgpt" {
		t.Fatalf("model = %q, want chatgpt", cfg.MainBrain.Model)
	}
	if cfg.MainBrain.Provider != "openai" {
		t.Fatalf("provider = %q, want openai", cfg.MainBrain.Provider)
	}
	if cfg.MainBrain.Backend != "sdk" {
		t.Fatalf("backend = %q, want sdk", cfg.MainBrain.Backend)
	}
}

func TestLoadDefaultsRoleWhenMissing(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	content := `trust_stage: 1
main_brain:
  family: "anthropic"
  model: "claude-sonnet"
  backend: "sdk"
`
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("load config: %v", err)
	}

	if cfg.MainBrain.Role != "modus" {
		t.Fatalf("default role = %q, want modus", cfg.MainBrain.Role)
	}
	if cfg.MainBrain.Provider != "anthropic" {
		t.Fatalf("default provider = %q, want anthropic", cfg.MainBrain.Provider)
	}
	if cfg.Officers.Librarian.Model != "gemma4:26b" {
		t.Fatalf("default librarian model = %q, want gemma4:26b", cfg.Officers.Librarian.Model)
	}
	if cfg.Officers.Coder.Backend != "ollama" {
		t.Fatalf("default coder backend = %q, want ollama", cfg.Officers.Coder.Backend)
	}
	if cfg.Officers.Inspector.Role != "inspector" {
		t.Fatalf("default inspector role = %q, want inspector", cfg.Officers.Inspector.Role)
	}
	if cfg.Officers.Scout.Model != "gemini-2.5-flash" {
		t.Fatalf("default scout model = %q, want gemini-2.5-flash", cfg.Officers.Scout.Model)
	}
}

func TestRecommendedAssignmentsMainBrainUsesCommandProfile(t *testing.T) {
	options := RecommendedAssignments("main_brain")
	if len(options) == 0 {
		t.Fatal("expected recommended assignments")
	}
	if options[0].Assignment.Provider == "" {
		t.Fatal("expected provider on recommended assignment")
	}
	if options[0].Assignment.Role != "modus" {
		t.Fatalf("role = %q, want modus", options[0].Assignment.Role)
	}
}

func TestProviderModelsIncludesExpandedCloudProviders(t *testing.T) {
	if got := ProviderModels("moonshot"); len(got) == 0 {
		t.Fatal("expected moonshot models")
	}
	if got := ProviderModels("minimax"); len(got) == 0 {
		t.Fatal("expected minimax models")
	}
	if got := ProviderModels("qwen"); len(got) == 0 {
		t.Fatal("expected qwen models")
	}
}
