package acceptance

import (
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
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

	// AGENTS.md contains only the stable discovery bootstrap.
	agentsContent, err := os.ReadFile(filepath.Join(dir, "AGENTS.md"))
	if err != nil {
		t.Fatal("AGENTS.md not created")
	}
	agentsText := string(agentsContent)
	for _, required := range []string{"skillex_query", "skillex_read", "### CLI (fallback)"} {
		if !strings.Contains(agentsText, required) {
			t.Errorf("AGENTS.md missing %q:\n%s", required, agentsText)
		}
	}
	for _, legacyHeading := range []string{"### Available scopes", "### Available topics", "### Available tags", "### Packages with skills"} {
		if strings.Contains(agentsText, legacyHeading) {
			t.Errorf("AGENTS.md must not embed legacy inventory %q:\n%s", legacyHeading, agentsText)
		}
	}
}

func TestInit_UpdatesAgentBridges(t *testing.T) {
	t.Run("root files", func(t *testing.T) {
		dir := helpers.CopyFixture(t, "monorepo-pnpm")
		for _, name := range []string{"CLAUDE.md", "GEMINI.md"} {
			if err := os.WriteFile(filepath.Join(dir, name), []byte("# Existing\n"), 0o644); err != nil {
				t.Fatal(err)
			}
		}

		res := helpers.Run(t, dir, "init", "--yes")
		if res.ExitCode != 0 {
			t.Fatalf("init failed (exit %d): %s", res.ExitCode, res.Stderr)
		}

		for _, name := range []string{"CLAUDE.md", "GEMINI.md"} {
			content, err := os.ReadFile(filepath.Join(dir, name))
			if err != nil {
				t.Fatalf("reading %s: %v", name, err)
			}
			if !strings.Contains(string(content), "@AGENTS.md") {
				t.Fatalf("%s missing AGENTS.md bridge:\n%s", name, content)
			}
		}
	})

	t.Run("claude directory", func(t *testing.T) {
		dir := helpers.CopyFixture(t, "monorepo-pnpm")
		if err := os.MkdirAll(filepath.Join(dir, ".claude"), 0o755); err != nil {
			t.Fatal(err)
		}

		res := helpers.Run(t, dir, "init", "--yes")
		if res.ExitCode != 0 {
			t.Fatalf("init failed (exit %d): %s", res.ExitCode, res.Stderr)
		}

		path := filepath.Join(dir, ".claude", "CLAUDE.md")
		content, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("reading %s: %v", path, err)
		}
		if !strings.Contains(string(content), "@../AGENTS.md") {
			t.Fatalf("%s missing relative AGENTS.md bridge:\n%s", path, content)
		}
	})
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

	agentsPath := filepath.Join(dir, "AGENTS.md")
	firstAgents, err := os.ReadFile(agentsPath)
	if err != nil {
		t.Fatal(err)
	}
	res = helpers.Run(t, dir, "init", "--yes")
	if res.ExitCode != 0 {
		t.Fatalf("second init failed (exit %d): %s", res.ExitCode, res.Stderr)
	}
	secondAgents, err := os.ReadFile(agentsPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(firstAgents) != string(secondAgents) {
		t.Errorf("AGENTS.md changed after idempotent init:\nfirst: %s\nsecond: %s", firstAgents, secondAgents)
	}
}

func TestInit_InvocationStrategies(t *testing.T) {
	for _, tc := range []struct{ strategy, command, prefix string }{
		{"global", "skillex", "skillex"},
		{"npm", "node", "node ./node_modules/@atheory-ai/skillex/bin/skillex.js"},
		{"pnpm", "node", "node ./node_modules/@atheory-ai/skillex/bin/skillex.js"},
		{"yarn-classic", "yarn", "yarn run skillex"},
		{"yarn-berry", "yarn", "yarn run skillex"},
		{"source", "./.skillex/bin/skillex", "./.skillex/bin/skillex"},
	} {
		if tc.strategy == "source" && runtime.GOOS == "windows" {
			tc.command += ".exe"
			tc.prefix += ".exe"
		}
		for _, harness := range []struct{ name, path string }{{"cursor", ".cursor/mcp.json"}, {"claude-code", ".mcp.json"}, {"windsurf", ".windsurf/mcp.json"}} {
			t.Run(tc.strategy+"/"+harness.name, func(t *testing.T) {
				dir := t.TempDir()
				res := helpers.Run(t, dir, "init", "--yes", "--invocation", tc.strategy, "--harness", harness.name)
				if res.ExitCode != 0 {
					t.Fatal(res.Stderr)
				}
				data, err := os.ReadFile(filepath.Join(dir, harness.path))
				if err != nil {
					t.Fatal(err)
				}
				var cfg struct {
					Servers map[string]struct {
						Command string   `json:"command"`
						Args    []string `json:"args"`
					} `json:"mcpServers"`
				}
				if err := json.Unmarshal(data, &cfg); err != nil {
					t.Fatal(err)
				}
				server := cfg.Servers["skillex"]
				if server.Command != tc.command || strings.Join(append([]string{server.Command}, server.Args...), " ") != tc.prefix+" mcp" {
					t.Fatalf("wrong server: %#v", server)
				}
				agents, err := os.ReadFile(filepath.Join(dir, "AGENTS.md"))
				if err != nil {
					t.Fatal(err)
				}
				if !strings.Contains(string(agents), tc.prefix+" query --search") {
					t.Fatalf("wrong AGENTS: %s", agents)
				}
				res = helpers.Run(t, dir, "init", "--yes")
				if res.ExitCode != 0 {
					t.Fatal(res.Stderr)
				}
				after, _ := os.ReadFile(filepath.Join(dir, "AGENTS.md"))
				if string(after) != string(agents) {
					t.Fatal("invocation was rediscovered")
				}
			})
		}
	}
}

func TestInit_DetectsLocalAndCachesSelection(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "package.json"), []byte(`{"devDependencies":{"@atheory-ai/skillex":"1.0.0"},"packageManager":"pnpm@10.0"}`), 0o644)
	res := helpers.Run(t, dir, "init", "--yes")
	if res.ExitCode != 0 {
		t.Fatal(res.Stderr)
	}
	before, _ := os.ReadFile(filepath.Join(dir, "skillex.json"))
	if !strings.Contains(string(before), `"PackageManager": "pnpm"`) {
		t.Fatal(string(before))
	}
	os.WriteFile(filepath.Join(dir, "package.json"), []byte(`{}`), 0o644)
	res = helpers.Run(t, dir, "init", "--yes")
	if res.ExitCode != 0 {
		t.Fatal(res.Stderr)
	}
	after, _ := os.ReadFile(filepath.Join(dir, "skillex.json"))
	if string(before) != string(after) {
		t.Fatal("cached selection changed")
	}
}

func TestInit_MergesExistingHarnessConfig(t *testing.T) {
	for _, harness := range []struct{ name, path string }{{"cursor", ".cursor/mcp.json"}, {"claude-code", ".mcp.json"}, {"windsurf", ".windsurf/mcp.json"}} {
		t.Run(harness.name, func(t *testing.T) {
			dir := t.TempDir()
			path := filepath.Join(dir, harness.path)
			os.MkdirAll(filepath.Dir(path), 0o755)
			original := `{"settings":{"largeNumber":12345678901234567890},"mcpServers":{"other":{"command":"other","env":{"TOKEN":"keep"}}}}`
			os.WriteFile(path, []byte(original), 0o600)
			res := helpers.Run(t, dir, "init", "--yes", "--harness", harness.name, "--invocation", "pnpm")
			if res.ExitCode != 0 {
				t.Fatal(res.Stderr)
			}
			data, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			for _, want := range []string{"12345678901234567890", `"TOKEN": "keep"`, `"command": "node"`, `"other"`} {
				if !strings.Contains(string(data), want) {
					t.Fatal(string(data))
				}
			}
			info, _ := os.Stat(path)
			if info.Mode().Perm() != 0o600 {
				t.Fatal("permissions changed")
			}
			res = helpers.Run(t, dir, "init", "--yes", "--harness", harness.name)
			if res.ExitCode != 0 {
				t.Fatal(res.Stderr)
			}
			after, _ := os.ReadFile(path)
			if string(after) != string(data) {
				t.Fatal("identical config rewritten")
			}
		})
	}
}

func TestInit_PreservesExistingSkillexUnlessOverridden(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, ".mcp.json")
	original := `{"mcpServers":{"skillex":{"command":"custom","args":["serve"],"env":{"TOKEN":"keep"},"timeout":60}}}`
	os.WriteFile(path, []byte(original), 0o600)
	res := helpers.Run(t, dir, "init", "--yes", "--harness", "claude-code", "--invocation", "pnpm")
	if res.ExitCode == 0 || !strings.Contains(res.Stderr, "--overwrite-mcp") {
		t.Fatal(res.Stderr)
	}
	data, _ := os.ReadFile(path)
	if string(data) != original {
		t.Fatal("conflicting server changed")
	}
	res = helpers.Run(t, dir, "init", "--yes", "--harness", "claude-code", "--overwrite-mcp")
	if res.ExitCode != 0 {
		t.Fatal(res.Stderr)
	}
	data, _ = os.ReadFile(path)
	for _, want := range []string{`"command": "node"`, `"TOKEN": "keep"`, `"timeout": 60`} {
		if !strings.Contains(string(data), want) {
			t.Fatal(string(data))
		}
	}
}

func TestInit_RefusesInvalidHarnessConfigs(t *testing.T) {
	for _, original := range []string{`broken`, `null`, `[]`, `{"mcpServers":null}`, `{"mcpServers":[]}`, `{"mcpServers":{"skillex":null}}`} {
		t.Run(original, func(t *testing.T) {
			dir := t.TempDir()
			path := filepath.Join(dir, ".mcp.json")
			os.WriteFile(path, []byte(original), 0o644)
			res := helpers.Run(t, dir, "init", "--yes", "--harness", "claude-code")
			if res.ExitCode == 0 {
				t.Fatal("invalid configuration accepted")
			}
			data, _ := os.ReadFile(path)
			if string(data) != original {
				t.Fatal("invalid file mutated")
			}
		})
	}
}

func TestInit_NoninteractiveMCPIsExplicit(t *testing.T) {
	for _, args := range [][]string{{"init", "--yes"}, {"init", "--no-mcp"}, {"init"}} {
		dir := t.TempDir()
		os.Mkdir(filepath.Join(dir, ".claude"), 0o755)
		res := helpers.Run(t, dir, args...)
		if res.ExitCode != 0 {
			t.Fatal(res.Stderr)
		}
		if _, err := os.Stat(filepath.Join(dir, ".mcp.json")); !os.IsNotExist(err) {
			t.Fatal("noninteractive init configured MCP implicitly")
		}
		if strings.Contains(res.Stderr, "Configure harness-managed") {
			t.Fatal("noninteractive prompt")
		}
	}
	for _, args := range [][]string{{"init", "--yes", "--harness", "unknown"}, {"init", "--yes", "--harness", "cursor", "--no-mcp"}, {"init", "--yes", "--overwrite-mcp"}} {
		dir := t.TempDir()
		res := helpers.Run(t, dir, args...)
		if res.ExitCode == 0 {
			t.Fatal("invalid flags accepted")
		}
		if _, err := os.Stat(filepath.Join(dir, "skillex.json")); !os.IsNotExist(err) {
			t.Fatal("invalid flags wrote project config")
		}
	}
}
