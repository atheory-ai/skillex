package packregistry

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"golang.org/x/mod/semver"
)

// Update carries the two authenticated snapshots used for review and installation.
type Update struct {
	From   Lock
	To     *Prepared
	engine string
}

type renameLinks struct {
	Successor string `json:"superseded-by"`
	Replaces  string `json:"replaces"`
}

func (e CatalogEntry) renameLinks() (renameLinks, error) {
	var links renameLinks
	if e.metadata != nil {
		if err := json.Unmarshal(e.metadata, &links); err != nil {
			return links, fmt.Errorf("invalid rename declaration on %s: %w", identity(e.Entry), err)
		}
	}
	return links, nil
}

// Versions are comparable only within a name. Check the old name's selected
// release before following its declaration to a separately resolved successor.
func (m *Manifest) updateTarget(old Entry, engine string) (Entry, error) {
	source, err := m.resolveCandidate(old.Name, engine)
	if err != nil {
		return Entry{}, err
	}
	switch semver.Compare("v"+source.Version, "v"+old.Version) {
	case -1:
		return Entry{}, fmt.Errorf("refusing downgrade of %s from %s to %s", old.Name, old.Version, source.Version)
	case 0:
		if source.Version != old.Version || source.Tarball.SHA256 != old.Tarball.SHA256 || source.Tarball.Size != old.Tarball.Size {
			return Entry{}, fmt.Errorf("published identity changed for %s", identity(old))
		}
	}
	links, err := source.renameLinks()
	if err != nil || links.Successor == "" {
		return source.Entry, err
	}
	if links.Successor == source.Name {
		return Entry{}, fmt.Errorf("%s declares itself as its successor", source.Name)
	}
	target, err := m.resolveCandidate(links.Successor, engine)
	if err != nil {
		return Entry{}, fmt.Errorf("successor of %s: %w", source.Name, err)
	}
	inverse, err := target.renameLinks()
	if err != nil {
		return Entry{}, err
	}
	if inverse.Replaces != old.Name || inverse.Successor != "" {
		return Entry{}, fmt.Errorf("conflicting rename declarations: %s must replace %s directly", identity(target.Entry), old.Name)
	}
	return target.Entry, nil
}

func PlanUpdates(ctx context.Context, root, url, selector, engine string) ([]Update, error) {
	installed, err := List(root)
	if err != nil {
		return nil, err
	}
	name, version, exact := strings.Cut(selector, "@")
	installedNames := make(map[string]bool, len(installed))
	installedIDs := make(map[string]bool, len(installed))
	var selected []Lock
	seen := map[string]bool{}
	for _, old := range installed {
		installedIDs[identity(old.Entry)] = true
		installedNames[old.Entry.Name] = true
		if selector != "" && (old.Entry.Name != name || (exact && old.Entry.Version != version)) {
			continue
		}
		if seen[old.Entry.Name] {
			return nil, fmt.Errorf("multiple versions of %s installed: select name@version", old.Entry.Name)
		}
		seen[old.Entry.Name] = true
		selected = append(selected, old)
	}
	if len(selected) == 0 {
		if selector != "" {
			return nil, fmt.Errorf("pack is not installed: %s", selector)
		}
		return nil, nil
	}
	catalog, manifest, bundle, err := Fetch(ctx, url)
	if err != nil {
		return nil, err
	}
	var changes []Update
	for _, old := range selected {
		if old.Registry != catalog.Registry {
			return nil, fmt.Errorf("registry changed for %s", old.Entry.Name)
		}
		next, err := catalog.updateTarget(old.Entry, engine)
		if err != nil {
			return nil, err
		}
		if next.Name == old.Entry.Name && next.Version == old.Entry.Version {
			continue
		}
		if next.Name != old.Entry.Name && installedNames[next.Name] {
			return nil, fmt.Errorf("successor %s already installed", next.Name)
		}
		if installedIDs[identity(next)] {
			return nil, fmt.Errorf("%s already installed", identity(next))
		}
		prepared, err := prepareEntry(ctx, Lock{Entry: next, Registry: catalog.Registry, ManifestURL: url, Manifest: manifest, Bundle: bundle}, download)
		if err != nil {
			return nil, err
		}
		changes = append(changes, Update{From: old, To: prepared, engine: engine})
	}
	return changes, nil
}
