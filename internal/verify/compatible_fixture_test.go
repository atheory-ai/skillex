package verify

import (
	"crypto/sha256"
	"os"
	"testing"

	"github.com/sigstore/sigstore-go/pkg/root"
	sigverify "github.com/sigstore/sigstore-go/pkg/verify"
)

func TestCompatibleManifestSignedFixtures(t *testing.T) {
	read := func(name string) []byte {
		data, err := os.ReadFile("testdata/" + name)
		if err != nil {
			t.Fatal(err)
		}
		return data
	}
	artifact := read("compatible-manifest.json")
	trusted, err := root.NewTrustedRootFromJSON(read("compatible-trusted-root.json"))
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"compatible-manifest.json.bundle", "compatible-manifest.legacy.bundle"} {
		t.Run(name, func(t *testing.T) {
			envelope := read(name)
			if err := manifestWithTrustedRoot(artifact, envelope, trusted); err != nil {
				t.Fatal(err)
			}
			if err := Manifest(artifact, envelope); err == nil {
				t.Fatal("production verifier accepted synthetic root")
			}
			if err := manifestWithTrustedRoot(append(append([]byte{}, artifact...), ' '), envelope, trusted); err == nil {
				t.Fatal("accepted changed artifact")
			}
			noCT, err := root.NewTrustedRoot(root.TrustedRootMediaType01, trusted.FulcioCertificateAuthorities(), nil, nil, trusted.RekorLogs())
			if err != nil {
				t.Fatal(err)
			}
			if err := manifestWithTrustedRoot(artifact, envelope, noCT); err == nil {
				t.Fatal("accepted untrusted SCT")
			}
			noRekor, err := root.NewTrustedRoot(root.TrustedRootMediaType01, trusted.FulcioCertificateAuthorities(), trusted.CTLogs(), nil, nil)
			if err != nil {
				t.Fatal(err)
			}
			if err := manifestWithTrustedRoot(artifact, envelope, noRekor); err == nil {
				t.Fatal("accepted untrusted Rekor timestamp")
			}
		})
	}
}

func TestCompatibleManifestRequiresExactIdentity(t *testing.T) {
	artifact := []byte(`{"schemaVersion":1,"registry":"test-fixture","packs":[]}`)
	for _, tc := range []struct{ name, identity, issuer string }{
		{"wrong SAN", "https://github.com/attacker/packs/.github/workflows/release.yml@refs/heads/main", "https://token.actions.githubusercontent.com"},
		{"wrong issuer", ManifestIdentity, "https://attacker.invalid"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			trusted, envelope := syntheticSignedManifest(t, artifact, tc.identity, tc.issuer)
			// Prove that the artifact, certificate, SCT, and Rekor SET are valid
			// before asserting the engine rejects this otherwise-valid signer.
			verifier, err := sigverify.NewVerifier(trusted, sigverify.WithTransparencyLog(1), sigverify.WithIntegratedTimestamps(1), sigverify.WithSignedCertificateTimestamps(1))
			if err != nil {
				t.Fatal(err)
			}
			expected, err := sigverify.NewShortCertificateIdentity(tc.issuer, "", tc.identity, "")
			if err != nil {
				t.Fatal(err)
			}
			signed, err := parseBundle(artifact, envelope)
			if err != nil {
				t.Fatal(err)
			}
			digest := sha256.Sum256(artifact)
			if _, err := verifier.Verify(signed, sigverify.NewPolicy(sigverify.WithArtifactDigest("sha256", digest[:]), sigverify.WithCertificateIdentity(expected))); err != nil {
				t.Fatalf("otherwise valid signer fixture: %v", err)
			}

			if err := manifestWithTrustedRoot(artifact, envelope, trusted); err == nil {
				t.Fatal("accepted fully signed bundle with incorrect identity")
			}
		})
	}
}
