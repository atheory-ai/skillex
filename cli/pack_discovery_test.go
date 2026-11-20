package cli

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/atheory-ai/skillex/internal/packregistry"
	"github.com/atheory-ai/skillex/internal/verify"
	"github.com/atheory-ai/skillex/test/helpers"
)

const mirrorURL = "https://mirror.example/registry/manifest.json"
const samplePack = "atheory-ai.javascript.tool.example"

// Re-enter the actual CLI with offline HTTP responses for the signed public
// release. Only transport is replaced. Signature policy and archive checks run.
func TestPackFixtureCommand(t *testing.T) {
	fixture := os.Getenv("SKILLEX_TEST_PACK_FIXTURE")
	i := slices.Index(os.Args, "--")
	if fixture == "" || i == -1 {
		return
	}
	http.DefaultTransport = packTransport(func(r *http.Request) (*http.Response, error) {
		var file string
		switch r.URL.String() {
		case mirrorURL:
			file = "manifest.json"
		case mirrorURL + ".bundle":
			file = "manifest.json.bundle"
		case "https://github.com/atheory-ai/skillex-packs/releases/download/pack/" + samplePack + "/v0.1.1/" + samplePack + "-0.1.1.tar.gz":
			file = "archive.tar.gz"
		default:
			return nil, fmt.Errorf("unexpected request %s", r.URL)
		}
		f, err := os.Open(filepath.Join(fixture, file))
		if err != nil {
			return nil, err
		}
		return &http.Response{StatusCode: http.StatusOK, Header: http.Header{}, Body: f}, nil
	})
	rootCmd.SetArgs(os.Args[i+1:])
	Execute()
	os.Exit(0)
}

func TestVerifiedPackDiscoveryAndProposal(t *testing.T) {
	project := t.TempDir()
	fixture := t.TempDir()
	binary, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	command := func(args ...string) *exec.Cmd {
		cmd := exec.CommandContext(t.Context(), binary, append([]string{"-test.run=^TestPackFixtureCommand$", "--"}, args...)...)
		cmd.Dir = project
		cmd.Env = append(os.Environ(), "SKILLEX_TEST_PACK_FIXTURE="+fixture, "SKILLEX_NO_UPDATE_CHECK=1")
		return cmd
	}
	run := func(args ...string) []byte {
		t.Helper()
		out, err := command(args...).Output()
		if err != nil {
			t.Fatalf("%v: %v", args, err)
		}
		return bytes.TrimSpace(out)
	}
	store := func(path string, data []byte) {
		t.Helper()
		if err := os.WriteFile(path, data, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	load := func(path string) []byte {
		t.Helper()
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		return data
	}
	config := []byte(`{"Version":4,"Rules":[],"registries":[{"name":"mirror","url":"` + mirrorURL + `","trustedRoot":"bundled"}]}`)
	configPath := filepath.Join(project, "skillex.json")
	store(configPath, config)
	manifest := load("testdata/pack-registry/manifest.json")
	manifestPath := filepath.Join(fixture, "manifest.json")
	store(manifestPath, manifest)
	store(manifestPath+".bundle", load("testdata/pack-registry/manifest.json.bundle"))

	// Search and info must succeed before the transport has access to any tarball.
	search := run("pack", "search", "javascript", "--tier", "core", "--limit", "1", "--json")
	var page packregistry.SearchPage
	if err := json.Unmarshal(search, &page); err != nil || page.Total != 1 || len(page.Results) != 1 || page.Results[0].Name != samplePack {
		t.Fatalf("unexpected search: %s", search)
	}
	var info packregistry.PackInfo
	var signed struct{ Packs []json.RawMessage }
	if err := json.Unmarshal(manifest, &signed); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(run("pack", "info", samplePack, "--json"), &info); err != nil {
		t.Fatal(err)
	}
	if info.ManifestURL != mirrorURL || info.ManifestSigner != verify.ManifestIdentity || info.Registry != page.Registry || len(info.Versions) != 1 {
		t.Fatalf("unexpected source or versions: %+v", info)
	}
	sameJSON := func(a, b []byte) {
		t.Helper()
		var x, y any
		if json.Unmarshal(a, &x) != nil || json.Unmarshal(b, &y) != nil || !reflect.DeepEqual(x, y) {
			t.Errorf("JSON mismatch:\n%s\n%s", a, b)
		}
	}
	sameJSON(signed.Packs[0], info.Versions[0])
	if info.ManifestBundleURL != mirrorURL+".bundle" || info.AttestationLookup["0.1.1"] != "https://api.github.com/repos/atheory-ai/skillex-packs/attestations/sha256:14536a902ddfbbb42c96c6eba04756a722bcb4900f42b03b7a07aa6307b52f00" {
		t.Fatalf("unexpected evidence URLs: %+v", info)
	}
	run("refresh")
	client := helpers.StartMCPCommand(t, command("mcp"))
	listArgs := map[string]any{"filter": "javascript", "tier": "core", "limit": 1}
	nameArg := map[string]any{"name": samplePack}
	tool := func(name string, args map[string]any) []byte {
		t.Helper()
		body, err := client.CallToolText(name, args)
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		return []byte(body)
	}
	sameJSON(search, tool("list_available_packs", listArgs))

	archive := load("testdata/pack-registry/archive.tar.gz")
	store(filepath.Join(fixture, "archive.tar.gz"), archive)
	preview := run("pack", "get", samplePack, "--preview", "--json")
	sameJSON(preview, tool("propose_pack_install", nameArg))
	var proposal struct {
		Installed bool
		Files     []string
		Contents  map[string]string
	}
	if err := json.Unmarshal(preview, &proposal); err != nil || proposal.Installed || len(proposal.Files) != 4 || proposal.Contents["skills/usage.md"] == "" {
		t.Fatalf("incomplete proposal: %s", preview)
	}
	if out, err := command("pack", "get", samplePack, "--json").CombinedOutput(); err == nil || !strings.Contains(string(out), "requires --yes") {
		t.Fatalf("expected explicit consent: %s (%v)", out, err)
	}

	store(manifestPath, append(bytes.Clone(manifest), ' '))
	for _, args := range [][]string{{"pack", "search"}, {"pack", "info", samplePack}} {
		if err := command(args...).Run(); err == nil {
			t.Fatalf("%v accepted a tampered manifest", args)
		}
	}
	for name, args := range map[string]map[string]any{"list_available_packs": listArgs, "propose_pack_install": nameArg} {
		if _, err := client.CallToolText(name, args); err == nil {
			t.Fatalf("%s accepted a tampered manifest", name)
		}
	}
	if string(run("pack", "list", "--json")) != "[]" || !bytes.Equal(config, load(configPath)) {
		t.Fatal("discovery or proposal changed installed packs or configuration")
	}
	if _, err := os.Stat(filepath.Join(project, ".skillex", "packs")); !os.IsNotExist(err) {
		t.Fatalf("unexpected pack cache after preview: %v", err)
	}
}
