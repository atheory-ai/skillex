package acceptance

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/atheory-ai/skillex/test/helpers"
)

func TestScanner_DiscoverViaPackageJsonBoolean(t *testing.T) {
	dir := helpers.CopyFixture(t, "monorepo-pnpm")

	res := helpers.Run(t, dir, "refresh")
	if res.ExitCode != 0 {
		t.Fatalf("refresh failed (exit %d):\n%s", res.ExitCode, res.Stderr)
	}

	uiSkills := queryResults(t, dir, "--package", "@test/ui", "--format", "summary")
	if len(uiSkills) == 0 {
		t.Fatal("expected @test/ui skills, got none")
	}
	helpers.AssertSkillPresent(t, uiSkills, "components.md")
	helpers.AssertSkillPresent(t, uiSkills, "architecture.md")

	utilsSkills := queryResults(t, dir, "--package", "@test/utils", "--format", "summary")
	if len(utilsSkills) == 0 {
		t.Fatal("expected @test/utils skills, got none")
	}
	helpers.AssertSkillPresent(t, utilsSkills, "api.md")
	helpers.AssertSkillPresent(t, utilsSkills, "contributing.md")
}

func TestScanner_DiscoverViaPackageJsonCustomPath(t *testing.T) {
	dir := helpers.CopyFixture(t, "monorepo-pnpm")

	// Change packages/ui/package.json to use custom path
	pkgJSON := filepath.Join(dir, "packages", "ui", "package.json")
	if err := os.WriteFile(pkgJSON, []byte(`{"name":"@test/ui","version":"2.0.0","skillex":{"path":"docs/skillex"}}`), 0o644); err != nil {
		t.Fatalf("writing package.json: %v", err)
	}
	// Move skillex/ to docs/skillex/
	srcDir := filepath.Join(dir, "packages", "ui", "skillex")
	dstDir := filepath.Join(dir, "packages", "ui", "docs", "skillex")
	if err := os.MkdirAll(filepath.Dir(dstDir), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(srcDir, dstDir); err != nil {
		t.Fatalf("moving skillex dir: %v", err)
	}

	res := helpers.Run(t, dir, "refresh")
	if res.ExitCode != 0 {
		t.Fatalf("refresh failed (exit %d):\n%s", res.ExitCode, res.Stderr)
	}

	skills := queryResults(t, dir, "--package", "@test/ui", "--format", "summary")
	helpers.AssertSkillPresent(t, skills, "components.md")
}

func TestScanner_SkipPackagesWithoutSkillex(t *testing.T) {
	dir := helpers.CopyFixture(t, "monorepo-pnpm")

	res := helpers.Run(t, dir, "refresh")
	if res.ExitCode != 0 {
		t.Fatalf("refresh failed: %s", res.Stderr)
	}
	// @test/data has no skillex field — should have no skills.
	// The query returns no_match (not results) when the package isn't indexed.
	resp, _ := helpers.RunQueryJSON(t, dir, "query", "--package", "@test/data", "--format", "summary")
	if resp.Type == "results" && len(resp.Results) != 0 {
		t.Errorf("expected no skills for @test/data, got %d: %v", len(resp.Results), resp.Results)
	}
}

func TestScanner_HandleEmptySkillexDirectories(t *testing.T) {
	dir := helpers.CopyFixture(t, "monorepo-pnpm")

	res := helpers.Run(t, dir, "refresh")
	if res.ExitCode != 0 {
		t.Fatalf("refresh failed (exit %d):\n%s", res.ExitCode, res.Stderr)
	}

	// @test/legacy has empty skill dirs — should have no skills.
	resp, _ := helpers.RunQueryJSON(t, dir, "query", "--package", "@test/legacy", "--format", "summary")
	if resp.Type == "results" && len(resp.Results) != 0 {
		t.Errorf("expected no skills for @test/legacy (empty dirs), got %d", len(resp.Results))
	}
}

func TestScanner_PnpmDefaultLayout(t *testing.T) {
	dir := helpers.CopyFixture(t, "monorepo-pnpm")

	// Verify pnpm layout
	if _, err := os.Stat(filepath.Join(dir, "node_modules", ".pnpm")); err != nil {
		t.Skip("pnpm .pnpm directory not found — fixture may not be installed")
	}

	res := helpers.Run(t, dir, "refresh")
	if res.ExitCode != 0 {
		t.Fatalf("refresh failed: %s", res.Stderr)
	}

	skills := queryResults(t, dir, "--path", "packages/app-a/src/index.ts", "--format", "summary")
	helpers.AssertSkillPresent(t, skills, "components.md")
	helpers.AssertSkillPresent(t, skills, "api.md")
}

func TestScanner_YarnWorkspaces(t *testing.T) {
	dir := helpers.CopyFixture(t, "monorepo-yarn")

	res := helpers.Run(t, dir, "refresh")
	if res.ExitCode != 0 {
		t.Fatalf("refresh failed: %s", res.Stderr)
	}

	skills := queryResults(t, dir, "--path", "packages/app-a/src/index.ts", "--format", "summary")
	if len(skills) == 0 {
		t.Fatal("expected skills for packages/app-a/**, got none")
	}
	helpers.AssertSkillPresent(t, skills, "repo.md")
}

func TestScanner_NpmWorkspaces(t *testing.T) {
	dir := helpers.CopyFixture(t, "monorepo-npm")

	res := helpers.Run(t, dir, "refresh")
	if res.ExitCode != 0 {
		t.Fatalf("refresh failed: %s", res.Stderr)
	}

	skills := queryResults(t, dir, "--path", "packages/app-a/src/index.ts", "--format", "summary")
	if len(skills) == 0 {
		t.Fatal("expected skills for packages/app-a/**, got none")
	}
	helpers.AssertSkillPresent(t, skills, "repo.md")
}
