package acceptance

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/atheory-ai/skillex/test/helpers"
)

func TestIntegration_FullLifecycle(t *testing.T) {
	dir := helpers.CopyFixture(t, "monorepo-pnpm")
	// Remove init artifacts so we test full init
	os.RemoveAll(filepath.Join(dir, ".skillex"))
	os.Remove(filepath.Join(dir, "AGENTS.md"))

	// 1. Init
	res := helpers.Run(t, dir, "init", "--yes")
	if res.ExitCode != 0 {
		t.Fatalf("step 1 (init) failed (exit %d):\n%s", res.ExitCode, res.Stderr)
	}

	// 2. Verify init artifacts
	if _, err := os.Stat(filepath.Join(dir, "skillex.yaml")); err != nil {
		t.Error("step 2: skillex.yaml missing after init")
	}
	if _, err := os.Stat(filepath.Join(dir, "AGENTS.md")); err != nil {
		t.Error("step 2: AGENTS.md missing after init")
	}

	// 3. Query app-a path
	var appASkills []helpers.SkillSummary
	helpers.RunJSON(t, dir, &appASkills, "query", "--path", "packages/app-a/src/auth.ts", "--format", "summary")
	if len(appASkills) == 0 {
		t.Error("step 3: expected skills for app-a path")
	}
}

func TestIntegration_CiPipeline(t *testing.T) {
	dir := helpers.CopyFixture(t, "monorepo-pnpm")

	// 1. Refresh
	res := helpers.Run(t, dir, "refresh")
	if res.ExitCode != 0 {
		t.Fatalf("step 1 (refresh) failed: %s", res.Stderr)
	}

	// 2. Check → 0
	res = helpers.Run(t, dir, "refresh", "--check")
	if res.ExitCode != 0 {
		t.Errorf("step 2 (check fresh) should exit 0, got %d: %s", res.ExitCode, res.Stderr)
	}

	// 3. Add a new skill file (changes count — what --check compares)
	newSkill := filepath.Join(dir, "packages", "ui", "skillex", "public", "ci-stale-skill.md")
	os.WriteFile(newSkill, []byte("# CI Stale Skill\n\nAdded to trigger staleness.\n"), 0o644)

	// 4. Check → non-zero (stale)
	res = helpers.Run(t, dir, "refresh", "--check")
	if res.ExitCode == 0 {
		t.Error("step 4 (check after modification) should exit non-zero")
	}

	// 5. Refresh again
	helpers.Run(t, dir, "refresh")

	// 6. Check → 0 again
	res = helpers.Run(t, dir, "refresh", "--check")
	if res.ExitCode != 0 {
		t.Errorf("step 6 (check after re-refresh) should exit 0, got %d", res.ExitCode)
	}
}

func TestIntegration_RefreshAfterNewSkill(t *testing.T) {
	dir := helpers.CopyFixture(t, "monorepo-pnpm")
	helpers.Run(t, dir, "refresh")

	// Create a new skill file
	newSkillPath := filepath.Join(dir, "packages", "ui", "skillex", "public", "new-api.md")
	os.WriteFile(newSkillPath, []byte("---\ntopics:\n  - api\n  - new-feature\n---\n# New API\n\nNew API skill.\n"), 0o644)
	newTestPath := filepath.Join(dir, "packages", "ui", "skillex", "public", "new-api.test.md")
	os.WriteFile(newTestPath, []byte("# Tests: new-api.md\n\n## Validation: Basic\n\nPrompt: How does the new API work?\nSuccess criteria:\n  - Explains the new API\n"), 0o644)

	helpers.Run(t, dir, "refresh")

	var skills []helpers.SkillSummary
	helpers.RunJSON(t, dir, &skills, "query", "--topic", "new-feature", "--format", "summary")

	helpers.AssertSkillPresent(t, skills, "new-api.md")
}

func TestIntegration_McpCliParity(t *testing.T) {
	dir := helpers.CopyFixture(t, "monorepo-pnpm")
	helpers.Run(t, dir, "refresh")

	client := helpers.StartMCPServer(t, dir)
	defer client.Close()

	// Query via CLI
	var cliSkills []helpers.SkillSummary
	helpers.RunJSON(t, dir, &cliSkills, "query", "--tags", "v2", "--format", "summary")

	// Query via MCP
	text, err := client.CallToolText("skillex_query", map[string]interface{}{
		"tags":   "v2",
		"format": "summary",
	})
	if err != nil {
		t.Fatalf("MCP query failed: %v", err)
	}

	// Both should return results with v2-tagged skills
	if len(cliSkills) == 0 {
		t.Error("CLI returned no v2-tagged skills")
	}
	if !strings.Contains(text, "migrations") && !strings.Contains(text, "components") {
		t.Errorf("MCP result should contain v2 skills, got: %q", text[:minInt(300, len(text))])
	}
}
