package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadJSONCAndPrecedence(t *testing.T) {
	home := t.TempDir()
	if err := os.Setenv("HOME", home); err != nil {
		t.Fatal(err)
	}
	work := t.TempDir()
	oldwd, _ := os.Getwd()
	if err := os.Chdir(work); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(oldwd) })

	globalDir := filepath.Join(home, ".offline-agent")
	if err := os.MkdirAll(globalDir, 0o755); err != nil {
		t.Fatal(err)
	}
	globalCfg := `{
  // global
  "provider": {"model": "global-model"},
  "compaction": {"auto": false}
}`
	if err := os.WriteFile(filepath.Join(globalDir, "config.jsonc"), []byte(globalCfg), 0o644); err != nil {
		t.Fatal(err)
	}
	projectCfg := `{
  "provider": {"model": "project-model"},
  "compaction": {"auto": true, "prune": false}
}`
	if err := os.WriteFile("agent.config.jsonc", []byte(projectCfg), 0o644); err != nil {
		t.Fatal(err)
	}

	cfg, err := Load("")
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Provider.Model != "project-model" {
		t.Fatalf("model=%q", cfg.Provider.Model)
	}
	if !cfg.Compaction.Auto {
		t.Fatalf("compaction.auto expected true")
	}
	if cfg.Compaction.Prune {
		t.Fatalf("compaction.prune expected false")
	}
}

func TestEnvOverride(t *testing.T) {
	t.Setenv("AGENT_MODEL", "env-model")
	cfg, err := Load("")
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Provider.Model != "env-model" {
		t.Fatalf("model=%q", cfg.Provider.Model)
	}
}

func TestProviderModelsNormalization(t *testing.T) {
	home := t.TempDir()
	if err := os.Setenv("HOME", home); err != nil {
		t.Fatal(err)
	}
	work := t.TempDir()
	oldwd, _ := os.Getwd()
	if err := os.Chdir(work); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(oldwd) })

	projectCfg := `{
  "provider": {
    "model": "m2",
    "models": ["m1", "m2", "m1", "  ", "m3"]
  }
}`
	if err := os.WriteFile("agent.config.jsonc", []byte(projectCfg), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg, err := Load("")
	if err != nil {
		t.Fatal(err)
	}
	if len(cfg.Provider.Models) != 3 {
		t.Fatalf("unexpected models: %#v", cfg.Provider.Models)
	}
	if cfg.Provider.Models[0] != "m1" || cfg.Provider.Models[1] != "m2" || cfg.Provider.Models[2] != "m3" {
		t.Fatalf("unexpected models order: %#v", cfg.Provider.Models)
	}
}

func TestLoadGlobalConfigCurrentPathOverridesLegacy(t *testing.T) {
	home := t.TempDir()
	if err := os.Setenv("HOME", home); err != nil {
		t.Fatal(err)
	}
	work := t.TempDir()
	oldwd, _ := os.Getwd()
	if err := os.Chdir(work); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(oldwd) })

	legacyDir := filepath.Join(home, ".offline-agent")
	if err := os.MkdirAll(legacyDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(legacyDir, "config.jsonc"), []byte(`{"provider":{"model":"legacy-model"}}`), 0o644); err != nil {
		t.Fatal(err)
	}

	currentDir := filepath.Join(home, ".coder")
	if err := os.MkdirAll(currentDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(currentDir, "config.jsonc"), []byte(`{"provider":{"model":"current-model"}}`), 0o644); err != nil {
		t.Fatal(err)
	}

	cfg, err := Load("")
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Provider.Model != "current-model" {
		t.Fatalf("model=%q", cfg.Provider.Model)
	}
}
