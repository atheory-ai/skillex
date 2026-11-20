package verify_test

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io/fs"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/atheory-ai/skillex/cli"
	"github.com/atheory-ai/skillex/internal/packregistry"
	"github.com/atheory-ai/skillex/internal/packs"
	"github.com/atheory-ai/skillex/internal/verify"
	"github.com/atheory-ai/skillex/test/helpers"
)

// The child serves fixture HTTPS requests from disk and runs the real CLI or MCP
// entry point. Its test-only trust root still requires a valid publisher signature.
type fixtureRegistry string

func (f fixtureRegistry) RoundTrip(r *http.Request) (*http.Response, error) {
	if r.URL.Host != "fixture.invalid" {
		return nil, fmt.Errorf("unexpected request %s", r.URL)
	}
	file, err := os.Open(filepath.Join(string(f), filepath.Base(r.URL.Path)))
	if err != nil {
		return nil, err
	}
	return &http.Response{StatusCode: 200, Header: http.Header{}, Body: file}, nil
}
func TestUpdateFixtureCLI(t *testing.T) {
	i := slices.Index(os.Args, "--")
	fixture := os.Getenv("SKILLEX_TEST_UPDATE_FIXTURE")
	if i < 0 || fixture == "" {
		return
	}
	data, err := os.ReadFile(filepath.Join(fixture, "roots.json"))
	if err != nil {
		t.Fatal(err)
	}
	verify.UseFixtureRoot(t, data)
	http.DefaultTransport = fixtureRegistry(fixture)
	if os.Args[i+1] == "interrupt-update" {
		root, err := os.Getwd()
		if err != nil {
			t.Fatal(err)
		}
		changes, err := packregistry.PlanUpdates(context.Background(), root, "https://fixture.invalid/manifest.json", "")
		if err != nil {
			t.Fatal(err)
		}
		err = packregistry.ApplyUpdates(root, changes, func(id string) error {
			fmt.Fprintln(os.Stderr, "simulated exit before SQLite commit")
			os.Exit(57)
			return nil
		})
		t.Fatalf("expected interruption: %v", err)
	}
	os.Args = append(os.Args[:1], os.Args[i+1:]...)
	cli.Execute()
	os.Exit(0)
}

func updateArchive(t *testing.T, version, packName string) []byte {
	t.Helper()
	dir := filepath.Join("..", "..", "test", "golden", "verified-pack-update", version)
	var data bytes.Buffer
	gz := gzip.NewWriter(&data)
	tarball := tar.NewWriter(gz)
	err := fs.WalkDir(os.DirFS(dir), ".", func(name string, entry fs.DirEntry, err error) error {
		if err != nil || entry.IsDir() {
			return err
		}
		body, err := os.ReadFile(filepath.Join(dir, filepath.FromSlash(name)))
		if err != nil {
			return err
		}
		if name == "pack.yaml" {
			body = bytes.ReplaceAll(body, []byte("test.example"), []byte(packName))
			body = bytes.ReplaceAll(body, []byte("go-project"), []byte(packName+"-go"))
		}
		if err := tarball.WriteHeader(&tar.Header{Name: name, Mode: 0o644, Size: int64(len(body))}); err != nil {
			return err
		}
		_, err = tarball.Write(body)
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := tarball.Close(); err != nil {
		t.Fatal(err)
	}
	if err := gz.Close(); err != nil {
		t.Fatal(err)
	}
	return data.Bytes()
}

func TestGoldenVerifiedPackUpdate(t *testing.T) {
	project, fixture := t.TempDir(), t.TempDir()
	write := func(path string, data []byte) {
		t.Helper()
		if err := os.WriteFile(path, data, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	const name = "test.example"
	const other = "test.other"
	v1, v2 := updateArchive(t, "v1", name), updateArchive(t, "v2", name)
	makeEntry := func(name, version, file string, archive []byte) packregistry.Entry {
		e := packregistry.Entry{Name: name, Handle: "test", Version: version, Tier: "community"}
		h := sha256.Sum256(archive)
		e.Tarball.SHA256 = hex.EncodeToString(h[:])
		e.Tarball.Size = int64(len(archive))
		e.Tarball.URL = "https://fixture.invalid/" + file
		write(filepath.Join(fixture, file), archive)
		return e
	}
	old, next := makeEntry(name, "1.0.0", "v1.tar.gz", v1), makeEntry(name, "2.0.0", "v2.tar.gz", v2)
	otherOld := makeEntry(other, "1.0.0", "other-v1.tar.gz", updateArchive(t, "v1", other))
	otherNext := makeEntry(other, "2.0.0", "other-v2.tar.gz", updateArchive(t, "v2", other))
	type snapshot struct{ manifest, bundle []byte }
	var combined map[string]json.RawMessage
	published := time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)
	catalog := func(entries []packregistry.Entry, revoked []packregistry.Revocation) snapshot {
		t.Helper()
		published = published.Add(time.Minute)
		if revoked == nil {
			revoked = []packregistry.Revocation{}
		}
		manifest, err := json.Marshal(map[string]any{"generatedAt": published, "schemaVersion": 1, "registry": "fixture", "packs": entries, "revocations": revoked})
		if err != nil {
			t.Fatal(err)
		}
		root, bundle := verify.SignFixture(t, manifest)
		var material map[string]json.RawMessage
		if err = json.Unmarshal(root, &material); err != nil {
			t.Fatal(err)
		}
		if combined == nil {
			combined = material
		} else {
			for _, key := range []string{"tlogs", "ctlogs", "certificateAuthorities", "timestampAuthorities"} {
				var a, b []json.RawMessage
				if len(material[key]) == 0 {
					continue
				}
				if err := json.Unmarshal(material[key], &b); err != nil {
					t.Fatal(err)
				}
				if len(combined[key]) > 0 {
					if err := json.Unmarshal(combined[key], &a); err != nil {
						t.Fatal(err)
					}
				}
				combined[key], err = json.Marshal(append(a, b...))
				if err != nil {
					t.Fatal(err)
				}
			}
		}
		return snapshot{manifest, bundle}
	}
	first := catalog([]packregistry.Entry{old, otherOld}, nil)
	second := catalog([]packregistry.Entry{old, next, otherOld, otherNext}, nil)
	withdrawn := catalog([]packregistry.Entry{old, next}, []packregistry.Revocation{{Name: name, Version: next.Version, Reason: "withdrawn"}})
	// A changed gzip header preserves file contents and version but changes
	// the authenticated digest. An earlier approval must not authorize it.
	repacked := bytes.Clone(v2)
	repacked[9] ^= 1
	repackedEntry := makeEntry(name, "2.0.0", "repacked.tar.gz", repacked)
	replacement := catalog([]packregistry.Entry{old, repackedEntry, otherOld, otherNext}, nil)
	broken := makeEntry(name, "2.0.0", "invalid.tar.gz", updateArchive(t, "invalid", name))
	invalid := catalog([]packregistry.Entry{old, broken, otherOld, otherNext}, nil)
	roots, err := json.Marshal(combined)
	if err != nil {
		t.Fatal(err)
	}
	write(filepath.Join(fixture, "roots.json"), roots)
	serve := func(s snapshot) {
		write(filepath.Join(fixture, "manifest.json"), s.manifest)
		write(filepath.Join(fixture, "manifest.json.bundle"), s.bundle)
	}
	serve(first)
	write(filepath.Join(project, "skillex.json"), []byte(`{"Version":4,"Rules":[],"registries":[{"name":"fixture","url":"https://fixture.invalid/manifest.json","trustedRoot":"bundled"}]}`))
	write(filepath.Join(project, "go.mod"), []byte("module example.test/project\n"))
	binary, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	command := func(args ...string) *exec.Cmd {
		cmd := exec.CommandContext(t.Context(), binary, append([]string{"-test.run=^TestUpdateFixtureCLI$", "--"}, args...)...)
		cmd.Dir = project
		cmd.Env = append(os.Environ(), "SKILLEX_TEST_UPDATE_FIXTURE="+fixture, "SKILLEX_NO_UPDATE_CHECK=1")
		return cmd
	}
	run := func(args ...string) []byte {
		t.Helper()
		out, err := command(args...).Output()
		if err != nil {
			t.Fatalf("%v: %v: %s", args, err, err.(*exec.ExitError).Stderr)
		}
		return out
	}
	fail := func(want string, args ...string) {
		t.Helper()
		out, err := command(args...).CombinedOutput()
		if err == nil || !strings.Contains(string(out), want) {
			t.Fatalf("%v: %s (%v), expected %s", args, out, err, want)
		}
	}
	run("pack", "get", name, "--yes", "--quiet")
	run("pack", "get", other, "--yes", "--quiet")
	client := helpers.StartMCPCommand(t, command("mcp"))
	check := func(topic, kind string) {
		t.Helper()
		a := run("query", "--topic", topic, "--json")
		b, err := client.CallToolText("skillex_query", map[string]any{"topic": topic})
		if err != nil {
			t.Fatal(err)
		}
		for _, data := range []string{string(a), b} {
			var resp helpers.QueryResponse
			if err := json.Unmarshal([]byte(data), &resp); err != nil || resp.Type != kind {
				t.Fatalf("%s: %s (%v)", topic, data, err)
			}
		}
	}
	oldWorks := func() { t.Helper(); check("original", "results"); check("updated", "no_match") }
	oldWorks()
	serve(second)
	var review packs.UpdatePreview
	out := run("pack", "update", "--preview", "--json")
	if err := json.Unmarshal(out, &review); err != nil || len(review.Updates) != 2 || len(review.Approval) != 64 || len(review.Updates[0].Files) != 3 || !bytes.Contains(out, []byte("io.example/checks")) {
		t.Fatalf("bad preview: %s", out)
	}
	var named packs.UpdatePreview
	if err := json.Unmarshal(run("pack", "update", name, "--preview", "--json"), &named); err != nil || len(named.Updates) != 1 || named.Approval == review.Approval {
		t.Fatal("named preview must bind only the selected pack")
	}
	apply := []string{"pack", "update", "--approve", review.Approval, "--json"}
	fail("review", "pack", "update", "--json")
	fail("does not match", "pack", "update", "--approve", "obsolete")
	serve(first)
	fail("does not match", apply...)
	oldWorks()
	serve(replacement)
	fail("does not match", apply...)
	oldWorks()
	serve(invalid)
	fail("missing.md", apply...)
	oldWorks()
	serve(withdrawn)
	fail("revoked", apply...)
	oldWorks()
	serve(second)
	write(filepath.Join(fixture, "v2.tar.gz"), append(bytes.Clone(v2), '!'))
	fail("SHA256", apply...)
	oldWorks()
	write(filepath.Join(fixture, "v2.tar.gz"), v2)
	db, err := sql.Open("sqlite", filepath.Join(project, ".skillex", "index.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if _, err = db.Exec("CREATE TRIGGER deny_update BEFORE INSERT ON skills BEGIN SELECT RAISE(ABORT,'fixture reindex failure'); END"); err != nil {
		t.Fatal(err)
	}
	fail("fixture reindex failure", apply...)
	oldWorks()
	if listed := run("pack", "list", "--json"); !bytes.Contains(listed, []byte("1.0.0")) || bytes.Contains(listed, []byte("2.0.0")) {
		t.Fatal(string(listed))
	}
	if _, err = db.Exec("DROP TRIGGER deny_update"); err != nil {
		t.Fatal(err)
	}
	fail("simulated exit before SQLite commit", "interrupt-update")
	fail("--recover", "pack", "list")
	cache := filepath.Join(project, ".skillex", "packs")
	journal := filepath.Join(cache, ".updating")
	saved := filepath.Join(t.TempDir(), "pending")
	if err := os.CopyFS(saved, os.DirFS(journal)); err != nil {
		t.Fatal(err)
	}
	run("pack", "update", "--recover")
	oldWorks()
	run(apply...)
	check("original", "no_match")
	check("updated", "results")
	if listed := run("pack", "list", "--json"); bytes.Contains(listed, []byte("1.0.0")) || !bytes.Contains(listed, []byte("2.0.0")) {
		t.Fatal(string(listed))
	}
	// Recreate the post-commit/pre-cleanup state with the actual SQLite receipt.
	var committed string
	if err := db.QueryRow("SELECT journal_id FROM pack_update_receipt").Scan(&committed); err != nil {
		t.Fatal(err)
	}
	if err := os.CopyFS(journal, os.DirFS(saved)); err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(journal, "journal.json")
	data, err := os.ReadFile(file)
	if err != nil {
		t.Fatal(err)
	}
	var record map[string]any
	if err := json.Unmarshal(data, &record); err != nil {
		t.Fatal(err)
	}
	record["ID"] = committed
	data, err = json.Marshal(record)
	if err != nil {
		t.Fatal(err)
	}
	write(file, data)
	run("pack", "update", "--recover")
	check("original", "no_match")
	check("updated", "results")
	if out := run("pack", "update", "--json"); strings.TrimSpace(string(out)) != `{"updates":[]}` {
		t.Fatal(string(out))
	}
	t.Run("explicit revocation check", func(t *testing.T) {
		decode := func(args ...string) []packregistry.PackStatus {
			t.Helper()
			var status []packregistry.PackStatus
			if err := json.Unmarshal(run(args...), &status); err != nil || len(status) != 2 {
				t.Fatalf("pack status: %v (%v)", status, err)
			}
			return status
		}
		if s := decode("pack", "list", "--json"); s[0].Evidence != "installation_snapshot" || s[0].CheckedAt != "" {
			t.Fatal(s)
		}
		if s := decode("pack", "check", "--json"); s[0].Evidence != "registry_check" || s[0].Status != "not_revoked" || s[0].CheckedAt == "" {
			t.Fatal(s)
		}
		var before helpers.QueryResponse
		if err := json.Unmarshal(run("query", "--topic", "updated", "--json"), &before); err != nil || len(before.Results) != 2 {
			t.Fatalf("expected two indexed skills: %+v %v", before, err)
		}
		var revokedRef, activeRef string
		for _, skill := range before.Results {
			if strings.Contains(skill.Path, name+"@") {
				revokedRef = skill.Ref
			} else {
				activeRef = skill.Ref
			}
		}
		// A rule, a task binding, and a project pack depend on the withdrawn pack.
		cfgPath := filepath.Join(project, "skillex.json")
		baseCfg, err := os.ReadFile(cfgPath)
		if err != nil {
			t.Fatal(err)
		}
		var cfg map[string]any
		if err = json.Unmarshal(baseCfg, &cfg); err != nil {
			t.Fatal(err)
		}
		target := ".skillex/packs/" + name + "@2.0.0/skills/checks.md"
		aliasDir := filepath.Join(project, ".agents", "skills", "pack-alias")
		if runtime.GOOS != "windows" {
			if err := os.MkdirAll(aliasDir, 0700); err != nil {
				t.Fatal(err)
			}
			if err := os.Symlink(filepath.Join(project, target), filepath.Join(aliasDir, "SKILL.md")); err != nil {
				t.Fatal(err)
			}
		}
		cfg["Tasks"] = []map[string]string{{"Name": "verify-pack", "When": "when checking", "Skill": target}}
		cfg["Rules"] = []map[string]any{{"Scope": "**", "Skills": []string{target}}}
		withTasks, err := json.Marshal(cfg)
		if err != nil {
			t.Fatal(err)
		}
		write(cfgPath, withTasks)
		localPack := filepath.Join(project, "skillex")
		if err = os.MkdirAll(localPack, 0o700); err != nil {
			t.Fatal(err)
		}
		write(filepath.Join(localPack, "pack.yaml"), []byte("name: local\nversion: 1.0.0\nskills:\n  - file: gated.md\n    scope: repo\n    activate-when: {detector: test.example-go}\n"))
		write(filepath.Join(localPack, "gated.md"), []byte("---\nname: Gated\ntopics: [gated]\n---\nDepends on registry detector."))
		run("refresh", "--quiet")
		check("gated", "results")
		taskClient := helpers.StartMCPCommand(t, command("mcp"))
		setup := func(want bool) {
			t.Helper()
			mcpSetup, err := taskClient.CallToolText("skillex_query", map[string]any{})
			if err != nil {
				t.Fatal(err)
			}
			for _, out := range []string{mcpSetup, string(run("query", "--json"))} {
				if strings.Contains(out, "verify-pack") != want {
					t.Fatalf("unexpected task setup: %s", out)
				}
			}
		}
		setup(true)
		serve(withdrawn)
		// A failed rebuild must preserve the prior receipt as well as both skills.
		if _, err := db.Exec("CREATE TRIGGER deny_check BEFORE INSERT ON skills BEGIN SELECT RAISE(ABORT,'denied check'); END"); err != nil {
			t.Fatal(err)
		}
		fail("denied check", "pack", "check")
		if s := decode("pack", "list", "--json"); s[0].Status != "not_revoked" {
			t.Fatal(s)
		}
		if _, err := db.Exec("DROP TRIGGER deny_check"); err != nil {
			t.Fatal(err)
		}
		status := decode("pack", "check", "--json")
		if status[0].Name != name || status[0].Reason != "withdrawn" || status[0].Status != "revoked" || status[0].CatalogGeneratedAt != "2026-10-01T00:03:00Z" || status[1].Name != other || status[1].Status != "not_revoked" {
			t.Fatal(status)
		}
		assertVisible := func() {
			t.Helper()
			a := run("query", "--topic", "updated", "--json")
			b, err := client.CallToolText("skillex_query", map[string]any{"topic": "updated"})
			if err != nil {
				t.Fatal(err)
			}
			for _, data := range [][]byte{a, []byte(b)} {
				var response helpers.QueryResponse
				if err := json.Unmarshal(data, &response); err != nil || len(response.Results) != 1 || response.Results[0].Ref != activeRef {
					t.Fatalf("unexpected guidance: %s", data)
				}
			}
			fail("not found", "read", "--ref", revokedRef)
			if _, err := client.CallToolText("skillex_read", map[string]any{"ref": revokedRef}); err == nil {
				t.Fatal("MCP served withdrawn ref")
			}
			if data, err := client.CallToolText("skillex_read", map[string]any{"ref": activeRef}); err != nil || !strings.Contains(data, "Run both project checks") {
				t.Fatalf("unaffected read: %s (%v)", data, err)
			}
		}
		assertVisible()
		setup(false)
		check("gated", "no_match")
		serve(second)
		fail("stale", "pack", "check")
		serve(replacement)
		fail("omits prior revocation", "pack", "check")
		serve(snapshot{withdrawn.manifest, first.bundle})
		if out, err := command("pack", "check").CombinedOutput(); err == nil {
			t.Fatalf("accepted mismatched signature: %s", out)
		}
		// Even with the catalog unavailable, refresh and list use the accepted snapshot.
		if err := os.Remove(filepath.Join(fixture, "manifest.json")); err != nil {
			t.Fatal(err)
		}
		run("refresh", "--quiet")
		run("refresh", "--check")
		if s := decode("pack", "list", "--json"); !slices.Equal(s, status) {
			t.Fatalf("saved status changed: %+v", s)
		}
		assertVisible()
		if _, err := os.Stat(filepath.Join(cache, name+"@2.0.0", "manifest.lock.json")); err != nil {
			t.Fatal(err)
		}
		if runtime.GOOS != "windows" {
			if err := os.Remove(filepath.Join(aliasDir, "SKILL.md")); err != nil {
				t.Fatal(err)
			}
		}
		write(cfgPath, baseCfg)
		if err = os.Remove(filepath.Join(localPack, "pack.yaml")); err != nil {
			t.Fatal(err)
		}
		run("pack", "remove", name)
		serve(second)
		fail("revoked", "pack", "get", name, "--yes")
	})
}
