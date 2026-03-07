package linker

import (
	"path/filepath"
	"strings"

	"github.com/gobwas/glob"
	"github.com/ladyhunterbear/skillex/internal/config"
	"github.com/ladyhunterbear/skillex/internal/scanner"
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

	// For dependency skills, determine scopes based on which rules reference
	// their boundary, and apply visibility rules.
	depScopes := l.resolveDepSkillScopes(result.DepSkills)
	for _, sf := range result.DepSkills {
		scopes := depScopes[sf.RelPath]
		linked = append(linked, LinkedSkill{SkillFile: sf, Scopes: scopes})
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
// Public skills are linked for scopes that declare a DependencyBoundary.
// Private skills are linked for paths inside their package.
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
			// Public skills are available in all scopes that declared a boundary
			// that resolves to the skill's package.
			// Since we don't know which boundary resolves which package here,
			// we attach all boundary scopes for simplicity.
			// A more precise implementation would track boundary->package mapping.
			for _, scopes := range boundaryScopes {
				result[sf.RelPath] = appendUnique(result[sf.RelPath], scopes...)
			}
		case "private":
			// Private skills apply when the working path is inside the package root.
			// We model this as a scope glob matching the package's install location.
			// For the registry, we store the package path as a scope.
			pkgScope := filepath.Join("node_modules", sf.PackageName) + "/**"
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
