//go:build darwin || linux

package packregistry

import (
	"os"
	"path/filepath"
	"syscall"
	"testing"
	"time"
)

func TestInstalledFIFORejectedWithoutBlocking(t *testing.T) {
	for _, file := range []string{lockName, archiveName, "pack.yaml"} {
		t.Run(file, func(t *testing.T) {
			_, dir := installedPublished(t)
			name := filepath.Join(dir, file)
			if err := os.Remove(name); err != nil {
				t.Fatal(err)
			}
			if err := syscall.Mkfifo(name, 0o600); err != nil {
				t.Fatal(err)
			}
			done := make(chan error, 1)
			go func() { _, err := ValidateDirectory(dir); done <- err }()
			select {
			case err := <-done:
				if err == nil {
					t.Fatal("accepted FIFO")
				}
			case <-time.After(2 * time.Second):
				t.Fatal("blocked opening FIFO evidence/content")
			}
		})
	}
}
