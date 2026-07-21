package registry

import (
	"database/sql"
	"fmt"
)

// currentSchemaVersion is incremented whenever a migration is added.
// Registry.Open sets this on all databases it manages.
const currentSchemaVersion = 4

// migrateSchema applies any pending schema migrations.
//
// Migrations are idempotent: each checks whether its changes are already
// present before applying them. BEGIN IMMEDIATE prevents two concurrent
// openers from both running the same migration; the second opener will
// block until the first commits, then see the updated user_version and
// exit early.
func migrateSchema(db *sql.DB) error {
	var ver int
	if err := db.QueryRow("PRAGMA user_version").Scan(&ver); err != nil {
		return fmt.Errorf("reading schema version: %w", err)
	}
	if ver >= currentSchemaVersion {
		return nil
	}

	// Acquire an exclusive write lock. A concurrent opener may have already
	// run the migration; we re-check the version inside the lock.
	if _, err := db.Exec("BEGIN IMMEDIATE"); err != nil {
		return fmt.Errorf("beginning migration transaction: %w", err)
	}

	if err := db.QueryRow("PRAGMA user_version").Scan(&ver); err != nil {
		db.Exec("ROLLBACK") //nolint:errcheck
		return fmt.Errorf("re-reading schema version: %w", err)
	}
	if ver >= currentSchemaVersion {
		db.Exec("ROLLBACK") //nolint:errcheck
		return nil
	}

	// v2: add path_prefix and pattern_type to skill_scopes for SQL-level
	// path filtering. ALTER TABLE in SQLite does not support IF NOT EXISTS,
	// so we check table_info first. Existing rows receive safe defaults:
	// path_prefix='' and pattern_type='glob' (falls back to in-process
	// matching until the next refresh re-populates the columns).
	cols, err := columnNames(db, "skill_scopes")
	if err != nil {
		db.Exec("ROLLBACK") //nolint:errcheck
		return err
	}
	if !cols["path_prefix"] {
		if _, err := db.Exec(`ALTER TABLE skill_scopes ADD COLUMN path_prefix TEXT NOT NULL DEFAULT ''`); err != nil {
			db.Exec("ROLLBACK") //nolint:errcheck
			return fmt.Errorf("adding path_prefix column: %w", err)
		}
	}
	if !cols["pattern_type"] {
		if _, err := db.Exec(`ALTER TABLE skill_scopes ADD COLUMN pattern_type TEXT NOT NULL DEFAULT 'glob'`); err != nil {
			db.Exec("ROLLBACK") //nolint:errcheck
			return fmt.Errorf("adding pattern_type column: %w", err)
		}
	}
	if _, err := db.Exec(`CREATE INDEX IF NOT EXISTS idx_skill_scopes_prefix ON skill_scopes(path_prefix)`); err != nil {
		db.Exec("ROLLBACK") //nolint:errcheck
		return fmt.Errorf("creating prefix index: %w", err)
	}
	// v3: add name and description columns to skills for keyword search (ATH-170).
	skillCols, err := columnNames(db, "skills")
	if err != nil {
		db.Exec("ROLLBACK") //nolint:errcheck
		return err
	}
	if !skillCols["name"] {
		if _, err := db.Exec(`ALTER TABLE skills ADD COLUMN name TEXT`); err != nil {
			db.Exec("ROLLBACK") //nolint:errcheck
			return fmt.Errorf("adding name column: %w", err)
		}
	}
	if !skillCols["description"] {
		if _, err := db.Exec(`ALTER TABLE skills ADD COLUMN description TEXT`); err != nil {
			db.Exec("ROLLBACK") //nolint:errcheck
			return fmt.Errorf("adding description column: %w", err)
		}
	}
	// v4: full-text discovery across metadata, headings, and skill bodies.
	if _, err := db.Exec(`CREATE VIRTUAL TABLE IF NOT EXISTS skill_search USING fts5(skill_id UNINDEXED, name, description, headings, body)`); err != nil {
		db.Exec("ROLLBACK") //nolint:errcheck
		return fmt.Errorf("creating full-text search index: %w", err)
	}
	if _, err := db.Exec(`DELETE FROM skill_search`); err != nil {
		db.Exec("ROLLBACK") //nolint:errcheck
		return fmt.Errorf("clearing full-text search index: %w", err)
	}
	rows, err := db.Query(`SELECT id, COALESCE(name,''), COALESCE(description,''), content FROM skills`)
	if err != nil {
		db.Exec("ROLLBACK")
		return fmt.Errorf("reading skills for full-text index: %w", err)
	}
	type ftsSkill struct {
		id                         int64
		name, description, content string
	}
	var ftsSkills []ftsSkill
	for rows.Next() {
		var id int64
		var name, description, content string
		if err := rows.Scan(&id, &name, &description, &content); err != nil {
			rows.Close()
			db.Exec("ROLLBACK")
			return err
		}
		ftsSkills = append(ftsSkills, ftsSkill{id, name, description, content})
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		db.Exec("ROLLBACK")
		return err
	}
	rows.Close()
	for _, skill := range ftsSkills {
		if _, err := db.Exec(`INSERT INTO skill_search (skill_id, name, description, headings, body) VALUES (?, ?, ?, ?, ?)`, skill.id, skill.name, skill.description, headingsText(skill.content), skill.content); err != nil {
			db.Exec("ROLLBACK")
			return fmt.Errorf("indexing skill %d for full-text search: %w", skill.id, err)
		}
	}

	if _, err := db.Exec(fmt.Sprintf("PRAGMA user_version = %d", currentSchemaVersion)); err != nil {
		db.Exec("ROLLBACK") //nolint:errcheck
		return fmt.Errorf("setting schema version: %w", err)
	}
	if _, err := db.Exec("COMMIT"); err != nil {
		return fmt.Errorf("committing migration: %w", err)
	}
	return nil
}

// columnNames returns the set of column names present in table.
func columnNames(db *sql.DB, table string) (map[string]bool, error) {
	// PRAGMA table_info is safe inside a transaction.
	rows, err := db.Query(fmt.Sprintf("PRAGMA table_info(%s)", table))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	cols := map[string]bool{}
	for rows.Next() {
		var cid, notnull, pk int
		var name, typ string
		var dflt sql.NullString
		if err := rows.Scan(&cid, &name, &typ, &notnull, &dflt, &pk); err != nil {
			return nil, err
		}
		cols[name] = true
	}
	return cols, rows.Err()
}
