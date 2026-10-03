package verify

// This regeneration helper is test-only. It uses standard X.509 CA construction
// and the CT SDK's SCT signing format. Generated fixtures contain public trust
// material only; no private keys or production trust overrides are retained.

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"crypto"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/asn1"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"encoding/pem"
	"math/big"
	"net/url"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/cyberphone/json-canonicalization/go/src/webpki.org/jsoncanonicalizer"
	ct "github.com/google/certificate-transparency-go"
	"github.com/google/certificate-transparency-go/tls"
	ctx509 "github.com/google/certificate-transparency-go/x509"
	ctx509util "github.com/google/certificate-transparency-go/x509util"
	"github.com/sigstore/sigstore-go/pkg/root"
)

func TestRegenerateCompatibleManifestFixture(t *testing.T) {
	if os.Getenv("SKILLEX_REGENERATE_VERIFY_FIXTURES") != "1" {
		t.Skip("set SKILLEX_REGENERATE_VERIFY_FIXTURES=1 to regenerate test-only public fixtures")
	}
	archive := compatibleArchive(t)
	digest := sha256.Sum256(archive)
	manifest, err := json.MarshalIndent(map[string]any{
		"schemaVersion": 1, "registry": "test-fixture", "revocations": []any{},
		"packs": []any{map[string]any{"name": "test.example", "handle": "test", "tier": "community", "version": "1.0.0", "description": "Engine-compatible signed test pack", "tarball": map[string]any{"url": "https://fixture.invalid/pack.tar.gz", "sha256": hex.EncodeToString(digest[:]), "size": len(archive)}}},
	}, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	trusted, envelope := syntheticSignedManifest(t, manifest, ManifestIdentity, "https://token.actions.githubusercontent.com")
	if err := manifestWithTrustedRoot(manifest, envelope, trusted); err != nil {
		t.Fatalf("generated fixture failed full verification: %v", err)
	}
	rootJSON, err := trusted.MarshalJSON()
	if err != nil {
		t.Fatal(err)
	}
	b, err := parseBundle(manifest, envelope)
	if err != nil {
		t.Fatal(err)
	}
	modern, err := b.MarshalJSON()
	if err != nil {
		t.Fatal(err)
	}
	for name, data := range map[string][]byte{"compatible-pack.tar.gz": archive, "compatible-manifest.json": manifest, "compatible-manifest.json.bundle": modern, "compatible-manifest.legacy.bundle": envelope, "compatible-trusted-root.json": rootJSON} {
		if err := os.WriteFile(filepath.Join("testdata", name), data, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	var revoked map[string]any
	if err := json.Unmarshal(manifest, &revoked); err != nil {
		t.Fatal(err)
	}
	revoked["revocations"] = []any{map[string]any{"name": "test.example", "version": "1.0.0", "reason": "fixture revocation"}}
	revokedManifest, err := json.MarshalIndent(revoked, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	revokedRoot, revokedLegacy := syntheticSignedManifest(t, revokedManifest, ManifestIdentity, "https://token.actions.githubusercontent.com")
	if err := manifestWithTrustedRoot(revokedManifest, revokedLegacy, revokedRoot); err != nil {
		t.Fatal(err)
	}
	revokedBundle, err := parseBundle(revokedManifest, revokedLegacy)
	if err != nil {
		t.Fatal(err)
	}
	revokedModern, err := revokedBundle.MarshalJSON()
	if err != nil {
		t.Fatal(err)
	}
	revokedRootJSON, err := revokedRoot.MarshalJSON()
	if err != nil {
		t.Fatal(err)
	}
	for name, data := range map[string][]byte{"compatible-revoked-manifest.json": revokedManifest, "compatible-revoked-manifest.json.bundle": revokedModern, "compatible-revoked-trusted-root.json": revokedRootJSON} {
		if err := os.WriteFile(filepath.Join("testdata", name), data, 0o644); err != nil {
			t.Fatal(err)
		}
	}

}

func compatibleArchive(t testing.TB) []byte {
	t.Helper()
	var output bytes.Buffer
	gz := gzip.NewWriter(&output)
	tw := tar.NewWriter(gz)
	files := []struct{ name, body string }{
		{"pack.yaml", "name: test.example\nversion: 1.0.0\ndescription: Engine-compatible signed test pack\nskills:\n  - file: skills/usage.md\n    activate-when:\n      files-present: [go.mod]\n    scope: repo\n"},
		{"skills/usage.md", "---\nname: Signed example guidance\ndescription: Verified registry guidance for Go repositories.\ntopics: [verified-example]\ntags: [fixture]\n---\n\n# Signed example guidance\n\nUse this verified guidance in Go projects.\n"},
	}
	for _, file := range files {
		if err := tw.WriteHeader(&tar.Header{Name: file.name, Mode: 0o644, Size: int64(len(file.body)), Typeflag: tar.TypeReg}); err != nil {
			t.Fatal(err)
		}
		if _, err := tw.Write([]byte(file.body)); err != nil {
			t.Fatal(err)
		}
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := gz.Close(); err != nil {
		t.Fatal(err)
	}
	return output.Bytes()
}

func syntheticSignedManifest(t testing.TB, artifact []byte, identity, issuer string) (*root.TrustedRoot, []byte) {
	t.Helper()
	now := time.Now().UTC().Truncate(time.Second)
	rootKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	rootTemplate := &x509.Certificate{SerialNumber: big.NewInt(1), Subject: pkix.Name{CommonName: "Skillex synthetic test CA"}, NotBefore: now.Add(-time.Hour), NotAfter: now.Add(time.Hour), KeyUsage: x509.KeyUsageCertSign | x509.KeyUsageCRLSign, BasicConstraintsValid: true, IsCA: true}
	rootDER, err := x509.CreateCertificate(rand.Reader, rootTemplate, rootTemplate, rootKey.Public(), rootKey)
	if err != nil {
		t.Fatal(err)
	}
	rootCert, err := x509.ParseCertificate(rootDER)
	if err != nil {
		t.Fatal(err)
	}
	intermediateKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	intermediateTemplate := &x509.Certificate{SerialNumber: big.NewInt(2), Subject: pkix.Name{CommonName: "Skillex synthetic Fulcio intermediate"}, NotBefore: now.Add(-time.Hour), NotAfter: now.Add(time.Hour), KeyUsage: x509.KeyUsageCertSign | x509.KeyUsageCRLSign, BasicConstraintsValid: true, IsCA: true, MaxPathLenZero: true}
	intermediateDER, err := x509.CreateCertificate(rand.Reader, intermediateTemplate, rootCert, intermediateKey.Public(), rootKey)
	if err != nil {
		t.Fatal(err)
	}
	intermediate, err := x509.ParseCertificate(intermediateDER)
	if err != nil {
		t.Fatal(err)
	}
	key := func() *ecdsa.PrivateKey {
		k, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
		if err != nil {
			t.Fatal(err)
		}
		return k
	}
	leafKey, ctKey, rekorKey := key(), key(), key()
	logID := func(k *ecdsa.PrivateKey) [32]byte {
		der, err := x509.MarshalPKIXPublicKey(k.Public())
		if err != nil {
			t.Fatal(err)
		}
		return sha256.Sum256(der)
	}
	ctID, rekorID := logID(ctKey), logID(rekorKey)
	san, err := url.Parse(identity)
	if err != nil {
		t.Fatal(err)
	}
	template := &x509.Certificate{SerialNumber: big.NewInt(3), NotBefore: now.Add(-time.Minute), NotAfter: now.Add(10 * time.Minute), KeyUsage: x509.KeyUsageDigitalSignature, ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageCodeSigning}, URIs: []*url.URL{san}, ExtraExtensions: []pkix.Extension{{Id: asn1.ObjectIdentifier{1, 3, 6, 1, 4, 1, 57264, 1, 1}, Value: []byte(issuer)}}}
	cert := func() *x509.Certificate {
		der, err := x509.CreateCertificate(rand.Reader, template, intermediate, leafKey.Public(), intermediateKey)
		if err != nil {
			t.Fatal(err)
		}
		parsed, err := x509.ParseCertificate(der)
		if err != nil {
			t.Fatal(err)
		}
		return parsed
	}
	precert := cert()
	sct := ct.SignedCertificateTimestamp{SCTVersion: ct.V1, LogID: ct.LogID{KeyID: ctID}, Timestamp: uint64(now.UnixMilli())}
	entry := ct.LogEntry{Leaf: ct.MerkleTreeLeaf{Version: ct.V1, LeafType: ct.TimestampedEntryLeafType, TimestampedEntry: &ct.TimestampedEntry{Timestamp: sct.Timestamp, EntryType: ct.PrecertLogEntryType, PrecertEntry: &ct.PreCert{IssuerKeyHash: sha256.Sum256(intermediate.RawSubjectPublicKeyInfo), TBSCertificate: precert.RawTBSCertificate}}}}
	sctInput, err := ct.SerializeSCTSignatureInput(sct, entry)
	if err != nil {
		t.Fatal(err)
	}
	sctDigest := sha256.Sum256(sctInput)
	sctSignature, err := ecdsa.SignASN1(rand.Reader, ctKey, sctDigest[:])
	if err != nil {
		t.Fatal(err)
	}
	sct.Signature = ct.DigitallySigned{Algorithm: tls.SignatureAndHashAlgorithm{Hash: tls.SHA256, Signature: tls.ECDSA}, Signature: sctSignature}
	list, err := ctx509util.MarshalSCTsIntoSCTList([]*ct.SignedCertificateTimestamp{&sct})
	if err != nil {
		t.Fatal(err)
	}
	sctBytes, err := tls.Marshal(*list)
	if err != nil {
		t.Fatal(err)
	}
	sctASN, err := asn1.Marshal(sctBytes)
	if err != nil {
		t.Fatal(err)
	}
	template.ExtraExtensions = append(template.ExtraExtensions, pkix.Extension{Id: asn1.ObjectIdentifier(ctx509.OIDExtensionCTSCT), Value: sctASN})
	leaf := cert()
	digest := sha256.Sum256(artifact)
	signature, err := ecdsa.SignASN1(rand.Reader, leafKey, digest[:])
	if err != nil {
		t.Fatal(err)
	}
	certificate := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: leaf.Raw})
	b64 := base64.StdEncoding.EncodeToString
	marshal := func(v any) []byte {
		data, err := json.Marshal(v)
		if err != nil {
			t.Fatal(err)
		}
		return data
	}
	body := marshal(map[string]any{"apiVersion": "0.0.1", "kind": "hashedrekord", "spec": map[string]any{"data": map[string]any{"hash": map[string]any{"algorithm": "sha256", "value": hex.EncodeToString(digest[:])}}, "signature": map[string]any{"content": b64(signature), "publicKey": map[string]any{"content": b64(certificate)}}}})
	payload := map[string]any{"body": b64(body), "integratedTime": now.Unix(), "logIndex": int64(0), "logID": hex.EncodeToString(rekorID[:])}
	canonical, err := jsoncanonicalizer.Transform(marshal(payload))
	if err != nil {
		t.Fatal(err)
	}
	setDigest := sha256.Sum256(canonical)
	set, err := ecdsa.SignASN1(rand.Reader, rekorKey, setDigest[:])
	if err != nil {
		t.Fatal(err)
	}
	envelope := marshal(map[string]any{"base64Signature": signature, "cert": certificate, "rekorBundle": map[string]any{"SignedEntryTimestamp": set, "Payload": payload}})
	log := func(k *ecdsa.PrivateKey, id [32]byte) *root.TransparencyLog {
		return &root.TransparencyLog{BaseURL: "https://fixture.invalid", ID: id[:], ValidityPeriodStart: now.Add(-time.Hour), ValidityPeriodEnd: now.Add(time.Hour), HashFunc: crypto.SHA256, SignatureHashFunc: crypto.SHA256, PublicKey: k.Public()}
	}
	trusted, err := root.NewTrustedRoot(root.TrustedRootMediaType01, []root.CertificateAuthority{&root.FulcioCertificateAuthority{Root: rootCert, Intermediates: []*x509.Certificate{intermediate}, URI: "https://fixture.invalid", ValidityPeriodStart: now.Add(-time.Hour), ValidityPeriodEnd: now.Add(time.Hour)}}, map[string]*root.TransparencyLog{hex.EncodeToString(ctID[:]): log(ctKey, ctID)}, nil, map[string]*root.TransparencyLog{hex.EncodeToString(rekorID[:]): log(rekorKey, rekorID)})
	if err != nil {
		t.Fatal(err)
	}
	return trusted, envelope
}
