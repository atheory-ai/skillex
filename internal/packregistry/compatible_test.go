package packregistry_test

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/atheory-ai/skillex/internal/packregistry"
	"github.com/atheory-ai/skillex/internal/packs"
	"github.com/atheory-ai/skillex/internal/verify"
	"github.com/sigstore/sigstore-go/pkg/bundle"
	"github.com/sigstore/sigstore-go/pkg/root"
	sigverify "github.com/sigstore/sigstore-go/pkg/verify"
	"gopkg.in/yaml.v3"
)

type fixtureTransport struct {
	target     *url.URL
	underlying http.RoundTripper
}

func (f fixtureTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	copy := request.Clone(request.Context())
	copy.URL = new(url.URL)
	*copy.URL = *request.URL
	copy.URL.Scheme = f.target.Scheme
	copy.URL.Host = f.target.Host
	copy.Host = f.target.Host
	return f.underlying.RoundTrip(copy)
}

// Test roots are passed only through package-local test adapters. The verifier
// below retains the production SDK thresholds and exact certificate identity.
func fixtureVerifier(t testing.TB, rootFile string) func([]byte, []byte) error {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("..", "verify", "testdata", rootFile))
	if err != nil {
		t.Fatal(err)
	}
	trusted, err := root.NewTrustedRootFromJSON(data)
	if err != nil {
		t.Fatal(err)
	}
	verifier, err := sigverify.NewVerifier(trusted, sigverify.WithTransparencyLog(1), sigverify.WithIntegratedTimestamps(1), sigverify.WithSignedCertificateTimestamps(1))
	if err != nil {
		t.Fatal(err)
	}
	identity, err := sigverify.NewShortCertificateIdentity("https://token.actions.githubusercontent.com", "", verify.ManifestIdentity, "")
	if err != nil {
		t.Fatal(err)
	}
	return func(artifact, envelope []byte) error {
		var b bundle.Bundle
		if err := b.UnmarshalJSON(envelope); err != nil {
			return err
		}
		digest := sha256.Sum256(artifact)
		_, err := verifier.Verify(&b, sigverify.NewPolicy(sigverify.WithArtifactDigest("sha256", digest[:]), sigverify.WithCertificateIdentity(identity)))
		return err
	}
}

func fixtureFetch(t *testing.T, revoked bool) (func(context.Context, string, int64) ([]byte, error), *atomic.Int32, *atomic.Int32) {
	t.Helper()
	prefix := "compatible-"
	if revoked {
		prefix = "compatible-revoked-"
	}
	read := func(name string) []byte {
		data, err := os.ReadFile(filepath.Join("..", "verify", "testdata", name))
		if err != nil {
			t.Fatal(err)
		}
		return data
	}
	manifest, envelope, archive := read(prefix+"manifest.json"), read(prefix+"manifest.json.bundle"), read("compatible-pack.tar.gz")
	var archiveRequests, damage atomic.Int32
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body []byte
		switch r.URL.Path {
		case "/manifest.json":
			body = manifest
			if damage.Load() == 1 {
				body = append(append([]byte{}, manifest...), ' ')
			}
		case "/manifest.json.bundle":
			body = envelope
		case "/pack.tar.gz":
			archiveRequests.Add(1)
			body = archive
			if damage.Load() == 2 {
				body = append(append([]byte{}, archive...), 'x')
			}
		default:
			http.NotFound(w, r)
			return
		}
		if _, err := w.Write(body); err != nil {
			t.Errorf("writing fixture response: %v", err)
		}
	}))
	t.Cleanup(server.Close)
	client := server.Client()
	target, err := url.Parse(server.URL)
	if err != nil {
		t.Fatal(err)
	}
	client.Transport = fixtureTransport{target: target, underlying: client.Transport}
	return func(ctx context.Context, address string, limit int64) ([]byte, error) {
		return packregistry.DownloadForTest(ctx, address, limit, client)
	}, &archiveRequests, &damage
}

func TestCompatibleSignedPackLifecycle(t *testing.T) {
	fetch, _, _ := fixtureFetch(t, false)
	verifier := fixtureVerifier(t, "compatible-trusted-root.json")
	prepared, err := packregistry.PrepareForTest(context.Background(), "https://fixture.invalid/manifest.json", "test.example", verifier, fetch)
	if err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "go.mod"), []byte("module example.com/fixture\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := prepared.Install(root); err != nil {
		t.Fatal(err)
	}
	installed := filepath.Join(root, ".skillex", "packs", "test.example@1.0.0")
	lock, err := packregistry.ValidateDirectoryForTest(installed, verifier)
	if err != nil {
		t.Fatal(err)
	}
	if len(lock.VerifiedFiles) != 2 || !bytes.Equal(lock.VerifiedFiles["skills/usage.md"], prepared.Files["skills/usage.md"]) {
		t.Fatal("missing authenticated snapshot")
	}
	serialized, err := json.Marshal(lock)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(serialized), "VerifiedFiles") {
		t.Fatal("serialized private snapshot")
	}
	listed, err := packregistry.ListForTest(root, verifier)
	if err != nil || len(listed) != 1 {
		t.Fatalf("listing verified install: %v %#v", err, listed)
	}
	if err := prepared.Install(root); err == nil {
		t.Fatal("overwrote existing install")
	}
	if _, err := packregistry.ValidateDirectory(installed); err == nil {
		t.Fatal("production root accepted synthetic signer")
	}
	// Feed the authenticated snapshot into the same engine manifest validation
	// and activation used after production validation; no filesystem trust bypass.
	var manifest packs.Manifest
	decoder := yaml.NewDecoder(bytes.NewReader(lock.VerifiedFiles[packs.Filename]))
	decoder.KnownFields(true)
	if err := decoder.Decode(&manifest); err != nil {
		t.Fatal(err)
	}
	pack := &packs.Pack{Path: filepath.Join(installed, packs.Filename), Dir: installed, Manifest: manifest, VerifiedFiles: lock.VerifiedFiles}
	if err := pack.Validate(); err != nil {
		t.Fatal(err)
	}
	scopes, err := packs.ActivateSkill(root, pack.Manifest.Skills[0])
	if err != nil || len(scopes) != 1 || scopes[0] != "**" {
		t.Fatalf("activation: %v %v", scopes, err)
	}
	skillPath := filepath.Join(installed, "skills", "usage.md")
	if err := os.WriteFile(skillPath, []byte("# Tampered disk copy\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(pack.VerifiedFiles["skills/usage.md"], prepared.Files["skills/usage.md"]) {
		t.Fatal("authenticated snapshot changed with disk")
	}
	if _, err := packregistry.ValidateDirectoryForTest(installed, verifier); err == nil {
		t.Fatal("accepted installed skill tampering")
	}
	if _, err := packregistry.ListForTest(root, verifier); err == nil {
		t.Fatal("listed tampered installed pack")
	}
}

func TestCompatibleSignedPackRejectsTamperedDownloadsAndRevocation(t *testing.T) {
	for _, tc := range []struct {
		name    string
		damage  int32
		revoked bool
		want    string
	}{{"manifest changed", 1, false, ""}, {"archive changed", 2, false, "SHA256"}, {"signed revocation", 0, true, "revoked"}} {
		t.Run(tc.name, func(t *testing.T) {
			fetch, requests, damage := fixtureFetch(t, tc.revoked)
			damage.Store(tc.damage)
			rootFile := "compatible-trusted-root.json"
			if tc.revoked {
				rootFile = "compatible-revoked-trusted-root.json"
			}
			_, err := packregistry.PrepareForTest(context.Background(), "https://fixture.invalid/manifest.json", "test.example", fixtureVerifier(t, rootFile), fetch)
			if err == nil || (tc.want != "" && !strings.Contains(err.Error(), tc.want)) {
				t.Fatalf("expected %s rejection, got %v", tc.name, err)
			}
			if tc.damage != 2 && requests.Load() != 0 {
				t.Fatal("downloaded archive before authenticating eligible manifest")
			}
		})
	}
}

func TestCompatibleSignedPackRejectsLockIdentityAndMissingFiles(t *testing.T) {
	fetch, _, _ := fixtureFetch(t, false)
	verifier := fixtureVerifier(t, "compatible-trusted-root.json")
	prepared, err := packregistry.PrepareForTest(context.Background(), "https://fixture.invalid/manifest.json", "test.example", verifier, fetch)
	if err != nil {
		t.Fatal(err)
	}
	for _, kind := range []string{"lock metadata", "directory name", "missing file", "archive digest"} {
		t.Run(kind, func(t *testing.T) {
			root := t.TempDir()
			if err := prepared.Install(root); err != nil {
				t.Fatal(err)
			}
			installed := filepath.Join(root, ".skillex", "packs", "test.example@1.0.0")
			switch kind {
			case "lock metadata":
				changed := prepared.Lock
				changed.Entry.Description = "forged description"
				data, err := json.Marshal(changed)
				if err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(filepath.Join(installed, "manifest.lock.json"), data, 0o600); err != nil {
					t.Fatal(err)
				}
			case "directory name":
				renamed := filepath.Join(root, ".skillex", "packs", "other@1.0.0")
				if err := os.Rename(installed, renamed); err != nil {
					t.Fatal(err)
				}
				installed = renamed
			case "missing file":
				if err := os.Remove(filepath.Join(installed, "skills", "usage.md")); err != nil {
					t.Fatal(err)
				}
			case "archive digest":
				if err := os.WriteFile(filepath.Join(installed, "archive.tar.gz"), []byte("forged archive"), 0o600); err != nil {
					t.Fatal(err)
				}
			}
			lock, err := packregistry.ValidateDirectoryForTest(installed, verifier)
			if err == nil {
				t.Fatalf("accepted %s", kind)
			}
			if lock.VerifiedFiles != nil {
				t.Fatal("returned authenticated snapshot after failed validation")
			}
		})
	}
}
