// Package verify verifies registry manifests without consulting registry-supplied trust.
package verify

import (
	"bytes"
	"crypto/sha256"
	_ "embed"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"encoding/pem"
	"fmt"

	"github.com/sigstore/sigstore-go/pkg/bundle"
	"github.com/sigstore/sigstore-go/pkg/root"
	sigverify "github.com/sigstore/sigstore-go/pkg/verify"
)

const ManifestIdentity = producerRepo + "/.github/workflows/release-manifest.yml@refs/heads/main"

// The manifest signer and per-registry producer policy are pinned together.
// Only test binaries substitute trust for synthetic publisher fixtures.
type registryPolicy struct{ requirePackEvidence bool }

var manifestRegistries = map[string]registryPolicy{PackRegistry: {requirePackEvidence: true}}

// RequiresPackEvidence is consulted after Manifest authenticates the registry.
func RequiresPackEvidence(registry string) bool {
	return manifestRegistries[registry].requirePackEvidence
}

// Trusted material from sigstore-go v1.3.0 examples/trusted-root-public-good.json.
// Rotation requires an engine release; no network or project trust override.
//
//go:embed trusted-root.json
var trustedRootJSON []byte

// Manifest requires an artifact-bound signature, Fulcio certificate identity,
// SCT, and authenticated Rekor timestamp. Verification is entirely offline.
func Manifest(artifact, envelope []byte) error {
	trusted, err := root.NewTrustedRootFromJSON(trustedRootJSON)
	if err != nil {
		return fmt.Errorf("loading bundled Sigstore root: %w", err)
	}
	if err := manifestWithTrustedRoot(artifact, envelope, trusted); err != nil {
		return err
	}
	var catalog struct{ Registry string }
	if err := json.Unmarshal(artifact, &catalog); err != nil {
		return err
	}
	if _, trusted := manifestRegistries[catalog.Registry]; !trusted {
		return fmt.Errorf("manifest signer is not trusted for registry %q", catalog.Registry)
	}
	return nil
}

func manifestWithTrustedRoot(artifact, envelope []byte, trusted root.TrustedMaterial) error {
	b, err := parseBundle(artifact, envelope)
	if err != nil {
		return fmt.Errorf("reading manifest signature bundle: %w", err)
	}
	digest := sha256.Sum256(artifact)
	if _, err := verifyDigest(b, trusted, ManifestIdentity, digest); err != nil {
		return fmt.Errorf("manifest signature verification failed: %w", err)
	}
	return nil
}

func verifyDigest(b *bundle.Bundle, trusted root.TrustedMaterial, identity string, digest [sha256.Size]byte) (*sigverify.VerificationResult, error) {
	v, err := sigverify.NewVerifier(trusted, sigverify.WithTransparencyLog(1), sigverify.WithIntegratedTimestamps(1), sigverify.WithSignedCertificateTimestamps(1))
	if err != nil {
		return nil, err
	}
	id, err := sigverify.NewShortCertificateIdentity("https://token.actions.githubusercontent.com", "", identity, "")
	if err != nil {
		return nil, err
	}
	return v.Verify(b, sigverify.NewPolicy(sigverify.WithArtifactDigest("sha256", digest[:]), sigverify.WithCertificateIdentity(id)))
}

func parseBundle(artifact, envelope []byte) (*bundle.Bundle, error) {
	var probe struct {
		MediaType string `json:"mediaType"`
	}
	if err := json.Unmarshal(envelope, &probe); err != nil {
		return nil, err
	}
	if probe.MediaType != "" {
		var b bundle.Bundle
		if err := b.UnmarshalJSON(envelope); err != nil {
			return nil, err
		}
		return &b, nil
	}
	// cosign v2's legacy bundle carries a signed-entry timestamp rather than a
	// Merkle inclusion proof. Translate losslessly to protobuf bundle v0.1;
	// sigstore-go verifies its SET, logged body, certificate, and signature.
	var old struct {
		Signature []byte `json:"base64Signature"`
		Cert      []byte `json:"cert"`
		Rekor     struct {
			SET     []byte `json:"SignedEntryTimestamp"`
			Payload struct {
				Body  []byte `json:"body"`
				Time  int64  `json:"integratedTime"`
				Index int64  `json:"logIndex"`
				ID    string `json:"logID"`
			} `json:"Payload"`
		} `json:"rekorBundle"`
	}
	if err := json.Unmarshal(envelope, &old); err != nil {
		return nil, err
	}
	cert, rest := pem.Decode(old.Cert)
	if cert == nil || cert.Type != "CERTIFICATE" || len(bytes.TrimSpace(rest)) != 0 || len(old.Signature) == 0 || len(old.Rekor.SET) == 0 {
		return nil, fmt.Errorf("incomplete legacy cosign bundle")
	}
	logID, err := hex.DecodeString(old.Rekor.Payload.ID)
	if err != nil || len(logID) != 32 {
		return nil, fmt.Errorf("invalid legacy Rekor log ID")
	}
	var body struct {
		Kind    string `json:"kind"`
		Version string `json:"apiVersion"`
	}
	if err := json.Unmarshal(old.Rekor.Payload.Body, &body); err != nil || body.Kind != "hashedrekord" || body.Version != "0.0.1" {
		return nil, fmt.Errorf("unsupported legacy Rekor entry")
	}
	digest := sha256.Sum256(artifact)
	b64 := base64.StdEncoding.EncodeToString
	converted := map[string]any{
		"mediaType":        "application/vnd.dev.sigstore.bundle+json;version=0.1",
		"messageSignature": map[string]any{"messageDigest": map[string]any{"algorithm": "SHA2_256", "digest": b64(digest[:])}, "signature": b64(old.Signature)},
		"verificationMaterial": map[string]any{
			"x509CertificateChain": map[string]any{"certificates": []any{map[string]any{"rawBytes": b64(cert.Bytes)}}},
			"tlogEntries":          []any{map[string]any{"logIndex": fmt.Sprint(old.Rekor.Payload.Index), "logId": map[string]any{"keyId": b64(logID)}, "kindVersion": map[string]any{"kind": body.Kind, "version": body.Version}, "integratedTime": fmt.Sprint(old.Rekor.Payload.Time), "inclusionPromise": map[string]any{"signedEntryTimestamp": b64(old.Rekor.SET)}, "canonicalizedBody": b64(old.Rekor.Payload.Body)}},
		},
	}
	data, err := json.Marshal(converted)
	if err != nil {
		return nil, err
	}
	var b bundle.Bundle
	if err := b.UnmarshalJSON(data); err != nil {
		return nil, err
	}
	return &b, nil
}
