package acceptance

import (
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/atheory-ai/skillex/internal/config"
	"github.com/atheory-ai/skillex/internal/query"
	"github.com/atheory-ai/skillex/test/helpers"
)

const taskSkillBody = "# Handoff\n\nReport the checks you ran and anything still unverified.\n"

func TestGoldenTaskBindings(t *testing.T) {
	t.Setenv("SKILLEX_NO_UPDATE_CHECK", "1")
	for _, format := range []string{"yaml", "json"} {
		t.Run(format, func(t *testing.T) {
			dir := helpers.CopyGoldenFixture(t, "task-bindings")
			if format == "json" {
				cfg, err := config.Load(dir)
				if err != nil {
					t.Fatal(err)
				}
				cfg.Version = config.SkillsOnlyConfigVersion
				data, err := json.Marshal(cfg)
				if err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(filepath.Join(dir, config.JSONFilename), data, 0o644); err != nil {
					t.Fatal(err)
				}
				if err := os.Remove(filepath.Join(dir, config.YAMLFilename)); err != nil {
					t.Fatal(err)
				}
			}
			expected := func(file string) string {
				return filepath.Join(dir, "expected", file)
			}
			cli := func(args ...string) []byte {
				t.Helper()
				result := helpers.Run(t, dir, args...)
				if result.ExitCode != 0 {
					t.Fatalf("skillex %v failed: %s", args, result.Stderr)
				}
				return []byte(result.Stdout)
			}

			bridges := []string{"CLAUDE.md", "GEMINI.md"}
			for _, name := range bridges {
				if err := os.WriteFile(filepath.Join(dir, name), []byte("# Project instructions\n"), 0o644); err != nil {
					t.Fatal(err)
				}
			}
			cli("init", "--yes")
			cli("test", "validate", "--check")
			for _, name := range bridges {
				bridge, err := os.ReadFile(filepath.Join(dir, name))
				if err != nil {
					t.Fatal(err)
				}
				if !strings.Contains(string(bridge), "@AGENTS.md") || !strings.Contains(string(bridge), "# Project instructions") {
					t.Fatalf("expected %s to import the shared setup and preserve project instructions: %s", name, bridge)
				}
			}
			agents, err := os.ReadFile(filepath.Join(dir, "AGENTS.md"))
			if err != nil {
				t.Fatal(err)
			}
			if !strings.Contains(string(agents), "with no filters if Skillex setup context is missing") ||
				!strings.Contains(string(agents), "use `skillex_read` for returned refs") ||
				strings.Contains(string(agents), "You are about to create a Git commit.") {
				t.Fatalf("expected setup guidance without embedded task data: %s", agents)
			}

			if !strings.Contains(string(agents), `query --search "<concepts>" --json`) {
				t.Fatalf("missing structured lookup in generated CLI instructions")
			}
			var matches query.Response
			if err := json.Unmarshal(cli("query", "--search", "staged", "--json"), &matches); err != nil {
				t.Fatal(err)
			}
			if len(matches.Results) != 1 {
				t.Fatalf("expected one staged-diff skill: %+v", matches)
			}
			if body := cli("read", "--ref", matches.Results[0].Ref); !strings.Contains(string(body), "Review the staged diff") {
				t.Fatalf("documented CLI lookup failed to return the skill: %s", body)
			}

			client := helpers.StartMCPServer(t, dir)
			defer client.Close()
			if !strings.Contains(string(client.Initialization), "The setup response includes configured tasks") ||
				!strings.Contains(string(client.Initialization), "with its returned ref") {
				t.Fatalf("missing MCP startup instructions: %s", client.Initialization)
			}
			mcp := func(tool string, args map[string]any) []byte {
				t.Helper()
				body, err := client.CallToolText(tool, args)
				if err != nil {
					t.Fatal(err)
				}
				return []byte(body)
			}

			setup := cli("query", "--json")
			assertNormalizedJSONGolden(t, expected("setup.json"), setup)
			assertNormalizedJSONGolden(t, expected("setup.json"), mcp("skillex_query", map[string]any{}))
			assertGoldenBytes(t, expected("setup-human.txt"), cli("query"))

			var discovered query.Response
			if err := json.Unmarshal(setup, &discovered); err != nil {
				t.Fatal(err)
			}
			for _, task := range discovered.Tasks {
				snapshot := expected("read-" + task.Name + ".json")
				assertNormalizedJSONGolden(t, snapshot, cli("read", "--ref", task.Ref, "--json"))
				assertNormalizedJSONGolden(t, snapshot, mcp("skillex_read", map[string]any{"ref": task.Ref}))
			}

			assertNormalizedJSONGolden(t, expected("query-scoped.json"), cli("query", "--path", "src/main.go", "--json"))
			assertNormalizedJSONGolden(t, expected("query-scoped.json"), mcp("skillex_query", map[string]any{"path": "src/main.go"}))
			assertNormalizedJSONGolden(t, expected("query-no-match.json"), cli("query", "--topic", "missing", "--json"))
			assertNormalizedJSONGolden(t, expected("query-no-match.json"), mcp("skillex_query", map[string]any{"topic": "missing"}))
		})
	}
}

func TestTaskBindings_MissingSkillPreservesIndex(t *testing.T) {
	dir := t.TempDir()
	const valid = `{"Version":4,"Tasks":[{"Name":"handoff","When":"Before handing back a code change.","Skill":"skills/handoff.md"}]}`
	writeTaskFixture(t, dir, "skillex.json", valid)
	if result := helpers.Run(t, dir, "refresh"); result.ExitCode != 0 {
		t.Fatalf("initial refresh failed: %s", result.Stderr)
	}
	var setup query.Response
	helpers.RunJSON(t, dir, &setup, "query")
	if len(setup.Tasks) != 1 {
		t.Fatalf("expected a task ref: %+v", setup)
	}
	if err := os.Remove(filepath.Join(dir, "skills", "handoff.md")); err != nil {
		t.Fatal(err)
	}
	result := helpers.Run(t, dir, "refresh")
	if result.ExitCode == 0 || !strings.Contains(result.Stderr, `task "handoff" skill "skills/handoff.md"`) {
		t.Fatalf("expected a task skill error: %+v", result)
	}
	var read query.ReadResponse
	helpers.RunJSON(t, dir, &read, "read", "--ref", setup.Tasks[0].Ref)
	if read.Content != taskSkillBody {
		t.Fatalf("refresh failed to preserve the original index: %+v", read)
	}
}

func TestTaskBindings_RejectsSkillOutsideProject(t *testing.T) {
	dir := t.TempDir()
	outside := t.TempDir()
	const config = `{"Version":4,"Tasks":[{"Name":"handoff","When":"Before handing back a code change.","Skill":"linked/handoff.md"}]}`
	writeTaskFixture(t, outside, "skillex.json", config)
	if err := os.WriteFile(filepath.Join(dir, "skillex.json"), []byte(config), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(outside, "skills"), filepath.Join(dir, "linked")); err != nil {
		if runtime.GOOS == "windows" {
			t.Skipf("creating symlink: %v", err)
		}
		t.Fatal(err)
	}
	result := helpers.Run(t, dir, "refresh")
	if result.ExitCode == 0 || !strings.Contains(result.Stderr, `task "handoff" skill "linked/handoff.md"`) {
		t.Fatalf("expected outside skill to be rejected: %+v", result)
	}
	overlap := strings.Replace(config, `"Version":4`, `"Version":4,"Rules":[{"Scope":"**","Skills":["linked/handoff.md"]}]`, 1)
	if err := os.WriteFile(filepath.Join(dir, "skillex.json"), []byte(overlap), 0o644); err != nil {
		t.Fatal(err)
	}
	result = helpers.Run(t, dir, "refresh")
	if result.ExitCode == 0 || !strings.Contains(result.Stderr, `task "handoff" skill "linked/handoff.md"`) {
		t.Fatalf("overlapping rule changed task containment: %+v", result)
	}
}

func writeTaskFixture(t *testing.T, dir, configFile, config string) {
	t.Helper()
	if err := os.Mkdir(filepath.Join(dir, "skills"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "skills", "handoff.md"), []byte(taskSkillBody), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, configFile), []byte(config), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestTaskOnlyValidation(t *testing.T) {
	dir := t.TempDir()
	writeTaskFixture(t, dir, "skillex.json", `{"Version":4,"Tasks":[{"Name":"handoff","When":"At handoff.","Skill":"policies/handoff.md"}]}`)
	policies := filepath.Join(dir, "policies")
	if err := os.Rename(filepath.Join(dir, "skills"), policies); err != nil {
		t.Fatal(err)
	}
	for name, body := range map[string]string{"handoff.test.md": "# Broken header", "unrelated.test.md": "# Broken neighbor"} {
		if err := os.WriteFile(filepath.Join(policies, name), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if res := helpers.Run(t, dir, "refresh"); res.ExitCode != 0 {
		t.Fatal(res.Stderr)
	}
	for _, args := range [][]string{{"test", "validate", "--check"}, {"test", "validate", "--check", "--json"}, {"doctor"}} {
		res := helpers.Run(t, dir, args...)
		out := res.Stderr + res.Stdout
		if res.ExitCode == 0 || !strings.Contains(out, "policies/handoff.test.md") || strings.Contains(out, "unrelated.test.md") {
			t.Fatalf("%v: %+v", args, res)
		}
	}
}
