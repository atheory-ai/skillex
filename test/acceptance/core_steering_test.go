package acceptance

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/atheory-ai/skillex/internal/query"
	"github.com/atheory-ai/skillex/internal/steering"
	"github.com/atheory-ai/skillex/test/helpers"
)

func TestGoldenCorePresetSelection(t *testing.T) {
	t.Setenv("SKILLEX_NO_UPDATE_CHECK", "1")
	dir := helpers.CopyGoldenFixture(t, "task-bindings")
	configPath := filepath.Join(dir, "skillex.yaml")
	base, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatal(err)
	}
	if result := helpers.Run(t, dir, "refresh"); result.ExitCode != 0 {
		t.Fatalf("refresh failed: %s", result.Stderr)
	}

	cases := []struct {
		name string
		cue  string
	}{
		{"minimal-comments", "Avoid comments that restate code."},
		{"verify-behavior", "Before declaring a change done"},
		{"recheck-assumptions", "If two fixes based on the same assumption fail the same check"},
	}
	for _, preset := range cases {
		t.Run(preset.name, func(t *testing.T) {
			config := string(base) + "Core:\n  Presets: [" + preset.name + "]\n"
			if err := os.WriteFile(configPath, []byte(config), 0o644); err != nil {
				t.Fatal(err)
			}
			ref := "skillex:" + preset.name
			var setup query.Response
			cli := helpers.RunJSON(t, dir, &setup, "query")
			if cli.ExitCode != 0 || setup.Core == nil || len(setup.CoreSkills) != 1 ||
				setup.CoreSkills[0].Name != preset.name || setup.CoreSkills[0].Ref != ref ||
				!strings.Contains(setup.CoreSkills[0].When, preset.cue) {
				t.Fatalf("expected only the selected preset in setup: %s (%s)", cli.Stdout, cli.Stderr)
			}

			client := helpers.StartMCPServer(t, dir)
			defer client.Close()
			initialization := string(client.Initialization)
			for _, candidate := range cases {
				if got, want := strings.Contains(initialization, "skillex:"+candidate.name), candidate.name == preset.name; got != want {
					t.Fatalf("unexpected cue selection in MCP initialization: %s", initialization)
				}
			}
			mcp, err := client.CallToolText("skillex_query", map[string]any{})
			if err != nil {
				t.Fatal(err)
			}
			assertNormalizedJSONEqual(t, []byte(cli.Stdout), []byte(mcp))
		})
	}
}

func TestGoldenCoreSteeringMCPWithoutAgentsFile(t *testing.T) {
	t.Setenv("SKILLEX_NO_UPDATE_CHECK", "1")
	dir := helpers.CopyGoldenFixture(t, "task-bindings")
	configPath := filepath.Join(dir, "skillex.yaml")
	base, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatal(err)
	}
	taskConfig := strings.Replace(string(base), "Skill: skills/open-pr.md", "Skill: skillex:verify-behavior", 1)
	configured := taskConfig + "Core:\n  Presets: [verify-behavior, recheck-assumptions]\n"
	if err := os.WriteFile(configPath, []byte(configured), 0o644); err != nil {
		t.Fatal(err)
	}
	if result := helpers.Run(t, dir, "refresh"); result.ExitCode != 0 {
		t.Fatalf("refresh failed: %s", result.Stderr)
	}
	if _, err := os.Stat(filepath.Join(dir, "AGENTS.md")); !os.IsNotExist(err) {
		t.Fatalf("test requires a project without AGENTS.md, got %v", err)
	}

	client := helpers.StartMCPServer(t, dir)
	defer client.Close()
	golden := func(name string, body []byte) {
		t.Helper()
		assertNormalizedJSONGolden(t, filepath.Join(dir, "expected", "core-"+name+".json"), body)
	}
	call := func(tool string, args map[string]any) []byte {
		t.Helper()
		body, err := client.CallToolText(tool, args)
		if err != nil {
			t.Fatal(err)
		}
		return []byte(body)
	}

	setup := call("skillex_query", map[string]any{})
	golden("setup", setup)
	var result query.Response
	if err := json.Unmarshal(setup, &result); err != nil {
		t.Fatal(err)
	}
	if result.Core == nil || result.Core.Ref != "core:steering" || result.Core.Revision == "" ||
		len(result.CoreSkills) != 2 || len(result.Tasks) != 2 {
		t.Fatalf("expected the core marker and task bindings at setup: %s", setup)
	}
	marker := *result.Core
	const verifyRef = "skillex:verify-behavior"
	const recheckRef = "skillex:recheck-assumptions"
	const guidanceText = "Before declaring a change done, verify its behavior through the normal entry point."
	const detailText = "Choose a check that exercises the same entry point"
	if result.CoreSkills[0].Ref != verifyRef || result.CoreSkills[1].Ref != recheckRef || result.Tasks[1].Ref != verifyRef {
		t.Fatalf("expected the enabled pointers and shared task ref: %s", setup)
	}
	initialization := string(client.Initialization)
	if !strings.Contains(initialization, marker.Revision) || !strings.Contains(initialization, guidanceText) ||
		!strings.Contains(initialization, "use `narrow_with` to refine the query") ||
		strings.Contains(initialization, detailText) || strings.Contains(initialization, "minimal-comments") {
		t.Fatalf("MCP initialization is missing the baseline or enabled guidance: %s", initialization)
	}
	if strings.Contains(string(setup), detailText) || strings.Contains(string(setup), "minimal-comments") {
		t.Fatalf("setup included skill content or a disabled cue: %s", setup)
	}
	golden("setup", []byte(helpers.Run(t, dir, "query", "--json").Stdout))

	assertCoreMarker := func(body []byte) {
		t.Helper()
		var envelope struct {
			Core *steering.Marker `json:"core"`
		}
		if err := json.Unmarshal(body, &envelope); err != nil {
			t.Fatal(err)
		}
		if envelope.Core == nil || *envelope.Core != marker || strings.Contains(string(body), guidanceText) ||
			strings.Contains(string(body), "core_skills") {
			t.Fatalf("expected only the core marker: %s", body)
		}
	}
	assertCoreMarker(call("skillex_query", map[string]any{"path": "src/main.go"}))
	assertCoreMarker(call("skillex_query", map[string]any{"topic": "missing"}))
	assertCoreMarker(call("skillex_read", map[string]any{"ref": result.Tasks[0].Ref}))

	var cliRead query.ReadResponse
	golden("recovery", []byte(helpers.RunJSON(t, dir, &cliRead, "read", "--ref", marker.Ref).Stdout))
	mcpRead := call("skillex_read", map[string]any{"ref": marker.Ref})
	golden("recovery", mcpRead)
	var recovered query.ReadResponse
	if err := json.Unmarshal(mcpRead, &recovered); err != nil {
		t.Fatal(err)
	}
	if recovered.Core == nil || *recovered.Core != marker || recovered.Content != cliRead.Content ||
		!strings.Contains(recovered.Content, guidanceText) || strings.Contains(recovered.Content, detailText) {
		t.Fatalf("CLI and MCP should recover the same core text and revision: %s", mcpRead)
	}
	var bounded query.ReadResponse
	helpers.RunJSON(t, dir, &bounded, "read", "--ref", marker.Ref, "--max-bytes", "12")
	if !bounded.Truncated || len(bounded.Content) != 12 {
		t.Fatalf("core read must honor the byte budget: %+v", bounded)
	}

	var catalog query.Response
	cliCatalog := helpers.RunJSON(t, dir, &catalog, "query", "--source", "skillex")
	golden("catalog", []byte(cliCatalog.Stdout))
	golden("catalog", call("skillex_query", map[string]any{"source": "skillex"}))
	if len(catalog.Results) != 3 || catalog.Results[0].Ref != "skillex:minimal-comments" || len(catalog.CoreSkills) != 0 {
		t.Fatalf("expected all three bundled summaries, including the disabled preset: %s", cliCatalog.Stdout)
	}
	for _, skill := range catalog.Results {
		if skill.SourceType != "skillex" || skill.Content != "" {
			t.Fatalf("expected a bundled summary: %+v", skill)
		}
	}
	var found query.Response
	helpers.RunJSON(t, dir, &found, "query", "--source", "skillex", "--topic", "verification", "--search", "verify")
	if len(found.Results) != 1 || found.Results[0].Ref != verifyRef {
		t.Fatalf("catalog filters should intersect: %+v", found)
	}
	var unrelated query.Response
	helpers.RunJSON(t, dir, &unrelated, "query", "--search", "recheck")
	if unrelated.Type != query.ResponseTypeNoMatch {
		t.Fatalf("ordinary lookup should omit bundled skills: %+v", unrelated)
	}
	golden("verification-skill", call("skillex_read", map[string]any{"ref": verifyRef}))
	golden("verification-skill", []byte(helpers.Run(t, dir, "read", "--ref", verifyRef, "--json").Stdout))
	if read := call("skillex_read", map[string]any{"ref": recheckRef}); !strings.Contains(string(read), "what both fixes assumed") {
		t.Fatalf("expected the detailed debugging skill by ref: %s", read)
	}

	if err := os.WriteFile(configPath, []byte(taskConfig), 0o644); err != nil {
		t.Fatal(err)
	}
	var disabled query.Response
	golden("disabled", []byte(helpers.RunJSON(t, dir, &disabled, "query").Stdout))
	if disabled.Core != nil || len(disabled.CoreSkills) != 0 || len(disabled.Tasks) != 2 || disabled.Tasks[1].Ref != verifyRef {
		t.Fatalf("expected the bundled task skill to remain bound with steering disabled: %+v", disabled)
	}
	if read := helpers.Run(t, dir, "read", "--ref", marker.Ref); read.ExitCode == 0 || !strings.Contains(read.Stderr, "core steering is disabled") {
		t.Fatalf("expected the old ref to be unavailable when disabled: %+v", read)
	}
	disabledMCP := helpers.StartMCPServer(t, dir)
	defer disabledMCP.Close()
	if strings.Contains(string(disabledMCP.Initialization), guidanceText) {
		t.Fatalf("disabled MCP returned core steering: %s", disabledMCP.Initialization)
	}
	disabledSetup, err := disabledMCP.CallToolText("skillex_query", map[string]any{})
	if err != nil {
		t.Fatal(err)
	}
	golden("disabled", []byte(disabledSetup))
	var standalone query.ReadResponse
	helpers.RunJSON(t, dir, &standalone, "read", "--ref", verifyRef)
	if standalone.Core != nil || !strings.Contains(standalone.Content, detailText) {
		t.Fatalf("expected to read the bound skill with core disabled: %+v", standalone)
	}
}

func TestCoreIndexUpgrade(t *testing.T) {
	dir := t.TempDir()
	writeTaskFixture(t, dir, "skillex.json", `{"Version":4,"Core":{"Presets":["verify-behavior"]}}`)
	refresh := func() {
		t.Helper()
		if res := helpers.Run(t, dir, "refresh"); res.ExitCode != 0 {
			t.Fatal(res.Stderr)
		}
	}
	refresh()
	db := helpers.OpenRegistry(t, dir)
	for _, change := range []string{
		"DELETE FROM skills WHERE source_type='skillex'",
		"UPDATE skills SET content='Previous release' WHERE path='skillex:verify-behavior'",
	} {
		if _, err := db.Exec(change); err != nil {
			t.Fatal(err)
		}
		client := helpers.StartMCPServer(t, dir)
		if !strings.Contains(string(client.Initialization), "skillex refresh") || strings.Contains(string(client.Initialization), "skillex:verify-behavior") {
			t.Fatalf("invalid startup guidance: %s", client.Initialization)
		}
		for _, args := range [][]string{{"query"}, {"query", "--source", "skillex", "--search", "missing"}} {
			res := helpers.Run(t, dir, args...)
			if res.ExitCode == 0 || !strings.Contains(res.Stderr, "skillex refresh") {
				t.Fatalf("%v: %+v", args, res)
			}
		}
		for _, args := range []map[string]any{{}, {"source": "skillex"}} {
			raw, err := client.CallTool("skillex_query", args)
			if err != nil || !strings.Contains(string(raw), "skillex refresh") {
				t.Fatalf("MCP query: %s, %v", raw, err)
			}
		}
		refresh()
		if body, err := client.CallToolText("skillex_query", map[string]any{}); err != nil || !strings.Contains(body, "skillex:verify-behavior") {
			t.Fatalf("recovery: %s, %v", body, err)
		}
		client.Close()
	}
}

func TestReadWithInvalidPreset(t *testing.T) {
	dir := helpers.CopyGoldenFixture(t, "task-bindings")
	if res := helpers.Run(t, dir, "refresh"); res.ExitCode != 0 {
		t.Fatal(res.Stderr)
	}
	var setup query.Response
	helpers.RunJSON(t, dir, &setup, "query")
	if len(setup.Tasks) == 0 {
		t.Fatal("expected task ref")
	}
	if err := os.WriteFile(filepath.Join(dir, "skillex.yaml"), []byte("Version: 5\nCore:\n  Presets: [typo]\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	var read query.ReadResponse
	res := helpers.RunJSON(t, dir, &read, "read", "--ref", setup.Tasks[0].Ref)
	if res.ExitCode != 0 || read.Content == "" || !strings.Contains(res.Stderr, `unknown core preset "typo"`) {
		t.Fatalf("indexed read: %+v", res)
	}
	res = helpers.Run(t, dir, "read", "--ref", "core:steering")
	if res.ExitCode == 0 || !strings.Contains(res.Stderr, `unknown core preset "typo"`) {
		t.Fatalf("core read: %+v", res)
	}
}
