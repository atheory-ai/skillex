package registry

import (
	"bytes"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"strconv"
)

// Signature computes a deterministic hash (fingerprint) of the registry's
// indexed contents.
//
// Despite the name, this is NOT a cryptographic signature (no signing keys).
// It's a stable identifier used to detect whether the *indexed output* would
// change if we rebuilt the registry.
//
// It is designed for CI "staleness" checks where we want to detect any
// indexing/linking change, without false negatives caused by non-deterministic
// fields (e.g. indexed_at timestamps).
func (r *Registry) Signature() (string, error) {
	var b bytes.Buffer

	// Skills (exclude indexed_at)
	if err := signatureWriteSkills(&b, r.db); err != nil {
		return "", err
	}

	// Derived metadata tables — topics, tags, and scopes are parsed from content
	// which is already hashed above, so in theory they can't diverge. Including
	// them guards against future cases where metadata is written independently
	// of content.
	if err := signatureWriteTopics(&b, r.db); err != nil {
		return "", err
	}
	if err := signatureWriteTags(&b, r.db); err != nil {
		return "", err
	}
	if err := signatureWriteScopes(&b, r.db); err != nil {
		return "", err
	}

	// Test scenarios (so indexing changes also invalidate CI)
	if err := signatureWriteTests(&b, r.db); err != nil {
		return "", err
	}
	if err := signatureWriteCapabilities(&b, r.db); err != nil {
		return "", err
	}

	sum := sha256.Sum256(b.Bytes())
	return hex.EncodeToString(sum[:]), nil
}

func signatureWriteCapabilities(b *bytes.Buffer, db *sql.DB) error {
	rows, err := db.Query(`
		SELECT s.canonical_name, s.publisher, sv.version, sv.package_digest,
			c.kind, c.name, c.title, c.description, c.input_schema,
			c.output_schema, c.schema_digest, c.risk, c.availability,
			v.visibility, v.auth_partition_hash, v.cache_scope,
			s.source_type, s.source_ref
		FROM mcp_capabilities c
		JOIN mcp_server_versions sv ON sv.id = c.server_version_id
		JOIN mcp_servers s ON s.id = sv.server_id
		JOIN mcp_capability_views v ON v.id = c.view_id
		ORDER BY s.canonical_name, sv.version, c.kind, c.name,
			v.auth_partition_hash`)
	if err != nil {
		return err
	}
	b.WriteString("mcp-capabilities;")
	for rows.Next() {
		fields := make([]string, 18)
		if err := rows.Scan(&fields[0], &fields[1], &fields[2], &fields[3],
			&fields[4], &fields[5], &fields[6], &fields[7], &fields[8],
			&fields[9], &fields[10], &fields[11], &fields[12], &fields[13],
			&fields[14], &fields[15], &fields[16], &fields[17]); err != nil {
			return err
		}
		for _, field := range fields {
			signatureWriteField(b, field)
		}
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return err
	}
	if err := rows.Close(); err != nil {
		return err
	}
	rows, err = db.Query(`
		SELECT s.canonical_name, sv.version, c.kind, c.name,
			b.scope, b.relationship, b.auth_profile
		FROM mcp_capability_bindings b
		JOIN mcp_capabilities c ON c.id = b.capability_id
		JOIN mcp_server_versions sv ON sv.id = c.server_version_id
		JOIN mcp_servers s ON s.id = sv.server_id
		ORDER BY s.canonical_name, sv.version, c.kind, c.name,
			b.scope, b.relationship, b.auth_profile`)
	if err != nil {
		return err
	}
	defer rows.Close()
	b.WriteString("mcp-bindings;")
	for rows.Next() {
		fields := make([]string, 7)
		if err := rows.Scan(&fields[0], &fields[1], &fields[2], &fields[3],
			&fields[4], &fields[5], &fields[6]); err != nil {
			return err
		}
		for _, field := range fields {
			signatureWriteField(b, field)
		}
	}
	return rows.Err()
}

func signatureWriteField(b *bytes.Buffer, s string) {
	b.WriteString(strconv.Itoa(len(s)))
	b.WriteByte(':')
	b.WriteString(s)
	b.WriteByte(';')
}

func signatureWriteSkills(b *bytes.Buffer, db *sql.DB) error {
	rows, err := db.Query(`
		SELECT
			path,
			content,
			COALESCE(package_name,''),
			COALESCE(package_ver,''),
			visibility,
			source_type
		FROM skills
		ORDER BY path
	`)
	if err != nil {
		return err
	}
	defer rows.Close()

	b.WriteString("skills;")
	for rows.Next() {
		var path, content, pkgName, pkgVer, visibility, sourceType string
		if err := rows.Scan(&path, &content, &pkgName, &pkgVer, &visibility, &sourceType); err != nil {
			return err
		}
		signatureWriteField(b, path)
		signatureWriteField(b, content)
		signatureWriteField(b, pkgName)
		signatureWriteField(b, pkgVer)
		signatureWriteField(b, visibility)
		signatureWriteField(b, sourceType)
	}
	return rows.Err()
}

func signatureWriteTopics(b *bytes.Buffer, db *sql.DB) error {
	rows, err := db.Query(`
		SELECT s.path, st.topic
		FROM skill_topics st
		JOIN skills s ON s.id = st.skill_id
		ORDER BY s.path, st.topic
	`)
	if err != nil {
		return err
	}
	defer rows.Close()

	b.WriteString("topics;")
	for rows.Next() {
		var path, topic string
		if err := rows.Scan(&path, &topic); err != nil {
			return err
		}
		signatureWriteField(b, path)
		signatureWriteField(b, topic)
	}
	return rows.Err()
}

func signatureWriteTags(b *bytes.Buffer, db *sql.DB) error {
	rows, err := db.Query(`
		SELECT s.path, st.tag
		FROM skill_tags st
		JOIN skills s ON s.id = st.skill_id
		ORDER BY s.path, st.tag
	`)
	if err != nil {
		return err
	}
	defer rows.Close()

	b.WriteString("tags;")
	for rows.Next() {
		var path, tag string
		if err := rows.Scan(&path, &tag); err != nil {
			return err
		}
		signatureWriteField(b, path)
		signatureWriteField(b, tag)
	}
	return rows.Err()
}

func signatureWriteScopes(b *bytes.Buffer, db *sql.DB) error {
	rows, err := db.Query(`
		SELECT s.path, sc.scope
		FROM skill_scopes sc
		JOIN skills s ON s.id = sc.skill_id
		ORDER BY s.path, sc.scope
	`)
	if err != nil {
		return err
	}
	defer rows.Close()

	b.WriteString("scopes;")
	for rows.Next() {
		var path, scope string
		if err := rows.Scan(&path, &scope); err != nil {
			return err
		}
		signatureWriteField(b, path)
		signatureWriteField(b, scope)
	}
	return rows.Err()
}

func signatureWriteTests(b *bytes.Buffer, db *sql.DB) error {
	rows, err := db.Query(`
		SELECT
			s.path,
			t.name,
			t.prompt,
			COALESCE(t.extra_skills,''),
			t.criteria
		FROM skill_tests t
		JOIN skills s ON s.id = t.skill_id
		ORDER BY s.path, t.name, t.prompt, t.extra_skills, t.criteria
	`)
	if err != nil {
		return err
	}
	defer rows.Close()

	b.WriteString("tests;")
	for rows.Next() {
		var path, name, prompt, extraSkills, criteria string
		if err := rows.Scan(&path, &name, &prompt, &extraSkills, &criteria); err != nil {
			return err
		}
		signatureWriteField(b, path)
		signatureWriteField(b, name)
		signatureWriteField(b, prompt)
		signatureWriteField(b, extraSkills)
		signatureWriteField(b, criteria)
	}
	return rows.Err()
}
