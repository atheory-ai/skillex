package capability

import (
	"errors"
	"strings"
	"testing"
)

func TestMarshalDescriptionEnforcesBoundedValidJSON(t *testing.T) {
	selected := Capability{
		Server:      ServerVersion{Identity: ServerIdentity{CanonicalName: "io.example/issues"}, Version: "1"},
		Kind:        CapabilityTool,
		Name:        "issues.create",
		Description: strings.Repeat("description", 20),
	}
	encoded, err := MarshalDescription(selected, MaximumDescriptionMaxBytes)
	if err != nil || len(encoded) == 0 || len(encoded) > MaximumDescriptionMaxBytes {
		t.Fatalf("MarshalDescription() = %d bytes, %v", len(encoded), err)
	}
	_, err = MarshalDescription(selected, 32)
	if !errors.Is(err, ErrDescriptionTooLarge) {
		t.Fatalf("small budget error = %v", err)
	}
	_, err = MarshalDescription(selected, MaximumDescriptionMaxBytes+1)
	if !errors.Is(err, ErrDescriptionBudgetInvalid) {
		t.Fatalf("invalid budget error = %v", err)
	}
}
