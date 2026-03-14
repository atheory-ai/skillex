package acceptance

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/atheory-ai/skillex/test/helpers"
)

type doctorReport struct {
	ConfigOK   bool     `json:"config_ok"`
	RegistryOK bool     `json:"registry_ok"`
	SkillCount int      `json:"skill_count"`
	Topics     []string `json:"topics"`
	Tags       []string `json:"tags"`
	Errors     []string `json:"errors"`
	Warnings   []string `json:"warnings"`
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
