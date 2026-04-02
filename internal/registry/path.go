package registry

import (
	"database/sql"
	"fmt"
	"strings"

	"github.com/gobwas/glob"
)

// classifyScope decomposes a scope pattern into a pattern_type and path_prefix
// for efficient SQL-level path filtering.
//
//	"**"                → ("universal", "")
//	"packages/ui/**"    → ("prefix",    "packages/ui/")
//	"src/index.ts"      → ("exact",     "src/index.ts")
//	"src/*.ts"          → ("glob",      "src/")
//	"packages/*/src/**" → ("glob",      "packages/")
func classifyScope(scope string) (patternType, pathPrefix string) {
	norm := strings.ReplaceAll(scope, "\\", "/")

	if norm == "**" {
		return "universal", ""
	}

	// Find the first wildcard character.
	wildIdx := strings.IndexAny(norm, "*?")
	if wildIdx == -1 {
		// No wildcards — exact file or directory path.
		return "exact", norm
	}

	// Simple "base/**" pattern: the base contains no wildcards.
	if strings.HasSuffix(norm, "/**") {
		base := norm[:len(norm)-3]
		if !strings.ContainsAny(base, "*?") {
			return "prefix", base + "/"
		}
	}

	// Complex glob: extract the longest literal prefix up to the first wildcard.
	prefix := ""
	if lastSlash := strings.LastIndex(norm[:wildIdx], "/"); lastSlash >= 0 {
		prefix = norm[:lastSlash+1]
	}
	return "glob", prefix
}

// pathPrefixes returns all directory-level prefixes of a normalised path,
// including the path itself (for exact-type lookups).
//
//	"packages/ui/src/button.tsx" →
//	    ["packages/", "packages/ui/", "packages/ui/src/", "packages/ui/src/button.tsx"]
func pathPrefixes(path string) []string {
	if path == "" {
		return nil
	}
	parts := strings.Split(path, "/")
	prefixes := make([]string, 0, len(parts))
	for i := 1; i <= len(parts); i++ {
		p := strings.Join(parts[:i], "/")
		if i < len(parts) {
			p += "/"
		}
		prefixes = append(prefixes, p)
	}
	return prefixes
}

// QueryByPath returns all skills whose scopes match path.
//
// universal and prefix/exact types are resolved via the SQL prefix index on
// skill_scopes(path_prefix). Complex glob patterns (e.g. "packages/*/src/**")
// fall back to in-process matching — these are rare in practice.
func (r *Registry) QueryByPath(path string) ([]Skill, error) {
	if path == "" {
		return nil, nil
	}
	normalPath := strings.ReplaceAll(path, "\\", "/")
	prefixes := pathPrefixes(normalPath)

	matchedIDs := map[int64]bool{}

	// Step 1: universal-type scopes match every path.
	uniRows, err := r.db.Query(`SELECT DISTINCT skill_id FROM skill_scopes WHERE pattern_type = 'universal'`)
	if err != nil {
		return nil, err
	}
	if err := scanIDs(uniRows, matchedIDs); err != nil {
		return nil, err
	}

	// Step 2: prefix and exact types — indexed lookup by path_prefix.
	// For a prefix scope "packages/ui/**" (path_prefix="packages/ui/"), we
	// look for that exact path_prefix value in the index. We generate all
	// directory-level prefixes of the query path and use an IN query so the
	// planner uses the index for each value.
	if len(prefixes) > 0 {
		placeholders := make([]string, len(prefixes))
		args := make([]any, len(prefixes))
		for i, p := range prefixes {
			placeholders[i] = "?"
			args[i] = p
		}
		prefixQuery := fmt.Sprintf(
			`SELECT DISTINCT skill_id FROM skill_scopes
			 WHERE pattern_type IN ('prefix', 'exact') AND path_prefix IN (%s)`,
			strings.Join(placeholders, ","),
		)
		prefixRows, err := r.db.Query(prefixQuery, args...)
		if err != nil {
			return nil, err
		}
		if err := scanIDs(prefixRows, matchedIDs); err != nil {
			return nil, err
		}
	}

	// Step 3: in-process glob matching for complex patterns (rare).
	globRows, err := r.db.Query(`SELECT skill_id, scope FROM skill_scopes WHERE pattern_type = 'glob'`)
	if err != nil {
		return nil, err
	}
	defer globRows.Close()
	for globRows.Next() {
		var skillID int64
		var scope string
		if err := globRows.Scan(&skillID, &scope); err != nil {
			return nil, err
		}
		if !matchedIDs[skillID] && globMatchPath(scope, normalPath) {
			matchedIDs[skillID] = true
		}
	}
	if err := globRows.Err(); err != nil {
		return nil, err
	}

	if len(matchedIDs) == 0 {
		return nil, nil
	}

	// Fetch full skill data for all matched IDs.
	ids := make([]any, 0, len(matchedIDs))
	placeholders := make([]string, 0, len(matchedIDs))
	for id := range matchedIDs {
		ids = append(ids, id)
		placeholders = append(placeholders, "?")
	}
	return r.querySkills(
		fmt.Sprintf(
			`SELECT id, path, content, COALESCE(name,''), COALESCE(description,''), COALESCE(package_name,''), COALESCE(package_ver,''), visibility, source_type
			 FROM skills WHERE id IN (%s) ORDER BY path`,
			strings.Join(placeholders, ","),
		),
		ids...,
	)
}

// scanIDs drains a single-column int64 result set into ids.
func scanIDs(rows *sql.Rows, ids map[int64]bool) error {
	defer rows.Close()
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return err
		}
		ids[id] = true
	}
	return rows.Err()
}

// globMatchPath reports whether a glob-type scope pattern matches path.
func globMatchPath(pattern, path string) bool {
	norm := strings.ReplaceAll(pattern, "\\", "/")
	if norm == "**" {
		return true
	}
	g, err := glob.Compile(norm, '/')
	if err != nil {
		// Fallback: prefix match (mirrors scopesMatchPath in query engine).
		trimmed := strings.TrimSuffix(norm, "/**")
		return strings.HasPrefix(path, trimmed)
	}
	return g.Match(path)
}
