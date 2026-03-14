package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLoad_JSONConfig(t *testing.T) {
	dir := t.TempDir()
	data := `{
  "Version": 4,
  "Rules": [
    {
      "Scope": "**",
      "Skills": ["skills/repo.md"]
    }
  ]
}`
	if err := os.WriteFile(filepath.Join(dir, JSONFilename), []byte(data), 0o644); err != nil {
		t.Fatal(err)
	}

	cfg, err := Load(dir)
	if err != nil {
		t.Fatalf("Load(json): %v", err)
	}
	if cfg.Version != 4 {
		t.Fatalf("Version: got %d, want 4", cfg.Version)
	}
	if len(cfg.Rules) != 1 || cfg.Rules[0].Scope != "**" {
		t.Fatalf("unexpected rules: %+v", cfg.Rules)
	}
}

func TestLoad_YAMLConfig(t *testing.T) {
	dir := t.TempDir()
	data := "Version: 4\nRules:\n  - Scope: \"**\"\n    Skills:\n      - skills/repo.md\n"
	if err := os.WriteFile(filepath.Join(dir, YAMLFilename), []byte(data), 0o644); err != nil {
		t.Fatal(err)
	}

	cfg, err := Load(dir)
	if err != nil {
		t.Fatalf("Load(yaml): %v", err)
	}
	if cfg.Version != 4 {
		t.Fatalf("Version: got %d, want 4", cfg.Version)
	}
	if len(cfg.Rules) != 1 || cfg.Rules[0].Scope != "**" {
		t.Fatalf("unexpected rules: %+v", cfg.Rules)
	}
}

func TestLoad_BothJSONAndYAMLRejected(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, JSONFilename), []byte(`{"Version":4,"Rules":[]}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, YAMLFilename), []byte("Version: 4\nRules: []\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	_, err := Load(dir)
	if err == nil {
		t.Fatal("expected error when both skillex.json and skillex.yaml exist")
	}
	if !strings.Contains(err.Error(), JSONFilename) || !strings.Contains(err.Error(), YAMLFilename) {
		t.Fatalf("unexpected error: %v", err)
	}
}
