package acceptance

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/atheory-ai/skillex/test/helpers"
)

func TestUpdateNoticesCLIAndMCP(t *testing.T) {
	// A release build exercises the real entry points; the regular development
	// binary intentionally suppresses release checks. All metadata is seeded and
	// offline mode makes this test independent of public release services.
	name := "skillex"
	if runtime.GOOS == "windows" {
		name += ".exe"
	}
	binary := filepath.Join(t.TempDir(), name)
	build := exec.Command("go", "build", "-ldflags", "-X github.com/atheory-ai/skillex/cli.Version=0.10.0", "-o", binary, "./cmd/skillex")
	build.Dir = filepath.Join("..", "..")
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("release fixture build: %v\n%s", err, out)
	}
	t.Setenv("SKILLEX_BINARY", binary)
	t.Setenv("SKILLEX_OFFLINE", "1")
	t.Setenv("SKILLEX_NO_UPDATE_CHECK", "0")
	t.Setenv("CI", "false")
	t.Setenv("NO_UPDATE_NOTIFIER", "")
	os.Unsetenv("NO_UPDATE_NOTIFIER")
	dir := helpers.CopyFixture(t, "monorepo-pnpm")
	cacheDir := t.TempDir()
	t.Setenv("SKILLEX_CACHE_DIR", cacheDir)
	if result := helpers.Run(t, dir, "refresh"); result.ExitCode != 0 {
		t.Fatal(result.Stderr)
	}
	seed := func(version string) {
		t.Helper()
		os.MkdirAll(filepath.Join(cacheDir, "updates"), 0o700)
		for _, source := range []string{"github", "npm"} {
			data, _ := json.Marshal(map[string]any{"schema": 1, "source": source, "latest_version": version, "last_success_at": time.Now(), "next_check_at": time.Now().Add(24 * time.Hour)})
			if err := os.WriteFile(filepath.Join(cacheDir, "updates", source+".json"), data, 0o600); err != nil {
				t.Fatal(err)
			}
		}
	}
	seed("0.11.0")
	for _, args := range [][]string{
		{"query", "--search", "keyboard", "--limit", "1"},
		{"query"}, // Vocabulary must carry the same advisory metadata.
		{"query", "--search", "no-such-skill-123456789"},
	} {
		with := helpers.Run(t, dir, append(append([]string{}, args...), "--json", "--quiet")...)
		var payload map[string]any
		if with.ExitCode != 0 || with.Stderr != "" || json.Unmarshal([]byte(with.Stdout), &payload) != nil {
			t.Fatalf("query: %#v", with)
		}
		assertUpdateNotice(t, payload, "0.11.0")
		t.Setenv("SKILLEX_NO_UPDATE_CHECK", "1")
		without := helpers.Run(t, dir, append(append([]string{}, args...), "--json", "--quiet")...)
		t.Setenv("SKILLEX_NO_UPDATE_CHECK", "0")
		var original map[string]any
		json.Unmarshal([]byte(without.Stdout), &original)
		delete(payload, "notices")
		if !reflect.DeepEqual(payload, original) {
			t.Fatalf("notice changed retrieval fields: %s vs %s", with.Stdout, without.Stdout)
		}
	}
	if result := helpers.Run(t, dir, "query", "--search", "keyboard"); !strings.Contains(result.Stderr, "Skillex 0.11.0 is available") {
		t.Fatal("missing human notice", result)
	}
	if result := helpers.Run(t, dir, "version"); strings.Contains(result.Stderr, "available") {
		t.Fatal("plain version emitted notice")
	}
	if result := helpers.Run(t, dir, "read", "--ref", "invalid", "--json"); result.ExitCode == 0 || strings.Contains(result.Stderr, "available") {
		t.Fatal("notice changed error behavior", result)
	}

	client := helpers.StartMCPServer(t, dir)
	var init struct {
		Instructions string `json:"instructions"`
	}
	if err := json.Unmarshal(client.Initialization, &init); err != nil || !strings.Contains(init.Instructions, "Skillex 0.11.0 is available") {
		t.Fatalf("initialize: %v %s", err, client.Initialization)
	}
	queryText, err := client.CallToolText("skillex_query", map[string]any{"search": "keyboard"})
	if err != nil {
		t.Fatal(err)
	}
	var query map[string]any
	json.Unmarshal([]byte(queryText), &query)
	assertUpdateNotice(t, query, "0.11.0")
	ref := query["results"].([]any)[0].(map[string]any)["ref"].(string)
	cliRead := helpers.Run(t, dir, "read", "--ref", ref, "--max-bytes", "120", "--json", "--quiet")
	mcpRead, err := client.CallToolText("skillex_read", map[string]any{"ref": ref, "max_bytes": 120})
	if err != nil || cliRead.ExitCode != 0 {
		t.Fatalf("reads: %v %#v", err, cliRead)
	}
	var cliPayload, mcpPayload map[string]any
	json.Unmarshal([]byte(cliRead.Stdout), &cliPayload)
	json.Unmarshal([]byte(mcpRead), &mcpPayload)
	assertUpdateNotice(t, cliPayload, "0.11.0")
	if !reflect.DeepEqual(cliPayload, mcpPayload) {
		t.Fatal("CLI/MCP read parity lost")
	}
	if len(mcpPayload["content"].(string)) > 120 {
		t.Fatal("notice altered content budget")
	}
	// An existing process must pick up new metadata, and repeated queries must
	// retain notices for later tasks reusing the same MCP connection.
	seed("0.12.0")
	for range 2 {
		text, err := client.CallToolText("skillex_query", map[string]any{})
		if err != nil {
			t.Fatal(err)
		}
		var payload map[string]any
		json.Unmarshal([]byte(text), &payload)
		assertUpdateNotice(t, payload, "0.12.0")
	}
	resources, err := client.ListResources()
	if err != nil || len(resources) == 0 {
		t.Fatal("resources", err)
	}
	resource, err := client.ReadResource(resources[0].URI)
	if err != nil || !strings.Contains(resource, "Skillex 0.12.0 is available") {
		t.Fatalf("resource notice: %v %s", err, resource)
	}
}

func assertUpdateNotice(t *testing.T, payload map[string]any, version string) {
	t.Helper()
	notices, ok := payload["notices"].([]any)
	if !ok || len(notices) != 1 {
		t.Fatalf("missing notice: %#v", payload)
	}
	notice := notices[0].(map[string]any)
	if notice["code"] != "skillex_update_available" || notice["latest_version"] != version || notice["restart_required"] != true {
		t.Fatalf("notice: %#v", notice)
	}
}
