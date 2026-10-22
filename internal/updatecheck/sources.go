package updatecheck

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"runtime/debug"
	"strconv"
	"strings"
	"time"

	"golang.org/x/mod/semver"
)

func installationSource(install Installation) string {
	if install.Strategy == "local-dev-dependency" || os.Getenv("SKILLEX_INSTALL_METHOD") == "npm" {
		return "npm"
	}
	if executable, err := os.Executable(); err == nil {
		if resolved, err := filepath.EvalSymlinks(executable); err == nil && strings.Contains(filepath.ToSlash(resolved), "/Cellar/skillex/") {
			return "homebrew"
		}
	}
	return "github"
}

func sourceURL(source string) string {
	switch source {
	case "npm":
		return "https://registry.npmjs.org/@atheory-ai%2fskillex/latest"
	case "homebrew":
		return "https://raw.githubusercontent.com/atheory-ai/homebrew-tap/main/Formula/skillex.rb"
	default:
		return "https://api.github.com/repos/atheory-ai/skillex/releases/latest"
	}
}

var formulaVersion = regexp.MustCompile(`(?m)^  version "([^"]+)"$`)

type retryError struct{ at time.Time }

func (*retryError) Error() string { return "release service requested retry backoff" }

func retryAt(header http.Header, now time.Time) time.Time {
	at := now.Add(retryInterval)
	if seconds, err := strconv.ParseInt(header.Get("Retry-After"), 10, 32); err == nil && seconds > 0 {
		at = now.Add(time.Duration(min(seconds, int64(interval/time.Second))) * time.Second)
	} else if date, err := http.ParseTime(header.Get("Retry-After")); err == nil {
		at = date
	}
	if reset, err := strconv.ParseInt(header.Get("X-RateLimit-Reset"), 10, 64); err == nil && time.Unix(reset, 0).After(at) {
		at = time.Unix(reset, 0)
	}
	if at.After(now.Add(interval)) {
		return now.Add(interval)
	}
	return at
}

func (c *Checker) fetch(ctx context.Context, state cache) (string, string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.opts.URL, nil)
	if err != nil {
		return "", "", err
	}
	req.Header.Set("User-Agent", "skillex-update-check")
	if state.ETag != "" && canonical(state.LatestVersion) != "" {
		req.Header.Set("If-None-Match", state.ETag)
	}
	resp, err := c.opts.Client.Do(req)
	if err != nil {
		return "", "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusNotModified && canonical(state.LatestVersion) != "" {
		return state.LatestVersion, state.ETag, nil
	}
	if resp.StatusCode != http.StatusOK {
		if resp.StatusCode == http.StatusTooManyRequests || resp.StatusCode == http.StatusForbidden || resp.StatusCode == http.StatusServiceUnavailable {
			return "", "", &retryError{at: retryAt(resp.Header, c.opts.Now())}
		}
		return "", "", fmt.Errorf("release service returned %d", resp.StatusCode)
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, maxMetadata+1))
	if err != nil {
		return "", "", err
	}
	if len(data) > maxMetadata {
		return "", "", fmt.Errorf("release metadata exceeds limit")
	}
	var version string
	if c.opts.Source == "homebrew" {
		match := formulaVersion.FindSubmatch(data)
		if len(match) == 2 {
			version = string(match[1])
		}
	} else {
		var release struct {
			Tag        string `json:"tag_name"`
			Version    string `json:"version"`
			Draft      bool   `json:"draft"`
			Prerelease bool   `json:"prerelease"`
		}
		if err := json.Unmarshal(data, &release); err != nil {
			return "", "", err
		}
		if release.Draft || release.Prerelease {
			return "", "", fmt.Errorf("release is not stable")
		}
		version = release.Tag
		if c.opts.Source == "npm" {
			version = release.Version
		}
	}
	version = canonical(version)
	if version == "" || len(version) > 100 || semver.Prerelease(version) != "" {
		return "", "", fmt.Errorf("invalid stable release version")
	}
	etag := resp.Header.Get("ETag")
	if len(etag) > 512 {
		etag = ""
	}
	return strings.TrimPrefix(version, "v"), etag, nil
}

func updateCommand(install Installation, source, version string) string {
	if install.Strategy == "local-dev-dependency" {
		switch install.PackageManager {
		case "npm":
			return "npm install --save-dev @atheory-ai/skillex@" + version
		case "pnpm":
			return "pnpm add -D @atheory-ai/skillex@" + version
		case "yarn-classic", "yarn-berry":
			return "yarn add -D @atheory-ai/skillex@" + version
		}
	}
	if source == "homebrew" {
		return "brew upgrade atheory-ai/tap/skillex"
	}
	if info, ok := debug.ReadBuildInfo(); ok && info.Main.Path == "github.com/atheory-ai/skillex" && canonical(info.Main.Version) != "" {
		return "go install github.com/atheory-ai/skillex/cmd/skillex@v" + version
	}
	return "" // Unknown global installs should use their original installation method.
}
