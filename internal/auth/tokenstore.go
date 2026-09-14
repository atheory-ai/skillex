package auth

import (
	"bytes"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

type TokenStore struct {
	KeyPath string
	Dir     string
}

type storedToken struct {
	AccessToken  string    `json:"access_token"`
	RefreshToken string    `json:"refresh_token,omitempty"`
	ExpiresAt    time.Time `json:"expires_at"`
	Scopes       string    `json:"scopes,omitempty"`
}

func (s TokenStore) Save(profile string, token Token) error {
	return s.seal(profile, ".token", storedToken{
		AccessToken: token.value, RefreshToken: token.refresh,
		ExpiresAt: token.expiresAt, Scopes: token.scopes,
	})
}

func (s TokenStore) Load(profile string) (Token, error) {
	var stored storedToken
	if err := s.open(profile, ".token", &stored); err != nil {
		return Token{}, err
	}
	return Token{value: stored.AccessToken, refresh: stored.RefreshToken, expiresAt: stored.ExpiresAt, scopes: stored.Scopes}, nil
}

func (s TokenStore) SavePending(profile string, pending PendingAuthorization) error {
	return s.seal(profile, ".pending", pending)
}

func (s TokenStore) LoadPending(profile string) (PendingAuthorization, error) {
	var pending PendingAuthorization
	err := s.open(profile, ".pending", &pending)
	return pending, err
}

func (s TokenStore) DeletePending(profile string) error {
	path, err := s.recordPath(profile, ".pending")
	if err != nil {
		return err
	}
	err = os.Remove(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	return err
}

func (s TokenStore) SaveRegistration(profile string, registration ClientRegistration) error {
	return s.seal(profile, ".registration", registration)
}

func (s TokenStore) LoadRegistration(profile string) (ClientRegistration, error) {
	var registration ClientRegistration
	err := s.open(profile, ".registration", &registration)
	return registration, err
}

func (s TokenStore) seal(profile, suffix string, value any) error {
	path, err := s.recordPath(profile, suffix)
	if err != nil {
		return err
	}
	key, err := loadOrCreateKey(s.KeyPath)
	if err != nil {
		return err
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return err
	}
	nonce := make([]byte, gcm.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return err
	}
	plain, err := json.Marshal(value)
	if err != nil {
		return err
	}
	sealed := gcm.Seal(nonce, nonce, plain, []byte(filepath.Base(path)))
	if err := os.MkdirAll(s.Dir, 0o700); err != nil {
		return err
	}
	return atomicSecretWrite(path, sealed)
}

func atomicSecretWrite(path string, data []byte) error {
	temporary, err := os.CreateTemp(filepath.Dir(path), ".oauth-*.tmp")
	if err != nil {
		return err
	}
	temporaryPath := temporary.Name()
	defer os.Remove(temporaryPath)
	if err := temporary.Chmod(0o600); err != nil {
		temporary.Close()
		return err
	}
	if _, err := io.Copy(temporary, bytes.NewReader(data)); err != nil {
		temporary.Close()
		return err
	}
	if err := temporary.Sync(); err != nil {
		temporary.Close()
		return err
	}
	if err := temporary.Close(); err != nil {
		return err
	}
	return os.Rename(temporaryPath, path)
}

func (s TokenStore) open(profile, suffix string, target any) error {
	path, err := s.recordPath(profile, suffix)
	if err != nil {
		return err
	}
	key, err := loadExistingKey(s.KeyPath)
	if err != nil {
		return err
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil || len(data) < gcm.NonceSize() {
		return errors.New("invalid encrypted OAuth token record")
	}
	plain, err := gcm.Open(nil, data[:gcm.NonceSize()], data[gcm.NonceSize():], []byte(filepath.Base(path)))
	if err != nil {
		return errors.New("invalid encrypted OAuth token record")
	}
	return json.Unmarshal(plain, target)
}

func (s TokenStore) recordPath(profile, suffix string) (string, error) {
	if !safeProfile(profile) || s.Dir == "" || s.KeyPath == "" {
		return "", errors.New("invalid OAuth token store configuration")
	}
	return filepath.Join(s.Dir, profile+suffix), nil
}

func loadOrCreateKey(path string) ([]byte, error) {
	if key, err := loadExistingKey(path); err == nil {
		return key, nil
	} else if !errors.Is(err, os.ErrNotExist) {
		return nil, err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, err
	}
	key := make([]byte, 32)
	if _, err := io.ReadFull(rand.Reader, key); err != nil {
		return nil, err
	}
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if errors.Is(err, os.ErrExist) {
		return loadExistingKey(path)
	}
	if err != nil {
		return nil, err
	}
	abort := func(err error) ([]byte, error) {
		_ = file.Close()
		_ = os.Remove(path)
		return nil, err
	}
	if _, err := file.Write(key); err != nil {
		return abort(err)
	}
	if err := file.Sync(); err != nil {
		return abort(err)
	}
	if err := file.Close(); err != nil {
		return nil, err
	}
	return key, nil
}

func loadExistingKey(path string) ([]byte, error) {
	info, err := os.Stat(path)
	if err != nil {
		return nil, err
	}
	if runtime.GOOS != "windows" && info.Mode().Perm()&0o077 != 0 {
		return nil, fmt.Errorf("OAuth token key must be owner-only")
	}
	key, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	if len(key) != 32 {
		return nil, errors.New("OAuth token key must contain 32 bytes")
	}
	return key, nil
}

func safeProfile(value string) bool {
	if value == "" || value == "." || value == ".." {
		return false
	}
	for _, r := range value {
		if !strings.ContainsRune("abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789-_.", r) {
			return false
		}
	}
	return true
}
