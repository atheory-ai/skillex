package capability

import (
	"encoding/json"
	"testing"
)

func TestComputeSchemaDigestCanonicalizesObjectKeys(t *testing.T) {
	first, err := ComputeSchemaDigest(
		CapabilityTool,
		"issues.search",
		json.RawMessage(`{"type":"object","properties":{"query":{"type":"string"},"limit":{"type":"integer"}}}`),
		nil,
	)
	if err != nil {
		t.Fatal(err)
	}
	second, err := ComputeSchemaDigest(
		CapabilityTool,
		"issues.search",
		json.RawMessage(`{"properties":{"limit":{"type":"integer"},"query":{"type":"string"}},"type":"object"}`),
		nil,
	)
	if err != nil {
		t.Fatal(err)
	}
	if first != second {
		t.Fatalf("equivalent schemas produced different digests: %q != %q", first, second)
	}
}

func TestCapabilityValidateRejectsStaleDigest(t *testing.T) {
	capability := Capability{
		Server: ServerVersion{
			Identity: ServerIdentity{CanonicalName: "io.example/issues"},
			Version:  "1.0.0",
		},
		Kind:         CapabilityTool,
		Name:         "issues.search",
		SchemaDigest: "sha256:stale",
	}
	if err := capability.Validate(); err == nil {
		t.Fatal("Validate() succeeded with a stale schema digest")
	}
}
