package packregistry

import (
	"context"
	"fmt"
	"strings"

	"golang.org/x/mod/semver"
)

// Update carries the two authenticated snapshots used for review and installation.
type Update struct {
	From Lock
	To   *Prepared
}

func PlanUpdates(ctx context.Context, root, url, selector string) ([]Update, error) {
	installed, err := List(root)
	if err != nil {
		return nil, err
	}
	name, version, exact := strings.Cut(selector, "@")
	installedIDs := make(map[string]bool, len(installed))
	var selected []Lock
	seen := map[string]bool{}
	for _, old := range installed {
		installedIDs[identity(old.Entry)] = true
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
		next, err := catalog.Resolve(old.Entry.Name)
		if err != nil {
			return nil, err
		}
		switch semver.Compare("v"+next.Version, "v"+old.Entry.Version) {
		case -1:
			return nil, fmt.Errorf("refusing downgrade of %s from %s to %s", next.Name, old.Entry.Version, next.Version)
		case 0:
			if next.Version != old.Entry.Version || next.Tarball.SHA256 != old.Entry.Tarball.SHA256 || next.Tarball.Size != old.Entry.Tarball.Size {
				return nil, fmt.Errorf("published identity changed for %s@%s", next.Name, next.Version)
			}
			continue
		}
		if installedIDs[identity(next)] {
			return nil, fmt.Errorf("%s already installed", identity(next))
		}
		prepared, err := prepareEntry(ctx, Lock{Entry: next, Registry: catalog.Registry, ManifestURL: url, Manifest: manifest, Bundle: bundle}, download)
		if err != nil {
			return nil, err
		}
		changes = append(changes, Update{From: old, To: prepared})
	}
	return changes, nil
}
