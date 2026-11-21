package packregistry

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestSignedUpdateTarget(t *testing.T) {
	raw, _, _ := published(t)
	var baseline Manifest
	if err := json.Unmarshal(raw, &baseline); err != nil {
		t.Fatal(err)
	}
	old := baseline.Packs[0].Entry
	old.Version = "5.0.0"
	marker := old
	marker.Version = "6.0.0"
	successor := old
	successor.Name, successor.Handle, successor.Version = "owner.new", "owner", "1.0.0"
	for _, tc := range []struct {
		name, source, target, revoked, want, wantErr string
	}{
		{name: "reciprocal across independent version lines", source: `{"superseded-by":"owner.new"}`, target: `{"replaces":"` + old.Name + `"}`, want: successor.Name},
		{name: "unilateral claim has no authority", source: `{}`, target: `{"replaces":"` + old.Name + `"}`, want: marker.Name},
		{name: "target does not confirm", source: `{"superseded-by":"owner.new"}`, target: `{}`, wantErr: "conflicting"},
		{name: "self link", source: `{"superseded-by":"` + old.Name + `"}`, target: `{}`, wantErr: "itself"},
		{name: "cycle", source: `{"superseded-by":"owner.new"}`, target: `{"replaces":"` + old.Name + `","superseded-by":"` + old.Name + `"}`, wantErr: "directly"},
		{name: "target missing", source: `{"superseded-by":"owner.missing"}`, target: `{}`, wantErr: "no stable pack"},
		{name: "incompatible target", source: `{"superseded-by":"owner.new"}`, target: `{"replaces":"` + old.Name + `","compatibility":{"skillex":"^2"}}`, wantErr: "no compatible"},
		{name: "withdrawn target", source: `{"superseded-by":"owner.new"}`, target: `{"replaces":"` + old.Name + `"}`, revoked: successor.Name, wantErr: "revoked"},
		{name: "withdrawn marker", source: `{"superseded-by":"owner.new"}`, target: `{"replaces":"` + old.Name + `"}`, revoked: marker.Name, wantErr: "revoked"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m := Manifest{Packs: []CatalogEntry{{Entry: marker, metadata: json.RawMessage(tc.source)}, {Entry: successor, metadata: json.RawMessage(tc.target)}}}
			for _, e := range m.Packs {
				if e.Name == tc.revoked {
					m.Revocations = []Revocation{{Name: e.Name, Version: e.Version, Reason: "withdrawn"}}
				}
			}
			got, err := m.updateTarget(old, "1.0.0")
			if tc.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
					t.Fatalf("got %v, want %s", err, tc.wantErr)
				}
			} else if err != nil || got.Name != tc.want {
				t.Fatalf("got %s, %v; want %s", got.Name, err, tc.want)
			}
		})
	}
}
