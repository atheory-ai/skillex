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

// TestInit_HarnessMCPUsesNpx verifies that the generated MCP config uses
// "command": "npx" rather than the bare "skillex" binary name.
//
// A bare "skillex" command requires a global install and fails silently when
// agent harnesses (Cursor, Claude Code) spawn the MCP server as a subprocess
// with a stripped PATH. npx resolves ./node_modules/.bin first, so it works
// with --save-dev installs, global installs, and version managers alike.
func TestInit_HarnessMCPUsesNpx(t *testing.T) {
	for _, harness := range []struct {
		flag    string
		mcpPath string
	}{
		{"cursor", ".cursor/mcp.json"},
		{"claude-code", ".claude/mcp.json"},
		{"windsurf", ".windsurf/mcp.json"},
	} {
		t.Run(harness.flag, func(t *testing.T) {
			dir := t.TempDir()
			if err := os.WriteFile(filepath.Join(dir, "package.json"), []byte(`{"name":"test","version":"1.0.0"}`), 0o644); err != nil {
				t.Fatal(err)
			}

			res := helpers.Run(t, dir, "init", "--harness", harness.flag, "--yes")
			if res.ExitCode != 0 {
				t.Fatalf("init --harness %s failed (exit %d):\n%s", harness.flag, res.ExitCode, res.Stderr)
			}

			data, err := os.ReadFile(filepath.Join(dir, harness.mcpPath))
			if err != nil {
				t.Fatalf("%s not created: %v", harness.mcpPath, err)
			}
			config := string(data)

			if strings.Contains(config, `"command": "skillex"`) {
				t.Errorf(
					"%s uses bare 'skillex' command which requires a global install.\n"+
						"Agent harnesses spawn MCP servers with a stripped PATH, so this fails\n"+
						"silently after a --save-dev install. Use 'npx' instead.\nGot:\n%s",
					harness.mcpPath, config,
				)
			}
			if !strings.Contains(config, `"command": "npx"`) {
				t.Errorf("%s: expected command to be 'npx', got:\n%s", harness.mcpPath, config)
			}
			if !strings.Contains(config, `@atheory-ai/skillex`) {
				t.Errorf("%s: expected args to include '@atheory-ai/skillex', got:\n%s", harness.mcpPath, config)
			}
		})
	}
}

// TestInit_AgentsMdCLIFallbackUsesNpx verifies that the generated AGENTS.md
// CLI fallback commands use npx rather than the bare skillex binary.
//
// Bare `skillex query` requires a global install and fails with
// "command not found" for developers (and autonomous agents) who only have
// a --save-dev install. npx resolves ./node_modules/.bin first and fetches
// from npm on demand if neither a local nor global install is present.
func TestInit_AgentsMdCLIFallbackUsesNpx(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "package.json"), []byte(`{"name":"test","version":"1.0.0"}`), 0o644); err != nil {
		t.Fatal(err)
	}

	res := helpers.Run(t, dir, "init", "--yes")
	if res.ExitCode != 0 {
		t.Fatalf("init failed (exit %d):\n%s", res.ExitCode, res.Stderr)
	}

	data, err := os.ReadFile(filepath.Join(dir, "AGENTS.md"))
	if err != nil {
		t.Fatalf("AGENTS.md not created: %v", err)
	}
	content := string(data)

	if strings.Contains(content, "skillex query") && !strings.Contains(content, "npx") {
		t.Errorf(
			"AGENTS.md CLI fallback uses bare 'skillex query' which requires a global install.\n"+
				"Developers with --save-dev installs (and autonomous agents on fresh machines)\n"+
				"will get 'command not found'. Use 'npx @atheory-ai/skillex query' instead.\nGot:\n%s",
			content,
		)
	}
	if !strings.Contains(content, "npx @atheory-ai/skillex query") {
		t.Errorf("AGENTS.md: expected CLI fallback to use 'npx @atheory-ai/skillex query', got:\n%s", content)
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
