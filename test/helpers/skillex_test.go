package helpers

import (
	"path/filepath"
	"runtime"
	"testing"
)

func TestSkilexBinaryUsesDevelopmentPath(t *testing.T) {
	t.Setenv("SKILLEX_BINARY", "")
	name := "skillex"
	if runtime.GOOS == "windows" {
		name += ".exe"
	}
	want := filepath.Join(repoRoot(), ".skillex", "bin", name)
	if got := SkilexBinary(); got != want {
		t.Fatalf("SkilexBinary() = %q, want %q", got, want)
	}
}
