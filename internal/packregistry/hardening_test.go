package packregistry

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func archiveWithPadding(t *testing.T, padding int64, suffix []byte) []byte {
	t.Helper()
	var out bytes.Buffer
	gz := gzip.NewWriter(&out)
	tw := tar.NewWriter(gz)
	body := []byte("name: fixture.pack\nversion: 1.0.0\n")
	if err := tw.WriteHeader(&tar.Header{Name: "pack.yaml", Typeflag: tar.TypeReg, Mode: 0o600, Size: int64(len(body))}); err != nil {
		t.Fatal(err)
	}
	if _, err := tw.Write(body); err != nil {
		t.Fatal(err)
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	zeros := make([]byte, 32<<10)
	for padding > 0 {
		n := min(padding, int64(len(zeros)))
		if _, err := gz.Write(zeros[:n]); err != nil {
			t.Fatal(err)
		}
		padding -= n
	}
	if _, err := gz.Write(suffix); err != nil {
		t.Fatal(err)
	}
	if err := gz.Close(); err != nil {
		t.Fatal(err)
	}
	return out.Bytes()
}

func TestExtractVerifiesGzipFooterAndAllExpandedBytes(t *testing.T) {
	valid := archiveWithPadding(t, 2048, nil)
	if _, err := Extract(valid); err != nil {
		t.Fatalf("valid trailing zero padding: %v", err)
	}
	corrupt := append([]byte{}, valid...)
	corrupt[len(corrupt)-8] ^= 0xff
	for _, tc := range []struct {
		name    string
		data    []byte
		message string
	}{
		{"bad footer", corrupt, "gzip"},
		{"truncated footer", valid[:len(valid)-4], "gzip"},
		{"hidden trailing payload", archiveWithPadding(t, 0, []byte("hidden payload")), "non-padding"},
		{"trailing decompression bomb", archiveWithPadding(t, maxExtracted, nil), "expansion limit"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := Extract(tc.data)
			if err == nil || !strings.Contains(err.Error(), tc.message) {
				t.Fatalf("got %v, expected %s rejection", err, tc.message)
			}
		})
	}
}

func installedPublished(t *testing.T) (string, string) {
	t.Helper()
	manifest, bundle, archive := published(t)
	m, err := authenticated(manifest, bundle)
	if err != nil {
		t.Fatal(err)
	}
	e, err := m.Resolve("atheory-ai.javascript.tool.example")
	if err != nil {
		t.Fatal(err)
	}
	files, err := verifiedArchive(e, archive)
	if err != nil {
		t.Fatal(err)
	}
	prepared := &Prepared{Lock: Lock{Entry: e, Registry: m.Registry, ManifestURL: DefaultURL, Manifest: manifest, Bundle: bundle}, Archive: archive, Files: files}
	root := t.TempDir()
	if err := prepared.Install(root); err != nil {
		t.Fatal(err)
	}
	dir := filepath.Join(root, ".skillex", "packs", e.Name+"@"+e.Version)
	return root, dir
}

func TestInstalledEvidenceMustBeBoundedRegularFiles(t *testing.T) {
	for _, file := range []string{lockName, archiveName, "pack.yaml"} {
		t.Run(file+" oversized", func(t *testing.T) {
			_, dir := installedPublished(t)
			f, err := os.OpenFile(filepath.Join(dir, file), os.O_WRONLY, 0)
			if err != nil {
				t.Fatal(err)
			}
			limit := int64(maxDownload + 1)
			if file == lockName {
				limit = 12<<20 + 1
			}
			if file == "pack.yaml" {
				limit = maxExtracted + 1
			}
			if err := f.Truncate(limit); err != nil {
				t.Fatal(err)
			}
			if err := f.Close(); err != nil {
				t.Fatal(err)
			}
			if _, err := ValidateDirectory(dir); err == nil {
				t.Fatal("accepted oversized evidence or content")
			}
		})
		t.Run(file+" symlink", func(t *testing.T) {
			_, dir := installedPublished(t)
			target := filepath.Join(t.TempDir(), "outside.txt")
			if err := os.WriteFile(target, []byte("external"), 0o600); err != nil {
				t.Fatal(err)
			}
			installed := filepath.Join(dir, file)
			if err := os.Remove(installed); err != nil {
				t.Fatal(err)
			}
			if err := os.Symlink(target, installed); err != nil {
				t.Skipf("symlink unavailable: %v", err)
			}
			if _, err := ValidateDirectory(dir); err == nil {
				t.Fatal("accepted symlink evidence or content")
			}
		})
	}
}

func TestSignedLockMetadataAndDirectoryIdentityCannotBeChanged(t *testing.T) {
	t.Run("tier", func(t *testing.T) {
		_, dir := installedPublished(t)
		data, err := os.ReadFile(filepath.Join(dir, lockName))
		if err != nil {
			t.Fatal(err)
		}
		var lock Lock
		if err := json.Unmarshal(data, &lock); err != nil {
			t.Fatal(err)
		}
		lock.Entry.Tier = "forged-tier"
		data, err = json.Marshal(lock)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, lockName), data, 0o600); err != nil {
			t.Fatal(err)
		}
		if _, err := ValidateDirectory(dir); err == nil {
			t.Fatal("accepted modified signed metadata")
		}
	})
	t.Run("basename", func(t *testing.T) {
		_, dir := installedPublished(t)
		renamed := filepath.Join(filepath.Dir(dir), "another.pack@1.0.0")
		if err := os.Rename(dir, renamed); err != nil {
			t.Fatal(err)
		}
		if _, err := ValidateDirectory(renamed); err == nil {
			t.Fatal("accepted directory identity mismatch")
		}
	})
	t.Run("symlink ancestor", func(t *testing.T) {
		root, dir := installedPublished(t)
		packs := filepath.Dir(dir)
		moved := filepath.Join(t.TempDir(), "packs")
		if err := os.Rename(packs, moved); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink(moved, packs); err != nil {
			t.Skipf("symlink unavailable: %v", err)
		}
		if _, err := List(root); err == nil {
			t.Fatal("followed symlink installation ancestor")
		}
	})
}

func TestPinnedArchiveMustMatchPackYAMLIdentity(t *testing.T) {
	archive := archiveWithPadding(t, 0, nil)
	var e Entry
	e.Name = "other.pack"
	e.Version = "1.0.0"
	// Pin actual bytes so only the independent pack.yaml identity binding fails.
	e.Tarball.Size = int64(len(archive))
	sum := sha256.Sum256(archive)
	e.Tarball.SHA256 = hex.EncodeToString(sum[:])
	if _, err := verifiedArchive(e, archive); err == nil || !strings.Contains(err.Error(), "identity") {
		t.Fatalf("got %v", err)
	}
}
