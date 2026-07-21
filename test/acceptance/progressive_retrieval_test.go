package acceptance

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/atheory-ai/skillex/test/helpers"
)

func TestProgressive_DefaultDiscoveryIsBoundedSummary(t *testing.T) {
	dir := helpers.CopyFixture(t, "monorepo-pnpm")
	helpers.Run(t, dir, "refresh")
	resp, result := helpers.RunQueryJSON(t, dir, "query", "--path", "packages/app-a/src/auth.ts", "--limit", "2")
	if result.ExitCode != 0 || resp.MatchCount <= 2 || !resp.TooBroad || resp.NextCursor == "" {
		t.Fatalf("expected bounded broad discovery, got %#v", resp)
	}
	if len(result.Stdout) > 16*1024 {
		t.Fatalf("discovery response exceeded budget: %d bytes", len(result.Stdout))
	}
	for _, skill := range resp.Results {
		if skill.Content != "" || skill.Ref == "" || skill.ContentBytes == 0 {
			t.Fatalf("discovery leaked content or omitted selection metadata: %#v", skill)
		}
	}
	if resp.NarrowWith == nil {
		t.Fatal("broad discovery must offer narrowing facets")
	}
}

func TestProgressive_BodySearchThenBoundedSectionRead(t *testing.T) {
	dir := helpers.CopyFixture(t, "monorepo-pnpm")
	helpers.Run(t, dir, "refresh")
	resp, _ := helpers.RunQueryJSON(t, dir, "query", "--search", "keyboard")
	if len(resp.Results) == 0 || resp.Results[0].Ref == "" || !strings.Contains(strings.Join(resp.Results[0].MatchedIn, ","), "body") {
		t.Fatalf("body search did not expose selectable ranked result: %#v", resp.Results)
	}
	var read struct {
		Content   string `json:"content"`
		Truncated bool   `json:"truncated"`
		Section   *struct {
			ID string `json:"id"`
		} `json:"section"`
	}
	result := helpers.RunJSON(t, dir, &read, "read", "--ref", resp.Results[0].Ref, "--section", "keyboard-navigation", "--max-bytes", "120")
	if result.ExitCode != 0 || read.Section == nil || read.Section.ID != "keyboard-navigation" || !strings.Contains(read.Content, "Keyboard navigation") || len(read.Content) > 120 {
		t.Fatalf("unexpected bounded section read: %#v", read)
	}
	// Ensure the tool response stays valid JSON even when it truncates.
	if !json.Valid([]byte(result.Stdout)) {
		t.Fatalf("invalid read json: %s", result.Stdout)
	}
}

func TestProgressive_MCPDiscoveryProvidesRefForBoundedRead(t *testing.T) {
	dir := helpers.CopyFixture(t, "monorepo-pnpm")
	helpers.Run(t, dir, "refresh")
	client := helpers.StartMCPServer(t, dir)
	text, err := client.CallToolText("skillex_query", map[string]interface{}{"search": "keyboard"})
	if err != nil {
		t.Fatal(err)
	}
	var discovered helpers.QueryResponse
	if err := json.Unmarshal([]byte(text), &discovered); err != nil || len(discovered.Results) == 0 || discovered.Results[0].Ref == "" {
		t.Fatalf("MCP discovery must return structured selectable results: %v; %s", err, text)
	}
	read, err := client.CallToolText("skillex_read", map[string]interface{}{"ref": discovered.Results[0].Ref, "section": "keyboard-navigation", "max_bytes": 120})
	if err != nil || len(read) > 1200 || !strings.Contains(read, "keyboard-navigation") {
		t.Fatalf("unexpected MCP bounded read: %v; %s", err, read)
	}
}
