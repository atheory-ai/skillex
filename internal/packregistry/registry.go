// Package packregistry consumes authenticated pack manifests and archives.
package packregistry

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"syscall"
	"time"

	"github.com/atheory-ai/skillex/internal/verify"
	"golang.org/x/mod/semver"
	"gopkg.in/yaml.v3"
)

const DefaultURL = "https://raw.githubusercontent.com/atheory-ai/skillex-packs/main/registry/manifest.json"
const maxDownload = 32 << 20
const maxExtracted = 64 << 20
const lockName = "manifest.lock.json"
const archiveName = "archive.tar.gz"

// InstalledFilePath classifies a normalized path under an installed pack cache.
// Callers still verify the directory before using any returned relative name.
func InstalledFilePath(file string) (string, string, bool) {
	absolute, err := filepath.Abs(file)
	if err != nil {
		return "", "", false
	}
	clean := filepath.ToSlash(filepath.Clean(absolute))
	marker := "/.skillex/packs/"
	index := strings.Index(clean, marker)
	if index < 0 {
		return "", "", false
	}
	start := index + len(marker)
	rest := clean[start:]
	boundary := strings.Index(rest, "/")
	if boundary < 0 {
		return "", "", false
	}
	return filepath.FromSlash(clean[:start+boundary]), rest[boundary+1:], true
}

type Manifest struct {
	SchemaVersion int          `json:"schemaVersion"`
	Registry      string       `json:"registry"`
	Packs         []Entry      `json:"packs"`
	Revocations   []Revocation `json:"revocations"`
}
type Entry struct {
	Name        string `json:"name"`
	Handle      string `json:"handle"`
	Tier        string `json:"tier"`
	Version     string `json:"version"`
	Description string `json:"description"`
	Tarball     struct {
		URL    string `json:"url"`
		SHA256 string `json:"sha256"`
		Size   int64  `json:"size"`
	} `json:"tarball"`
}
type Revocation struct {
	Name    string `json:"name"`
	Version string `json:"version"`
	Reason  string `json:"reason"`
}
type Lock struct {
	// VerifiedFiles is an archive-derived authenticated snapshot, never serialized.
	VerifiedFiles map[string][]byte `json:"-"`
	Entry         Entry             `json:"pack"`
	Registry      string            `json:"registry"`
	ManifestURL   string            `json:"manifestURL"`
	Manifest      []byte            `json:"signedManifest"`
	Bundle        []byte            `json:"signatureBundle"`
}
type Prepared struct {
	Lock    Lock
	Archive []byte
	Files   map[string][]byte
}

// Fetch verifies bytes before interpreting any manifest-controlled metadata.
func Fetch(ctx context.Context, manifestURL string) (*Manifest, []byte, []byte, error) {
	return fetchWithVerifier(ctx, manifestURL, verify.Manifest, download)
}

func fetchWithVerifier(ctx context.Context, manifestURL string, verifier manifestVerifier, fetch registryDownload) (*Manifest, []byte, []byte, error) {
	data, err := fetch(ctx, manifestURL, 4<<20)
	if err != nil {
		return nil, nil, nil, err
	}
	envelope, err := fetch(ctx, manifestURL+".bundle", 4<<20)
	if err != nil {
		return nil, nil, nil, err
	}
	m, err := authenticatedWithVerifier(data, envelope, verifier)
	return m, data, envelope, err
}

// Package-local seam for SDK-authentic test fixtures; production always uses
// the compiled-in verification policy, with no environment or config override.
type manifestVerifier func([]byte, []byte) error
type registryDownload func(context.Context, string, int64) ([]byte, error)

func authenticated(data, envelope []byte) (*Manifest, error) {
	return authenticatedWithVerifier(data, envelope, verify.Manifest)
}

func authenticatedWithVerifier(data, envelope []byte, verifier manifestVerifier) (*Manifest, error) {
	if err := verifier(data, envelope); err != nil {
		return nil, err
	}
	var m Manifest
	if err := json.Unmarshal(data, &m); err != nil {
		return nil, err
	}
	if m.SchemaVersion != 1 || m.Registry == "" {
		return nil, fmt.Errorf("unsupported registry manifest")
	}
	return &m, nil
}

var safeName = regexp.MustCompile(`^[a-z0-9][a-z0-9.-]*$`)

func (m *Manifest) Resolve(name string) (Entry, error) {
	var candidates []Entry
	seenVersions := map[string]bool{}
	for _, e := range m.Packs {
		if e.Name == name && semver.IsValid("v"+e.Version) && semver.Prerelease("v"+e.Version) == "" {
			if seenVersions[e.Version] {
				return Entry{}, fmt.Errorf("ambiguous signed pack identity %s@%s", e.Name, e.Version)
			}
			seenVersions[e.Version] = true
			candidates = append(candidates, e)
		}
	}
	if len(candidates) == 0 {
		return Entry{}, fmt.Errorf("no stable pack %q in signed manifest", name)
	}
	sort.Slice(candidates, func(i, j int) bool { return semver.Compare("v"+candidates[i].Version, "v"+candidates[j].Version) > 0 })
	e := candidates[0]
	if err := validateEntry(e); err != nil {
		return Entry{}, err
	}
	if err := m.CheckRevocation(e); err != nil {
		return Entry{}, err
	}
	return e, nil
}
func validateEntry(e Entry) error {
	if !safeName.MatchString(e.Name) || !safeName.MatchString(e.Handle) || !strings.HasPrefix(e.Name, e.Handle+".") || !semver.IsValid("v"+e.Version) || semver.Prerelease("v"+e.Version) != "" {
		return fmt.Errorf("unsafe pack identity or inconsistent handle")
	}
	if digest, err := hex.DecodeString(e.Tarball.SHA256); err != nil || len(digest) != 32 || e.Tarball.SHA256 != strings.ToLower(e.Tarball.SHA256) || e.Tarball.Size <= 0 || e.Tarball.Size > maxDownload {
		return fmt.Errorf("invalid signed tarball pin")
	}
	return nil
}

func (m *Manifest) CheckRevocation(e Entry) error {
	for _, r := range m.Revocations {
		if r.Name == e.Name && r.Version == e.Version {
			return fmt.Errorf("pack %s@%s revoked: %s", e.Name, e.Version, r.Reason)
		}
	}
	return nil
}

func Prepare(ctx context.Context, manifestURL, name string) (*Prepared, error) {
	return prepareWithVerifier(ctx, manifestURL, name, verify.Manifest, download)
}

func prepareWithVerifier(ctx context.Context, manifestURL, name string, verifier manifestVerifier, fetch registryDownload) (*Prepared, error) {
	m, data, envelope, err := fetchWithVerifier(ctx, manifestURL, verifier, fetch)
	if err != nil {
		return nil, err
	}
	e, err := m.Resolve(name)
	if err != nil {
		return nil, err
	}
	archive, err := fetch(ctx, e.Tarball.URL, maxDownload)
	if err != nil {
		return nil, err
	}
	files, err := verifiedArchive(e, archive)
	if err != nil {
		return nil, err
	}
	return &Prepared{Lock: Lock{Entry: e, Registry: m.Registry, ManifestURL: manifestURL, Manifest: data, Bundle: envelope}, Archive: archive, Files: files}, nil
}
func verifiedArchive(e Entry, data []byte) (map[string][]byte, error) {
	digest := sha256.Sum256(data)
	if int64(len(data)) != e.Tarball.Size || hex.EncodeToString(digest[:]) != e.Tarball.SHA256 {
		return nil, fmt.Errorf("tarball SHA256 or size mismatch")
	}
	files, err := Extract(data)
	if err != nil {
		return nil, err
	}
	var identity struct {
		Name    string `yaml:"name"`
		Version string `yaml:"version"`
	}
	if err := yaml.Unmarshal(files["pack.yaml"], &identity); err != nil {
		return nil, err
	}
	if identity.Name != e.Name || identity.Version != e.Version {
		return nil, fmt.Errorf("pack.yaml identity differs from signed manifest")
	}
	return files, nil
}

// Extract rejects every archive entry that could escape or execute. It returns
// an in-memory tree only after the entire archive passes validation.
func Extract(data []byte) (map[string][]byte, error) {
	gz, err := gzip.NewReader(bytes.NewReader(data))
	if err != nil {
		return nil, err
	}
	defer gz.Close()
	// Bound all expansion, including metadata, skipped payloads, and trailing padding.
	expanded := &io.LimitedReader{R: gz, N: maxExtracted + 1}
	tr := tar.NewReader(expanded)
	files := map[string][]byte{}
	directories := map[string]struct{}{}
	var total int64
	for count := 0; ; count++ {
		h, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, err
		}
		if count >= 4096 {
			return nil, fmt.Errorf("too many archive entries")
		}
		if h.Typeflag == tar.TypeDir && h.Size != 0 {
			return nil, fmt.Errorf("archive directory has nonzero size")
		}
		name := strings.TrimSuffix(h.Name, "/")
		if name == "" || name == "." {
			if h.Typeflag == tar.TypeDir {
				continue
			}
			return nil, fmt.Errorf("empty archive path")
		}
		if path.IsAbs(name) || path.Clean(name) != name || strings.ContainsAny(name, "\\:\x00") || name == ".." || strings.HasPrefix(name, "../") {
			return nil, fmt.Errorf("unsafe archive path %q", h.Name)
		}
		if h.Typeflag == tar.TypeDir {
			directories[name] = struct{}{}
			continue
		}
		if h.Typeflag != tar.TypeReg && h.Typeflag != 0 {
			return nil, fmt.Errorf("archive links and special entries are forbidden: %s", name)
		}
		switch path.Ext(name) {
		case ".md", ".yaml", ".yml", ".json", ".txt":
		default:
			return nil, fmt.Errorf("archive extension forbidden: %s", name)
		}
		if name == lockName || name == archiveName {
			return nil, fmt.Errorf("archive contains reserved file %s", name)
		}
		if _, ok := files[name]; ok {
			return nil, fmt.Errorf("duplicate archive entry %s", name)
		}
		total += h.Size
		if h.Size < 0 || total > maxExtracted {
			return nil, fmt.Errorf("archive exceeds extraction limit")
		}
		body, err := io.ReadAll(io.LimitReader(tr, h.Size+1))
		if err != nil {
			return nil, err
		}
		if int64(len(body)) != h.Size {
			return nil, fmt.Errorf("truncated archive file")
		}
		files[name] = body
	}
	// tar EOF is not gzip EOF: drain within the same bound to verify its footer.
	padding := make([]byte, 32<<10)
	for {
		n, err := expanded.Read(padding)
		for _, value := range padding[:n] {
			if value != 0 {
				return nil, fmt.Errorf("non-padding bytes after tar EOF")
			}
		}
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("invalid gzip stream: %w", err)
		}
	}
	if expanded.N == 0 {
		return nil, fmt.Errorf("archive exceeds full expansion limit")
	}
	for name := range files {
		if _, exists := directories[name]; exists {
			return nil, fmt.Errorf("archive file conflicts with directory %s", name)
		}
		for parent := path.Dir(name); parent != "."; parent = path.Dir(parent) {
			if _, exists := files[parent]; exists {
				return nil, fmt.Errorf("archive file blocks descendant path %s", name)
			}
		}
	}
	for name := range directories {
		for parent := name; parent != "."; parent = path.Dir(parent) {
			if _, exists := files[parent]; exists {
				return nil, fmt.Errorf("archive file blocks directory path %s", name)
			}
		}
	}
	if _, ok := files["pack.yaml"]; !ok {
		return nil, fmt.Errorf("archive has no root pack.yaml")
	}
	return files, nil
}

// WriteTree writes a previously verified tree into a fresh staging directory.
func (p *Prepared) WriteTree(dir string) error {
	scoped, err := os.OpenRoot(dir)
	if err != nil {
		return err
	}
	defer scoped.Close()
	return writeTree(scoped, p.Files)
}
func writeTree(scoped *os.Root, files map[string][]byte) error {
	for name, data := range files {
		if err := scoped.MkdirAll(filepath.Dir(filepath.FromSlash(name)), 0o700); err != nil {
			return err
		}
		if err := writeNew(scoped, filepath.FromSlash(name), data); err != nil {
			return err
		}
	}
	return nil
}
func writeNew(scoped *os.Root, name string, data []byte) error {
	f, err := scoped.OpenFile(name, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return err
	}
	_, writeErr := f.Write(data)
	closeErr := f.Close()
	if writeErr != nil {
		return writeErr
	}
	return closeErr
}

// installationParent pins each directory handle and prevents symlink escapes,
// including an ancestor swapped concurrently after the initial lstat check.
func installationParent(root string, create bool) (*os.Root, error) {
	current, err := os.OpenRoot(root)
	if err != nil {
		return nil, err
	}
	for _, name := range []string{".skillex", "packs"} {
		if create {
			if err := current.Mkdir(name, 0o700); err != nil && !os.IsExist(err) {
				current.Close()
				return nil, err
			}
		}
		info, err := current.Lstat(name)
		if err != nil {
			current.Close()
			return nil, err
		}
		if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
			current.Close()
			return nil, fmt.Errorf("unsafe installation directory %s", name)
		}
		next, err := current.OpenRoot(name)
		current.Close()
		if err != nil {
			return nil, err
		}
		current = next
	}
	return current, nil
}

// Install atomically publishes a verified tree; existing installs are not overwritten.
func (p *Prepared) Install(root string) error {
	if err := validateEntry(p.Lock.Entry); err != nil {
		return err
	}
	// Re-derive the tree from its pinned bytes rather than trusting mutable preview data.
	files, err := verifiedArchive(p.Lock.Entry, p.Archive)
	if err != nil {
		return err
	}
	parent, err := installationParent(root, true)
	if err != nil {
		return err
	}
	defer parent.Close()
	target := p.Lock.Entry.Name + "@" + p.Lock.Entry.Version
	if _, err := parent.Lstat(target); err == nil {
		return fmt.Errorf("pack already installed: %s", target)
	} else if !os.IsNotExist(err) {
		return err
	}
	stage := ".install-" + rand.Text()
	if err := parent.Mkdir(stage, 0o700); err != nil {
		return err
	}
	defer parent.RemoveAll(stage) //nolint:errcheck // Best-effort cleanup of an unpublished staging directory.
	scoped, err := parent.OpenRoot(stage)
	if err != nil {
		return err
	}
	defer scoped.Close()
	if err := writeTree(scoped, files); err != nil {
		return err
	}
	lock, err := json.MarshalIndent(p.Lock, "", "  ")
	if err != nil {
		return err
	}
	if err := writeNew(scoped, lockName, lock); err != nil {
		return err
	}
	if err := writeNew(scoped, archiveName, p.Archive); err != nil {
		return err
	}
	// A valid existing install is nonempty, so a competing install cannot replace it.
	return parent.Rename(stage, target)
}

func readRegular(scoped *os.Root, name string, limit int64) ([]byte, error) {
	info, err := scoped.Lstat(name)
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() || info.Size() > limit {
		return nil, fmt.Errorf("not a bounded regular file: %s", name)
	}
	// Nonblocking open avoids hanging if a local writer replaces the file with a FIFO.
	f, err := scoped.OpenFile(name, os.O_RDONLY|syscall.O_NONBLOCK, 0)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	opened, err := f.Stat()
	if err != nil {
		return nil, err
	}
	if !opened.Mode().IsRegular() || !os.SameFile(info, opened) || opened.Size() > limit {
		return nil, fmt.Errorf("installed file changed or is unsafe: %s", name)
	}
	data, err := io.ReadAll(io.LimitReader(f, limit+1))
	if err != nil {
		return nil, err
	}
	if int64(len(data)) > limit {
		return nil, fmt.Errorf("oversized installed file: %s", name)
	}
	return data, nil
}

// ValidateDirectory re-verifies signed evidence and derives the expected file
// bytes from the SHA-pinned saved archive, so changing lock digests cannot forge content.
func ValidateDirectory(dir string) (Lock, error) {
	return validateDirectoryWithVerifier(dir, verify.Manifest)
}

func validateDirectoryWithVerifier(dir string, verifier manifestVerifier) (Lock, error) {
	// Installs must live below the engine-owned project cache, never through links.
	packs := filepath.Dir(filepath.Clean(dir))
	skillex := filepath.Dir(packs)
	if filepath.Base(packs) != "packs" || filepath.Base(skillex) != ".skillex" {
		return Lock{}, fmt.Errorf("invalid installed pack directory")
	}
	parent, err := installationParent(filepath.Dir(skillex), false)
	if err != nil {
		return Lock{}, err
	}
	defer parent.Close()
	return validateChildWithVerifier(parent, filepath.Base(dir), verifier)
}
func validateChildWithVerifier(parent *os.Root, name string, verifier manifestVerifier) (Lock, error) {
	var lock Lock
	info, err := parent.Lstat(name)
	if err != nil {
		return lock, err
	}
	if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return lock, fmt.Errorf("unsafe installed pack directory")
	}
	scoped, err := parent.OpenRoot(name)
	if err != nil {
		return lock, err
	}
	defer scoped.Close()
	data, err := readRegular(scoped, lockName, 12<<20)
	if err != nil {
		return lock, err
	}
	if err := json.Unmarshal(data, &lock); err != nil {
		return lock, err
	}
	m, err := authenticatedWithVerifier(lock.Manifest, lock.Bundle, verifier)
	if err != nil {
		return lock, err
	}
	var signed *Entry
	for _, e := range m.Packs {
		if e.Name == lock.Entry.Name && e.Version == lock.Entry.Version {
			if signed != nil {
				return lock, fmt.Errorf("ambiguous signed pack identity")
			}
			candidate := e
			signed = &candidate
		}
	}
	if signed == nil || *signed != lock.Entry || lock.Registry != m.Registry {
		return lock, fmt.Errorf("pack lock differs from signed manifest")
	}
	if err := validateEntry(*signed); err != nil {
		return lock, err
	}
	if name != signed.Name+"@"+signed.Version {
		return lock, fmt.Errorf("installed directory differs from signed identity")
	}
	if err := m.CheckRevocation(*signed); err != nil {
		return lock, err
	}
	archive, err := readRegular(scoped, archiveName, maxDownload)
	if err != nil {
		return lock, err
	}
	files, err := verifiedArchive(*signed, archive)
	if err != nil {
		return lock, err
	}
	verifiedFiles := make(map[string][]byte, len(files))
	for name, body := range files {
		verifiedFiles[name] = body
	}
	count := 0
	err = fs.WalkDir(scoped.FS(), ".", func(file string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		count++
		if count > 8192 {
			return fmt.Errorf("too many installed entries")
		}
		if entry.Type()&os.ModeSymlink != 0 {
			return fmt.Errorf("installed pack contains symlink")
		}
		if entry.IsDir() {
			return nil
		}
		if !entry.Type().IsRegular() {
			return fmt.Errorf("installed pack contains special file")
		}
		if file == lockName || file == archiveName {
			return nil
		}
		expected, ok := files[file]
		if !ok {
			return fmt.Errorf("unexpected installed file %s", file)
		}
		actual, err := readRegular(scoped, filepath.FromSlash(file), int64(len(expected)))
		if err != nil {
			return err
		}
		if !bytes.Equal(actual, expected) {
			return fmt.Errorf("installed pack tampered: %s", file)
		}
		delete(files, file)
		return nil
	})
	if err == nil && len(files) > 0 {
		err = fmt.Errorf("installed pack files are missing")
	}
	if err == nil {
		lock.VerifiedFiles = verifiedFiles
	}
	return lock, err
}

func List(root string) ([]Lock, error) {
	return listWithVerifier(root, verify.Manifest)
}

func listWithVerifier(root string, verifier manifestVerifier) ([]Lock, error) {
	parent, err := installationParent(root, false)
	if os.IsNotExist(err) {
		return []Lock{}, nil
	}
	if err != nil {
		return nil, err
	}
	defer parent.Close()
	directory, err := parent.Open(".")
	if err != nil {
		return nil, err
	}
	defer directory.Close()
	entries, err := directory.ReadDir(-1)
	if err != nil {
		return nil, err
	}
	var result []Lock
	for _, entry := range entries {
		if strings.HasPrefix(entry.Name(), ".") {
			continue
		}
		if !entry.IsDir() || entry.Type()&os.ModeSymlink != 0 {
			return nil, fmt.Errorf("unexpected installed pack entry")
		}
		lock, err := validateChildWithVerifier(parent, entry.Name(), verifier)
		if err != nil {
			return nil, err
		}
		result = append(result, lock)
	}
	sort.Slice(result, func(i, j int) bool {
		return result[i].Entry.Name+"@"+result[i].Entry.Version < result[j].Entry.Name+"@"+result[j].Entry.Version
	})
	return result, nil
}

func download(ctx context.Context, address string, limit int64) ([]byte, error) {
	client := &http.Client{Timeout: 30 * time.Second, CheckRedirect: func(req *http.Request, via []*http.Request) error {
		if len(via) >= 10 || req.URL.Scheme != "https" {
			return fmt.Errorf("unsafe download redirect")
		}
		return nil
	}}
	return downloadWithClient(ctx, address, limit, client)
}

func downloadWithClient(ctx context.Context, address string, limit int64, client *http.Client) ([]byte, error) {
	u, err := url.Parse(address)
	if err != nil || u.Scheme != "https" || u.Host == "" || u.User != nil {
		return nil, fmt.Errorf("registry downloads require HTTPS")
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, address, nil)
	if err != nil {
		return nil, err
	}

	resp, err := client.Do(request)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("download HTTP %d", resp.StatusCode)
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, limit+1))
	if err != nil {
		return nil, err
	}
	if int64(len(data)) > limit {
		return nil, fmt.Errorf("download exceeds size limit")
	}
	return data, nil
}
