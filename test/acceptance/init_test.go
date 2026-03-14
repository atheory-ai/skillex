package acceptance

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/atheory-ai/skillex/test/helpers"
)

func TestInit_BootstrapEmptyRepo(t *testing.T) {
	dir := t.TempDir()
	// Minimal package.json
	if err := os.WriteFile(filepath.Join(dir, "package.json"), []byte(`{"name":"test-repo","version":"1.0.0"}`), 0o644); err != nil {
		t.Fatal(err)
	}

	res := helpers.Run(t, dir, "init", "--yes")
	if res.ExitCode != 0 {
		t.Fatalf("init failed (exit %d):\nstdout: %s\nstderr: %s", res.ExitCode, res.Stdout, res.Stderr)
	}

	// skillex.json exists by default
	if _, err := os.Stat(filepath.Join(dir, "skillex.json")); err != nil {
		t.Error("skillex.json not created")
	}
	if _, err := os.Stat(filepath.Join(dir, "skillex.yaml")); err == nil {
		t.Error("skillex.yaml should not be created by default")
	}

	// skills/ directory exists with at least one .md file
	entries, err := os.ReadDir(filepath.Join(dir, "skills"))
	if err != nil {
		t.Error("skills/ directory not created")
	} else {
		hasMD := false
		for _, e := range entries {
			if strings.HasSuffix(e.Name(), ".md") {
				hasMD = true
			}
		}
		if !hasMD {
			t.Error("skills/ has no .md files")
		}
	}

	// .skillex/ directory exists
	if _, err := os.Stat(filepath.Join(dir, ".skillex")); err != nil {
		t.Error(".skillex/ directory not created")
	}

	// AGENTS.md exists
	if _, err := os.Stat(filepath.Join(dir, "AGENTS.md")); err != nil {
		t.Error("AGENTS.md not created")
	}
}

func TestInit_HarnessCursor(t *testing.T) {
	dir := helpers.CopyFixture(t, "monorepo-pnpm")
	// Remove .cursor if it exists
	os.RemoveAll(filepath.Join(dir, ".cursor"))

	res := helpers.Run(t, dir, "init", "--harness", "cursor", "--yes")
	if res.ExitCode != 0 {
		t.Fatalf("init --harness cursor failed (exit %d):\n%s", res.ExitCode, res.Stderr)
	}

	mcpJSON := filepath.Join(dir, ".cursor", "mcp.json")
	data, err := os.ReadFile(mcpJSON)
	if err != nil {
		t.Fatalf(".cursor/mcp.json not created: %v", err)
	}
	if !strings.Contains(string(data), "skillex") {
		t.Errorf(".cursor/mcp.json should contain 'skillex', got: %s", data)
	}
}

func TestInit_YAMLFlagCreatesYAMLConfig(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "package.json"), []byte(`{"name":"test-repo","version":"1.0.0"}`), 0o644); err != nil {
		t.Fatal(err)
	}

	res := helpers.Run(t, dir, "init", "--yes", "--yaml")
	if res.ExitCode != 0 {
		t.Fatalf("init --yaml failed (exit %d):\nstdout: %s\nstderr: %s", res.ExitCode, res.Stdout, res.Stderr)
	}

	if _, err := os.Stat(filepath.Join(dir, "skillex.yaml")); err != nil {
		t.Error("skillex.yaml not created with --yaml")
	}
	if _, err := os.Stat(filepath.Join(dir, "skillex.json")); err == nil {
		t.Error("skillex.json should not be created when --yaml is used")
	}
}

func TestInit_PreservesExistingAgentsMd(t *testing.T) {
	dir := helpers.CopyFixture(t, "monorepo-pnpm")
	agentsPath := filepath.Join(dir, "AGENTS.md")
	if err := os.WriteFile(agentsPath, []byte("# My Project\n\nExisting content.\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	res := helpers.Run(t, dir, "init", "--yes")
	if res.ExitCode != 0 {
		t.Fatalf("init failed (exit %d): %s", res.ExitCode, res.Stderr)
	}

	content, err := os.ReadFile(agentsPath)
	if err != nil {
		t.Fatal(err)
	}
	contentStr := string(content)
	if !strings.Contains(contentStr, "My Project") {
		t.Error("init should preserve existing AGENTS.md content")
	}
	if !strings.Contains(contentStr, "Existing content.") {
		t.Error("init should preserve existing AGENTS.md content")
	}
}

func TestInit_Idempotent(t *testing.T) {
	dir := helpers.CopyFixture(t, "monorepo-pnpm")

	before, err := os.ReadFile(filepath.Join(dir, "skillex.yaml"))
	if err != nil {
		t.Fatal(err)
	}

	res := helpers.Run(t, dir, "init", "--yes")
	if res.ExitCode != 0 {
		t.Fatalf("init failed (exit %d): %s", res.ExitCode, res.Stderr)
	}

	after, err := os.ReadFile(filepath.Join(dir, "skillex.yaml"))
	if err != nil {
		t.Fatal(err)
	}

	if string(before) != string(after) {
		t.Errorf("skillex.yaml changed after idempotent init:\nbefore: %s\nafter: %s", before, after)
	}
}
