package packregistry

import (
	"encoding/json"
	"math"
	"strings"
	"testing"
	"unicode/utf8"
)

func TestCatalogPagesAndAvailableVersions(t *testing.T) {
	base, _, _ := published(t)
	var m Manifest
	if err := json.Unmarshal(base, &m); err != nil {
		t.Fatal(err)
	}
	template := m.Packs[0].Entry
	m.Packs = nil
	add := func(name, version, tier string) {
		t.Helper()
		entry := template
		entry.Name, entry.Version, entry.Tier = "atheory-ai."+name, version, tier
		entry.Description = "JavaScript " + strings.Repeat("例", 255)
		wire, err := json.Marshal(entry)
		if err != nil {
			t.Fatal(err)
		}
		var e CatalogEntry
		if err := json.Unmarshal(wire, &e); err != nil {
			t.Fatal(err)
		}
		m.Packs = append(m.Packs, e)
	}
	add("tool-b", "2.0.0", "core")
	add("tool-a", "1.0.0", "core")
	add("tool-a", "2.0.0", "core")
	add("tool-a", "1.10.0", "core")
	add("tool-a", "3.0.0-beta.1", "core")
	add("tool-withdrawn", "1.0.0", "core")
	add("tool-withdrawn", "2.0.0", "core")
	add("tool-other", "1.0.0", "community")
	m.Revocations = []Revocation{{Name: "atheory-ai.tool-a", Version: "1.0.0"}, {Name: "atheory-ai.tool-withdrawn", Version: "2.0.0"}}
	search := func(offset int) *SearchPage {
		t.Helper()
		p, err := m.Search("javascript TOOL", "core", 1, offset, "0.13.0")
		if err != nil || p.Total != 2 {
			t.Fatalf("search: %+v, %v", p, err)
		}
		return p
	}
	p := search(0)
	if p.NextOffset == nil || *p.NextOffset != 1 || len(p.Results) != 1 || p.Results[0].Name != "atheory-ai.tool-a" || p.Results[0].Version != "2.0.0" || utf8.RuneCountInString(p.Results[0].Description) != 240 {
		t.Fatalf("first page: %+v", p)
	}
	if p := search(1); p.NextOffset != nil || len(p.Results) != 1 || p.Results[0].Name != "atheory-ai.tool-b" {
		t.Fatalf("last page: %+v", p)
	}
	if p := search(math.MaxInt); p.NextOffset != nil || len(p.Results) != 0 {
		t.Fatalf("past last page: %+v", p)
	}
	for _, bounds := range [][2]int{{0, 0}, {MaxSearchLimit + 1, 0}, {1, -1}} {
		if _, err := m.Search("", "", bounds[0], bounds[1], "0.13.0"); err == nil {
			t.Fatal("accepted invalid page bounds", bounds)
		}
	}
	info, err := m.Info(DefaultURL, "atheory-ai.tool-a")
	if err != nil || len(info.Versions) != 2 {
		t.Fatalf("info: %+v, %v", info, err)
	}
	for i, want := range []string{"2.0.0", "1.10.0"} {
		var e Entry
		if err := json.Unmarshal(info.Versions[i], &e); err != nil || e.Version != want {
			t.Fatalf("version %d: %s", i, info.Versions[i])
		}
	}
	for _, name := range []string{"atheory-ai.tool-withdrawn", "absent"} {
		if _, err := m.Info(DefaultURL, name); err == nil {
			t.Fatalf("unexpected info for %s", name)
		}
	}
	m.Packs = append(m.Packs, m.Packs[0])
	if _, err := m.Search("", "", 10, 0, "0.13.0"); err == nil {
		t.Fatal("listed an ambiguous signed identity")
	}
}
