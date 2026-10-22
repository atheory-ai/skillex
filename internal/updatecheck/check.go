// Package updatecheck provides best-effort, periodically cached release notices.
// It never installs updates or changes the project registry.
package updatecheck

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime/debug"
	"strings"
	"sync"
	"time"

	"golang.org/x/mod/semver"
)

const (
	interval      = 24 * time.Hour
	retryInterval = time.Hour
	maxAge        = 7 * 24 * time.Hour
	checkTimeout  = 750 * time.Millisecond
	maxMetadata   = 64 * 1024
)

// Notice is advisory service metadata, separate from retrieved skill content.
type Notice struct {
	Code            string    `json:"code"`
	CurrentVersion  string    `json:"current_version"`
	LatestVersion   string    `json:"latest_version"`
	CheckedAt       time.Time `json:"checked_at"`
	Message         string    `json:"message"`
	ReleaseURL      string    `json:"release_url"`
	UpdateCommand   string    `json:"update_command,omitempty"`
	RestartRequired bool      `json:"restart_required"`
}

// Installation describes the invocation selected by the project, when known.
type Installation struct {
	Strategy       string
	PackageManager string
}

// Options allows deterministic tests without contacting a public release service.
type Options struct {
	Version      string
	CacheDir     string
	Installation Installation
	Source       string
	URL          string
	Client       *http.Client
	Now          func() time.Time
	Timeout      time.Duration
	Disabled     bool
	Offline      bool
}

// Checker shares a persistent checking schedule across projects and processes.
type Checker struct {
	opts Options
	mu   sync.Mutex
}

type cache struct {
	Schema        int       `json:"schema"`
	Source        string    `json:"source"`
	LatestVersion string    `json:"latest_version,omitempty"`
	LastSuccessAt time.Time `json:"last_success_at"`
	LastAttemptAt time.Time `json:"last_attempt_at"`
	NextCheckAt   time.Time `json:"next_check_at"`
	ETag          string    `json:"etag,omitempty"`
}

// Version resolves tagged go-install builds as well as release linker flags.
func Version(injected string) string {
	info, _ := debug.ReadBuildInfo()
	return resolveVersion(injected, info)
}

func resolveVersion(injected string, info *debug.BuildInfo) string {
	if injected != "" && injected != "dev" {
		return injected
	}
	if info != nil && info.Main.Path == "github.com/atheory-ai/skillex" && canonical(info.Main.Version) != "" {
		return strings.TrimPrefix(info.Main.Version, "v")
	}
	return injected
}

// NewEnvironment honors offline and opt-out settings before any cache or network IO.
func NewEnvironment(version string, install Installation) *Checker {
	dir := os.Getenv("SKILLEX_CACHE_DIR")
	if dir == "" {
		if base, err := os.UserCacheDir(); err == nil {
			dir = filepath.Join(base, "skillex")
		}
	}
	_, noNotifier := os.LookupEnv("NO_UPDATE_NOTIFIER")
	disabled := os.Getenv("SKILLEX_NO_UPDATE_CHECK") == "1" || noNotifier || envEnabled("CI")
	return New(Options{Version: Version(version), CacheDir: dir, Installation: install,
		Disabled: disabled, Offline: os.Getenv("SKILLEX_OFFLINE") == "1"})
}

func envEnabled(name string) bool {
	v := strings.ToLower(os.Getenv(name))
	return v != "" && v != "0" && v != "false"
}

// New constructs a checker. Source and URL overrides are for embedding and tests;
// automatic CLI checks always use the fixed public endpoints in sources.go.
func New(opts Options) *Checker {
	if opts.Now == nil {
		opts.Now = time.Now
	}
	if opts.Timeout <= 0 {
		opts.Timeout = checkTimeout
	}
	if opts.Client == nil {
		opts.Client = &http.Client{Timeout: opts.Timeout, CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if req.URL.Scheme != "https" || len(via) >= 3 {
				return http.ErrUseLastResponse
			}
			return nil
		}}
	}
	if opts.Source == "" {
		opts.Source = installationSource(opts.Installation)
	}
	if opts.URL == "" {
		opts.URL = sourceURL(opts.Source)
	}
	return &Checker{opts: opts}
}

func canonical(version string) string {
	v := "v" + strings.TrimPrefix(version, "v")
	// Do not accept semver's shorthand v1 or v1.2 for release metadata.
	if strings.Count(strings.SplitN(strings.SplitN(v, "-", 2)[0], "+", 2)[0], ".") != 2 || !semver.IsValid(v) {
		return ""
	}
	return semver.Canonical(v)
}

func (c *Checker) enabled() bool {
	return c != nil && !c.opts.Disabled && c.opts.CacheDir != "" && canonical(c.opts.Version) != "" &&
		!strings.Contains(c.opts.Version, "-dev") && c.opts.Installation.Strategy != "source"
}

func (c *Checker) path() string {
	// Never use caller-controlled source text as a path component.
	name := c.opts.Source
	if name != "npm" && name != "homebrew" {
		name = "github"
	}
	return filepath.Join(c.opts.CacheDir, "updates", name+".json")
}

func (c *Checker) read() cache {
	var state cache
	f, err := os.Open(c.path())
	if err != nil {
		return state
	}
	defer f.Close()
	if json.NewDecoder(io.LimitReader(f, maxMetadata)).Decode(&state) != nil || state.Schema != 1 || state.Source != c.opts.Source {
		return cache{}
	}
	return state
}

func (c *Checker) write(state cache) error {
	f, err := os.CreateTemp(filepath.Dir(c.path()), ".update-*")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	err = json.NewEncoder(f).Encode(state)
	closeErr := f.Close()
	if err != nil {
		return err
	}
	if closeErr != nil {
		return closeErr
	}
	return os.Rename(f.Name(), c.path())
}

// CachedNotices never performs a network request, including during MCP startup.
func (c *Checker) CachedNotices() []Notice {
	if !c.enabled() {
		return nil
	}
	return c.notices(c.read())
}

// Notices refreshes due metadata within a total deadline, then returns any known
// update. Failed checks leave command behavior and the last good result intact.
func (c *Checker) Notices(ctx context.Context) []Notice {
	if !c.enabled() {
		return nil
	}
	// Do not wait behind another request in this process.
	if !c.mu.TryLock() {
		return c.CachedNotices()
	}
	defer c.mu.Unlock()
	state := c.read()
	now := c.opts.Now().UTC()
	if c.opts.Offline || !due(state, now) {
		return c.notices(state)
	}
	if err := os.MkdirAll(filepath.Dir(c.path()), 0o700); err != nil {
		return c.notices(state)
	}
	lease, err := os.OpenFile(c.path()+".lock", os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return c.notices(state)
	}
	defer lease.Close()
	if lockFile(lease) != nil {
		return c.notices(state)
	}
	// The OS releases this nonblocking lease even if the process crashes. Keep
	// the lock file in place so every process locks the same underlying file.
	// Another process may have refreshed the cache before we acquired the lease.
	state = c.read()
	if !due(state, now) {
		return c.notices(state)
	}
	state.Schema, state.Source = 1, c.opts.Source
	state.LastAttemptAt, state.NextCheckAt = now, now.Add(retryInterval)
	// Persist backoff before contacting the service, including on crashes. An
	// unwritable cache must not turn every invocation into an uncached request.
	if c.write(state) != nil {
		return c.notices(state)
	}
	ctx, cancel := context.WithTimeout(ctx, c.opts.Timeout)
	defer cancel()
	latest, etag, err := c.fetch(ctx, state)
	if err == nil {
		state.LatestVersion, state.ETag = latest, etag
		state.LastSuccessAt, state.NextCheckAt = now, now.Add(interval)
		if c.write(state) != nil {
			return c.notices(state)
		}
	} else if retry, ok := err.(*retryError); ok && retry.at.After(state.NextCheckAt) {
		state.NextCheckAt = retry.at
		if c.write(state) != nil {
			return c.notices(state)
		}
	}
	return c.notices(state)
}

func due(state cache, now time.Time) bool {
	return !state.NextCheckAt.After(now) || state.NextCheckAt.After(now.Add(interval+time.Minute)) || state.LastAttemptAt.After(now.Add(5*time.Minute))
}

func (c *Checker) notices(state cache) []Notice {
	now := c.opts.Now().UTC()
	latest, current := canonical(state.LatestVersion), canonical(c.opts.Version)
	if latest == "" || semver.Prerelease(latest) != "" || state.LastSuccessAt.IsZero() || now.Sub(state.LastSuccessAt) > maxAge ||
		state.LastSuccessAt.After(now.Add(5*time.Minute)) || semver.Compare(latest, current) <= 0 {
		return nil
	}
	version := strings.TrimPrefix(latest, "v")
	message := "Skillex " + version + " is available; running " + strings.TrimPrefix(current, "v") + ". Suggest updating when appropriate; restart or reconnect MCP after updating."
	return []Notice{{Code: "skillex_update_available", CurrentVersion: strings.TrimPrefix(current, "v"),
		LatestVersion: version, CheckedAt: state.LastSuccessAt, Message: message,
		ReleaseURL:    "https://github.com/atheory-ai/skillex/releases/tag/v" + version,
		UpdateCommand: updateCommand(c.opts.Installation, c.opts.Source, version), RestartRequired: true}}
}

// AddNotices decorates a JSON object without touching retrieval fields. With no
// notices the original bytes are preserved, including their field order.
func AddNotices(data []byte, notices []Notice) []byte {
	if len(notices) == 0 {
		return data
	}
	var object map[string]json.RawMessage
	if json.Unmarshal(data, &object) != nil || object == nil {
		return data
	}
	encoded, err := json.Marshal(notices)
	if err != nil || len(encoded) > 1024 {
		return data
	}
	object["notices"] = encoded
	result, err := json.MarshalIndent(object, "", "  ")
	if err != nil {
		return data
	}
	return result
}
