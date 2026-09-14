package capability

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"
)

func TestLoadOrCreateReferenceSignerPersistsLocalKey(t *testing.T) {
	path := filepath.Join(t.TempDir(), "private", "mcp-signing.key")
	first, err := LoadOrCreateReferenceSigner(path, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	second, err := LoadOrCreateReferenceSigner(path, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	selected := testKeyCapability(t)
	ref, err := first.Issue(selected, "public", "sha256:context")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := second.Verify(ref); err != nil {
		t.Fatalf("persisted signer could not verify prior reference: %v", err)
	}
	if runtime.GOOS != "windows" {
		info, err := os.Stat(path)
		if err != nil {
			t.Fatal(err)
		}
		if got := info.Mode().Perm(); got != 0o600 {
			t.Fatalf("signing key permissions = %o, want 600", got)
		}
	}
}

func testKeyCapability(t *testing.T) Capability {
	t.Helper()
	selected, err := (Capability{
		Server: ServerVersion{Identity: ServerIdentity{CanonicalName: "io.example/test"}, Version: "1.0.0"},
		Kind:   CapabilityTool, Name: "test.call", InputSchemaJSON: []byte(`{"type":"object"}`), Availability: AvailabilityReady,
	}).WithComputedSchemaDigest()
	if err != nil {
		t.Fatal(err)
	}
	return selected
}
