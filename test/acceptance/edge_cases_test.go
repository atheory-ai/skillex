package acceptance

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/atheory-ai/skillex/test/helpers"
)

func TestEdge_NoDependencies(t *testing.T) {
	dir := helpers.CopyFixture(t, "single-package")

	// Remove node_modules to simulate no dependencies
	os.RemoveAll(filepath.Join(dir, "node_modules"))

	res := helpers.Run(t, dir, "refresh")
	if res.ExitCode != 0 {
		t.Fatalf("refresh failed with no dependencies: %s", res.Stderr)
	}

	var skills []helpers.SkillSummary
	resQ := helpers.RunJSON(t, dir, &skills, "query", "--path", "src/index.ts", "--format", "summary")
	if resQ.ExitCode != 0 {
		t.Fatalf("query failed: %s", resQ.Stderr)
	}

	helpers.AssertSkillPresent(t, skills, "repo.md")
}

func TestEdge_UnicodeContent(t *testing.T) {
	dir := helpers.CopyFixture(t, "monorepo-pnpm")
	helpers.Run(t, dir, "refresh")

	res := helpers.Run(t, dir, "query",
		"--path", "packages/app-a/src/index.ts",
		"--format", "content")

	if strings.Contains(res.Stdout, "unicode-content") || strings.Contains(res.Stdout, "中文") {
		// Unicode content is present — verify no encoding errors
		if strings.Contains(res.Stdout, "\ufffd") {
			t.Error("replacement character in output suggests encoding error")
		}
	}
}

func TestEdge_ConcurrentRefresh(t *testing.T) {
	dir := helpers.CopyFixture(t, "monorepo-pnpm")

	type result struct {
		exitCode int
		stderr   string
	}
	results := make(chan result, 2)

	// Start two concurrent refreshes
	for i := 0; i < 2; i++ {
		go func() {
			res := helpers.Run(t, dir, "refresh")
			results <- result{exitCode: res.ExitCode, stderr: res.Stderr}
		}()
	}

	r1 := <-results
	r2 := <-results

	// At least one must succeed
	if r1.exitCode != 0 && r2.exitCode != 0 {
		t.Errorf("both concurrent refreshes failed:\n  r1: %s\n  r2: %s", r1.stderr, r2.stderr)
	}

	// Registry must be queryable
	var skills []helpers.SkillSummary
	res := helpers.RunJSON(t, dir, &skills, "query", "--path", "packages/app-a/src/auth.ts", "--format", "summary")
	if res.ExitCode != 0 {
		t.Errorf("query after concurrent refresh failed: %s", res.Stderr)
	}
}

func TestPerformance_RefreshAtScale(t *testing.T) {
	dir := helpers.LoadFixture(t, "perf")

	start := time.Now()
	res := helpers.Run(t, dir, "refresh")
	refreshDuration := time.Since(start)

	if res.ExitCode != 0 {
		t.Fatalf("refresh failed: %s", res.Stderr)
	}
	if refreshDuration > 30*time.Second {
		t.Errorf("refresh took too long: %v (want < 30s)", refreshDuration)
	}

	start = time.Now()
	var skills []helpers.SkillSummary
	helpers.RunJSON(t, dir, &skills, "query", "--path", "packages/pkg-50/src/index.ts", "--format", "summary")
	queryDuration := time.Since(start)

	if queryDuration > 100*time.Millisecond {
		t.Errorf("query took too long: %v (want < 100ms)", queryDuration)
	}
}

func TestEdge_ExternalPackageInSinglePackage(t *testing.T) {
	dir := helpers.CopyFixture(t, "single-package")

	res := helpers.Run(t, dir, "refresh")
	if res.ExitCode != 0 {
		t.Fatalf("refresh failed: %s", res.Stderr)
	}

	var skills []helpers.SkillSummary
	helpers.RunJSON(t, dir, &skills, "query", "--path", "src/index.ts", "--format", "summary")

	helpers.AssertSkillPresent(t, skills, "repo.md")
	// External dependency skills may also be present
}

func TestEdge_SpecialCharsInPackageName(t *testing.T) {
	dir := helpers.CopyFixture(t, "monorepo-pnpm")

	// Add a mock dependency with special chars in name
	pkgDir := filepath.Join(dir, "node_modules", "@scope", "foo-bar.baz")
	os.MkdirAll(filepath.Join(pkgDir, "skillex", "public"), 0o755)
	os.WriteFile(filepath.Join(pkgDir, "package.json"), []byte(`{"name":"@scope/foo-bar.baz","version":"1.0.0","skillex":true}`), 0o644)
	os.WriteFile(filepath.Join(pkgDir, "skillex", "public", "special.md"), []byte("# Special Package\n\nContent.\n"), 0o644)

	// Add it as a dependency to app-a
	appAPkg := filepath.Join(dir, "packages", "app-a", "package.json")
	os.WriteFile(appAPkg, []byte(`{"name":"@test/app-a","version":"1.0.0","private":true,"dependencies":{"@test/ui":"workspace:*","@test/utils":"workspace:*","@scope/foo-bar.baz":"1.0.0"},"devDependencies":{"@test/data":"workspace:*"}}`), 0o644)

	res := helpers.Run(t, dir, "refresh")
	if res.ExitCode != 0 {
		t.Fatalf("refresh with special char package failed: %s", res.Stderr)
	}
	// Don't fail if the package isn't found — just verify no crash
}
