package acceptance

// Golden test suite for the scoped vocabulary hint behaviour introduced in
// https://github.com/atheory-ai/skillex/issues/10
//
// These tests assert the precise shape of every response type (results,
// vocabulary, no_match) across all filter dimensions and their combinations.
// They use the monorepo-pnpm fixture whose skill inventory is:
//
//   repo skills (no package):
//     skills/repo.md            topics: repo, conventions         tags: global
//     skills/package-dev.md     topics: development, workflow     tags: contributing
//     skills/skill-testing.md   topics: testing, meta             tags: (none)
//
//   @test/ui (packages/ui):
//     public/components.md      topics: components, react         tags: v2
//     public/migrations.md      topics: migration                 tags: v2, breaking-change
//     public/empty-frontmatter.md  topics: (none)                 tags: (none)
//     public/no-frontmatter.md     topics: (none)                 tags: (none)
//     public/unicode-content.md    topics: see fixture            tags: see fixture
//     private/architecture.md   topics: architecture, internals   tags: (none)
//     private/dev-workflow.md   topics: workflow, testing         tags: (none)
//
//   @test/utils (packages/utils):
//     public/api.md             topics: api, helpers              tags: stable
//     private/contributing.md   topics: workflow                  tags: (none)
//
//   vendor (imported):
//     hooks.md                  topics: react, hooks              tags: external
//     imported-guide.md         topics: onboarding                tags: imported

import (
	"sort"
	"strings"
	"testing"

	"github.com/atheory-ai/skillex/test/helpers"
)

// ---- Response type assertions -----------------------------------------------

func TestGolden_NoFilters_ResponseType(t *testing.T) {
	dir := helpers.CopyFixture(t, "monorepo-pnpm")
	helpers.Run(t, dir, "refresh")

	resp, _ := helpers.RunQueryJSON(t, dir, "query")
	if resp.Type != "vocabulary" {
		t.Errorf("no-filter query: want type=vocabulary, got %q", resp.Type)
	}
}

func TestGolden_WithFilter_ResponseType(t *testing.T) {
	dir := helpers.CopyFixture(t, "monorepo-pnpm")
	helpers.Run(t, dir, "refresh")

	resp, _ := helpers.RunQueryJSON(t, dir, "query", "--topic", "migration")
	if resp.Type != "results" {
		t.Errorf("topic filter query: want type=results, got %q", resp.Type)
	}
}

func TestGolden_NoMatch_ResponseType(t *testing.T) {
	dir := helpers.CopyFixture(t, "monorepo-pnpm")
	helpers.Run(t, dir, "refresh")

	resp, _ := helpers.RunQueryJSON(t, dir, "query", "--topic", "does-not-exist-xyz")
	if resp.Type != "no_match" {
		t.Errorf("nonexistent topic: want type=no_match, got %q", resp.Type)
	}
}

// ---- Vocabulary structure ---------------------------------------------------

func TestGolden_Vocabulary_HasTotalSkills(t *testing.T) {
	dir := helpers.CopyFixture(t, "monorepo-pnpm")
	helpers.Run(t, dir, "refresh")

	resp, _ := helpers.RunQueryJSON(t, dir, "query")
	if resp.Vocabulary == nil {
		t.Fatal("vocabulary is nil")
	}
	if resp.Vocabulary.TotalSkills <= 0 {
		t.Errorf("total_skills should be > 0, got %d", resp.Vocabulary.TotalSkills)
	}
}

func TestGolden_Vocabulary_TopicsSorted(t *testing.T) {
	dir := helpers.CopyFixture(t, "monorepo-pnpm")
	helpers.Run(t, dir, "refresh")

	resp, _ := helpers.RunQueryJSON(t, dir, "query")
	if resp.Vocabulary == nil {
		t.Fatal("vocabulary is nil")
	}
	names := make([]string, len(resp.Vocabulary.Topics))
	for i, e := range resp.Vocabulary.Topics {
		names[i] = e.Name
	}
	sorted := append([]string(nil), names...)
	sort.Strings(sorted)
	for i := range names {
		if names[i] != sorted[i] {
			t.Errorf("topics not sorted: got %v", names)
			break
		}
	}
}

func TestGolden_Vocabulary_TagsSorted(t *testing.T) {
	dir := helpers.CopyFixture(t, "monorepo-pnpm")
	helpers.Run(t, dir, "refresh")

	resp, _ := helpers.RunQueryJSON(t, dir, "query")
	if resp.Vocabulary == nil {
		t.Fatal("vocabulary is nil")
	}
	names := make([]string, len(resp.Vocabulary.Tags))
	for i, e := range resp.Vocabulary.Tags {
		names[i] = e.Name
	}
	sorted := append([]string(nil), names...)
	sort.Strings(sorted)
	for i := range names {
		if names[i] != sorted[i] {
			t.Errorf("tags not sorted: got %v", names)
			break
		}
	}
}

func TestGolden_Vocabulary_PackagesSorted(t *testing.T) {
	dir := helpers.CopyFixture(t, "monorepo-pnpm")
	helpers.Run(t, dir, "refresh")

	resp, _ := helpers.RunQueryJSON(t, dir, "query")
	if resp.Vocabulary == nil {
		t.Fatal("vocabulary is nil")
	}
	names := make([]string, len(resp.Vocabulary.Packages))
	for i, e := range resp.Vocabulary.Packages {
		names[i] = e.Name
	}
	sorted := append([]string(nil), names...)
	sort.Strings(sorted)
	for i := range names {
		if names[i] != sorted[i] {
			t.Errorf("packages not sorted: got %v", names)
			break
		}
	}
}

func TestGolden_Vocabulary_TopicCountsPositive(t *testing.T) {
	dir := helpers.CopyFixture(t, "monorepo-pnpm")
	helpers.Run(t, dir, "refresh")

	resp, _ := helpers.RunQueryJSON(t, dir, "query")
	if resp.Vocabulary == nil {
		t.Fatal("vocabulary is nil")
	}
	for _, e := range resp.Vocabulary.Topics {
		if e.Count <= 0 {
			t.Errorf("topic %q has count %d, want > 0", e.Name, e.Count)
		}
	}
}

func TestGolden_Vocabulary_TagCountsPositive(t *testing.T) {
	dir := helpers.CopyFixture(t, "monorepo-pnpm")
	helpers.Run(t, dir, "refresh")

	resp, _ := helpers.RunQueryJSON(t, dir, "query")
	if resp.Vocabulary == nil {
		t.Fatal("vocabulary is nil")
	}
	for _, e := range resp.Vocabulary.Tags {
		if e.Count <= 0 {
			t.Errorf("tag %q has count %d, want > 0", e.Name, e.Count)
		}
	}
}

func TestGolden_Vocabulary_PackageCountsPositive(t *testing.T) {
	dir := helpers.CopyFixture(t, "monorepo-pnpm")
	helpers.Run(t, dir, "refresh")

	resp, _ := helpers.RunQueryJSON(t, dir, "query")
	if resp.Vocabulary == nil {
		t.Fatal("vocabulary is nil")
	}
	for _, e := range resp.Vocabulary.Packages {
		if e.Count <= 0 {
			t.Errorf("package %q has count %d, want > 0", e.Name, e.Count)
		}
	}
}

// ---- Vocabulary topic presence ----------------------------------------------

func TestGolden_Vocabulary_HasTopic_Migration(t *testing.T) {
	dir := helpers.CopyFixture(t, "monorepo-pnpm")
	helpers.Run(t, dir, "refresh")
	resp, _ := helpers.RunQueryJSON(t, dir, "query")
	helpers.AssertTopicInVocab(t, resp.Vocabulary, "migration")
}

func TestGolden_Vocabulary_HasTopic_Components(t *testing.T) {
	dir := helpers.CopyFixture(t, "monorepo-pnpm")
	helpers.Run(t, dir, "refresh")
	resp, _ := helpers.RunQueryJSON(t, dir, "query")
	helpers.AssertTopicInVocab(t, resp.Vocabulary, "components")
}

func TestGolden_Vocabulary_HasTopic_API(t *testing.T) {
	dir := helpers.CopyFixture(t, "monorepo-pnpm")
	helpers.Run(t, dir, "refresh")
	resp, _ := helpers.RunQueryJSON(t, dir, "query")
	helpers.AssertTopicInVocab(t, resp.Vocabulary, "api")
}

func TestGolden_Vocabulary_HasTopic_React(t *testing.T) {
	dir := helpers.CopyFixture(t, "monorepo-pnpm")
	helpers.Run(t, dir, "refresh")
	resp, _ := helpers.RunQueryJSON(t, dir, "query")
	helpers.AssertTopicInVocab(t, resp.Vocabulary, "react")
}

func TestGolden_Vocabulary_HasTopic_Workflow(t *testing.T) {
	dir := helpers.CopyFixture(t, "monorepo-pnpm")
	helpers.Run(t, dir, "refresh")
	resp, _ := helpers.RunQueryJSON(t, dir, "query")
	helpers.AssertTopicInVocab(t, resp.Vocabulary, "workflow")
}

func TestGolden_Vocabulary_HasTopic_Architecture(t *testing.T) {
	dir := helpers.CopyFixture(t, "monorepo-pnpm")
	helpers.Run(t, dir, "refresh")
	resp, _ := helpers.RunQueryJSON(t, dir, "query")
	helpers.AssertTopicInVocab(t, resp.Vocabulary, "architecture")
}

// ---- Vocabulary tag presence ------------------------------------------------

func TestGolden_Vocabulary_HasTag_V2(t *testing.T) {
	dir := helpers.CopyFixture(t, "monorepo-pnpm")
	helpers.Run(t, dir, "refresh")
	resp, _ := helpers.RunQueryJSON(t, dir, "query")
	helpers.AssertTagInVocab(t, resp.Vocabulary, "v2")
}

func TestGolden_Vocabulary_HasTag_BreakingChange(t *testing.T) {
	dir := helpers.CopyFixture(t, "monorepo-pnpm")
	helpers.Run(t, dir, "refresh")
	resp, _ := helpers.RunQueryJSON(t, dir, "query")
	helpers.AssertTagInVocab(t, resp.Vocabulary, "breaking-change")
}

func TestGolden_Vocabulary_HasTag_Stable(t *testing.T) {
	dir := helpers.CopyFixture(t, "monorepo-pnpm")
	helpers.Run(t, dir, "refresh")
	resp, _ := helpers.RunQueryJSON(t, dir, "query")
	helpers.AssertTagInVocab(t, resp.Vocabulary, "stable")
}

func TestGolden_Vocabulary_HasTag_Global(t *testing.T) {
	dir := helpers.CopyFixture(t, "monorepo-pnpm")
	helpers.Run(t, dir, "refresh")
	resp, _ := helpers.RunQueryJSON(t, dir, "query")
	helpers.AssertTagInVocab(t, resp.Vocabulary, "global")
}

// ---- Vocabulary package presence --------------------------------------------

func TestGolden_Vocabulary_HasPackage_UI(t *testing.T) {
	dir := helpers.CopyFixture(t, "monorepo-pnpm")
	helpers.Run(t, dir, "refresh")
	resp, _ := helpers.RunQueryJSON(t, dir, "query")
	helpers.AssertPackageInVocab(t, resp.Vocabulary, "@test/ui")
}

func TestGolden_Vocabulary_HasPackage_Utils(t *testing.T) {
	dir := helpers.CopyFixture(t, "monorepo-pnpm")
	helpers.Run(t, dir, "refresh")
	resp, _ := helpers.RunQueryJSON(t, dir, "query")
	helpers.AssertPackageInVocab(t, resp.Vocabulary, "@test/utils")
}

// ---- Vocabulary topic counts ------------------------------------------------

func TestGolden_Vocabulary_MigrationCount(t *testing.T) {
	dir := helpers.CopyFixture(t, "monorepo-pnpm")
	helpers.Run(t, dir, "refresh")
	resp, _ := helpers.RunQueryJSON(t, dir, "query")
	count := helpers.VocabTopicCount(resp.Vocabulary, "migration")
	// pnpm creates per-app node_modules symlinks so skills may be registered under
	// multiple paths (packages/app-a/node_modules/... and packages/app-b/...).
	// Assert >= 1 rather than an exact count that depends on pnpm layout.
	if count < 1 {
		t.Errorf("topic 'migration' count: got %d, want >= 1", count)
	}
}

func TestGolden_Vocabulary_WorkflowCount(t *testing.T) {
	dir := helpers.CopyFixture(t, "monorepo-pnpm")
	helpers.Run(t, dir, "refresh")
	resp, _ := helpers.RunQueryJSON(t, dir, "query")
	// workflow appears in: package-dev.md, dev-workflow.md, contributing.md = 3
	count := helpers.VocabTopicCount(resp.Vocabulary, "workflow")
	if count < 2 {
		t.Errorf("topic 'workflow' count: got %d, want >= 2", count)
	}
}

func TestGolden_Vocabulary_ReactCount(t *testing.T) {
	dir := helpers.CopyFixture(t, "monorepo-pnpm")
	helpers.Run(t, dir, "refresh")
	resp, _ := helpers.RunQueryJSON(t, dir, "query")
	// react appears in: components.md and hooks.md = at least 2
	count := helpers.VocabTopicCount(resp.Vocabulary, "react")
	if count < 2 {
		t.Errorf("topic 'react' count: got %d, want >= 2", count)
	}
}

// ---- Vocabulary tag counts --------------------------------------------------

func TestGolden_Vocabulary_V2Count(t *testing.T) {
	dir := helpers.CopyFixture(t, "monorepo-pnpm")
	helpers.Run(t, dir, "refresh")
	resp, _ := helpers.RunQueryJSON(t, dir, "query")
	// v2 appears in components.md and migrations.md; pnpm may register multiple
	// copies per app's node_modules, so assert >= 2.
	count := helpers.VocabTagCount(resp.Vocabulary, "v2")
	if count < 2 {
		t.Errorf("tag 'v2' count: got %d, want >= 2", count)
	}
}

func TestGolden_Vocabulary_BreakingChangeCount(t *testing.T) {
	dir := helpers.CopyFixture(t, "monorepo-pnpm")
	helpers.Run(t, dir, "refresh")
	resp, _ := helpers.RunQueryJSON(t, dir, "query")
	// breaking-change appears only in migrations.md; pnpm may register copies per
	// app's node_modules, so assert >= 1.
	count := helpers.VocabTagCount(resp.Vocabulary, "breaking-change")
	if count < 1 {
		t.Errorf("tag 'breaking-change' count: got %d, want >= 1", count)
	}
}

// ---- No-match vocabulary hint -----------------------------------------------

func TestGolden_NoMatch_Topic_VocabPresent(t *testing.T) {
	dir := helpers.CopyFixture(t, "monorepo-pnpm")
	helpers.Run(t, dir, "refresh")

	resp, _ := helpers.RunQueryJSON(t, dir, "query", "--topic", "no-such-topic")

	if resp.Type != "no_match" {
		t.Fatalf("want no_match, got %q", resp.Type)
	}
	if resp.Vocabulary == nil {
		t.Fatal("no_match must include vocabulary")
	}
	// The vocabulary should still contain real topics
	helpers.AssertTopicInVocab(t, resp.Vocabulary, "migration")
	helpers.AssertTopicInVocab(t, resp.Vocabulary, "components")
}

func TestGolden_NoMatch_Tag_VocabPresent(t *testing.T) {
	dir := helpers.CopyFixture(t, "monorepo-pnpm")
	helpers.Run(t, dir, "refresh")

	resp, _ := helpers.RunQueryJSON(t, dir, "query", "--tags", "no-such-tag")

	if resp.Type != "no_match" {
		t.Fatalf("want no_match, got %q", resp.Type)
	}
	helpers.AssertTagInVocab(t, resp.Vocabulary, "v2")
	helpers.AssertTagInVocab(t, resp.Vocabulary, "breaking-change")
}

func TestGolden_NoMatch_Package_VocabPresent(t *testing.T) {
	dir := helpers.CopyFixture(t, "monorepo-pnpm")
	helpers.Run(t, dir, "refresh")

	resp, _ := helpers.RunQueryJSON(t, dir, "query", "--package", "@no-such/package")

	if resp.Type != "no_match" {
		t.Fatalf("want no_match, got %q", resp.Type)
	}
	helpers.AssertPackageInVocab(t, resp.Vocabulary, "@test/ui")
	helpers.AssertPackageInVocab(t, resp.Vocabulary, "@test/utils")
}

func TestGolden_NoMatch_Path_VocabPresent(t *testing.T) {
	dir := helpers.CopyFixture(t, "monorepo-pnpm")
	helpers.Run(t, dir, "refresh")

	// Combine a path filter with a package that genuinely has no skills at that path.
	// packages/app-b/** does not include @test/utils skills (only @test/ui).
	// Using an impossible intersection guarantees no_match without relying on
	// path-only behaviour (repo skills have ** scope and match any path).
	resp, _ := helpers.RunQueryJSON(t, dir, "query",
		"--path", "no/such/path/file.ts",
		"--topic", "nonexistent-topic-xyz")

	if resp.Type != "no_match" {
		t.Fatalf("want no_match, got %q", resp.Type)
	}
	if resp.Vocabulary == nil {
		t.Fatal("no_match must include vocabulary")
	}
}

// ---- No-match query echo ----------------------------------------------------

func TestGolden_NoMatch_Echo_Topic(t *testing.T) {
	dir := helpers.CopyFixture(t, "monorepo-pnpm")
	helpers.Run(t, dir, "refresh")

	resp, _ := helpers.RunQueryJSON(t, dir, "query", "--topic", "ghost-topic")

	if resp.Query == nil {
		t.Fatal("no_match must include query echo")
	}
	if len(resp.Query.Topics) != 1 || resp.Query.Topics[0] != "ghost-topic" {
		t.Errorf("query echo topics: got %v, want [ghost-topic]", resp.Query.Topics)
	}
	if resp.Query.Tags != nil {
		t.Errorf("query echo tags should be absent for topic-only query, got %v", resp.Query.Tags)
	}
}

func TestGolden_NoMatch_Echo_Tags(t *testing.T) {
	dir := helpers.CopyFixture(t, "monorepo-pnpm")
	helpers.Run(t, dir, "refresh")

	resp, _ := helpers.RunQueryJSON(t, dir, "query", "--tags", "ghost-a,ghost-b")

	if resp.Query == nil {
		t.Fatal("no_match must include query echo")
	}
	if len(resp.Query.Tags) != 2 {
		t.Errorf("query echo tags: got %v, want [ghost-a ghost-b]", resp.Query.Tags)
	}
}

func TestGolden_NoMatch_Echo_Package(t *testing.T) {
	dir := helpers.CopyFixture(t, "monorepo-pnpm")
	helpers.Run(t, dir, "refresh")

	resp, _ := helpers.RunQueryJSON(t, dir, "query", "--package", "@ghost/pkg")

	if resp.Query == nil {
		t.Fatal("no_match must include query echo")
	}
	if resp.Query.Package != "@ghost/pkg" {
		t.Errorf("query echo package: got %q, want @ghost/pkg", resp.Query.Package)
	}
}

func TestGolden_NoMatch_Echo_Path(t *testing.T) {
	dir := helpers.CopyFixture(t, "monorepo-pnpm")
	helpers.Run(t, dir, "refresh")

	// Combine path + nonexistent topic to guarantee no_match
	// (path-only can still return results from **-scoped repo skills).
	resp, _ := helpers.RunQueryJSON(t, dir, "query",
		"--path", "ghost/path/file.ts",
		"--topic", "nonexistent-topic-xyz")

	if resp.Type != "no_match" {
		t.Fatalf("want no_match, got %q", resp.Type)
	}
	if resp.Query == nil {
		t.Fatal("no_match must include query echo")
	}
	if resp.Query.Path != "ghost/path/file.ts" {
		t.Errorf("query echo path: got %q, want ghost/path/file.ts", resp.Query.Path)
	}
}

func TestGolden_NoMatch_Echo_MultipleFilters(t *testing.T) {
	dir := helpers.CopyFixture(t, "monorepo-pnpm")
	helpers.Run(t, dir, "refresh")

	// Both filters individually would match, but their intersection won't:
	// --topic migration --package @test/utils
	// (migrations.md is in @test/ui, not @test/utils)
	resp, _ := helpers.RunQueryJSON(t, dir, "query",
		"--topic", "migration",
		"--package", "@test/utils")

	if resp.Type != "no_match" {
		t.Fatalf("want no_match for impossible filter intersection, got %q", resp.Type)
	}
	if resp.Query == nil {
		t.Fatal("no_match must include query echo")
	}
	if len(resp.Query.Topics) == 0 || resp.Query.Topics[0] != "migration" {
		t.Errorf("query echo topics: got %v", resp.Query.Topics)
	}
	if resp.Query.Package != "@test/utils" {
		t.Errorf("query echo package: got %q", resp.Query.Package)
	}
}

// ---- Results response — no vocabulary or query echo ------------------------

func TestGolden_Results_NoVocabulary(t *testing.T) {
	dir := helpers.CopyFixture(t, "monorepo-pnpm")
	helpers.Run(t, dir, "refresh")

	resp, _ := helpers.RunQueryJSON(t, dir, "query", "--topic", "migration")

	if resp.Type != "results" {
		t.Fatalf("want results, got %q", resp.Type)
	}
	if resp.Vocabulary != nil {
		t.Error("results response should not include vocabulary")
	}
	if resp.Query != nil {
		t.Error("results response should not include query echo")
	}
}

func TestGolden_Results_HasSkills(t *testing.T) {
	dir := helpers.CopyFixture(t, "monorepo-pnpm")
	helpers.Run(t, dir, "refresh")

	resp, _ := helpers.RunQueryJSON(t, dir, "query", "--topic", "migration")

	if len(resp.Results) == 0 {
		t.Error("results response should contain at least one skill")
	}
}

func TestGolden_Results_SkillsHaveRequiredFields(t *testing.T) {
	dir := helpers.CopyFixture(t, "monorepo-pnpm")
	helpers.Run(t, dir, "refresh")

	resp, _ := helpers.RunQueryJSON(t, dir, "query", "--topic", "migration", "--format", "summary")

	for _, s := range resp.Results {
		if s.Path == "" {
			t.Error("result skill has empty path")
		}
		if s.Visibility == "" {
			t.Errorf("result skill %s has empty visibility", s.Path)
		}
		if s.SourceType == "" {
			t.Errorf("result skill %s has empty source_type", s.Path)
		}
	}
}

func TestGolden_Results_FormatSummaryNoContent(t *testing.T) {
	dir := helpers.CopyFixture(t, "monorepo-pnpm")
	helpers.Run(t, dir, "refresh")

	resp, _ := helpers.RunQueryJSON(t, dir, "query", "--topic", "migration", "--format", "summary")

	for _, s := range resp.Results {
		if s.Content != "" {
			t.Errorf("format=summary should not include content, but %s has content", s.Path)
		}
	}
}

func TestGolden_Results_FormatContentHasContent(t *testing.T) {
	dir := helpers.CopyFixture(t, "monorepo-pnpm")
	helpers.Run(t, dir, "refresh")

	resp, _ := helpers.RunQueryJSON(t, dir, "query", "--topic", "migration", "--format", "content")

	hasContent := false
	for _, s := range resp.Results {
		if s.Content != "" {
			hasContent = true
			break
		}
	}
	if !hasContent {
		t.Error("format=content response should include skill content")
	}
}

// ---- Vocabulary response — no results or query echo -------------------------

func TestGolden_Vocabulary_NoResults(t *testing.T) {
	dir := helpers.CopyFixture(t, "monorepo-pnpm")
	helpers.Run(t, dir, "refresh")

	resp, _ := helpers.RunQueryJSON(t, dir, "query")

	if resp.Type != "vocabulary" {
		t.Fatalf("want vocabulary, got %q", resp.Type)
	}
	if resp.Results != nil {
		t.Errorf("vocabulary response should not have results, got %d", len(resp.Results))
	}
	if resp.Query != nil {
		t.Error("vocabulary response should not have query echo")
	}
}

// ---- Exit codes -------------------------------------------------------------

func TestGolden_ExitCode_Results(t *testing.T) {
	dir := helpers.CopyFixture(t, "monorepo-pnpm")
	helpers.Run(t, dir, "refresh")

	_, res := helpers.RunQueryJSON(t, dir, "query", "--topic", "migration")
	if res.ExitCode != 0 {
		t.Errorf("results response should exit 0, got %d", res.ExitCode)
	}
}

func TestGolden_ExitCode_Vocabulary(t *testing.T) {
	dir := helpers.CopyFixture(t, "monorepo-pnpm")
	helpers.Run(t, dir, "refresh")

	_, res := helpers.RunQueryJSON(t, dir, "query")
	if res.ExitCode != 0 {
		t.Errorf("vocabulary response should exit 0, got %d", res.ExitCode)
	}
}

func TestGolden_ExitCode_NoMatch(t *testing.T) {
	dir := helpers.CopyFixture(t, "monorepo-pnpm")
	helpers.Run(t, dir, "refresh")

	_, res := helpers.RunQueryJSON(t, dir, "query", "--topic", "nonexistent-xyz")
	if res.ExitCode != 0 {
		t.Errorf("no_match response should exit 0, got %d", res.ExitCode)
	}
}

// ---- Vocabulary consistency: counts match actual query results --------------

func TestGolden_Vocabulary_MigrationCountMatchesQuery(t *testing.T) {
	dir := helpers.CopyFixture(t, "monorepo-pnpm")
	helpers.Run(t, dir, "refresh")

	vocabResp, _ := helpers.RunQueryJSON(t, dir, "query")
	resultsResp, _ := helpers.RunQueryJSON(t, dir, "query", "--topic", "migration")

	vocabCount := helpers.VocabTopicCount(vocabResp.Vocabulary, "migration")
	// pnpm creates per-app node_modules so the same skill appears under multiple
	// paths; vocabulary counts and query results must agree on the same total.
	if vocabCount != len(resultsResp.Results) {
		t.Errorf("vocabulary count for topic 'migration' (%d) does not match query results (%d)",
			vocabCount, len(resultsResp.Results))
	}
}

func TestGolden_Vocabulary_V2CountMatchesQuery(t *testing.T) {
	dir := helpers.CopyFixture(t, "monorepo-pnpm")
	helpers.Run(t, dir, "refresh")

	vocabResp, _ := helpers.RunQueryJSON(t, dir, "query")
	resultsResp, _ := helpers.RunQueryJSON(t, dir, "query", "--tags", "v2")

	vocabCount := helpers.VocabTagCount(vocabResp.Vocabulary, "v2")
	if vocabCount != len(resultsResp.Results) {
		t.Errorf("vocabulary count for tag 'v2' (%d) does not match query results (%d)",
			vocabCount, len(resultsResp.Results))
	}
}

// ---- All code paths do not return all-skills content -----------------------

func TestGolden_NoFilter_DoesNotReturnContent(t *testing.T) {
	dir := helpers.CopyFixture(t, "monorepo-pnpm")
	helpers.Run(t, dir, "refresh")

	resp, _ := helpers.RunQueryJSON(t, dir, "query")

	if resp.Type != "vocabulary" {
		t.Fatalf("want vocabulary, got %q", resp.Type)
	}
	// Vocabulary must not contain any skill content
	if resp.Results != nil {
		for _, s := range resp.Results {
			if s.Content != "" {
				t.Errorf("no-filter response must not return skill content, but %s has content", s.Path)
			}
		}
	}
}

func TestGolden_NoMatch_DoesNotReturnContent(t *testing.T) {
	dir := helpers.CopyFixture(t, "monorepo-pnpm")
	helpers.Run(t, dir, "refresh")

	resp, _ := helpers.RunQueryJSON(t, dir, "query", "--topic", "ghost-xyz")

	if resp.Type != "no_match" {
		t.Fatalf("want no_match, got %q", resp.Type)
	}
	if resp.Results != nil {
		for _, s := range resp.Results {
			if s.Content != "" {
				t.Errorf("no_match response must not return skill content, but %s has content", s.Path)
			}
		}
	}
}

// ---- Scoped no-match vocabulary (path filter) -------------------------------

func TestGolden_NoMatch_PathScoped_VocabIsScoped(t *testing.T) {
	dir := helpers.CopyFixture(t, "monorepo-pnpm")
	helpers.Run(t, dir, "refresh")

	// Query with a path that reaches skills (so path filter works) + a topic that
	// doesn't exist → no_match, but vocabulary should be scoped to path-reachable skills.
	// packages/app-a/src/auth.ts is reachable by @test/ui and repo skills.
	resp, _ := helpers.RunQueryJSON(t, dir, "query",
		"--path", "packages/app-a/src/auth.ts",
		"--topic", "nonexistent-topic-xyz")

	if resp.Type != "no_match" {
		t.Fatalf("want no_match, got %q", resp.Type)
	}
	if resp.Vocabulary == nil {
		t.Fatal("no_match must include vocabulary")
	}
	// The scoped vocabulary should include topics from path-reachable skills
	// (e.g. components, migration from @test/ui, api from @test/utils, repo from **)
	helpers.AssertTopicInVocab(t, resp.Vocabulary, "components")
	helpers.AssertTopicInVocab(t, resp.Vocabulary, "migration")
	// Total skills should be the count of path-reachable skills (not the whole registry)
	if resp.Vocabulary.TotalSkills == 0 {
		t.Error("scoped vocabulary total_skills should be > 0")
	}
}

func TestGolden_NoMatch_NoPath_VocabIsGlobal(t *testing.T) {
	dir := helpers.CopyFixture(t, "monorepo-pnpm")
	helpers.Run(t, dir, "refresh")

	// Topic-only no_match — no path provided, vocabulary should be global
	resp, _ := helpers.RunQueryJSON(t, dir, "query", "--topic", "nonexistent-topic-xyz")

	if resp.Type != "no_match" {
		t.Fatalf("want no_match, got %q", resp.Type)
	}
	// Global vocabulary includes all topics across the registry
	helpers.AssertTopicInVocab(t, resp.Vocabulary, "architecture") // @test/ui private — only in global
	helpers.AssertTopicInVocab(t, resp.Vocabulary, "migration")
}

// ---- MCP vocabulary hints (structured JSON) ---------------------------------

func TestGolden_MCP_NoFilters_ReturnsStructuredJSON(t *testing.T) {
	dir := helpers.CopyFixture(t, "monorepo-pnpm")
	helpers.Run(t, dir, "refresh")

	client := helpers.StartMCPServer(t, dir)
	defer client.Close()

	text, err := client.CallToolText("skillex_query", map[string]interface{}{})
	if err != nil {
		t.Fatalf("skillex_query with no params failed: %v", err)
	}
	if text == "" {
		t.Error("expected non-empty vocabulary response from MCP")
	}
	// Response must be valid JSON containing the type field
	if !strings.Contains(text, `"type"`) || !strings.Contains(text, `"vocabulary"`) {
		t.Errorf("MCP vocabulary response should be JSON with type/vocabulary fields, got: %q", truncate(text, 300))
	}
	if !strings.Contains(text, `"vocabulary"`) {
		t.Errorf("MCP vocabulary response should contain 'vocabulary' key, got: %q", truncate(text, 300))
	}
}

func TestGolden_MCP_NoMatch_ReturnsStructuredJSON(t *testing.T) {
	dir := helpers.CopyFixture(t, "monorepo-pnpm")
	helpers.Run(t, dir, "refresh")

	client := helpers.StartMCPServer(t, dir)
	defer client.Close()

	text, err := client.CallToolText("skillex_query", map[string]interface{}{
		"topic": "ghost-topic-xyz",
	})
	if err != nil {
		t.Fatalf("skillex_query no-match failed: %v", err)
	}
	// Response must be JSON with type=no_match
	if !strings.Contains(text, `"no_match"`) {
		t.Errorf("MCP no-match response should contain 'no_match' type, got: %q", truncate(text, 300))
	}
	if !strings.Contains(text, `"vocabulary"`) {
		t.Errorf("MCP no-match response should include vocabulary, got: %q", truncate(text, 300))
	}
}

func TestGolden_MCP_WithFilter_ReturnsContent(t *testing.T) {
	dir := helpers.CopyFixture(t, "monorepo-pnpm")
	helpers.Run(t, dir, "refresh")

	client := helpers.StartMCPServer(t, dir)
	defer client.Close()

	text, err := client.CallToolText("skillex_query", map[string]interface{}{
		"topic":  "migration",
		"format": "content",
	})
	if err != nil {
		t.Fatalf("skillex_query topic filter failed: %v", err)
	}
	if !strings.Contains(text, "Migration") && !strings.Contains(text, "migration") {
		t.Errorf("expected migration skill content, got: %q", truncate(text, 300))
	}
}

// ---- Format flag does not change vocabulary/no_match response type ----------

func TestGolden_NoFilter_FormatSummary_StillVocabulary(t *testing.T) {
	dir := helpers.CopyFixture(t, "monorepo-pnpm")
	helpers.Run(t, dir, "refresh")

	resp, _ := helpers.RunQueryJSON(t, dir, "query", "--format", "summary")
	if resp.Type != "vocabulary" {
		t.Errorf("no-filter + format=summary should still be vocabulary, got %q", resp.Type)
	}
}

func TestGolden_NoFilter_FormatContent_StillVocabulary(t *testing.T) {
	dir := helpers.CopyFixture(t, "monorepo-pnpm")
	helpers.Run(t, dir, "refresh")

	resp, _ := helpers.RunQueryJSON(t, dir, "query", "--format", "content")
	if resp.Type != "vocabulary" {
		t.Errorf("no-filter + format=content should still be vocabulary, got %q", resp.Type)
	}
}

func TestGolden_NoMatch_FormatSummary_StillNoMatch(t *testing.T) {
	dir := helpers.CopyFixture(t, "monorepo-pnpm")
	helpers.Run(t, dir, "refresh")

	resp, _ := helpers.RunQueryJSON(t, dir, "query", "--topic", "ghost-topic", "--format", "summary")
	if resp.Type != "no_match" {
		t.Errorf("no-match + format=summary should still be no_match, got %q", resp.Type)
	}
}

// ---- Helpers ----------------------------------------------------------------

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}
