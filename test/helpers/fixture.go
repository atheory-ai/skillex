package helpers

import (
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

// repoRoot returns the absolute path to the repository root.
// It walks up from this source file's location.
func repoRoot() string {
	if v := os.Getenv("SKILLEX_REPO_ROOT"); v != "" {
		return v
	}
	_, file, _, _ := runtime.Caller(0)
	// file is .../skillex/test/helpers/fixture.go
	// repo root is 2 levels up
	return filepath.Join(filepath.Dir(file), "..", "..")
}

// fixtureBase returns the path to the fixtures directory.
func fixtureBase() string {
	if v := os.Getenv("SKILLEX_FIXTURE_ROOT"); v != "" {
		return v
	}
	return filepath.Join(repoRoot(), "test", "fixtures")
}

// goldenBase returns the path to the golden directory.
func goldenBase() string {
	return filepath.Join(repoRoot(), "test", "golden")
}

// LoadFixture returns the path to a named fixture under test/fixtures/.
// Fails the test if the fixture doesn't exist.
func LoadFixture(t *testing.T, name string) string {
	t.Helper()
	path := filepath.Join(fixtureBase(), name)
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("fixture %q not found at %s: run test/setup.sh first", name, path)
	}
	return path
}

// TryLoadFixture returns (path, true) if the fixture exists, or ("", false) if not.
// Use this for optional fixtures (e.g. the perf fixture) instead of failing the test.
func TryLoadFixture(name string) (string, bool) {
	path := filepath.Join(fixtureBase(), name)
	if _, err := os.Stat(path); err != nil {
		return "", false
	}
	return path, true
}

// CopyFixture creates a temporary copy of a fixture for tests that modify it.
// The copy is cleaned up automatically when the test ends.
func CopyFixture(t *testing.T, name string) string {
	t.Helper()
	src := LoadFixture(t, name)
	dst := t.TempDir()
	if err := copyDir(src, dst); err != nil {
		t.Fatalf("copying fixture %q: %v", name, err)
	}
	return dst
}

// copyDir recursively copies src to dst, preserving symlinks.
func copyDir(src, dst string) error {
	return filepath.WalkDir(src, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(src, path)
		target := filepath.Join(dst, rel)

		if d.Type()&fs.ModeSymlink != 0 {
			// Recreate symlink rather than following it (pnpm node_modules uses many symlinks).
			linkTarget, err := os.Readlink(path)
			if err != nil {
				return err
			}
			return os.Symlink(linkTarget, target)
		}

		if d.IsDir() {
			return os.MkdirAll(target, 0o755)
		}

		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		return os.WriteFile(target, data, 0o644)
	})
}

// GoldenPath returns the path to a file in the golden directory.
func GoldenPath(name string) string {
	return filepath.Join(goldenBase(), name)
}
