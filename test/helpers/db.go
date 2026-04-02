package helpers

import (
	"database/sql"
	"path/filepath"
	"strings"
	"testing"

	_ "modernc.org/sqlite"
)

// SkillRow represents a row from the skills table.
type SkillRow struct {
	ID          int64
	Path        string
	PackageName string
	Visibility  string
	SourceType  string
}

// TestRow represents a row from the skill_tests table.
type TestRow struct {
	ID          int64
	SkillID     int64
	Name        string
	Prompt      string
	ExtraSkills []string
	Criteria    []string
}

// OpenRegistry opens the SQLite database at .skillex/index.db within the given directory.
func OpenRegistry(t *testing.T, dir string) *sql.DB {
	t.Helper()
	dbPath := filepath.Join(dir, ".skillex", "index.db")
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatalf("opening registry at %s: %v", dbPath, err)
	}
	t.Cleanup(func() { db.Close() })
	return db
}

// QuerySkillsTable returns all rows from the skills table.
func QuerySkillsTable(t *testing.T, db *sql.DB) []SkillRow {
	t.Helper()
	rows, err := db.Query(`SELECT id, path, COALESCE(package_name,''), visibility, source_type FROM skills ORDER BY path`)
	if err != nil {
		t.Fatalf("querying skills table: %v", err)
	}
	defer rows.Close()

	var result []SkillRow
	for rows.Next() {
		var r SkillRow
		if err := rows.Scan(&r.ID, &r.Path, &r.PackageName, &r.Visibility, &r.SourceType); err != nil {
			t.Fatalf("scanning skill row: %v", err)
		}
		result = append(result, r)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("iterating skills: %v", err)
	}
	return result
}

// QueryTopicsFor returns topics for a skill whose path contains the given substring.
func QueryTopicsFor(t *testing.T, db *sql.DB, pathFragment string) []string {
	t.Helper()
	id := skillIDByFragment(t, db, pathFragment)
	if id == 0 {
		t.Fatalf("no skill found with path containing %q", pathFragment)
	}
	return queryStringList(t, db, `SELECT topic FROM skill_topics WHERE skill_id = ? ORDER BY topic`, id)
}

// QueryTagsFor returns tags for a skill whose path contains the given substring.
func QueryTagsFor(t *testing.T, db *sql.DB, pathFragment string) []string {
	t.Helper()
	id := skillIDByFragment(t, db, pathFragment)
	if id == 0 {
		t.Fatalf("no skill found with path containing %q", pathFragment)
	}
	return queryStringList(t, db, `SELECT tag FROM skill_tags WHERE skill_id = ? ORDER BY tag`, id)
}

// QueryNameFor returns the name stored for a skill whose path contains the given substring.
func QueryNameFor(t *testing.T, db *sql.DB, pathFragment string) string {
	t.Helper()
	id := skillIDByFragment(t, db, pathFragment)
	if id == 0 {
		t.Fatalf("no skill found with path containing %q", pathFragment)
	}
	var name string
	err := db.QueryRow(`SELECT COALESCE(name,'') FROM skills WHERE id = ?`, id).Scan(&name)
	if err != nil {
		t.Fatalf("querying name for skill %d: %v", id, err)
	}
	return name
}

// QueryDescriptionFor returns the description stored for a skill whose path contains the given substring.
func QueryDescriptionFor(t *testing.T, db *sql.DB, pathFragment string) string {
	t.Helper()
	id := skillIDByFragment(t, db, pathFragment)
	if id == 0 {
		t.Fatalf("no skill found with path containing %q", pathFragment)
	}
	var desc string
	err := db.QueryRow(`SELECT COALESCE(description,'') FROM skills WHERE id = ?`, id).Scan(&desc)
	if err != nil {
		t.Fatalf("querying description for skill %d: %v", id, err)
	}
	return desc
}

// QueryScopesFor returns scopes for a skill whose path contains the given substring.
func QueryScopesFor(t *testing.T, db *sql.DB, pathFragment string) []string {
	t.Helper()
	id := skillIDByFragment(t, db, pathFragment)
	if id == 0 {
		t.Fatalf("no skill found with path containing %q", pathFragment)
	}
	return queryStringList(t, db, `SELECT scope FROM skill_scopes WHERE skill_id = ? ORDER BY scope`, id)
}

// QueryTestsFor returns test scenarios for a skill whose path contains the given substring.
func QueryTestsFor(t *testing.T, db *sql.DB, pathFragment string) []TestRow {
	t.Helper()
	id := skillIDByFragment(t, db, pathFragment)
	if id == 0 {
		t.Fatalf("no skill found with path containing %q", pathFragment)
	}

	rows, err := db.Query(
		`SELECT id, skill_id, name, prompt, COALESCE(extra_skills,''), criteria FROM skill_tests WHERE skill_id = ? ORDER BY id`,
		id,
	)
	if err != nil {
		t.Fatalf("querying tests for skill %d: %v", id, err)
	}
	defer rows.Close()

	var result []TestRow
	for rows.Next() {
		var r TestRow
		var extraStr, criteriaStr string
		if err := rows.Scan(&r.ID, &r.SkillID, &r.Name, &r.Prompt, &extraStr, &criteriaStr); err != nil {
			t.Fatalf("scanning test row: %v", err)
		}
		if extraStr != "" {
			r.ExtraSkills = strings.Split(extraStr, ",")
		}
		if criteriaStr != "" {
			r.Criteria = strings.Split(criteriaStr, "\n")
		}
		result = append(result, r)
	}
	return result
}

// TableExists checks if a table exists in the database.
func TableExists(t *testing.T, db *sql.DB, table string) bool {
	t.Helper()
	var count int
	err := db.QueryRow(
		`SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name=?`, table,
	).Scan(&count)
	if err != nil {
		t.Fatalf("checking table existence: %v", err)
	}
	return count > 0
}

// skillIDByFragment returns the ID of the first skill whose path contains pathFragment.
func skillIDByFragment(t *testing.T, db *sql.DB, pathFragment string) int64 {
	t.Helper()
	// Use GLOB-style: try exact match, then suffix match
	var id int64
	// Try exact
	err := db.QueryRow(`SELECT id FROM skills WHERE path = ?`, pathFragment).Scan(&id)
	if err == nil {
		return id
	}
	// Try ends-with (contains the fragment as a path segment)
	err = db.QueryRow(`SELECT id FROM skills WHERE path LIKE ? OR path LIKE ? OR path = ?`,
		"%/"+pathFragment, pathFragment+"/%", pathFragment).Scan(&id)
	if err == nil {
		return id
	}
	// Try LIKE contains
	err = db.QueryRow(`SELECT id FROM skills WHERE path LIKE ?`, "%"+pathFragment+"%").Scan(&id)
	if err == nil {
		return id
	}
	return 0
}

func queryStringList(t *testing.T, db *sql.DB, query string, args ...interface{}) []string {
	t.Helper()
	rows, err := db.Query(query, args...)
	if err != nil {
		t.Fatalf("query failed: %v", err)
	}
	defer rows.Close()
	var result []string
	for rows.Next() {
		var s string
		if err := rows.Scan(&s); err != nil {
			t.Fatalf("scan failed: %v", err)
		}
		result = append(result, s)
	}
	return result
}
