package acceptance

import (
	"crypto/sha256"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/atheory-ai/skillex/test/helpers"
)

func TestRefresh_AgentsMdUpdated(t *testing.T) {
	dir := helpers.CopyFixture(t, "monorepo-pnpm")

	res := helpers.Run(t, dir, "refresh")
	if res.ExitCode != 0 {
		t.Fatalf("refresh failed: %s", res.Stderr)
	}

	content, err := os.ReadFile(filepath.Join(dir, "AGENTS.md"))
	if err != nil {
		t.Fatal("AGENTS.md not found after refresh")
	}
	contentStr := string(content)
	if !strings.Contains(contentStr, "skillex") {
		t.Error("AGENTS.md should contain skillex section")
	}
}

func TestRefresh_CheckDetectsStale(t *testing.T) {
	dir := helpers.CopyFixture(t, "monorepo-pnpm")

	helpers.Run(t, dir, "refresh")

	// Modify a skill file
	compPath := filepath.Join(dir, "packages", "ui", "skillex", "public", "components.md")
	f, err := os.OpenFile(compPath, os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		t.Fatal(err)
	}
	f.WriteString("\nNew content appended.")
	f.Close()

	res := helpers.Run(t, dir, "refresh", "--check")
	if res.ExitCode == 0 {
		t.Error("expected non-zero exit code from refresh --check after modification")
	}
}

func TestRefresh_CheckPassesWhenFresh(t *testing.T) {
	dir := helpers.CopyFixture(t, "monorepo-pnpm")

	helpers.Run(t, dir, "refresh")
	res := helpers.Run(t, dir, "refresh", "--check")
	if res.ExitCode != 0 {
		t.Errorf("refresh --check should pass when fresh (exit %d):\n%s", res.ExitCode, res.Stderr)
	}
}

func TestRefresh_DryRunDoesNotModify(t *testing.T) {
	dir := helpers.CopyFixture(t, "monorepo-pnpm")

	helpers.Run(t, dir, "refresh")

	dbPath := filepath.Join(dir, ".skillex", "index.db")
	hashBefore := fileHash(t, dbPath)

	// Add a new skill file
	newSkill := filepath.Join(dir, "packages", "ui", "skillex", "public", "new-feature.md")
	os.WriteFile(newSkill, []byte("# New Feature\n\nNew content.\n"), 0o644)

	helpers.Run(t, dir, "refresh", "--dry-run")
	hashAfter := fileHash(t, dbPath)

	if hashBefore != hashAfter {
		t.Error("dry-run modified the registry database")
	}
}

func TestRefresh_TestFilesExcludedFromIndex(t *testing.T) {
	dir := helpers.CopyFixture(t, "monorepo-pnpm")
	helpers.Run(t, dir, "refresh")

	var skills []helpers.SkillSummary
	helpers.RunJSON(t, dir, &skills, "query", "--path", "packages/app-a/src/index.ts", "--format", "summary")

	for _, s := range skills {
		if strings.HasSuffix(s.Path, ".test.md") {
			t.Errorf("test file in skill index: %s", s.Path)
		}
	}

	// Verify .test.md files ARE in skill_tests table
	db := helpers.OpenRegistry(t, dir)
	tests := helpers.QueryTestsFor(t, db, "components.md")
	if len(tests) == 0 {
		t.Error("expected test scenarios stored for components.md")
	}
}

func fileHash(t *testing.T, path string) string {
	t.Helper()
	f, err := os.Open(path)
	if err != nil {
		t.Fatalf("opening file for hash: %v", err)
	}
	defer f.Close()
	h := sha256.New()
	io.Copy(h, f)
	return fmt.Sprintf("%x", h.Sum(nil))
}
