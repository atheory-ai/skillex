package packregistry

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestSkillexVersionRange(t *testing.T) {
	base, _, _ := published(t)
	var m Manifest
	if err := json.Unmarshal(base, &m); err != nil {
		t.Fatal(err)
	}
	candidate := m.Packs[0]
	for _, tc := range []struct{ field, engine, wantError string }{
		{`{}`, "dev", ""},
		{`{"node":">=99"}`, "dev", ""},
		{`{"skillex":">=0.7 <2"}`, "v0.13.0-dev", ""},
		{`{"skillex":"^0.13 || ~2.3"}`, "2.3.1", ""},
		{`{"skillex":"^0.13"}`, "0.14.0", "no compatible stable version"},
		{`{"skillex":">=1"}`, "dev", "unknown engine version"},
		{`{"skillex":">=1"}`, "2.0.0-beta.1", "no compatible stable version"},
		{`{"skillex":"broken"}`, "1.0.0", "invalid compatibility.skillex"},
		{`{"skillex":null}`, "1.0.0", "nonempty range"},
		{`{"skillex":42}`, "1.0.0", "nonempty range"},
	} {
		candidate.metadata = json.RawMessage(`{"compatibility":` + tc.field + `}`)
		m.Packs = []CatalogEntry{candidate}
		_, err := m.Resolve(candidate.Name, tc.engine)
		if tc.wantError == "" && err != nil || tc.wantError != "" && (err == nil || !strings.Contains(err.Error(), tc.wantError)) {
			t.Errorf("%s on %s: got %v, want %q", tc.field, tc.engine, err, tc.wantError)
		}
	}
}

func TestCompatibilityFallbackHonorsWithdrawals(t *testing.T) {
	raw, _, _ := published(t)
	var m Manifest
	if err := json.Unmarshal(raw, &m); err != nil {
		t.Fatal(err)
	}
	older := m.Packs[0]
	eligible := older
	eligible.Version = "2.0.0"
	future := older
	future.Version = "3.0.0"
	future.metadata = json.RawMessage(`{"compatibility":{"skillex":">=9"}}`)
	m.Packs = []CatalogEntry{older, eligible, future}
	m.Revocations = []Revocation{{Name: eligible.Name, Version: eligible.Version, Reason: "withdrawn"}}
	got, err := m.Resolve(older.Name, "0.13.0")
	if err != nil || got.Version != older.Version {
		t.Fatalf("fallback: %v, %v", got, err)
	}
	m.Revocations = append(m.Revocations, Revocation{Name: future.Name, Version: future.Version, Reason: "withdrawn latest"})
	if _, err := m.Resolve(older.Name, "0.13.0"); err == nil || !strings.Contains(err.Error(), "withdrawn latest") {
		t.Fatalf("revoked latest: %v", err)
	}
}
