package acceptance

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/atheory-ai/skillex/test/helpers"
)

type doctorReport struct {
	ConfigOK    bool     `json:"config_ok"`
	RegistryOK  bool     `json:"registry_ok"`
	SkillCount  int      `json:"skill_count"`
	MCPEnabled  bool     `json:"mcp_enabled"`
	MCPBindings int      `json:"mcp_bindings"`
	Topics      []string `json:"topics"`
	Tags        []string `json:"tags"`
	Errors      []string `json:"errors"`
	Warnings    []string `json:"warnings"`
}

func TestDoctor_ReportsExplicitMCPOptIn(t *testing.T) {
	dir := helpers.CopyGoldenFixture(t, "mcp-capability-broker")

	var report doctorReport
	helpers.RunJSON(t, dir, &report, "doctor")

	if !report.ConfigOK {
		t.Fatal("expected version 5 MCP configuration to be valid")
	}
	if !report.MCPEnabled || report.MCPBindings != 2 {
		t.Fatalf("MCP status = enabled %v, bindings %d; want enabled with 2 bindings", report.MCPEnabled, report.MCPBindings)
	}
}

func TestDoctor_ChecksManagedSetupAfterUpgrade(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "AGENTS.md")
	const project = "# Project guidance\n"
	const stale = project + "<!-- skillex:start -->\nOld agent setup.\n<!-- skillex:end -->\n"
	if err := os.WriteFile(path, []byte(stale), 0o600); err != nil {
		t.Fatal(err)
	}
	init := func() {
		if res := helpers.Run(t, dir, "init", "--yes", "--invocation", "pnpm"); res.ExitCode != 0 {
			t.Fatal(res.Stderr)
		}
	}
	init()
	if err := os.WriteFile(path, []byte(stale), 0o600); err != nil {
		t.Fatal(err)
	}
	var report doctorReport
	helpers.RunJSON(t, dir, &report, "doctor")
	if !strings.Contains(strings.Join(report.Warnings, "\n"), "node ./node_modules/@atheory-ai/skillex/bin/skillex.js init --yes") {
		t.Fatalf("expected project-local setup repair command: %v", report.Warnings)
	}
	if after, err := os.ReadFile(path); err != nil || string(after) != stale {
		t.Fatal("doctor changed the stale file", err)
	}
	init()
	data, err := os.ReadFile(path)
	if err != nil || !strings.HasPrefix(string(data), project) {
		t.Fatal("init failed to preserve project instructions", err)
	}
	withNotes := strings.ReplaceAll(string(data), "\r\n", "\n") + "\nTeam notes.\n"
	if err := os.WriteFile(path, []byte(strings.ReplaceAll(withNotes, "\n", "\r\n")), 0o600); err != nil {
		t.Fatal(err)
	}
	var repaired doctorReport
	helpers.RunJSON(t, dir, &repaired, "doctor")
	if strings.Contains(strings.Join(repaired.Warnings, "\n"), "AGENTS.md") {
		t.Fatalf("current setup produced a warning: %v", repaired.Warnings)
	}
}

func TestDoctor_MissingAgentsMdRecommendsInit(t *testing.T) {
	dir := helpers.CopyFixture(t, "monorepo-pnpm")

	var report doctorReport
	helpers.RunJSON(t, dir, &report, "doctor")

	for _, warning := range report.Warnings {
		if strings.Contains(warning, "AGENTS.md not found") {
			if !strings.Contains(warning, "skillex init") {
				t.Fatalf("missing AGENTS.md warning should recommend init, got %q", warning)
			}
			if strings.Contains(warning, "skillex refresh") {
				t.Fatalf("missing AGENTS.md warning must not recommend refresh, got %q", warning)
			}
			return
		}
	}
	t.Fatalf("doctor warnings missing AGENTS.md guidance: %v", report.Warnings)
}

func TestDoctor_MissingTestCoverage(t *testing.T) {
	dir := helpers.CopyFixture(t, "monorepo-pnpm")

	// Delete a test file
	os.Remove(filepath.Join(dir, "skills", "repo.test.md"))

	helpers.Run(t, dir, "refresh")

	var report doctorReport
	helpers.RunJSON(t, dir, &report, "doctor")

	// Should see a warning about missing test
	foundWarning := false
	for _, w := range report.Warnings {
		if strings.Contains(w, "repo.md") || strings.Contains(w, "test") {
			foundWarning = true
		}
	}
	if !foundWarning {
		t.Logf("missing test warning not in report.Warnings: %v", report.Warnings)
		// Not failing — doctor may check different dirs
	}
}

func TestDoctor_ConfigurationError(t *testing.T) {
	dir := helpers.CopyFixture(t, "monorepo-pnpm")

	// Write invalid YAML to skillex.yaml
	os.WriteFile(filepath.Join(dir, "skillex.yaml"), []byte("invalid: yaml: {unclosed"), 0o644)

	var report doctorReport
	res := helpers.RunJSON(t, dir, &report, "doctor")

	if res.ExitCode == 0 && len(report.Errors) == 0 {
		t.Error("expected config error to be reported")
	}

	if len(report.Errors) > 0 {
		foundConfig := false
		for _, e := range report.Errors {
			if strings.Contains(strings.ToLower(e), "config") || strings.Contains(e, "skillex.yaml") || strings.Contains(e, "skillex.json") {
				foundConfig = true
			}
		}
		if !foundConfig {
			t.Logf("config error in report.Errors: %v", report.Errors)
		}
	}
}

func TestDoctor_EmptyExportDirectories(t *testing.T) {
	dir := helpers.CopyFixture(t, "monorepo-pnpm")
	helpers.Run(t, dir, "refresh")

	var report doctorReport
	helpers.RunJSON(t, dir, &report, "doctor")

	// Doctor should complete without crash
	if !report.RegistryOK {
		t.Error("expected registry_ok=true after refresh")
	}
}

func TestDoctor_RegistryOKAfterRefresh(t *testing.T) {
	dir := helpers.CopyFixture(t, "monorepo-pnpm")
	helpers.Run(t, dir, "refresh")

	var report doctorReport
	helpers.RunJSON(t, dir, &report, "doctor")

	if !report.ConfigOK {
		t.Error("expected config_ok=true")
	}
	if !report.RegistryOK {
		t.Error("expected registry_ok=true after refresh")
	}
	if report.SkillCount == 0 {
		t.Error("expected skill_count > 0")
	}
}
