package acceptance

// Search acceptance tests (name/description frontmatter + --search)
// and no results returned when nothing matches.
// Fixture inventory used by these tests (monorepo-pnpm):
//
//	packages/ui/skillex/public/button-accessibility.md
//	  name: "Button Accessibility"
//	  description: "ARIA labels, keyboard navigation, and focus management guidelines for Button components."
//	  topics: (none)   tags: (none)
//
// This skill is intentionally topic/tag-free to prove search works without taxonomy.

import (
	"strings"
	"testing"

	"github.com/atheory-ai/skillex/test/helpers"
)

// --- name/description stored in registry ---

func TestSearch_NameStoredInRegistry(t *testing.T) {
	dir := helpers.CopyFixture(t, "monorepo-pnpm")
	helpers.Run(t, dir, "refresh")

	db := helpers.OpenRegistry(t, dir)
	name := helpers.QueryNameFor(t, db, "button-accessibility")
	if name != "Button Accessibility" {
		t.Errorf("name: got %q, want %q", name, "Button Accessibility")
	}
}

func TestSearch_DescriptionStoredInRegistry(t *testing.T) {
	dir := helpers.CopyFixture(t, "monorepo-pnpm")
	helpers.Run(t, dir, "refresh")

	db := helpers.OpenRegistry(t, dir)
	desc := helpers.QueryDescriptionFor(t, db, "button-accessibility")
	if !strings.Contains(desc, "ARIA") {
		t.Errorf("description should contain 'ARIA', got %q", desc)
	}
}

// --- basic search matching ---

func TestSearch_MatchesName(t *testing.T) {
	dir := helpers.CopyFixture(t, "monorepo-pnpm")
	helpers.Run(t, dir, "refresh")

	resp, _ := helpers.RunQueryJSON(t, dir, "query", "--search", "Button Accessibility", "--format", "summary")

	if resp.Type != "results" {
		t.Fatalf("expected results, got %q", resp.Type)
	}
	helpers.AssertSkillPresent(t, resp.Results, "button-accessibility.md")
}

func TestSearch_MatchesDescription(t *testing.T) {
	dir := helpers.CopyFixture(t, "monorepo-pnpm")
	helpers.Run(t, dir, "refresh")

	// "ARIA" appears only in button-accessibility.md description
	resp, _ := helpers.RunQueryJSON(t, dir, "query", "--search", "ARIA", "--format", "summary")

	if resp.Type != "results" {
		t.Fatalf("expected results, got %q", resp.Type)
	}
	helpers.AssertSkillPresent(t, resp.Results, "button-accessibility.md")
}

func TestSearch_CaseInsensitive(t *testing.T) {
	dir := helpers.CopyFixture(t, "monorepo-pnpm")
	helpers.Run(t, dir, "refresh")

	resp, _ := helpers.RunQueryJSON(t, dir, "query", "--search", "aria", "--format", "summary")

	if resp.Type != "results" {
		t.Fatalf("expected results, got %q", resp.Type)
	}
	helpers.AssertSkillPresent(t, resp.Results, "button-accessibility.md")
}

// --- multi-token OR matching ---

func TestSearch_MultiTokenFindsMultipleSkills(t *testing.T) {
	dir := helpers.CopyFixture(t, "monorepo-pnpm")
	helpers.Run(t, dir, "refresh")

	// "button" matches button-accessibility.md (name)
	// "components" matches components.md (name or description if set, or via content — but
	// components.md has topics:components so this also validates token independence)
	// We assert at least two distinct skills are returned.
	resp, _ := helpers.RunQueryJSON(t, dir, "query", "--search", "Button components", "--format", "summary")

	if resp.Type != "results" {
		t.Fatalf("expected results, got %q", resp.Type)
	}
	if len(resp.Results) < 2 {
		t.Errorf("multi-token search should return multiple skills; got %d result(s)", len(resp.Results))
	}
	helpers.AssertSkillPresent(t, resp.Results, "button-accessibility.md")
}

func TestSearch_CommaSeparatedTokens(t *testing.T) {
	dir := helpers.CopyFixture(t, "monorepo-pnpm")
	helpers.Run(t, dir, "refresh")

	// Comma-separated should behave identically to space-separated
	resp, _ := helpers.RunQueryJSON(t, dir, "query", "--search", "ARIA,keyboard", "--format", "summary")

	if resp.Type != "results" {
		t.Fatalf("expected results, got %q", resp.Type)
	}
	helpers.AssertSkillPresent(t, resp.Results, "button-accessibility.md")
}

// --- ATH-172: no results when nothing matches, NOT all results ---

func TestSearch_NoMatchReturnsNoMatchType(t *testing.T) {
	dir := helpers.CopyFixture(t, "monorepo-pnpm")
	helpers.Run(t, dir, "refresh")

	resp, res := helpers.RunQueryJSON(t, dir, "query", "--search", "xyzzy-does-not-exist-abc123")

	if res.ExitCode != 0 {
		t.Errorf("no_match should exit 0, got %d", res.ExitCode)
	}
	if resp.Type != "no_match" {
		t.Fatalf("expected no_match, got %q (ATH-172: must not return all skills)", resp.Type)
	}
}

func TestSearch_NoMatchHasZeroResults(t *testing.T) {
	dir := helpers.CopyFixture(t, "monorepo-pnpm")
	helpers.Run(t, dir, "refresh")

	resp, _ := helpers.RunQueryJSON(t, dir, "query", "--search", "xyzzy-does-not-exist-abc123")

	// ATH-172: the no_match response must have no Results — it must not silently
	// return all skills because search was the only filter and found nothing.
	if len(resp.Results) > 0 {
		t.Errorf("no_match response must have 0 results, got %d (ATH-172 regression)", len(resp.Results))
	}
}

func TestSearch_NoMatchIncludesVocabularyHint(t *testing.T) {
	dir := helpers.CopyFixture(t, "monorepo-pnpm")
	helpers.Run(t, dir, "refresh")

	resp, _ := helpers.RunQueryJSON(t, dir, "query", "--search", "xyzzy-does-not-exist-abc123")

	if resp.Vocabulary == nil {
		t.Fatal("no_match response must include vocabulary hint")
	}
	if resp.Vocabulary.TotalSkills == 0 {
		t.Error("vocabulary total_skills should be > 0")
	}
}

func TestSearch_NoMatchEchoesSearchTerm(t *testing.T) {
	dir := helpers.CopyFixture(t, "monorepo-pnpm")
	helpers.Run(t, dir, "refresh")

	resp, _ := helpers.RunQueryJSON(t, dir, "query", "--search", "xyzzy-does-not-exist-abc123")

	if resp.Query == nil {
		t.Fatal("no_match must include query echo")
	}
	if resp.Query.Search != "xyzzy-does-not-exist-abc123" {
		t.Errorf("query echo search: got %q, want %q", resp.Query.Search, "xyzzy-does-not-exist-abc123")
	}
}

// --- format defaulting ---

func TestSearch_DefaultsToSummaryFormat(t *testing.T) {
	dir := helpers.CopyFixture(t, "monorepo-pnpm")
	helpers.Run(t, dir, "refresh")

	// No --format flag: search should default to summary (no content).
	resp, _ := helpers.RunQueryJSON(t, dir, "query", "--search", "ARIA")

	if resp.Type != "results" {
		t.Fatalf("expected results, got %q", resp.Type)
	}
	for _, s := range resp.Results {
		if s.Content != "" {
			t.Errorf("search without --format should default to summary (no content); skill %s has content", s.Path)
		}
	}
}

func TestSearch_ExplicitContentFormatReturnsContent(t *testing.T) {
	dir := helpers.CopyFixture(t, "monorepo-pnpm")
	helpers.Run(t, dir, "refresh")

	resp, _ := helpers.RunQueryJSON(t, dir, "query", "--search", "ARIA", "--format", "content")

	if resp.Type != "results" {
		t.Fatalf("expected results, got %q", resp.Type)
	}
	hasContent := false
	for _, s := range resp.Results {
		if s.Content != "" {
			hasContent = true
			break
		}
	}
	if !hasContent {
		t.Error("--format content should include skill content even when --search is used")
	}
}

// --- result fields ---

func TestSearch_ResultIncludesNameAndDescription(t *testing.T) {
	dir := helpers.CopyFixture(t, "monorepo-pnpm")
	helpers.Run(t, dir, "refresh")

	resp, _ := helpers.RunQueryJSON(t, dir, "query", "--search", "Button Accessibility", "--format", "summary")

	if resp.Type != "results" {
		t.Fatalf("expected results, got %q", resp.Type)
	}
	for _, s := range resp.Results {
		if strings.HasSuffix(s.Path, "button-accessibility.md") {
			if s.Name != "Button Accessibility" {
				t.Errorf("result name: got %q, want %q", s.Name, "Button Accessibility")
			}
			if !strings.Contains(s.Description, "ARIA") {
				t.Errorf("result description should contain 'ARIA', got %q", s.Description)
			}
			return
		}
	}
	t.Error("button-accessibility.md not found in results")
}

// --- intersection with other filters ---

func TestSearch_IntersectsWithTopic(t *testing.T) {
	dir := helpers.CopyFixture(t, "monorepo-pnpm")
	helpers.Run(t, dir, "refresh")

	// "components" alone would return button-accessibility.md (name match) plus components.md.
	// Adding --topic components constrains to only skills that ALSO have that topic.
	// button-accessibility.md has no topics so it must be excluded.
	resp, _ := helpers.RunQueryJSON(t, dir, "query",
		"--search", "components",
		"--topic", "components",
		"--format", "summary")

	if resp.Type != "results" {
		t.Fatalf("expected results, got %q", resp.Type)
	}
	helpers.AssertSkillAbsent(t, resp.Results, "button-accessibility.md")
	helpers.AssertSkillPresent(t, resp.Results, "components.md")
}

func TestSearch_IntersectNoMatchReturnsNoMatch(t *testing.T) {
	dir := helpers.CopyFixture(t, "monorepo-pnpm")
	helpers.Run(t, dir, "refresh")

	// "ARIA" only matches button-accessibility.md, which has no topics.
	// Adding --topic migration makes the intersection empty → no_match.
	resp, _ := helpers.RunQueryJSON(t, dir, "query",
		"--search", "ARIA",
		"--topic", "migration")

	if resp.Type != "no_match" {
		t.Fatalf("expected no_match for impossible intersection, got %q (ATH-172 regression)", resp.Type)
	}
	if len(resp.Results) > 0 {
		t.Errorf("no_match must have 0 results, got %d", len(resp.Results))
	}
}

// --- MCP search ---

func TestMCP_QueryToolSearch(t *testing.T) {
	dir := helpers.CopyFixture(t, "monorepo-pnpm")
	helpers.Run(t, dir, "refresh")

	client := helpers.StartMCPServer(t, dir)
	defer client.Close()

	text, err := client.CallToolText("skillex_query", map[string]interface{}{
		"search": "Button Accessibility",
	})
	if err != nil {
		t.Fatalf("skillex_query search failed: %v", err)
	}
	if !strings.Contains(text, "button-accessibility") {
		t.Errorf("expected button-accessibility in search result, got: %q", truncate(text, 300))
	}
}

func TestMCP_QueryToolSearchNoMatch(t *testing.T) {
	dir := helpers.CopyFixture(t, "monorepo-pnpm")
	helpers.Run(t, dir, "refresh")

	client := helpers.StartMCPServer(t, dir)
	defer client.Close()

	text, err := client.CallToolText("skillex_query", map[string]interface{}{
		"search": "xyzzy-does-not-exist-abc123",
	})
	if err != nil {
		t.Fatalf("skillex_query search no-match failed: %v", err)
	}
	// ATH-172: must return no_match JSON, not all skill content.
	if !strings.Contains(text, `"no_match"`) {
		t.Errorf("MCP search with no match should return no_match type, got: %q", truncate(text, 300))
	}
}

func TestMCP_QueryToolSearchMultiToken(t *testing.T) {
	dir := helpers.CopyFixture(t, "monorepo-pnpm")
	helpers.Run(t, dir, "refresh")

	client := helpers.StartMCPServer(t, dir)
	defer client.Close()

	text, err := client.CallToolText("skillex_query", map[string]interface{}{
		"search": "ARIA components",
		"format": "summary",
	})
	if err != nil {
		t.Fatalf("skillex_query multi-token search failed: %v", err)
	}
	// Both button-accessibility.md (ARIA) and components.md (components) should appear.
	if !strings.Contains(text, "button-accessibility") {
		t.Errorf("expected button-accessibility in multi-token result, got: %q", truncate(text, 300))
	}
	if !strings.Contains(text, "components") {
		t.Errorf("expected components in multi-token result, got: %q", truncate(text, 300))
	}
}
