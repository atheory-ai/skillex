package cli

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/atheory-ai/skillex/internal/packregistry"
	"github.com/atheory-ai/skillex/internal/packs"
)

type packTransport func(*http.Request) (*http.Response, error)

func (f packTransport) RoundTrip(request *http.Request) (*http.Response, error) { return f(request) }

func TestPackGetFailsClosedBeforeProjectWrites(t *testing.T) {
	read := func(name string) []byte {
		t.Helper()
		data, err := os.ReadFile(name)
		if err != nil {
			t.Fatal(err)
		}
		return data
	}
	manifest := read("../internal/verify/testdata/manifest.json")
	bundle := read("../internal/verify/testdata/manifest.json.bundle")
	archive := read("../internal/packregistry/testdata/example.tar.gz")
	for _, tc := range []struct {
		name                      string
		manifest, bundle, archive []byte
	}{
		{"published obsolete engine schema", manifest, bundle, archive},
		{"tampered manifest", append(append([]byte{}, manifest...), ' '), bundle, archive},
		{"missing signature", manifest, []byte(`{}`), archive},
		{"tampered archive", manifest, bundle, append(append([]byte{}, archive...), 0)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root := t.TempDir()
			if err := os.WriteFile(filepath.Join(root, "skillex.json"), []byte(`{"Version":4,"Rules":[]}`), 0o600); err != nil {
				t.Fatal(err)
			}
			t.Chdir(root)
			old := http.DefaultTransport
			t.Cleanup(func() { http.DefaultTransport = old })
			http.DefaultTransport = packTransport(func(request *http.Request) (*http.Response, error) {
				data := tc.archive
				if request.URL.String() == packregistry.DefaultURL {
					data = tc.manifest
				} else if request.URL.String() == packregistry.DefaultURL+".bundle" {
					data = tc.bundle
				}
				return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(bytes.NewReader(data)), Header: http.Header{}}, nil
			})
			cmd := newPackCmd()
			cmd.SetArgs([]string{"get", "atheory-ai.javascript.tool.example", "--yes"})
			cmd.SetOut(io.Discard)
			cmd.SetErr(io.Discard)
			if err := cmd.Execute(); err == nil {
				t.Fatal("installed invalid pack")
			}
			if _, err := os.Stat(filepath.Join(root, ".skillex")); !os.IsNotExist(err) {
				t.Fatalf("invalid pack wrote project files: %v", err)
			}
		})
	}
}

func TestPackPreviewIncludesContentAndAllActivationConsequences(t *testing.T) {
	prepared := &packregistry.Prepared{Files: map[string][]byte{"skills/usage.md": []byte("Guidance requiring human review")}}
	prepared.Lock.Entry.Name = "fixture"
	prepared.Lock.Entry.Version = "1.0.0"
	prepared.Files["pack.yaml"] = []byte(`name: fixture
version: 1.0.0
detectors:
  custom:
    matches:
      - file: {path: project.marker}
skills:
  - file: skills/usage.md
    scope: subtree
    activate-when: {detector: custom}
mcp-servers:
  - ref: io.example/server
    version: 1.0.0
    relationship: suggested
    scope: matching-files
    activate-when: {files-present: [project.marker]}
`)
	preview, err := packs.Preview(prepared)
	if err != nil {
		t.Fatal(err)
	}
	data, err := json.Marshal(preview)
	if err != nil {
		t.Fatal(err)
	}
	for _, expected := range []string{"Guidance requiring human review", "project.marker", "custom", "subtree", "io.example/server", "matching-files"} {
		if !strings.Contains(string(data), expected) {
			t.Fatalf("consent preview omitted %q: %s", expected, data)
		}
	}
	if preview.Installed {
		t.Fatal("preview must never claim installation")
	}
}
