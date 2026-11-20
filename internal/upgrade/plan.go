// Package upgrade selects migration notes from a target release's guide.
package upgrade

import (
	"context"
	_ "embed"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"

	"golang.org/x/mod/semver"
)

//go:embed guide.md
var bundled string

// Prefix identifies a plan whose endpoints survive an executable update.
const Prefix = "upgrade:"

type entry struct {
	version string
	text    string
}

func stable(version string) bool {
	v := "v" + version
	return semver.IsValid(v) && semver.Canonical(v) == v && semver.Prerelease(v) == ""
}

func parse(guide string) (string, []entry, error) {
	parts := strings.Split(strings.TrimSpace(strings.ReplaceAll(guide, "\r\n", "\n")), "\n## ")
	if len(parts) < 2 {
		return "", nil, fmt.Errorf("upgrade guide has no release entries")
	}
	var entries []entry
	for _, part := range parts[1:] {
		version, body, _ := strings.Cut(part, "\n")
		body = strings.TrimSpace(body)
		if !stable(version) || body == "" || (len(entries) > 0 && semver.Compare("v"+entries[len(entries)-1].version, "v"+version) >= 0) {
			return "", nil, fmt.Errorf("invalid, empty, or out-of-order upgrade entry %q", version)
		}
		entries = append(entries, entry{version, body})
	}
	return strings.TrimSpace(parts[0]), entries, nil
}

// Plan reads the bundled guide, or the target tag's guide for a newer release.
// Only an explicit upgrade ref can initiate this request.
func Plan(ctx context.Context, ref string) (string, error) {
	return plan(ctx, ref, fetchGuide)
}

func plan(ctx context.Context, ref string, fetch func(context.Context, string) (string, error)) (string, error) {
	from, to, ok := strings.Cut(strings.TrimPrefix(ref, Prefix), ":")
	if !strings.HasPrefix(ref, Prefix) || !ok || !stable(from) || !stable(to) || semver.Compare("v"+from, "v"+to) >= 0 {
		return "", fmt.Errorf("expected upgrade:<installed>:<target> with two ascending stable versions")
	}
	header, entries, err := parse(bundled)
	if err != nil {
		return "", err
	}
	if semver.Compare("v"+to, "v"+entries[len(entries)-1].version) > 0 {
		guide, err := fetch(ctx, to)
		if err != nil {
			return "", err
		}
		header, entries, err = parse(guide)
		if err != nil {
			return "", err
		}
		if entries[len(entries)-1].version != to {
			return "", fmt.Errorf("upgrade guide at v%s does not end at the target release", to)
		}
	}
	var result strings.Builder
	fmt.Fprintf(&result, "%s\n\nInstalled: %s. Target: %s.\n", header, from, to)
	foundFrom := false
	for _, entry := range entries {
		if foundFrom {
			fmt.Fprintf(&result, "\n## %s\n\n%s\n", entry.version, entry.text)
			if entry.version == to {
				return result.String(), nil
			}
		}
		if entry.version == from {
			foundFrom = true
		}
	}
	return "", fmt.Errorf("upgrade guide does not cover %s to %s", from, to)
}

func fetchGuide(ctx context.Context, version string) (string, error) {
	if os.Getenv("SKILLEX_OFFLINE") == "1" {
		return "", fmt.Errorf("target upgrade guide is unavailable offline; consult the target release notes before updating")
	}
	url := "https://raw.githubusercontent.com/atheory-ai/skillex/v" + version + "/internal/upgrade/guide.md"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return "", err
	}
	client := &http.Client{Timeout: 5 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("fetch target upgrade guide: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("target upgrade guide returned HTTP %d; consult the target release notes", resp.StatusCode)
	}
	const limit = 1 << 20
	data, err := io.ReadAll(io.LimitReader(resp.Body, limit+1))
	if err != nil {
		return "", err
	}
	if len(data) > limit {
		return "", fmt.Errorf("upgrade guide exceeds %d bytes", limit)
	}
	return string(data), nil
}
