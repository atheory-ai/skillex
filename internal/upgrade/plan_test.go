package upgrade

import (
	"context"
	"errors"
	"io"
	"net/http"
	"os"
	"strings"
	"testing"
)

// CI and the tag workflow both run this test before publication.
func TestReleaseHasUpgradeNotes(t *testing.T) {
	_, entries, err := parse(bundled)
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile("../../VERSION")
	if err != nil {
		t.Fatal(err)
	}
	if target := strings.TrimSpace(string(data)); entries[len(entries)-1].version != target {
		t.Fatalf("append a non-empty ## %s entry to internal/upgrade/guide.md", target)
	}
	changelog, err := os.ReadFile("../../CHANGELOG.md")
	if err != nil {
		t.Fatal(err)
	}
	known := make(map[string]bool)
	for _, e := range entries {
		known[e.version] = true
	}
	for line := range strings.Lines(string(changelog)) {
		heading, releaseHeading := strings.CutPrefix(line, "## [")
		v, _, _ := strings.Cut(heading, "]")
		if releaseHeading && stable(v) && !known[v] {
			t.Errorf("missing historical upgrade entry for %s", v)
		}
	}
}

func TestPlanSelectsExactRange(t *testing.T) {
	t.Setenv("SKILLEX_OFFLINE", "1")
	got, err := Plan(t.Context(), "upgrade:0.9.1:0.11.1")
	if err != nil {
		t.Fatal(err)
	}
	_, entries, err := parse(got)
	if err != nil || len(entries) != 3 || entries[0].version != "0.10.0" || entries[1].version != "0.11.0" || entries[2].version != "0.11.1" {
		t.Fatalf("unexpected range: %v %v", entries, err)
	}
	for _, ref := range []string{"upgrade:0.12.0:0.11.0", "upgrade:0.10.0:0.10.0", "upgrade:0.8.99:0.12.0", "upgrade:0.8.0:0.8.99", "upgrade:0.12.0:../remote", "upgrade:0.12.0:999999.0.0"} {
		if _, err := Plan(t.Context(), ref); err == nil {
			t.Errorf("expected an error for %s", ref)
		}
	}
}

func TestNewerTargetUsesTaggedGuide(t *testing.T) {
	const target = "999999.0.0"
	from := "0.12.0"
	guide := bundled + "\n## " + target + "\n\nBefore installing, check prerequisites.\n"
	fetch := func(_ context.Context, tag string) (string, error) {
		if tag != target {
			t.Fatalf("requested %s", tag)
		}
		return guide, nil
	}
	ref := Prefix + from + ":" + target
	content, err := plan(t.Context(), ref, fetch)
	if err != nil || !strings.Contains(content, "Before installing, check prerequisites.") {
		t.Fatalf("missing future release instructions: %s, %v", content, err)
	}
	for _, invalid := range []string{bundled, guide + "\n## " + target + "\nDuplicate.", bundled + "\n## " + target + "\n", guide + "\n## 0.9.0\nOut of order."} {
		guide = invalid
		if _, err := plan(t.Context(), ref, fetch); err == nil {
			t.Fatal("accepted incomplete or invalid target guide")
		}
	}
	unavailable := errors.New("service unavailable")
	if _, err := plan(t.Context(), ref, func(context.Context, string) (string, error) { return "", unavailable }); !errors.Is(err, unavailable) {
		t.Fatalf("expected target fetch failure, got %v", err)
	}
}

type transportFunc func(*http.Request) (*http.Response, error)

func (f transportFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestFuturePlanReadsOnlyOfficialTargetTag(t *testing.T) {
	t.Setenv("SKILLEX_OFFLINE", "0")
	original := http.DefaultTransport
	t.Cleanup(func() { http.DefaultTransport = original })
	const target = "999999.0.0"
	const url = "https://raw.githubusercontent.com/atheory-ai/skillex/v999999.0.0/internal/upgrade/guide.md"
	body := strings.ReplaceAll(bundled+"\n## "+target+"\n\nCheck the target version.\n", "\n", "\r\n")
	status := http.StatusOK
	http.DefaultTransport = transportFunc(func(r *http.Request) (*http.Response, error) {
		if r.Method != http.MethodGet || r.URL.String() != url {
			t.Fatalf("unexpected upgrade guide request: %s %s", r.Method, r.URL)
		}
		return &http.Response{StatusCode: status, Header: http.Header{"Location": {"https://example.org/redirect"}}, Body: io.NopCloser(strings.NewReader(body))}, nil
	})
	ref := Prefix + "0.12.0:" + target
	if got, err := Plan(t.Context(), ref); err != nil || !strings.Contains(got, "Check the target version.") {
		t.Fatalf("could not read the target tag: %v %s", err, got)
	}
	for _, status = range []int{http.StatusFound, http.StatusNotFound} {
		if _, err := Plan(t.Context(), ref); err == nil {
			t.Fatalf("accepted HTTP %d", status)
		}
	}
}
