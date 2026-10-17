package acceptance

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/atheory-ai/skillex/test/helpers"
)

func TestMCP_GoldenProjectSkillWatch(t *testing.T) {
	dir := helpers.CopyGoldenFixture(t, "go-basic")
	writeProjectFile(t, dir, ".cursor/skills/land-it/SKILL.md", agentSkillMarkdown("land-it", "Ship the branch."))

	if res := helpers.Run(t, dir, "refresh"); res.ExitCode != 0 {
		t.Fatalf("refresh failed: %s", res.Stderr)
	}
	if !queryHasDescription(t, dir, "land-it", "Ship the branch.") {
		t.Fatal("refresh did not index the golden project's Cursor skill")
	}

	writeProjectFile(t, dir, ".cursor/skills/land-it/SKILL.md", agentSkillMarkdown("land-it", "Ship it twice."))
	if queryHasDescription(t, dir, "land-it", "Ship it twice.") {
		t.Fatal("CLI query updated a skill without refresh")
	}

	client := helpers.StartMCPServer(t, dir)
	waitMCPSkill(t, client, "land-it", "Ship it twice.")

	writeProjectFile(t, dir, ".cursor/skills/review/SKILL.md", agentSkillMarkdown("review", "Review the pull request."))
	waitMCPSkill(t, client, "review", "Review the pull request.")

	writeProjectFile(t, dir, "apps/web/.claude/skills/deploy-web/SKILL.md", agentSkillMarkdown("deploy-web", "Deploy the web app."))
	time.Sleep(700 * time.Millisecond)
	if mcpHasSkill(t, client, "deploy-web", "Deploy the web app.") {
		t.Fatal("MCP indexed a new skill root without refresh")
	}

	if res := helpers.Run(t, dir, "refresh"); res.ExitCode != 0 {
		t.Fatalf("refresh of the new root failed: %s", res.Stderr)
	}
	waitMCPSkill(t, client, "deploy-web", "Deploy the web app.")
	waitMCPSkill(t, client, "land-it", "Ship it twice.")
	if !queryHasPath(t, dir, "modules.md") {
		t.Fatal("golden module skill disappeared")
	}
}

func queryHasDescription(t *testing.T, dir, name, description string) bool {
	t.Helper()
	resp, result := helpers.RunQueryJSON(t, dir, "query", "--search", name, "--limit", "20")
	if result.ExitCode != 0 {
		t.Fatalf("query failed: %s", result.Stderr)
	}
	for _, skill := range resp.Results {
		if skill.Name == name && (description == "" || strings.Contains(skill.Description, description)) {
			return true
		}
	}
	return false
}

func queryHasPath(t *testing.T, dir, suffix string) bool {
	t.Helper()
	resp, result := helpers.RunQueryJSON(t, dir, "query", "--search", "modules", "--limit", "20")
	if result.ExitCode != 0 {
		t.Fatalf("query failed: %s", result.Stderr)
	}
	for _, skill := range resp.Results {
		if strings.HasSuffix(skill.Path, suffix) {
			return true
		}
	}
	return false
}

func waitMCPSkill(t *testing.T, client *helpers.MCPClient, name, description string) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if mcpHasSkill(t, client, name, description) {
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %s (%s)", name, description)
}

func mcpHasSkill(t *testing.T, client *helpers.MCPClient, name, description string) bool {
	t.Helper()
	text, err := client.CallToolText("skillex_query", map[string]interface{}{
		"search": name,
		"limit":  20,
	})
	if err != nil {
		t.Fatalf("skillex_query: %v", err)
	}
	var resp helpers.QueryResponse
	if err := json.Unmarshal([]byte(text), &resp); err != nil {
		t.Fatalf("parsing query: %v\n%s", err, text)
	}
	for _, skill := range resp.Results {
		if skill.Name == name && strings.Contains(skill.Description, description) {
			return true
		}
	}
	return false
}
