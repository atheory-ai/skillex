package linker

import (
	"path/filepath"
	"strings"

	"github.com/atheory-ai/skillex/internal/config"
	"github.com/atheory-ai/skillex/internal/scanner"
	"github.com/gobwas/glob"
)

// LinkedSkill is a skill with its resolved scope assignments.
type LinkedSkill struct {
	scanner.SkillFile
	// Scopes are the glob patterns this skill is visible under.
	Scopes []string
}

// Linker resolves scope and visibility for discovered skills.
type Linker struct {
	root string
	cfg  *config.Config
}

// New creates a new Linker.
func New(root string, cfg *config.Config) *Linker {
	return &Linker{root: root, cfg: cfg}
}

// Link takes scan results and produces linked skills with scope assignments.
func (l *Linker) Link(result *scanner.ScanResult) []LinkedSkill {
	var linked []LinkedSkill

	// Map from skill relPath -> scopes for repo-level skills
	repoSkillScopes := l.resolveRepoSkillScopes(result.RepoSkills)

	// Add repo-level skills
	for _, sf := range result.RepoSkills {
		if sf.IsTest {
			// Tests are linked via skill_tests, not as regular scoped skills
			linked = append(linked, LinkedSkill{SkillFile: sf, Scopes: []string{}})
			continue
		}
		scopes := repoSkillScopes[sf.RelPath]
		linked = append(linked, LinkedSkill{SkillFile: sf, Scopes: scopes})
	}

	// For dependency skills, determine scopes from the specific boundary that
	// resolved the package and merge duplicate discoveries by relPath.
	depScopes := l.resolveDepSkillScopes(result.DepSkills)
	depByPath := map[string]LinkedSkill{}
	for _, sf := range result.DepSkills {
		if existing, ok := depByPath[sf.RelPath]; ok {
			existing.Scopes = appendUnique(existing.Scopes, depScopes[sf.RelPath]...)
			depByPath[sf.RelPath] = existing
			continue
		}
		depByPath[sf.RelPath] = LinkedSkill{
			SkillFile: sf,
			Scopes:    append([]string(nil), depScopes[sf.RelPath]...),
		}
	}
	for _, sf := range result.DepSkills {
		ls, ok := depByPath[sf.RelPath]
		if !ok {
			continue
		}
		linked = append(linked, ls)
		delete(depByPath, sf.RelPath)
	}

	return linked
}

// resolveRepoSkillScopes maps each repo skill relPath to the scopes declared for it.
func (l *Linker) resolveRepoSkillScopes(skills []scanner.SkillFile) map[string][]string {
	result := map[string][]string{}

	// Build a quick lookup: relPath -> skill
	skillSet := map[string]bool{}
	for _, sf := range skills {
		skillSet[sf.RelPath] = true
	}

	// For each rule, assign its Skills to its Scope
	for _, rule := range l.cfg.Rules {
		scope := rule.Scope
		for _, skillRef := range rule.Skills {
			if skillSet[skillRef] {
				result[skillRef] = appendUnique(result[skillRef], scope)
			}
		}

		// If this rule has a DependencyBoundary, all public skills from that boundary
		// are scoped to rule.Scope. This is handled in resolveDepSkillScopes.
	}

	return result
}

// resolveDepSkillScopes determines scopes for dependency skills.
// Public skills are linked only to the scopes of the boundary that resolved
// them. Private skills are linked to their package install path.
func (l *Linker) resolveDepSkillScopes(skills []scanner.SkillFile) map[string][]string {
	result := map[string][]string{}

	// Collect boundary -> scope mappings from Rules
	boundaryScopes := map[string][]string{} // boundary relPath -> scopes
	for _, rule := range l.cfg.Rules {
		if rule.DependencyBoundary == "" {
			continue
		}
		boundaryScopes[rule.DependencyBoundary] = appendUnique(
			boundaryScopes[rule.DependencyBoundary], rule.Scope,
		)
	}

	for _, sf := range skills {
		if sf.IsTest {
			result[sf.RelPath] = []string{}
			continue
		}

		switch sf.Visibility {
		case "public":
			if sf.DependencyBoundary == "" {
				continue
			}
			result[sf.RelPath] = appendUnique(result[sf.RelPath], boundaryScopes[sf.DependencyBoundary]...)
		case "private":
			if sf.PackageRoot == "" {
				continue
			}
			pkgScope := filepath.ToSlash(filepath.Join(sf.PackageRoot, "**"))
			result[sf.RelPath] = appendUnique(result[sf.RelPath], pkgScope)
		}
	}

	return result
}

// MatchesPath returns true if the given working path matches the scope glob.
func MatchesPath(scope, workingPath string) bool {
	// Normalize separators
	scope = filepath.ToSlash(scope)
	workingPath = filepath.ToSlash(workingPath)

	// Handle ** glob
	if scope == "**" || scope == "**/**" {
		return true
	}

	g, err := glob.Compile(scope, '/')
	if err != nil {
		// Fallback to simple prefix match
		return strings.HasPrefix(workingPath, strings.TrimSuffix(scope, "/**"))
	}
	return g.Match(workingPath)
}

// ScopesForPath returns all scopes from the config that match the given path.
func ScopesForPath(cfg *config.Config, workingPath string) []string {
	var scopes []string
	for _, rule := range cfg.Rules {
		if MatchesPath(rule.Scope, workingPath) {
			scopes = appendUnique(scopes, rule.Scope)
		}
	}
	return scopes
}

func appendUnique(slice []string, items ...string) []string {
	seen := map[string]bool{}
	for _, s := range slice {
		seen[s] = true
	}
	for _, item := range items {
		if !seen[item] {
			seen[item] = true
			slice = append(slice, item)
		}
	}
	return slice
}
