package acceptance

import (
	"os"
	"path/filepath"
	"strings"
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

func TestScanner_ProjectAgentSkillsRefresh(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "skillex.yaml"), []byte("Version: 4\nRules: []\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	writeProjectFile(t, dir, ".cursor/skills/land-it/SKILL.md", agentSkillMarkdown("land-it", "Ship the branch."))
	writeProjectFile(t, dir, ".cursor/skills/land-it/references/guide.md", "# guide\n")
	writeProjectFile(t, dir, "apps/web/.claude/skills/deploy-web/SKILL.md", agentSkillMarkdown("deploy-web", "Deploy the web app."))
	writeProjectFile(t, dir, ".github/skills/review-pr/SKILL.md", agentSkillMarkdown("review-pr", "Review a pull request."))
	writeProjectFile(t, dir, ".cursor/skills/react-patterns/SKILL.md", "---\nname: react-patterns\ndescription: React conventions.\npaths: \"**/*.tsx\"\n---\n\n# React\n")

	if res := helpers.Run(t, dir, "refresh"); res.ExitCode != 0 {
		t.Fatalf("refresh failed: %s", res.Stderr)
	}

	rootSkills := queryResults(t, dir, "--path", "main.go", "--format", "summary")
	helpers.AssertSkillPresent(t, rootSkills, ".cursor/skills/land-it/SKILL.md")
	helpers.AssertSkillPresent(t, rootSkills, ".github/skills/review-pr/SKILL.md")
	helpers.AssertSkillAbsent(t, rootSkills, "deploy-web/SKILL.md")
	helpers.AssertSkillAbsent(t, rootSkills, "references/guide.md")
	helpers.AssertSkillAbsent(t, rootSkills, "react-patterns/SKILL.md")
	assertAgentSkill(t, rootSkills, "land-it/SKILL.md", "land-it", "**")

	tsxSkills := queryResults(t, dir, "--path", "page.tsx", "--format", "summary")
	helpers.AssertSkillPresent(t, tsxSkills, "react-patterns/SKILL.md")
	nestedTSX := queryResults(t, dir, "--path", "src/page.tsx", "--format", "summary")
	helpers.AssertSkillPresent(t, nestedTSX, "react-patterns/SKILL.md")

	webSkills := queryResults(t, dir, "--path", "apps/web/page.tsx", "--format", "summary")
	helpers.AssertSkillPresent(t, webSkills, "deploy-web/SKILL.md")
	helpers.AssertSkillPresent(t, webSkills, "land-it/SKILL.md")
	assertAgentSkill(t, webSkills, "deploy-web/SKILL.md", "deploy-web", "apps/web/**")

	writeProjectFile(t, dir, ".agents/skills/new-review/SKILL.md", agentSkillMarkdown("new-review", "Review the new diff."))
	if err := os.Remove(filepath.Join(dir, ".cursor", "skills", "land-it", "SKILL.md")); err != nil {
		t.Fatal(err)
	}
	if res := helpers.Run(t, dir, "refresh"); res.ExitCode != 0 {
		t.Fatalf("second refresh failed: %s", res.Stderr)
	}
	again := queryResults(t, dir, "--path", "main.go", "--format", "summary")
	helpers.AssertSkillAbsent(t, again, "land-it/SKILL.md")
	helpers.AssertSkillPresent(t, again, ".agents/skills/new-review/SKILL.md")
}

func writeProjectFile(t *testing.T, root, rel, body string) {
	t.Helper()
	path := filepath.Join(root, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func agentSkillMarkdown(name, description string) string {
	return "---\nname: " + name + "\ndescription: " + description + "\n---\n\n# " + name + "\n"
}

func assertAgentSkill(t *testing.T, skills []helpers.SkillSummary, suffix, name, scope string) {
	t.Helper()
	for _, skill := range skills {
		if !strings.HasSuffix(skill.Path, suffix) {
			continue
		}
		if skill.SourceType != "agent" || skill.Name != name {
			t.Fatalf("%s = %+v", suffix, skill)
		}
		if len(skill.Scopes) != 1 || skill.Scopes[0] != scope {
			t.Fatalf("%s scopes = %v", suffix, skill.Scopes)
		}
		return
	}
	t.Fatalf("skill %s not found", suffix)
}
