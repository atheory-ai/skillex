package packregistry

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"golang.org/x/mod/semver"
)

const checkFile = ".catalog-check.json"

// CatalogCheck preserves the signed bytes. CheckedAt is the local observation time.
type CatalogCheck struct {
	Manifest  []byte    `json:"signedManifest"`
	Bundle    []byte    `json:"signatureBundle"`
	CheckedAt time.Time `json:"checkedAt"`
	catalog   *Manifest
	published time.Time
	reasons   map[packVersion]string
}

func FetchCheck(ctx context.Context, url string) (*CatalogCheck, error) {
	m, raw, bundle, err := Fetch(ctx, url)
	if err != nil {
		return nil, err
	}
	c := &CatalogCheck{Manifest: raw, Bundle: bundle, CheckedAt: time.Now().UTC()}
	return c, c.parse(m)
}

func publication(data []byte) (time.Time, error) {
	var fields struct {
		GeneratedAt time.Time `json:"generatedAt"`
	}
	err := json.Unmarshal(data, &fields)
	return fields.GeneratedAt, err
}

func (c *CatalogCheck) parse(m *Manifest) error {
	t, err := publication(c.Manifest)
	if err != nil || t.IsZero() || m.Packs == nil || m.Revocations == nil || c.CheckedAt.IsZero() {
		return fmt.Errorf("incomplete signed catalog or check timestamp")
	}
	seen := make(map[packVersion]bool, len(m.Packs))
	for _, p := range m.Packs {
		key := packVersion{p.Name, p.Version}
		if seen[key] || !semver.IsValid("v"+p.Version) {
			return fmt.Errorf("invalid or duplicate catalog entry %s@%s", p.Name, p.Version)
		}
		seen[key] = true
		if semver.Prerelease("v"+p.Version) == "" {
			if err := validateEntry(p.Entry); err != nil {
				return err
			}
		}
	}
	c.catalog, c.published = m, t
	c.reasons = make(map[packVersion]string, len(m.Revocations))
	for _, r := range m.Revocations {
		key := packVersion{r.Name, r.Version}
		if !safeName.MatchString(r.Name) || !semver.IsValid("v"+r.Version) || strings.TrimSpace(r.Reason) == "" || c.reasons[key] != "" {
			return fmt.Errorf("invalid or duplicate signed revocation for %s@%s", r.Name, r.Version)
		}
		c.reasons[key] = r.Reason
	}
	return nil
}

func DecodeCheck(data []byte) (*CatalogCheck, error) {
	var c CatalogCheck
	if err := json.Unmarshal(data, &c); err != nil {
		return nil, err
	}
	m, err := authenticated(c.Manifest, c.Bundle)
	if err != nil {
		return nil, err
	}
	return &c, c.parse(m)
}

func savedCheck(parent *os.Root) (*CatalogCheck, error) {
	data, err := readRegular(parent, checkFile, 12<<20)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return DecodeCheck(data)
}

func SavedCheck(root string) (*CatalogCheck, error) {
	parent, err := installationParent(root, false)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	defer parent.Close()
	return savedCheck(parent)
}

func (c *CatalogCheck) acceptsSnapshot(registry string, raw []byte) error {
	if registry != c.catalog.Registry {
		return fmt.Errorf("signed catalog registry %q differs from saved registry %q", c.catalog.Registry, registry)
	}
	t, err := publication(raw)
	if err != nil {
		return err
	}
	if c.published.Before(t) || (t.Equal(c.published) && !bytes.Equal(raw, c.Manifest)) {
		return fmt.Errorf("stale or conflicting signed catalog")
	}
	var prior Manifest
	if err := json.Unmarshal(raw, &prior); err != nil {
		return err
	}
	for _, r := range prior.Revocations {
		if c.reasons[packVersion{r.Name, r.Version}] == "" {
			return fmt.Errorf("signed catalog omits prior revocation for %s@%s", r.Name, r.Version)
		}
	}
	return nil
}

func (c *CatalogCheck) Accepts(previous *CatalogCheck) error {
	if previous == nil {
		return nil
	}
	return c.acceptsSnapshot(previous.catalog.Registry, previous.Manifest)
}

// PublishCheck stages the portable evidence before committing the index. The
// index stores the same signed receipt so a committed check survives interruption
// between the SQLite commit and the final rename.
func (c *CatalogCheck) PublishCheck(root string, commit func([]Lock, []byte) error) error {
	return withMutationLock(root, true, func(parent *os.Root) error {
		previous, err := savedCheck(parent)
		if err != nil {
			return err
		}
		if err = c.Accepts(previous); err != nil {
			return err
		}
		locks, err := List(root)
		if err != nil {
			return err
		}
		for _, l := range locks {
			if err = c.acceptsSnapshot(l.Registry, l.Manifest); err != nil {
				return err
			}
		}
		data, err := json.Marshal(c)
		if err != nil {
			return err
		}
		temp := ".catalog-" + rand.Text()
		if err := writeNew(parent, temp, data); err != nil {
			return err
		}
		defer parent.Remove(temp) //nolint:errcheck // Rename consumes the temporary file on success.
		if err := commit(locks, data); err != nil {
			return err
		}
		if err := parent.Rename(temp, checkFile); err != nil {
			return fmt.Errorf("pack index committed, saving the portable check failed; retry pack check: %w", err)
		}
		return nil
	})
}

type PackStatus struct {
	Name               string `json:"name"`
	Version            string `json:"version"`
	Tier               string `json:"tier"`
	Registry           string `json:"registry"`
	Status             string `json:"status"`
	Evidence           string `json:"evidence"`
	CheckedAt          string `json:"checked_at,omitempty"`
	CatalogGeneratedAt string `json:"catalog_generated_at,omitempty"`
	Reason             string `json:"reason,omitempty"`
}

func (c *CatalogCheck) Status(locks []Lock) []PackStatus {
	out := make([]PackStatus, 0, len(locks))
	for _, l := range locks {
		e := l.Entry
		item := PackStatus{Name: e.Name, Version: e.Version, Tier: e.Tier, Registry: l.Registry, Status: "not_revoked", Evidence: "installation_snapshot"}
		t, _ := publication(l.Manifest) //nolint:errcheck // Older installation snapshots may lack a usable publication time.
		if c != nil && l.Registry == c.catalog.Registry {
			reason := c.reasons[packVersion{e.Name, e.Version}]
			if reason != "" || !t.After(c.published) {
				t = c.published
				item.Evidence = "registry_check"
				item.CheckedAt = c.CheckedAt.Format(time.RFC3339Nano)
				if reason != "" {
					item.Status = "revoked"
					item.Reason = reason
				}
			}
		}
		if !t.IsZero() {
			item.CatalogGeneratedAt = t.Format(time.RFC3339Nano)
		}
		out = append(out, item)
	}
	return out
}

func (c *CatalogCheck) AllowsInstall(lock Lock) error {
	if c != nil && c.catalog.Registry == lock.Registry {
		return c.catalog.CheckRevocation(lock.Entry)
	}
	return nil
}

// DisabledPaths matches both installed pack paths and repository symlinks to them.
type DisabledPaths map[string]bool

func (c *CatalogCheck) Disabled(root string, locks []Lock) DisabledPaths {
	var disabled DisabledPaths
	for _, e := range c.Status(locks) {
		if e.Status != "revoked" {
			continue
		}
		if disabled == nil {
			disabled = make(DisabledPaths)
		}
		dir := filepath.Join(root, ".skillex", "packs", e.Name+"@"+e.Version)
		disabled[filepath.Clean(dir)] = true
		if resolved, err := filepath.EvalSymlinks(dir); err == nil {
			disabled[resolved] = true
		}
	}
	return disabled
}

func (d DisabledPaths) Contains(path string) bool {
	if len(d) == 0 {
		return false
	}
	dir, _, ok := ResolvedInstalledFilePath(path)
	return ok && d[dir]
}
