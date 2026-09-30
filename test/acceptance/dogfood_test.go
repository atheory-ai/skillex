package acceptance

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"

	"github.com/atheory-ai/skillex/test/helpers"
)

// Exercise the actual repository knowledge from a clean index, without touching
// the checkout's generated state or requiring installed fixture dependencies.
func TestDogfood_RepositoryKnowledge(t *testing.T) {
	_, file, _, _ := runtime.Caller(0)
	root := filepath.Join(filepath.Dir(file), "..", "..")
	dir := t.TempDir()
	for _, name := range []string{"skillex.yaml", "AGENTS.md", "skills"} {
		err := filepath.WalkDir(filepath.Join(root, name), func(path string, entry os.DirEntry, err error) error {
			if err != nil {
				return err
			}
			rel, err := filepath.Rel(root, path)
			if err != nil {
				return err
			}
			target := filepath.Join(dir, rel)
			if entry.IsDir() {
				return os.MkdirAll(target, 0o755)
			}
			data, err := os.ReadFile(path)
			if err != nil {
				return err
			}
			return os.WriteFile(target, data, 0o644)
		})
		if err != nil {
			t.Fatal(err)
		}
	}
	for _, args := range [][]string{{"init", "--yes"}, {"test", "validate", "--check"}} {
		if result := helpers.Run(t, dir, args...); result.ExitCode != 0 {
			t.Fatalf("%v failed: %s", args, result.Stderr)
		}
	}
	agentsPath := filepath.Join(dir, "AGENTS.md")
	agents, err := os.ReadFile(agentsPath)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(agents), "make dev-binary") || !strings.Contains(string(agents), "skillex_read") {
		t.Fatal("init must retain contributor instructions and generate bounded retrieval guidance")
	}
	if result := helpers.Run(t, dir, "refresh"); result.ExitCode != 0 {
		t.Fatal(result.Stderr)
	}
	after, err := os.ReadFile(agentsPath)
	if err != nil || !bytes.Equal(agents, after) {
		t.Fatalf("refresh changed agent instructions: %v", err)
	}
	vocab, _ := helpers.RunQueryJSON(t, dir, "query")
	if vocab.Type != "vocabulary" || len(vocab.Results) != 0 {
		t.Fatalf("unfiltered discovery must return vocabulary: %#v", vocab)
	}
	for _, topic := range []string{"repo-conventions", "registry", "migration", "testing", "mcp", "releases"} {
		helpers.AssertTopicInVocab(t, vocab.Vocabulary, topic)
	}
	client := helpers.StartMCPServer(t, dir)
	defer client.Close()
	for _, tc := range []struct{ flag, value, skill string }{
		{"path", "internal/registry/migrate.go", "skills/registry-and-migrations.md"},
		{"path", "mcp/server.go", "skills/retrieval-contract.md"},
		{"path", "npm/skillex/bin/skillex.js", "skills/release-and-recovery.md"},
		{"topic", "testing", "skills/testing-and-golden-harness.md"},
	} {
		t.Run(tc.value, func(t *testing.T) {
			cli, result := helpers.RunQueryJSON(t, dir, "query", "--"+tc.flag, tc.value)
			if result.ExitCode != 0 {
				t.Fatal(result.Stderr)
			}
			found, repo := false, false
			for _, skill := range cli.Results {
				found = found || skill.Path == tc.skill
				repo = repo || skill.Path == "skills/repo.md"
				if skill.Ref == "" || skill.Content != "" {
					t.Fatalf("discovery must expose refs without content: %#v", skill)
				}
			}
			if !found || (tc.flag == "path" && !repo) {
				t.Fatalf("missing scoped knowledge: %#v", cli.Results)
			}
			text, err := client.CallToolText("skillex_query", map[string]interface{}{tc.flag: tc.value})
			var mcp helpers.QueryResponse
			if err != nil || json.Unmarshal([]byte(text), &mcp) != nil || !reflect.DeepEqual(cli.Results, mcp.Results) {
				t.Fatalf("CLI/MCP discovery differs: %v; %s", err, text)
			}
			if tc.skill == "skills/registry-and-migrations.md" {
				for _, skill := range mcp.Results {
					if skill.Path != tc.skill {
						continue
					}
					read, err := client.CallToolText("skillex_read", map[string]interface{}{"ref": skill.Ref, "section": "add-a-migration"})
					if err != nil || !strings.Contains(read, "BEGIN IMMEDIATE") || !strings.Contains(read, "currentSchemaVersion") {
						t.Fatalf("migration onboarding unavailable through selected read: %v; %s", err, read)
					}
					var cliRead, mcpRead struct {
						Content string `json:"content"`
					}
					result := helpers.RunJSON(t, dir, &cliRead, "read", "--ref", skill.Ref, "--section", "add-a-migration")
					if result.ExitCode != 0 || json.Unmarshal([]byte(read), &mcpRead) != nil || cliRead.Content != mcpRead.Content {
						t.Fatalf("CLI/MCP selected reads differ: %s; %s", result.Stdout, read)
					}
				}
			}
		})
	}
}
