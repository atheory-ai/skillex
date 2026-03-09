package acceptance

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/atheory-ai/skillex/test/helpers"
)

func TestRegistry_SchemaCreation(t *testing.T) {
	dir := helpers.CopyFixture(t, "monorepo-pnpm")
	// Remove .skillex directory
	os.RemoveAll(filepath.Join(dir, ".skillex"))

	res := helpers.Run(t, dir, "refresh")
	if res.ExitCode != 0 {
		t.Fatalf("refresh failed (exit %d):\n%s", res.ExitCode, res.Stderr)
	}

	db := helpers.OpenRegistry(t, dir)
	if !helpers.TableExists(t, db, "skills") {
		t.Error("table 'skills' not found")
	}
	if !helpers.TableExists(t, db, "skill_topics") {
		t.Error("table 'skill_topics' not found")
	}
	if !helpers.TableExists(t, db, "skill_tags") {
		t.Error("table 'skill_tags' not found")
	}
	if !helpers.TableExists(t, db, "skill_scopes") {
		t.Error("table 'skill_scopes' not found")
	}
	if !helpers.TableExists(t, db, "skill_tests") {
		t.Error("table 'skill_tests' not found")
	}
}

func TestRegistry_FullRebuildNoAppend(t *testing.T) {
	dir := helpers.CopyFixture(t, "monorepo-pnpm")

	helpers.Run(t, dir, "refresh")
	db := helpers.OpenRegistry(t, dir)
	rows1 := helpers.QuerySkillsTable(t, db)

	helpers.Run(t, dir, "refresh")
	rows2 := helpers.QuerySkillsTable(t, db)

	if len(rows1) != len(rows2) {
		t.Errorf("double refresh changed skill count: %d → %d", len(rows1), len(rows2))
	}

	// Check no duplicate paths
	seen := map[string]int{}
	for _, r := range rows2 {
		seen[r.Path]++
	}
	for path, count := range seen {
		if count > 1 {
			t.Errorf("duplicate skill path after double refresh: %s (count %d)", path, count)
		}
	}
}

func TestRegistry_DeterministicOutput(t *testing.T) {
	dir := helpers.CopyFixture(t, "monorepo-pnpm")

	helpers.Run(t, dir, "refresh")
	db1 := helpers.OpenRegistry(t, dir)
	rows1 := helpers.QuerySkillsTable(t, db1)

	// Remove and re-create registry
	os.Remove(filepath.Join(dir, ".skillex", "index.db"))
	helpers.Run(t, dir, "refresh")
	db2 := helpers.OpenRegistry(t, dir)
	rows2 := helpers.QuerySkillsTable(t, db2)

	if len(rows1) != len(rows2) {
		t.Errorf("non-deterministic: first run %d skills, second run %d skills", len(rows1), len(rows2))
	}

	paths1 := map[string]bool{}
	for _, r := range rows1 {
		paths1[r.Path] = true
	}
	for _, r := range rows2 {
		if !paths1[r.Path] {
			t.Errorf("second run has skill not in first: %s", r.Path)
		}
	}
}

func TestRegistry_SkillsTableContent(t *testing.T) {
	dir := helpers.CopyFixture(t, "monorepo-pnpm")
	helpers.Run(t, dir, "refresh")
	db := helpers.OpenRegistry(t, dir)

	rows := helpers.QuerySkillsTable(t, db)

	findRow := func(pathSuffix string) *helpers.SkillRow {
		for i := range rows {
			if strings.HasSuffix(rows[i].Path, pathSuffix) || rows[i].Path == pathSuffix {
				return &rows[i]
			}
		}
		return nil
	}

	repoRow := findRow("skills/repo.md")
	if repoRow == nil {
		t.Error("skills/repo.md not found in registry")
	} else {
		if repoRow.Visibility != "repo" {
			t.Errorf("skills/repo.md visibility: got %q, want %q", repoRow.Visibility, "repo")
		}
		if repoRow.SourceType != "repo" {
			t.Errorf("skills/repo.md source_type: got %q, want %q", repoRow.SourceType, "repo")
		}
	}

	compRow := findRow("skillex/public/components.md")
	if compRow == nil {
		t.Error("components.md not found in registry")
	} else {
		if compRow.Visibility != "public" {
			t.Errorf("components.md visibility: got %q, want %q", compRow.Visibility, "public")
		}
		if compRow.PackageName != "@test/ui" {
			t.Errorf("components.md package_name: got %q, want %q", compRow.PackageName, "@test/ui")
		}
	}

	archRow := findRow("skillex/private/architecture.md")
	if archRow == nil {
		t.Error("architecture.md not found in registry")
	} else {
		if archRow.Visibility != "private" {
			t.Errorf("architecture.md visibility: got %q, want %q", archRow.Visibility, "private")
		}
	}
}

func TestRegistry_TopicsAndTagsIndexed(t *testing.T) {
	dir := helpers.CopyFixture(t, "monorepo-pnpm")
	helpers.Run(t, dir, "refresh")
	db := helpers.OpenRegistry(t, dir)

	compTopics := helpers.QueryTopicsFor(t, db, "components.md")
	if !containsAll(compTopics, "components", "react") {
		t.Errorf("components.md topics: got %v, want [components react]", compTopics)
	}

	compTags := helpers.QueryTagsFor(t, db, "components.md")
	if !containsAll(compTags, "v2") {
		t.Errorf("components.md tags: got %v, want [v2]", compTags)
	}

	migTopics := helpers.QueryTopicsFor(t, db, "migrations.md")
	if !containsAll(migTopics, "migration") {
		t.Errorf("migrations.md topics: got %v, want [migration]", migTopics)
	}

	migTags := helpers.QueryTagsFor(t, db, "migrations.md")
	if !containsAll(migTags, "v2", "breaking-change") {
		t.Errorf("migrations.md tags: got %v, want [v2 breaking-change]", migTags)
	}
}

func TestRegistry_NoFrontmatterStillIndexed(t *testing.T) {
	dir := helpers.CopyFixture(t, "monorepo-pnpm")
	helpers.Run(t, dir, "refresh")
	db := helpers.OpenRegistry(t, dir)

	rows := helpers.QuerySkillsTable(t, db)
	found := false
	for _, r := range rows {
		if strings.Contains(r.Path, "no-frontmatter.md") {
			found = true
		}
	}
	if !found {
		t.Error("no-frontmatter.md not found in registry")
	}

	topics := helpers.QueryTopicsFor(t, db, "no-frontmatter.md")
	if len(topics) != 0 {
		t.Errorf("no-frontmatter.md should have no topics, got: %v", topics)
	}

	tags := helpers.QueryTagsFor(t, db, "no-frontmatter.md")
	if len(tags) != 0 {
		t.Errorf("no-frontmatter.md should have no tags, got: %v", tags)
	}

	scopes := helpers.QueryScopesFor(t, db, "no-frontmatter.md")
	if len(scopes) == 0 {
		t.Error("no-frontmatter.md should have at least one scope")
	}
}

func TestRegistry_TestScenariosStored(t *testing.T) {
	dir := helpers.CopyFixture(t, "monorepo-pnpm")
	helpers.Run(t, dir, "refresh")
	db := helpers.OpenRegistry(t, dir)

	tests := helpers.QueryTestsFor(t, db, "components.md")
	if len(tests) != 2 {
		t.Fatalf("expected 2 test scenarios for components.md, got %d", len(tests))
	}

	if tests[0].Name != "Basic component import" {
		t.Errorf("tests[0].Name: got %q, want %q", tests[0].Name, "Basic component import")
	}
	if !strings.Contains(tests[0].Prompt, "Button") {
		t.Errorf("tests[0].Prompt should contain 'Button', got: %q", tests[0].Prompt)
	}
	if len(tests[0].Criteria) == 0 {
		t.Error("tests[0].Criteria should be non-empty")
	}

	// Second scenario should reference migrations.md
	foundMig := false
	for _, extra := range tests[1].ExtraSkills {
		if strings.Contains(extra, "migrations.md") {
			foundMig = true
		}
	}
	if !foundMig {
		t.Errorf("tests[1].ExtraSkills should contain migrations.md, got: %v", tests[1].ExtraSkills)
	}
}

func TestRegistry_ScopeAssignmentsPrecomputed(t *testing.T) {
	dir := helpers.CopyFixture(t, "monorepo-pnpm")
	helpers.Run(t, dir, "refresh")
	db := helpers.OpenRegistry(t, dir)

	repoScopes := helpers.QueryScopesFor(t, db, "skills/repo.md")
	if !containsStr(repoScopes, "**") {
		t.Errorf("skills/repo.md scopes should include '**', got: %v", repoScopes)
	}
}

func containsAll(slice []string, items ...string) bool {
	set := map[string]bool{}
	for _, s := range slice {
		set[s] = true
	}
	for _, item := range items {
		if !set[item] {
			return false
		}
	}
	return true
}

func containsStr(slice []string, s string) bool {
	for _, v := range slice {
		if v == s {
			return true
		}
	}
	return false
}
