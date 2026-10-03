package packs

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLoadNormalizesInstalledPathsBeforeVerification(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, ".skillex", "packs", "example@1.0.0")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "pack.yaml"), []byte("name: example\nversion: 1.0.0\nskills:\n  - file: usage.md\n    activate-when:\n      files-present: [go.mod]\n    scope: repo\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "usage.md"), []byte("Unsigned guidance"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Chdir(root)
	for _, file := range []string{".skillex/packs/example@1.0.0/pack.yaml", ".skillex/other/../packs/example@1.0.0/pack.yaml"} {
		if _, err := Load(file); err == nil || !strings.Contains(err.Error(), "validating installed pack") {
			t.Fatalf("normalized cache path bypassed signature verification: %s; %v", file, err)
		}
	}
}
