package helpers

import (
	"encoding/json"
	"sort"
	"testing"
)

// SkillSummary mirrors a single result entry in a skillex query --json response.
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

// QueryResponse mirrors the top-level JSON envelope returned by skillex query --json.
type QueryResponse struct {
	Type       string        `json:"type"`
	Results    []SkillSummary `json:"results"`
	Vocabulary *VocabSummary `json:"vocabulary"`
	Query      *QueryEcho    `json:"query"`
}

// VocabSummary mirrors the vocabulary object in a QueryResponse.
type VocabSummary struct {
	Topics      []TopicEntry   `json:"topics"`
	Tags        []TagEntry     `json:"tags"`
	Packages    []PkgEntry     `json:"packages"`
	TotalSkills int            `json:"total_skills"`
}

// TopicEntry is a topic name + count pair in a vocabulary response.
type TopicEntry struct {
	Name  string `json:"name"`
	Count int    `json:"count"`
}

// TagEntry is a tag name + count pair in a vocabulary response.
type TagEntry struct {
	Name  string `json:"name"`
	Count int    `json:"count"`
}

// PkgEntry is a package name + version + count in a vocabulary response.
type PkgEntry struct {
	Name    string `json:"name"`
	Version string `json:"version"`
	Count   int    `json:"count"`
}

// QueryEcho mirrors the query echo object in a no_match response.
type QueryEcho struct {
	Path    string   `json:"path"`
	Topics  []string `json:"topics"`
	Tags    []string `json:"tags"`
	Package string   `json:"package"`
}

// RunQueryJSON executes skillex query with --json and unmarshals the response envelope.
// Fails the test if stdout is not valid JSON or cannot be decoded.
func RunQueryJSON(t *testing.T, dir string, args ...string) (QueryResponse, Result) {
	t.Helper()
	result := Run(t, dir, append(args, "--json")...)
	if result.Stdout == "" {
		t.Fatalf("RunQueryJSON: empty stdout (exit=%d, stderr=%s)", result.ExitCode, result.Stderr)
	}
	var resp QueryResponse
	if err := json.Unmarshal([]byte(result.Stdout), &resp); err != nil {
		t.Fatalf("RunQueryJSON: stdout is not valid QueryResponse JSON: %v\nstdout: %s", err, result.Stdout)
	}
	return resp, result
}

// AssertTopicInVocab fails the test if the named topic is absent from the vocabulary.
func AssertTopicInVocab(t *testing.T, v *VocabSummary, name string) {
	t.Helper()
	if v == nil {
		t.Errorf("vocabulary is nil, cannot check for topic %q", name)
		return
	}
	for _, e := range v.Topics {
		if e.Name == name {
			return
		}
	}
	names := make([]string, len(v.Topics))
	for i, e := range v.Topics {
		names[i] = e.Name
	}
	t.Errorf("topic %q not found in vocabulary; available: %v", name, names)
}

// AssertTagInVocab fails the test if the named tag is absent from the vocabulary.
func AssertTagInVocab(t *testing.T, v *VocabSummary, name string) {
	t.Helper()
	if v == nil {
		t.Errorf("vocabulary is nil, cannot check for tag %q", name)
		return
	}
	for _, e := range v.Tags {
		if e.Name == name {
			return
		}
	}
	names := make([]string, len(v.Tags))
	for i, e := range v.Tags {
		names[i] = e.Name
	}
	t.Errorf("tag %q not found in vocabulary; available: %v", name, names)
}

// AssertPackageInVocab fails the test if the named package is absent from the vocabulary.
func AssertPackageInVocab(t *testing.T, v *VocabSummary, name string) {
	t.Helper()
	if v == nil {
		t.Errorf("vocabulary is nil, cannot check for package %q", name)
		return
	}
	for _, e := range v.Packages {
		if e.Name == name {
			return
		}
	}
	names := make([]string, len(v.Packages))
	for i, e := range v.Packages {
		names[i] = e.Name
	}
	t.Errorf("package %q not found in vocabulary; available: %v", name, names)
}

// VocabTopicCount returns the count for a named topic, or 0 if absent.
func VocabTopicCount(v *VocabSummary, name string) int {
	if v == nil {
		return 0
	}
	for _, e := range v.Topics {
		if e.Name == name {
			return e.Count
		}
	}
	return 0
}

// VocabTagCount returns the count for a named tag, or 0 if absent.
func VocabTagCount(v *VocabSummary, name string) int {
	if v == nil {
		return 0
	}
	for _, e := range v.Tags {
		if e.Name == name {
			return e.Count
		}
	}
	return 0
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
