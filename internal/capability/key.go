package capability

import (
	"crypto/rand"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"time"
)

const referenceSigningKeyBytes = 32

// LoadOrCreateReferenceSigner loads a private local signing key or creates one
// with owner-only permissions. The key never enters the registry or a
// capability reference.
func LoadOrCreateReferenceSigner(path string, ttl time.Duration) (*ReferenceSigner, error) {
	key, err := loadOrCreateSigningKey(path)
	if err != nil {
		return nil, err
	}
	return NewReferenceSigner(key, ttl)
}

func loadOrCreateSigningKey(path string) ([]byte, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, fmt.Errorf("creating signing key directory: %w", err)
	}
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err == nil {
		abort := func(cause error) ([]byte, error) {
			_ = file.Close()
			_ = os.Remove(path)
			return nil, cause
		}
		key := make([]byte, referenceSigningKeyBytes)
		if _, err := io.ReadFull(rand.Reader, key); err != nil {
			return abort(fmt.Errorf("generating capability signing key: %w", err))
		}
		if _, err := file.Write(key); err != nil {
			return abort(fmt.Errorf("writing capability signing key: %w", err))
		}
		if err := file.Sync(); err != nil {
			return abort(fmt.Errorf("syncing capability signing key: %w", err))
		}
		if err := file.Close(); err != nil {
			return nil, err
		}
		return key, nil
	}
	if !errors.Is(err, os.ErrExist) {
		return nil, fmt.Errorf("creating capability signing key: %w", err)
	}
	info, err := os.Stat(path)
	if err != nil {
		return nil, fmt.Errorf("inspecting capability signing key: %w", err)
	}
	if runtime.GOOS != "windows" && info.Mode().Perm()&0o077 != 0 {
		return nil, fmt.Errorf("capability signing key %s must be owner-only (0600)", path)
	}
	key, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("reading capability signing key: %w", err)
	}
	if len(key) != referenceSigningKeyBytes {
		return nil, fmt.Errorf("capability signing key %s has %d bytes, want %d", path, len(key), referenceSigningKeyBytes)
	}
	return key, nil
}
