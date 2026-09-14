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
	if cfg.MCPEnabled() {
		t.Fatal("version 4 config unexpectedly enabled MCP")
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
	if cfg.MCPEnabled() {
		t.Fatal("version 4 config unexpectedly enabled MCP")
	}
}

func TestLoad_Version5MCPOptIn(t *testing.T) {
	dir := t.TempDir()
	data := `Version: 5
Rules:
  - Scope: "**"
    Skills: [skills/repo.md]
MCP:
  Enabled: true
  Catalogs:
    - Type: static
      Path: .skillex/mcp/catalog.json
  Bindings:
    - Server: io.example/issues
      Version: 1.0.0
      AuthProfile: issues-work
      Scope: "packages/app/**"
`
	if err := os.WriteFile(filepath.Join(dir, YAMLFilename), []byte(data), 0o644); err != nil {
		t.Fatal(err)
	}

	cfg, err := Load(dir)
	if err != nil {
		t.Fatalf("Load(version 5 MCP): %v", err)
	}
	if !cfg.MCPEnabled() {
		t.Fatal("version 5 MCP opt-in was not enabled")
	}
	if len(cfg.MCP.Bindings) != 1 {
		t.Fatalf("MCP bindings = %d, want 1", len(cfg.MCP.Bindings))
	}
	if len(cfg.MCP.Catalogs) != 1 || cfg.MCP.Catalogs[0].Path != ".skillex/mcp/catalog.json" {
		t.Fatalf("MCP catalogs = %#v", cfg.MCP.Catalogs)
	}
	if got := cfg.MCP.Bindings[0].AuthProfile; got != "issues-work" {
		t.Fatalf("AuthProfile = %q, want issues-work", got)
	}
}

func TestLoad_Version5WithoutEnabledMCPRemainsSkillsOnly(t *testing.T) {
	for _, data := range []string{
		"Version: 5\nRules: []\n",
		"Version: 5\nRules: []\nMCP:\n  Enabled: false\n",
	} {
		dir := t.TempDir()
		if err := os.WriteFile(filepath.Join(dir, YAMLFilename), []byte(data), 0o644); err != nil {
			t.Fatal(err)
		}
		cfg, err := Load(dir)
		if err != nil {
			t.Fatalf("Load(skills-only version 5): %v", err)
		}
		if cfg.MCPEnabled() {
			t.Fatal("version 5 config without explicit opt-in enabled MCP")
		}
	}
}

func TestLoad_Version4RejectsMCPFields(t *testing.T) {
	dir := t.TempDir()
	data := "Version: 4\nRules: []\nMCP:\n  Enabled: true\n  Bindings: []\n"
	if err := os.WriteFile(filepath.Join(dir, YAMLFilename), []byte(data), 0o644); err != nil {
		t.Fatal(err)
	}

	_, err := Load(dir)
	if err == nil || !strings.Contains(err.Error(), "requires Version 5") {
		t.Fatalf("Load(version 4 with MCP) error = %v, want version gate", err)
	}
}

func TestLoad_Version5RejectsUnknownFields(t *testing.T) {
	dir := t.TempDir()
	data := `{
  "Version": 5,
  "Rules": [],
  "MCP": {
    "Enabled": true,
    "Bindings": [{
      "Server": "io.example/issues",
      "Version": "1.0.0",
      "Scope": "**",
      "AuthProfiles": "misspelled"
    }]
  }
}`
	if err := os.WriteFile(filepath.Join(dir, JSONFilename), []byte(data), 0o644); err != nil {
		t.Fatal(err)
	}

	_, err := Load(dir)
	if err == nil || !strings.Contains(err.Error(), "unknown field") {
		t.Fatalf("Load(version 5 unknown field) error = %v, want strict decoding error", err)
	}
}

func TestLoad_MCPBindingsRequireExplicitEnablementAndExactIdentity(t *testing.T) {
	tests := []struct {
		name string
		mcp  string
		want string
	}{
		{
			name: "bindings while disabled",
			mcp:  "  Enabled: false\n  Bindings:\n    - Server: io.example/issues\n      Version: 1.0.0\n      Scope: \"**\"\n",
			want: "requires MCP.Enabled: true",
		},
		{
			name: "enabled without bindings",
			mcp:  "  Enabled: true\n",
			want: "requires at least one",
		},
		{
			name: "missing exact version",
			mcp:  "  Enabled: true\n  Bindings:\n    - Server: io.example/issues\n      Scope: \"**\"\n",
			want: "Version is required",
		},
		{
			name: "version range",
			mcp:  "  Enabled: true\n  Bindings:\n    - Server: io.example/issues\n      Version: \"^1.0.0\"\n      Scope: \"**\"\n",
			want: "must be exact",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			data := "Version: 5\nRules: []\nMCP:\n" + tt.mcp
			if err := os.WriteFile(filepath.Join(dir, YAMLFilename), []byte(data), 0o644); err != nil {
				t.Fatal(err)
			}
			_, err := Load(dir)
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("Load() error = %v, want substring %q", err, tt.want)
			}
		})
	}
}

func TestLoad_MCPStaticCatalogPathMustStayInsideProject(t *testing.T) {
	for _, catalog := range []string{
		"    - Type: registry\n      Path: catalog.json\n",
		"    - Type: static\n      Path: ../catalog.json\n",
		"    - Type: static\n      Path: /tmp/catalog.json\n",
	} {
		dir := t.TempDir()
		data := "Version: 5\nRules: []\nMCP:\n  Enabled: true\n  Catalogs:\n" + catalog +
			"  Bindings:\n    - Server: io.example/issues\n      Version: 1.0.0\n      Scope: \"**\"\n"
		if err := os.WriteFile(filepath.Join(dir, YAMLFilename), []byte(data), 0o644); err != nil {
			t.Fatal(err)
		}
		if _, err := Load(dir); err == nil {
			t.Fatalf("Load() accepted unsafe catalog configuration:\n%s", data)
		}
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
