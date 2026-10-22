package updatecheck

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"runtime/debug"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func testChecker(t *testing.T, handler http.HandlerFunc) (*Checker, *atomic.Int32, *time.Time) {
	t.Helper()
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		handler(w, r)
	}))
	t.Cleanup(server.Close)
	now := time.Now().UTC()
	c := New(Options{Version: "0.10.0", CacheDir: t.TempDir(), Source: "github", URL: server.URL, Now: func() time.Time { return now }})
	return c, &calls, &now
}

func seed(t *testing.T, c *Checker, state cache) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(c.path()), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := c.write(state); err != nil {
		t.Fatal(err)
	}
}

func TestSchedulePersistsAcrossInvocationsAndVersions(t *testing.T) {
	c, calls, now := testChecker(t, func(w http.ResponseWriter, _ *http.Request) { fmt.Fprint(w, `{"tag_name":"v0.11.0"}`) })
	for range 3 {
		// Independent checkers model separate CLI invocations sharing one cache.
		other := New(c.opts)
		if notices := other.Notices(context.Background()); len(notices) != 1 || notices[0].LatestVersion != "0.11.0" {
			t.Fatalf("notices: %#v", notices)
		}
	}
	if calls.Load() != 1 {
		t.Fatalf("requests = %d", calls.Load())
	}
	*now = now.Add(24 * time.Hour)
	c.Notices(context.Background())
	if calls.Load() != 2 {
		t.Fatalf("due requests = %d", calls.Load())
	}
	opts := c.opts
	opts.Version = "0.11.0"
	if notices := New(opts).Notices(context.Background()); len(notices) != 0 {
		t.Fatalf("notice after upgrade: %#v", notices)
	}
	if calls.Load() != 2 {
		t.Fatal("upgrade bypassed shared TTL")
	}
}

func TestFailureBackoffPreservesLastGoodNotice(t *testing.T) {
	c, calls, now := testChecker(t, func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusServiceUnavailable) })
	seed(t, c, cache{Schema: 1, Source: "github", LatestVersion: "0.11.0", LastSuccessAt: now.Add(-25 * time.Hour)})
	for range 3 {
		if notices := New(c.opts).Notices(context.Background()); len(notices) != 1 {
			t.Fatalf("lost cached notice: %#v", notices)
		}
	}
	if calls.Load() != 1 {
		t.Fatalf("failure requests = %d", calls.Load())
	}
	*now = now.Add(time.Hour)
	c.Notices(context.Background())
	if calls.Load() != 2 {
		t.Fatal("failed check did not retry after backoff")
	}
	*now = now.Add(7 * 24 * time.Hour)
	if len(c.CachedNotices()) != 0 {
		t.Fatal("stale notice retained")
	}
}

func TestOfflineOptOutAndDevelopmentBuildsNeverContactService(t *testing.T) {
	c, calls, now := testChecker(t, func(http.ResponseWriter, *http.Request) { t.Error("unexpected request") })
	seed(t, c, cache{Schema: 1, Source: "github", LatestVersion: "0.11.0", LastSuccessAt: *now})
	for _, tc := range []struct {
		name   string
		change func(*Options)
		want   int
	}{
		{"offline uses cache", func(o *Options) { o.Offline = true }, 1},
		{"opt out", func(o *Options) { o.Disabled = true }, 0},
		{"dev", func(o *Options) { o.Version = "dev" }, 0},
		{"source version", func(o *Options) { o.Version = "0.10.0-dev" }, 0},
		{"source invocation", func(o *Options) { o.Installation.Strategy = "source" }, 0},
		{"unknown version", func(o *Options) { o.Version = "garbage" }, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			opts := c.opts
			tc.change(&opts)
			if got := len(New(opts).Notices(context.Background())); got != tc.want {
				t.Fatalf("notices = %d", got)
			}
		})
	}
	if calls.Load() != 0 {
		t.Fatal("disabled network")
	}
}

func TestTimeoutIsBoundedAndBackedOff(t *testing.T) {
	c, calls, _ := testChecker(t, func(_ http.ResponseWriter, r *http.Request) { <-r.Context().Done() })
	c.opts.Timeout = 30 * time.Millisecond
	start := time.Now()
	if len(c.Notices(context.Background())) != 0 {
		t.Fatal("timeout produced notice")
	}
	if time.Since(start) > 500*time.Millisecond {
		t.Fatal("timeout did not bound check")
	}
	c.Notices(context.Background())
	if calls.Load() != 1 {
		t.Fatal("timeout retried immediately")
	}
}

func TestConditionalRequest(t *testing.T) {
	c, _, now := testChecker(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("If-None-Match") != `"release"` {
			t.Error("missing ETag")
		}
		w.WriteHeader(http.StatusNotModified)
	})
	seed(t, c, cache{Schema: 1, Source: "github", LatestVersion: "0.11.0", LastSuccessAt: now.Add(-25 * time.Hour), ETag: `"release"`})
	if len(c.Notices(context.Background())) != 1 || !c.read().LastSuccessAt.Equal(*now) {
		t.Fatal("304 did not refresh cached metadata")
	}
}

func TestInvalidMetadataAndCorruptCache(t *testing.T) {
	for _, body := range []string{`not json`, `{"tag_name":"v1"}`, `{"tag_name":"v1.0.0-rc.1"}`, `{"tag_name":"v1.0.0","draft":true}`, `{"tag_name":"v1.0.0","prerelease":true}`, strings.Repeat("x", maxMetadata+1)} {
		t.Run(body[:min(len(body), 40)], func(t *testing.T) {
			c, calls, _ := testChecker(t, func(w http.ResponseWriter, _ *http.Request) { fmt.Fprint(w, body) })
			os.MkdirAll(filepath.Dir(c.path()), 0o700)
			os.WriteFile(c.path(), []byte("corrupt"), 0o600)
			if len(c.Notices(context.Background())) != 0 {
				t.Fatal("invalid metadata produced notice")
			}
			c.Notices(context.Background())
			if calls.Load() != 1 {
				t.Fatal("invalid metadata not backed off")
			}
		})
	}
}

func TestLeaseRecoveryAndUnwritableCache(t *testing.T) {
	c, calls, _ := testChecker(t, func(w http.ResponseWriter, _ *http.Request) { fmt.Fprint(w, `{"tag_name":"v0.11.0"}`) })
	os.MkdirAll(filepath.Dir(c.path()), 0o700)
	lease, err := os.OpenFile(c.path()+".lock", os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	if err := lockFile(lease); err != nil {
		t.Fatal(err)
	}
	c.Notices(context.Background())
	if calls.Load() != 0 {
		t.Fatal("ignored active lease")
	}
	lease.Close()
	// A subprocess exits without explicitly releasing its lease. The OS must
	// recover it immediately; no stale-file time heuristic is necessary.
	child := exec.Command(os.Args[0], "-test.run=^TestCheckerProcess$")
	child.Env = append(os.Environ(), "SKILLEX_CHECK_TEST_CHILD=1", "SKILLEX_CHECK_TEST_HOLD_LEASE=1", "SKILLEX_CHECK_TEST_CACHE="+c.opts.CacheDir)
	if out, err := child.CombinedOutput(); err != nil {
		t.Fatalf("lease child: %v %s", err, out)
	}
	if len(c.Notices(context.Background())) != 1 {
		t.Fatal("abandoned lease not recovered")
	}
	// A file in place of the cache directory fails reliably even under root.
	opts := c.opts
	opts.CacheDir = filepath.Join(t.TempDir(), "file")
	os.WriteFile(opts.CacheDir, nil, 0o600)
	New(opts).Notices(context.Background())
	if calls.Load() != 1 {
		t.Fatal("unwritable cache caused uncached network request")
	}
}

func TestConcurrentProcessesShareLease(t *testing.T) {
	c, calls, _ := testChecker(t, func(w http.ResponseWriter, _ *http.Request) {
		time.Sleep(40 * time.Millisecond)
		fmt.Fprint(w, `{"tag_name":"v0.11.0"}`)
	})
	var wg sync.WaitGroup
	for range 6 {
		wg.Go(func() {
			cmd := exec.Command(os.Args[0], "-test.run=^TestCheckerProcess$")
			cmd.Env = append(os.Environ(), "SKILLEX_CHECK_TEST_CHILD=1", "SKILLEX_CHECK_TEST_CACHE="+c.opts.CacheDir, "SKILLEX_CHECK_TEST_URL="+c.opts.URL)
			if out, err := cmd.CombinedOutput(); err != nil {
				t.Errorf("child: %v: %s", err, out)
			}
		})
	}
	wg.Wait()
	if calls.Load() != 1 {
		t.Fatalf("concurrent requests = %d", calls.Load())
	}
}

func TestCheckerProcess(t *testing.T) {
	if os.Getenv("SKILLEX_CHECK_TEST_CHILD") != "1" {
		return
	}
	if os.Getenv("SKILLEX_CHECK_TEST_HOLD_LEASE") == "1" {
		file, err := os.OpenFile(filepath.Join(os.Getenv("SKILLEX_CHECK_TEST_CACHE"), "updates", "github.json.lock"), os.O_CREATE|os.O_RDWR, 0o600)
		if err != nil {
			t.Fatal(err)
		}
		if err := lockFile(file); err != nil {
			t.Fatal(err)
		}
		os.Exit(0)
	}
	New(Options{Version: "0.10.0", CacheDir: os.Getenv("SKILLEX_CHECK_TEST_CACHE"), Source: "github", URL: os.Getenv("SKILLEX_CHECK_TEST_URL")}).Notices(context.Background())
}

func TestSourcesAndInstallAdvice(t *testing.T) {
	for _, tc := range []struct {
		source, body string
		install      Installation
		command      string
	}{
		{"github", `{"tag_name":"v0.11.0"}`, Installation{}, ""},
		{"npm", `{"version":"0.11.0"}`, Installation{"local-dev-dependency", "pnpm"}, "pnpm add -D @atheory-ai/skillex@0.11.0"},
		{"homebrew", "class Skillex < Formula\n  version \"0.11.0\"\nend", Installation{}, "brew upgrade atheory-ai/tap/skillex"},
	} {
		t.Run(tc.source, func(t *testing.T) {
			c, _, _ := testChecker(t, func(w http.ResponseWriter, _ *http.Request) { fmt.Fprint(w, tc.body) })
			c.opts.Source, c.opts.Installation = tc.source, tc.install
			notices := c.Notices(context.Background())
			if len(notices) != 1 || notices[0].UpdateCommand != tc.command {
				t.Fatalf("notice: %#v", notices)
			}
		})
	}
}

func TestNoticeDecorationPreservesPayloadAndBound(t *testing.T) {
	data := []byte(`{"type":"results","results":[{"ref":"skill:abc"}],"next_cursor":"cursor"}`)
	if string(AddNotices(data, nil)) != string(data) {
		t.Fatal("changed response without notice")
	}
	var payload map[string]json.RawMessage
	json.Unmarshal(AddNotices(data, []Notice{{Code: "skillex_update_available"}}), &payload)
	if string(payload["next_cursor"]) != `"cursor"` || len(payload["notices"]) == 0 {
		t.Fatalf("payload: %s", payload)
	}
	if string(AddNotices(data, []Notice{{Message: strings.Repeat("x", 2000)}})) != string(data) {
		t.Fatal("notice exceeded bound")
	}
}

func TestVersionResolutionAndSemanticComparison(t *testing.T) {
	info := &debug.BuildInfo{Main: debug.Module{Path: "github.com/atheory-ai/skillex", Version: "v0.11.0"}}
	for _, tc := range []struct{ injected, want string }{{"dev", "0.11.0"}, {"", "0.11.0"}, {"0.12.0-dev", "0.12.0-dev"}, {"0.10.0", "0.10.0"}} {
		if got := resolveVersion(tc.injected, info); got != tc.want {
			t.Fatalf("%s resolved to %s", tc.injected, got)
		}
	}
	c := New(Options{Version: "0.9.0", CacheDir: t.TempDir()})
	state := cache{LatestVersion: "v0.10.0+build", LastSuccessAt: time.Now()}
	if len(c.notices(state)) != 1 {
		t.Fatal("compared versions lexically")
	}
	state.LatestVersion = "0.8.0"
	if len(c.notices(state)) != 0 {
		t.Fatal("suggested downgrade")
	}
	state.LatestVersion = "0.10.0-rc.1"
	if len(c.notices(state)) != 0 {
		t.Fatal("suggested prerelease")
	}
}

func TestRetryHintsAndClockRollback(t *testing.T) {
	c, calls, now := testChecker(t, func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Retry-After", "7200")
		w.WriteHeader(http.StatusTooManyRequests)
	})
	c.Notices(context.Background())
	if !c.read().NextCheckAt.Equal(now.Add(2 * time.Hour)) {
		t.Fatal("ignored Retry-After")
	}
	*now = now.Add(time.Hour)
	c.Notices(context.Background())
	if calls.Load() != 1 {
		t.Fatal("retried before server deadline")
	}
	state := c.read()
	state.LastAttemptAt = now.Add(48 * time.Hour)
	state.NextCheckAt = now.Add(49 * time.Hour)
	seed(t, c, state)
	c.Notices(context.Background())
	if calls.Load() != 2 {
		t.Fatal("future timestamp blocked recovery")
	}
}

func TestEnvironmentControls(t *testing.T) {
	t.Setenv("SKILLEX_CACHE_DIR", t.TempDir())
	t.Setenv("CI", "false")
	t.Setenv("SKILLEX_NO_UPDATE_CHECK", "0")
	t.Setenv("NO_UPDATE_NOTIFIER", "")
	os.Unsetenv("NO_UPDATE_NOTIFIER")
	if !NewEnvironment("0.10.0", Installation{}).enabled() {
		t.Fatal("release disabled")
	}
	for _, name := range []string{"CI", "SKILLEX_NO_UPDATE_CHECK", "NO_UPDATE_NOTIFIER"} {
		t.Run(name, func(t *testing.T) {
			t.Setenv(name, "1")
			if NewEnvironment("0.10.0", Installation{}).enabled() {
				t.Fatal("opt-out ignored")
			}
		})
	}
	t.Setenv("SKILLEX_OFFLINE", "1")
	if !NewEnvironment("0.10.0", Installation{}).opts.Offline {
		t.Fatal("offline ignored")
	}
	t.Setenv("SKILLEX_INSTALL_METHOD", "npm")
	if NewEnvironment("0.10.0", Installation{}).opts.Source != "npm" {
		t.Fatal("wrapper provenance ignored")
	}
}
