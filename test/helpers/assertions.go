package helpers

import (
	"sort"
	"testing"
)

// SkillSummary mirrors the JSON output of skillex query --json.
type SkillSummary struct {
	Path        string   `json:"path"`
	PackageName string   `json:"package"`
	Version     string   `json:"version"`
	Visibility  string   `json:"visibility"`
	SourceType  string   `json:"source_type"`
	Topics      []string `json:"topics"`
	Tags        []string `json:"tags"`
	Scopes      []string `json:"scopes"`
	Content     string   `json:"content"`
}

// AssertSkillPaths checks that the query result contains exactly the given skill paths (order-independent).
func AssertSkillPaths(t *testing.T, got []SkillSummary, wantPaths ...string) {
	t.Helper()
	gotPaths := make([]string, len(got))
	for i, s := range got {
		gotPaths[i] = s.Path
	}
	sort.Strings(gotPaths)
	sort.Strings(wantPaths)

	if len(gotPaths) != len(wantPaths) {
		t.Errorf("skill path count: got %d, want %d\ngot:  %v\nwant: %v", len(gotPaths), len(wantPaths), gotPaths, wantPaths)
		return
	}
	for i := range wantPaths {
		if gotPaths[i] != wantPaths[i] {
			t.Errorf("skill paths mismatch at index %d: got %q, want %q\nfull got:  %v\nfull want: %v", i, gotPaths[i], wantPaths[i], gotPaths, wantPaths)
			return
		}
	}
}

// AssertSkillPresent checks a specific skill exists in results (by path suffix match).
func AssertSkillPresent(t *testing.T, got []SkillSummary, pathSuffix string) {
	t.Helper()
	for _, s := range got {
		if matchesPath(s.Path, pathSuffix) {
			return
		}
	}
	gotPaths := make([]string, len(got))
	for i, s := range got {
		gotPaths[i] = s.Path
	}
	t.Errorf("expected skill matching %q to be present, got: %v", pathSuffix, gotPaths)
}

// AssertSkillAbsent checks a specific skill does NOT exist in results (by path suffix match).
func AssertSkillAbsent(t *testing.T, got []SkillSummary, pathSuffix string) {
	t.Helper()
	for _, s := range got {
		if matchesPath(s.Path, pathSuffix) {
			t.Errorf("expected skill matching %q to be absent, but found: %s", pathSuffix, s.Path)
			return
		}
	}
}

// AssertTopics checks a skill's topics match expected values (order-independent).
func AssertTopics(t *testing.T, got []SkillSummary, pathSuffix string, topics ...string) {
	t.Helper()
	s := findSkill(got, pathSuffix)
	if s == nil {
		t.Errorf("skill matching %q not found", pathSuffix)
		return
	}
	assertStringSliceEq(t, "topics for "+s.Path, s.Topics, topics)
}

// AssertTags checks a skill's tags match expected values (order-independent).
func AssertTags(t *testing.T, got []SkillSummary, pathSuffix string, tags ...string) {
	t.Helper()
	s := findSkill(got, pathSuffix)
	if s == nil {
		t.Errorf("skill matching %q not found", pathSuffix)
		return
	}
	assertStringSliceEq(t, "tags for "+s.Path, s.Tags, tags)
}

// AssertVisibility checks a skill's visibility field.
func AssertVisibility(t *testing.T, got []SkillSummary, pathSuffix string, visibility string) {
	t.Helper()
	s := findSkill(got, pathSuffix)
	if s == nil {
		t.Errorf("skill matching %q not found", pathSuffix)
		return
	}
	if s.Visibility != visibility {
		t.Errorf("visibility for %s: got %q, want %q", s.Path, s.Visibility, visibility)
	}
}

// findSkill finds a SkillSummary by path suffix.
func findSkill(got []SkillSummary, pathSuffix string) *SkillSummary {
	for i := range got {
		if matchesPath(got[i].Path, pathSuffix) {
			return &got[i]
		}
	}
	return nil
}

// matchesPath returns true if path equals suffix or ends with /suffix.
func matchesPath(path, suffix string) bool {
	if path == suffix {
		return true
	}
	if len(path) > len(suffix) && path[len(path)-len(suffix)-1] == '/' && path[len(path)-len(suffix):] == suffix {
		return true
	}
	// Also handle suffix being a full path fragment
	for i := 0; i <= len(path)-len(suffix); i++ {
		if path[i:i+len(suffix)] == suffix {
			if i == 0 || path[i-1] == '/' {
				if i+len(suffix) == len(path) || path[i+len(suffix)] == '/' {
					return true
				}
			}
		}
	}
	return false
}

func assertStringSliceEq(t *testing.T, label string, got, want []string) {
	t.Helper()
	a := append([]string(nil), got...)
	b := append([]string(nil), want...)
	sort.Strings(a)
	sort.Strings(b)
	if len(a) != len(b) {
		t.Errorf("%s: got %v, want %v", label, a, b)
		return
	}
	for i := range b {
		if a[i] != b[i] {
			t.Errorf("%s: got %v, want %v", label, a, b)
			return
		}
	}
}
