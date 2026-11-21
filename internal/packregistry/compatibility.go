package packregistry

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/Masterminds/semver/v3"
)

var errUnavailable = errors.New("pack unavailable")

// selectVersion receives stable candidates in descending version order.
// Preserve the existing withdrawal policy: a revoked latest release blocks
// the pack, and older withdrawn versions cannot be selected as fallbacks.
func (m *Manifest) selectVersion(versions []CatalogEntry, engine string, revoked map[packVersion]bool) (CatalogEntry, error) {
	latest := versions[0]
	if revoked[packVersion{latest.Name, latest.Version}] {
		return CatalogEntry{}, fmt.Errorf("%w: %w", errUnavailable, m.CheckRevocation(latest.Entry))
	}
	// make dev-binary appends -dev to a numeric checkout version. Preserve
	// the actual prerelease identifiers on other versioned builds.
	build := strings.TrimPrefix(engine, "v")
	if base, ok := strings.CutSuffix(build, "-dev"); ok && !strings.ContainsAny(base, "-+") {
		build = base
	}
	running, versionErr := semver.StrictNewVersion(build)
	var latestRange string
	for i, candidate := range versions {
		if revoked[packVersion{candidate.Name, candidate.Version}] {
			continue
		}
		required, err := candidate.engineRange()
		if err != nil {
			return CatalogEntry{}, fmt.Errorf("%s@%s: %w", candidate.Name, candidate.Version, err)
		}
		if required != "" {
			if i == 0 {
				latestRange = required
			}
			constraint, err := semver.NewConstraint(required)
			if err != nil {
				return CatalogEntry{}, fmt.Errorf("%s@%s: invalid compatibility.skillex range %q: %w", candidate.Name, candidate.Version, required, err)
			}
			if versionErr != nil {
				return CatalogEntry{}, fmt.Errorf("cannot check %s@%s against compatibility.skillex %q with unknown engine version %q; use a versioned Skillex build", candidate.Name, candidate.Version, required, engine)
			}
			if !constraint.Check(running) {
				continue
			}
		}
		return candidate, nil
	}
	return CatalogEntry{}, fmt.Errorf("%w: no compatible stable version of %s for Skillex %s (latest %s requires %q)", errUnavailable, latest.Name, engine, latest.Version, latestRange)
}

func (e CatalogEntry) engineRange() (string, error) {
	if e.metadata == nil {
		return "", nil
	}
	var fields struct {
		Compatibility struct{ Skillex json.RawMessage }
	}
	if err := json.Unmarshal(e.metadata, &fields); err != nil {
		return "", err
	}
	if fields.Compatibility.Skillex == nil {
		return "", nil
	}
	var required string
	if err := json.Unmarshal(fields.Compatibility.Skillex, &required); err != nil || strings.TrimSpace(required) == "" {
		return "", fmt.Errorf("compatibility.skillex must be a nonempty range string")
	}
	return required, nil
}
