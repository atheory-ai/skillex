package packregistry

import (
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"slices"
	"sort"
	"strings"

	"github.com/atheory-ai/skillex/internal/config"
	"github.com/atheory-ai/skillex/internal/verify"
	"golang.org/x/mod/semver"
)

// CatalogEntry extends the installed lock's identity with producer declarations.
type CatalogEntry struct {
	Entry
	metadata json.RawMessage
}

// Preserve the producer's complete signed entry, including optional and future fields.
func (e *CatalogEntry) UnmarshalJSON(data []byte) error {
	if err := json.Unmarshal(data, &e.Entry); err != nil {
		return err
	}
	e.metadata = append(json.RawMessage(nil), data...)
	return nil
}

func ManifestURL(cfg *config.Config) string {
	if cfg != nil && len(cfg.Registries) > 0 {
		return cfg.Registries[0].URL
	}
	return DefaultURL
}

// stableVersions returns signed candidates newest first, rejecting duplicate identities.
func stableVersions(entries []CatalogEntry, name string) ([]CatalogEntry, error) {
	var versions []CatalogEntry
	seen := make(map[string]bool)
	for _, e := range entries {
		v := "v" + e.Version
		if e.Name != name || !semver.IsValid(v) || semver.Prerelease(v) != "" {
			continue
		}
		if seen[e.Version] {
			return nil, fmt.Errorf("ambiguous signed pack identity %s@%s", name, e.Version)
		}
		seen[e.Version] = true
		versions = append(versions, e)
	}
	sort.SliceStable(versions, func(i, j int) bool {
		return semver.Compare("v"+versions[i].Version, "v"+versions[j].Version) > 0
	})
	return versions, nil
}

type PackInfo struct {
	Registry          string            `json:"registry"`
	ManifestURL       string            `json:"manifest_url"`
	ManifestBundleURL string            `json:"manifest_bundle_url"`
	AttestationLookup map[string]string `json:"attestation_lookup,omitempty"`
	ManifestSigner    string            `json:"manifest_signer"`
	Versions          []json.RawMessage `json:"versions"`
}

type packVersion struct{ name, version string }

func (m *Manifest) revokedVersions() map[packVersion]bool {
	revoked := make(map[packVersion]bool, len(m.Revocations))
	for _, r := range m.Revocations {
		revoked[packVersion{r.Name, r.Version}] = true
	}
	return revoked
}

// Info describes non-revoked stable releases advertised by the signed catalog.
func (m *Manifest) Info(url, name string) (*PackInfo, error) {
	versions, err := stableVersions(m.Packs, name)
	if err != nil {
		return nil, err
	}
	if len(versions) == 0 {
		return nil, fmt.Errorf("no stable pack %q in signed manifest", name)
	}
	if err := m.CheckRevocation(versions[0].Entry); err != nil {
		return nil, err
	}
	info := &PackInfo{Registry: m.Registry, ManifestURL: url, ManifestBundleURL: url + ".bundle", ManifestSigner: verify.ManifestIdentity}
	if m.Registry == "atheory-ai/skillex-packs" {
		info.AttestationLookup = make(map[string]string)
	}
	revoked := m.revokedVersions()
	for _, v := range versions {
		if revoked[packVersion{v.Name, v.Version}] {
			continue
		}
		if err := validateEntry(v.Entry); err != nil {
			return nil, err
		}
		info.Versions = append(info.Versions, v.metadata)
		if info.AttestationLookup != nil {
			info.AttestationLookup[v.Version] = "https://api.github.com/repos/atheory-ai/skillex-packs/attestations/sha256:" + v.Tarball.SHA256
		}
	}
	return info, nil
}

const DefaultSearchLimit = 10
const MaxSearchLimit = 50

type SearchResult struct {
	Name        string `json:"name"`
	Version     string `json:"version"`
	Tier        string `json:"tier"`
	Description string `json:"description"`
}

type SearchPage struct {
	Registry   string         `json:"registry"`
	Results    []SearchResult `json:"results"`
	Total      int            `json:"total"`
	NextOffset *int           `json:"next_offset,omitempty"`
}

// Search returns one summary per matching pack that get can resolve.
func (m *Manifest) Search(search, tier string, limit, offset int, engine string) (*SearchPage, error) {
	if limit < 1 || limit > MaxSearchLimit || offset < 0 {
		return nil, fmt.Errorf("limit must be 1–%d and offset must be non-negative", MaxSearchLimit)
	}
	grouped := make(map[string][]CatalogEntry)
	for _, e := range m.Packs {
		grouped[e.Name] = append(grouped[e.Name], e)
	}
	tokens := strings.Fields(strings.ToLower(search))
	page := &SearchPage{Registry: m.Registry, Results: []SearchResult{}}
	revoked := m.revokedVersions()
	for _, name := range slices.Sorted(maps.Keys(grouped)) {
		versions, err := stableVersions(grouped[name], name)
		if err != nil {
			return nil, err
		}
		if len(versions) == 0 {
			continue
		}
		e, err := m.selectVersion(versions, engine, revoked)
		if errors.Is(err, errUnavailable) {
			continue
		}
		if err != nil {
			return nil, err
		}
		if tier != "" && tier != e.Tier {
			continue
		}
		haystack := strings.ToLower(name + " " + e.Description)
		matches := true
		for _, token := range tokens {
			if !strings.Contains(haystack, token) {
				matches = false
				break
			}
		}
		if !matches {
			continue
		}
		if err := validateEntry(e.Entry); err != nil {
			return nil, err
		}
		page.Total++
		if page.Total <= offset || len(page.Results) == limit {
			continue
		}
		description := []rune(e.Description)
		if len(description) > 240 {
			description = append(description[:239], '…')
		}
		page.Results = append(page.Results, SearchResult{Name: name, Version: e.Version, Tier: e.Tier, Description: string(description)})
	}
	if offset < page.Total-len(page.Results) {
		next := offset + len(page.Results)
		page.NextOffset = &next
	}
	return page, nil
}
