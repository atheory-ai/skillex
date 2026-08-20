package capability

import (
	"errors"
	"strings"
	"testing"
	"time"
)

func TestReferenceSignerRoundTripAndTamperDetection(t *testing.T) {
	now := time.Unix(1_800_000_000, 0)
	signer, err := NewReferenceSigner([]byte("0123456789abcdef0123456789abcdef"), time.Minute, WithClock(func() time.Time { return now }))
	if err != nil {
		t.Fatal(err)
	}
	capability := testCapability(t)
	ref, err := signer.Issue(capability, "private:workspace", "sha256:context")
	if err != nil {
		t.Fatal(err)
	}
	claims, err := signer.Verify(ref)
	if err != nil {
		t.Fatal(err)
	}
	if claims.Server != capability.Server.Identity.CanonicalName || claims.Capability != capability.Name {
		t.Fatalf("unexpected claims: %#v", claims)
	}

	last := ref[len(ref)-1]
	replacement := byte('A')
	if last == replacement {
		replacement = 'B'
	}
	tampered := ref[:len(ref)-1] + string(replacement)
	if _, err := signer.Verify(tampered); !errors.Is(err, ErrInvalidReference) {
		t.Fatalf("Verify(tampered) error = %v, want %v", err, ErrInvalidReference)
	}
	if strings.Contains(ref, capability.Server.Identity.CanonicalName) {
		t.Fatal("reference exposed its server identity in clear text")
	}
}

func TestReferenceSignerRejectsExpiredReference(t *testing.T) {
	now := time.Unix(1_800_000_000, 0)
	signer, err := NewReferenceSigner([]byte("0123456789abcdef0123456789abcdef"), time.Minute, WithClock(func() time.Time { return now }))
	if err != nil {
		t.Fatal(err)
	}
	ref, err := signer.Issue(testCapability(t), "public", "sha256:context")
	if err != nil {
		t.Fatal(err)
	}
	now = now.Add(time.Minute)
	if _, err := signer.Verify(ref); !errors.Is(err, ErrExpiredReference) {
		t.Fatalf("Verify(expired) error = %v, want %v", err, ErrExpiredReference)
	}
}

func testCapability(t *testing.T) Capability {
	t.Helper()
	capability, err := (Capability{
		Server: ServerVersion{
			Identity: ServerIdentity{CanonicalName: "io.example/issues", Publisher: "example"},
			Version:  "1.0.0",
		},
		Kind:            CapabilityTool,
		Name:            "issues.search",
		InputSchemaJSON: []byte(`{"type":"object"}`),
		Availability:    AvailabilityReady,
	}).WithComputedSchemaDigest()
	if err != nil {
		t.Fatal(err)
	}
	return capability
}
