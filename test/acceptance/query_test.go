package acceptance

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/atheory-ai/skillex/test/helpers"
)

// --- helpers for the new response envelope ---

func queryResults(t *testing.T, dir string, args ...string) []helpers.SkillSummary {
	t.Helper()
	resp, _ := helpers.RunQueryJSON(t, dir, append([]string{"query"}, args...)...)
	if resp.Type != "results" {
		t.Fatalf("expected response type 'results', got %q", resp.Type)
	}
	return resp.Results
}

// --- path filtering ---

func TestQuery_PathExact(t *testing.T) {
	dir := helpers.CopyFixture(t, "monorepo-pnpm")
	helpers.Run(t, dir, "refresh")

	skills := queryResults(t, dir, "--path", "packages/app-a/src/auth.ts", "--format", "summary")

	helpers.AssertSkillPresent(t, skills, "repo.md")
	helpers.AssertSkillPresent(t, skills, "package-dev.md")
	helpers.AssertSkillPresent(t, skills, "components.md")
	helpers.AssertSkillPresent(t, skills, "api.md")
}

func TestQuery_PathGlob(t *testing.T) {
	dir := helpers.CopyFixture(t, "monorepo-pnpm")
	helpers.Run(t, dir, "refresh")

	exact := queryResults(t, dir, "--path", "packages/app-a/src/auth.ts", "--format", "summary")
	glob := queryResults(t, dir, "--path", "packages/app-a/**", "--format", "summary")

	// Both should contain the same skills
	for _, s := range exact {
		helpers.AssertSkillPresent(t, glob, s.Path)
	}
}

// --- topic filtering ---

func TestQuery_TopicSingle(t *testing.T) {
	dir := helpers.CopyFixture(t, "monorepo-pnpm")
	helpers.Run(t, dir, "refresh")

	skills := queryResults(t, dir, "--topic", "migration", "--format", "summary")

	helpers.AssertSkillPresent(t, skills, "migrations.md")
	helpers.AssertSkillAbsent(t, skills, "components.md")

	for _, s := range skills {
		found := false
		for _, tp := range s.Topics {
			if tp == "migration" {
				found = true
			}
		}
		if !found {
			t.Errorf("skill %s lacks topic 'migration', topics: %v", s.Path, s.Topics)
		}
	}
}

func TestQuery_TopicMultipleIsAND(t *testing.T) {
	dir := helpers.CopyFixture(t, "monorepo-pnpm")
	helpers.Run(t, dir, "refresh")

	skills := queryResults(t, dir, "--topic", "components", "--format", "summary")
	helpers.AssertSkillPresent(t, skills, "components.md")

	skills2 := queryResults(t, dir, "--topic", "migration", "--format", "summary")
	helpers.AssertSkillPresent(t, skills2, "migrations.md")
}

// --- tag filtering ---

func TestQuery_TagsSingle(t *testing.T) {
	dir := helpers.CopyFixture(t, "monorepo-pnpm")
	helpers.Run(t, dir, "refresh")

	skills := queryResults(t, dir, "--tags", "v2", "--format", "summary")

	helpers.AssertSkillPresent(t, skills, "components.md")
	helpers.AssertSkillPresent(t, skills, "migrations.md")
	helpers.AssertSkillAbsent(t, skills, "api.md")
}

func TestQuery_TagsMultipleIsAND(t *testing.T) {
	dir := helpers.CopyFixture(t, "monorepo-pnpm")
	helpers.Run(t, dir, "refresh")

	skills := queryResults(t, dir, "--tags", "v2,breaking-change", "--format", "summary")

	// Only migrations.md has both v2 AND breaking-change
	helpers.AssertSkillPresent(t, skills, "migrations.md")
	helpers.AssertSkillAbsent(t, skills, "components.md")
}

// --- package filtering ---

func TestQuery_PackageFilter(t *testing.T) {
	dir := helpers.CopyFixture(t, "monorepo-pnpm")
	helpers.Run(t, dir, "refresh")

	skills := queryResults(t, dir, "--package", "@test/ui", "--format", "summary")

	for _, s := range skills {
		if s.PackageName != "@test/ui" {
			t.Errorf("skill %s has package %q, want @test/ui", s.Path, s.PackageName)
		}
	}
	helpers.AssertSkillPresent(t, skills, "components.md")
	helpers.AssertSkillPresent(t, skills, "architecture.md")
	helpers.AssertSkillAbsent(t, skills, "api.md")
	helpers.AssertSkillAbsent(t, skills, "repo.md")
}

// --- flag composition ---

func TestQuery_FlagCompositionIntersection(t *testing.T) {
	dir := helpers.CopyFixture(t, "monorepo-pnpm")
	helpers.Run(t, dir, "refresh")

	skills := queryResults(t, dir,
		"--path", "packages/app-a/**",
		"--topic", "migration",
		"--tags", "breaking-change",
		"--format", "summary")

	// pnpm creates per-app node_modules symlinks so the same skill may appear under
	// multiple paths (packages/app-a/node_modules/@test/ui/... and packages/app-b/...).
	// Assert at least one result and that all results are migrations.md.
	if len(skills) == 0 {
		t.Fatal("expected at least one result (migrations.md), got none")
	}
	for _, s := range skills {
		if !strings.HasSuffix(s.Path, "migrations.md") {
			t.Errorf("unexpected skill in result: %s", s.Path)
		}
	}
}

// --- format ---

func TestQuery_FormatContent(t *testing.T) {
	dir := helpers.CopyFixture(t, "monorepo-pnpm")
	helpers.Run(t, dir, "refresh")

	res := helpers.Run(t, dir, "query",
		"--path", "packages/app-a/src/auth.ts",
		"--topic", "components",
		"--format", "content")

	if !strings.Contains(res.Stdout, "@test/ui") {
		t.Errorf("content output should contain '@test/ui', got: %q", res.Stdout[:minInt(200, len(res.Stdout))])
	}
	if strings.Contains(res.Stdout, "topics:") {
		t.Error("content output should not contain 'topics:' frontmatter")
	}
}

func TestQuery_FormatSummary(t *testing.T) {
	dir := helpers.CopyFixture(t, "monorepo-pnpm")
	helpers.Run(t, dir, "refresh")

	// With a filter, format=summary should return results without content.
	skills := queryResults(t, dir, "--topic", "migration", "--format", "summary")

	if len(skills) == 0 {
		t.Fatal("expected skills, got none")
	}
	for _, s := range skills {
		if s.Path == "" {
			t.Error("skill has empty path")
		}
		if s.Content != "" {
			t.Errorf("summary format should not include content, but skill %s has content", s.Path)
		}
	}
}

// --- no filters → vocabulary ---

func TestQuery_NoFiltersReturnsVocabulary(t *testing.T) {
	dir := helpers.CopyFixture(t, "monorepo-pnpm")
	helpers.Run(t, dir, "refresh")

	resp, _ := helpers.RunQueryJSON(t, dir, "query", "--format", "summary")

	if resp.Type != "vocabulary" {
		t.Fatalf("expected response type 'vocabulary', got %q", resp.Type)
	}
	if resp.Vocabulary == nil {
		t.Fatal("vocabulary field is nil")
	}
	if resp.Results != nil {
		t.Errorf("vocabulary response should not include results, got %d", len(resp.Results))
	}
	if resp.Vocabulary.TotalSkills == 0 {
		t.Error("total_skills should be > 0")
	}
	if len(resp.Vocabulary.Topics) == 0 {
		t.Error("vocabulary topics should not be empty")
	}
}

func TestQuery_NoFiltersVocabularyHasExpectedTopics(t *testing.T) {
	dir := helpers.CopyFixture(t, "monorepo-pnpm")
	helpers.Run(t, dir, "refresh")

	resp, _ := helpers.RunQueryJSON(t, dir, "query")

	helpers.AssertTopicInVocab(t, resp.Vocabulary, "migration")
	helpers.AssertTopicInVocab(t, resp.Vocabulary, "components")
	helpers.AssertTopicInVocab(t, resp.Vocabulary, "api")
}

func TestQuery_NoFiltersVocabularyHasExpectedTags(t *testing.T) {
	dir := helpers.CopyFixture(t, "monorepo-pnpm")
	helpers.Run(t, dir, "refresh")

	resp, _ := helpers.RunQueryJSON(t, dir, "query")

	helpers.AssertTagInVocab(t, resp.Vocabulary, "v2")
	helpers.AssertTagInVocab(t, resp.Vocabulary, "breaking-change")
}

func TestQuery_NoFiltersVocabularyHasExpectedPackages(t *testing.T) {
	dir := helpers.CopyFixture(t, "monorepo-pnpm")
	helpers.Run(t, dir, "refresh")

	resp, _ := helpers.RunQueryJSON(t, dir, "query")

	helpers.AssertPackageInVocab(t, resp.Vocabulary, "@test/ui")
	helpers.AssertPackageInVocab(t, resp.Vocabulary, "@test/utils")
}

// --- no match → no_match with vocabulary hint ---

func TestQuery_NoMatchReturnsVocabularyHint(t *testing.T) {
	dir := helpers.CopyFixture(t, "monorepo-pnpm")
	helpers.Run(t, dir, "refresh")

	resp, res := helpers.RunQueryJSON(t, dir, "query", "--topic", "nonexistent-topic-xyz", "--format", "summary")

	if res.ExitCode != 0 {
		t.Errorf("no_match should exit 0, got %d", res.ExitCode)
	}
	if resp.Type != "no_match" {
		t.Fatalf("expected response type 'no_match', got %q", resp.Type)
	}
	if resp.Vocabulary == nil {
		t.Fatal("no_match response must include vocabulary")
	}
	if len(resp.Vocabulary.Topics) == 0 {
		t.Error("no_match vocabulary should list available topics")
	}
}

func TestQuery_NoMatchEchoesQuery(t *testing.T) {
	dir := helpers.CopyFixture(t, "monorepo-pnpm")
	helpers.Run(t, dir, "refresh")

	resp, _ := helpers.RunQueryJSON(t, dir, "query", "--topic", "nonexistent-topic-xyz")

	if resp.Query == nil {
		t.Fatal("no_match response must include query echo")
	}
	if len(resp.Query.Topics) == 0 || resp.Query.Topics[0] != "nonexistent-topic-xyz" {
		t.Errorf("query echo topics: got %v, want [nonexistent-topic-xyz]", resp.Query.Topics)
	}
}

func TestQuery_NoMatchTagEchoesQuery(t *testing.T) {
	dir := helpers.CopyFixture(t, "monorepo-pnpm")
	helpers.Run(t, dir, "refresh")

	resp, _ := helpers.RunQueryJSON(t, dir, "query", "--tags", "nonexistent-tag-xyz")

	if resp.Type != "no_match" {
		t.Fatalf("expected no_match, got %q", resp.Type)
	}
	if resp.Query == nil || len(resp.Query.Tags) == 0 || resp.Query.Tags[0] != "nonexistent-tag-xyz" {
		t.Errorf("query echo tags: got %v, want [nonexistent-tag-xyz]", resp.Query.Tags)
	}
}

func TestQuery_NoMatchPackageEchoesQuery(t *testing.T) {
	dir := helpers.CopyFixture(t, "monorepo-pnpm")
	helpers.Run(t, dir, "refresh")

	resp, _ := helpers.RunQueryJSON(t, dir, "query", "--package", "nonexistent-package-xyz")

	if resp.Type != "no_match" {
		t.Fatalf("expected no_match, got %q", resp.Type)
	}
	if resp.Query == nil || resp.Query.Package != "nonexistent-package-xyz" {
		t.Errorf("query echo package: got %v", resp.Query)
	}
}

// --- JSON output validity ---

func TestQuery_JsonOutputValid(t *testing.T) {
	dir := helpers.CopyFixture(t, "monorepo-pnpm")
	helpers.Run(t, dir, "refresh")

	res := helpers.Run(t, dir, "query", "--path", "packages/app-a/src/auth.ts", "--json", "--format", "summary")

	if !json.Valid([]byte(res.Stdout)) {
		t.Errorf("stdout is not valid JSON: %q", res.Stdout)
	}
}

func TestQuery_JsonNoFiltersOutputValid(t *testing.T) {
	dir := helpers.CopyFixture(t, "monorepo-pnpm")
	helpers.Run(t, dir, "refresh")

	res := helpers.Run(t, dir, "query", "--json")

	if !json.Valid([]byte(res.Stdout)) {
		t.Errorf("stdout is not valid JSON: %q", res.Stdout)
	}
}

func TestQuery_JsonTypeFieldPresent(t *testing.T) {
	dir := helpers.CopyFixture(t, "monorepo-pnpm")
	helpers.Run(t, dir, "refresh")

	// Results response
	resp, _ := helpers.RunQueryJSON(t, dir, "query", "--topic", "migration")
	if resp.Type == "" {
		t.Error("JSON response missing 'type' field")
	}

	// Vocabulary response
	resp2, _ := helpers.RunQueryJSON(t, dir, "query")
	if resp2.Type == "" {
		t.Error("JSON vocabulary response missing 'type' field")
	}

	// No-match response
	resp3, _ := helpers.RunQueryJSON(t, dir, "query", "--topic", "nonexistent-xyz")
	if resp3.Type == "" {
		t.Error("JSON no_match response missing 'type' field")
	}
}

// --- registry missing error ---

func TestQuery_NoRegistryError(t *testing.T) {
	dir := helpers.CopyFixture(t, "monorepo-pnpm")

	res := helpers.Run(t, dir, "query", "--path", "packages/app-a/src/auth.ts")

	if res.ExitCode == 0 {
		t.Error("expected non-zero exit code when registry missing")
	}
	if !strings.Contains(res.Stderr, "refresh") {
		t.Errorf("expected 'refresh' guidance in stderr, got: %q", res.Stderr)
	}
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}
