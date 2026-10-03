package packregistry

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"testing"
)

type transportFunc func(*http.Request) (*http.Response, error)

func (f transportFunc) RoundTrip(req *http.Request) (*http.Response, error) { return f(req) }

func published(t *testing.T) ([]byte, []byte, []byte) {
	t.Helper()
	read := func(file string) []byte {
		data, err := os.ReadFile(file)
		if err != nil {
			t.Fatal(err)
		}
		return data
	}
	return read("../verify/testdata/manifest.json"), read("../verify/testdata/manifest.json.bundle"), read("testdata/example.tar.gz")
}

func TestPrepareVerifiesBeforeInstall(t *testing.T) {
	manifest, bundle, archive := published(t)
	for _, tc := range []struct {
		name                      string
		manifest, bundle, archive []byte
		valid                     bool
	}{
		{"published", manifest, bundle, archive, true},
		{"manifest tampered", append(append([]byte{}, manifest...), ' '), bundle, archive, false},
		{"missing signature", manifest, []byte(`{}`), archive, false},
		{"tarball tampered", manifest, bundle, append(append([]byte{}, archive...), 0), false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			old := http.DefaultTransport
			t.Cleanup(func() { http.DefaultTransport = old })
			http.DefaultTransport = transportFunc(func(req *http.Request) (*http.Response, error) {
				data := tc.archive
				if req.URL.String() == DefaultURL {
					data = tc.manifest
				} else if req.URL.String() == DefaultURL+".bundle" {
					data = tc.bundle
				}
				return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(bytes.NewReader(data)), Header: http.Header{}}, nil
			})
			prepared, err := Prepare(context.Background(), DefaultURL, "atheory-ai.javascript.tool.example")
			if tc.valid {
				if err != nil || len(prepared.Files) == 0 {
					t.Fatalf("published pin verification: %v", err)
				}
			} else if err == nil {
				t.Fatal("accepted tampering")
			}
		})
	}
}

func TestExtractRejectsHostileEntries(t *testing.T) {
	for _, tc := range []struct {
		name string
		kind byte
		link string
	}{
		{"../escape.md", tar.TypeReg, ""}, {"/absolute.md", tar.TypeReg, ""}, {"C:/absolute.md", tar.TypeReg, ""}, {"dir\\escape.md", tar.TypeReg, ""}, {"script.js", tar.TypeReg, ""}, {"link.md", tar.TypeSymlink, "../outside"}, {"hard.md", tar.TypeLink, "outside"}, {"fifo.md", tar.TypeFifo, ""}, {"manifest.lock.json", tar.TypeReg, ""},
		{"pack.yaml/child.md", tar.TypeReg, ""}, {"pack.yaml/", tar.TypeDir, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var out bytes.Buffer
			gz := gzip.NewWriter(&out)
			tw := tar.NewWriter(gz)
			if err := tw.WriteHeader(&tar.Header{Name: "pack.yaml", Typeflag: tar.TypeReg, Mode: 0o600, Size: 0}); err != nil {
				t.Fatal(err)
			}
			if err := tw.WriteHeader(&tar.Header{Name: tc.name, Typeflag: tc.kind, Mode: 0o600, Linkname: tc.link}); err != nil {
				t.Fatal(err)
			}
			if err := tw.Close(); err != nil {
				t.Fatal(err)
			}
			if err := gz.Close(); err != nil {
				t.Fatal(err)
			}
			if _, err := Extract(out.Bytes()); err == nil {
				t.Fatal("accepted hostile archive")
			}
		})
	}
}

func TestResolverRefusesRevokedLatest(t *testing.T) {
	data, _, _ := published(t)
	var m Manifest
	if err := json.Unmarshal(data, &m); err != nil {
		t.Fatal(err)
	}
	m.Revocations = append(m.Revocations, Revocation{Name: m.Packs[0].Name, Version: m.Packs[0].Version, Reason: "withdrawn"})
	if _, err := m.Resolve(m.Packs[0].Name); err == nil {
		t.Fatal("accepted revoked version")
	}
}

func TestInstalledEvidenceRejectsSymlinks(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, ".skillex", "packs", "example@1.0.0")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	outside := filepath.Join(root, "outside.json")
	if err := os.WriteFile(outside, []byte(`{}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(dir, lockName)); err != nil {
		t.Fatal(err)
	}
	if _, err := List(root); err == nil {
		t.Fatal("accepted symlinked lock")
	}
}
