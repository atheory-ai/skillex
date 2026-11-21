package verify

import (
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/sigstore/sigstore-go/pkg/root"
)

func TestPublishedPackProducer(t *testing.T) {
	const name = "atheory-ai.javascript.tool.example"
	const version = "0.1.1"
	read := func(file string) []byte {
		t.Helper()
		data, err := os.ReadFile(filepath.Join("../../cli/testdata/pack-registry", file))
		if err != nil {
			t.Fatal(err)
		}
		return data
	}
	archive := read("archive.tar.gz")
	signature := read("archive.tar.gz.bundle")
	proofs := read("attestations.json")
	parsed, err := parseBundle(archive, signature)
	if err != nil {
		t.Fatal(err)
	}
	modern, err := parsed.MarshalJSON()
	if err != nil {
		t.Fatal(err)
	}
	for _, signed := range [][]byte{signature, modern} {
		if err := PackRelease(name, version, archive, signed, proofs); err != nil {
			t.Fatalf("published producer: %v", err)
		}
	}
	var page struct {
		Attestations []struct{ Bundle json.RawMessage }
	}
	if err := json.Unmarshal(proofs, &page); err != nil {
		t.Fatal(err)
	}
	// The public API also returned a GitHub release statement for the same digest.
	releaseOnly := page
	releaseOnly.Attestations = releaseOnly.Attestations[:1]
	irrelevant, err := json.Marshal(releaseOnly)
	if err != nil {
		t.Fatal(err)
	}
	modified := bytes.ReplaceAll(proofs, []byte(`"payload": "`), []byte(`"payload": "QUFB`))
	if bytes.Equal(modified, proofs) {
		t.Fatal("fixture mutation had no effect")
	}
	for _, tc := range []struct {
		label                   string
		data, sig, attestations []byte
	}{
		{"wrong producer workflow", read("manifest.json"), read("manifest.json.bundle"), proofs},
		{"missing provenance", archive, signature, []byte(`{"attestations":[]}`)},
		{"release statement alone", archive, signature, irrelevant},
		{"modified attestations", archive, signature, modified},
		{"modified archive", append(bytes.Clone(archive), 0), signature, proofs},
		{"attestation substituted for blob signature", archive, page.Attestations[1].Bundle, proofs},
	} {
		t.Run(tc.label, func(t *testing.T) {
			if err := PackRelease(name, version, tc.data, tc.sig, tc.attestations); err == nil {
				t.Fatal("accepted invalid producer evidence")
			}
		})
	}
	if err := PackRelease(name, "0.1.2", archive, signature, proofs); err == nil {
		t.Fatal("accepted a valid signature from a different release tag")
	}
	// Exercise the checked claims independently of envelope authentication.
	trusted, err := root.NewTrustedRootFromJSON(trustedRootJSON)
	if err != nil {
		t.Fatal(err)
	}
	digest := sha256.Sum256(archive)
	ref := "refs/tags/pack/" + name + "/v" + version
	build, err := parseBundle(archive, page.Attestations[1].Bundle)
	if err != nil {
		t.Fatal(err)
	}
	verified, err := verifyDigest(build, trusted, producerRepo+"/"+releaseWorkflow+"@"+ref, digest)
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		file, ref string
		digest    [32]byte
	}{
		{"other.tar.gz", ref, digest}, {name + "-" + version + ".tar.gz", ref, sha256.Sum256(nil)}, {name + "-" + version + ".tar.gz", "refs/heads/main", digest},
	} {
		if err := checkBuildStatement(verified, tc.file, tc.ref, tc.digest); err == nil {
			t.Fatal("accepted mismatched build claims")
		}
	}
}

func TestManifestSignerBoundToRegistry(t *testing.T) {
	data := []byte(`{"schemaVersion":1,"registry":"other","packs":[]}`)
	trusted, signed := syntheticSignedManifest(t, data, ManifestIdentity, "https://token.actions.githubusercontent.com")
	raw, err := trusted.MarshalJSON()
	if err != nil {
		t.Fatal(err)
	}
	original := trustedRootJSON
	t.Cleanup(func() { trustedRootJSON = original })
	trustedRootJSON = raw
	if err := manifestWithTrustedRoot(data, signed, trusted); err != nil {
		t.Fatal(err)
	}
	if err := Manifest(data, signed); err == nil {
		t.Fatal("catalog selected a different registry under the pinned workflow identity")
	}
}
