package registry

import (
	"bytes"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/atheory-ai/skillex/internal/capability"
	"github.com/atheory-ai/skillex/internal/config"
	"github.com/atheory-ai/skillex/internal/scanner"
)

const maxStaticCatalogBytes = 16 << 20

const capabilitySchema = `
CREATE TABLE IF NOT EXISTS mcp_servers (
	id              INTEGER PRIMARY KEY AUTOINCREMENT,
	canonical_name  TEXT NOT NULL UNIQUE,
	publisher       TEXT NOT NULL DEFAULT '',
	title           TEXT NOT NULL DEFAULT '',
	description     TEXT NOT NULL DEFAULT '',
	source_type     TEXT NOT NULL,
	source_ref      TEXT NOT NULL,
	indexed_at      TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS mcp_server_versions (
	id              INTEGER PRIMARY KEY AUTOINCREMENT,
	server_id       INTEGER NOT NULL REFERENCES mcp_servers(id) ON DELETE CASCADE,
	version         TEXT NOT NULL,
	package_digest  TEXT NOT NULL DEFAULT '',
	status          TEXT NOT NULL DEFAULT 'active',
	raw_metadata    BLOB NOT NULL,
	indexed_at      TEXT NOT NULL,
	UNIQUE(server_id, version)
);

CREATE TABLE IF NOT EXISTS mcp_transports (
	id                  INTEGER PRIMARY KEY AUTOINCREMENT,
	server_version_id   INTEGER NOT NULL REFERENCES mcp_server_versions(id) ON DELETE CASCADE,
	kind                TEXT NOT NULL,
	package_registry    TEXT NOT NULL DEFAULT '',
	package_id          TEXT NOT NULL DEFAULT '',
	package_version     TEXT NOT NULL DEFAULT '',
	package_digest      TEXT NOT NULL DEFAULT '',
	endpoint            TEXT NOT NULL DEFAULT '',
	command_template    BLOB NOT NULL DEFAULT '',
	origin              TEXT NOT NULL DEFAULT '',
	UNIQUE(server_version_id, kind, package_id, endpoint)
);

CREATE TABLE IF NOT EXISTS mcp_capability_views (
	id                  INTEGER PRIMARY KEY AUTOINCREMENT,
	server_version_id   INTEGER NOT NULL REFERENCES mcp_server_versions(id) ON DELETE CASCADE,
	visibility          TEXT NOT NULL,
	auth_partition_hash TEXT NOT NULL,
	cache_scope         TEXT NOT NULL,
	observed_at         TEXT NOT NULL DEFAULT '',
	expires_at          TEXT NOT NULL DEFAULT '',
	provenance          BLOB NOT NULL,
	UNIQUE(server_version_id, auth_partition_hash)
);

CREATE TABLE IF NOT EXISTS mcp_capabilities (
	id                  INTEGER PRIMARY KEY AUTOINCREMENT,
	server_version_id   INTEGER NOT NULL REFERENCES mcp_server_versions(id) ON DELETE CASCADE,
	view_id             INTEGER NOT NULL REFERENCES mcp_capability_views(id) ON DELETE CASCADE,
	kind                TEXT NOT NULL,
	name                TEXT NOT NULL,
	title               TEXT NOT NULL DEFAULT '',
	description         TEXT NOT NULL DEFAULT '',
	input_schema        BLOB NOT NULL DEFAULT '',
	output_schema       BLOB NOT NULL DEFAULT '',
	schema_digest       TEXT NOT NULL,
	risk                TEXT NOT NULL DEFAULT '',
	availability        TEXT NOT NULL,
	raw_definition      BLOB NOT NULL,
	indexed_at          TEXT NOT NULL,
	UNIQUE(view_id, kind, name)
);

CREATE TABLE IF NOT EXISTS mcp_capability_bindings (
	capability_id  INTEGER NOT NULL REFERENCES mcp_capabilities(id) ON DELETE CASCADE,
	scope          TEXT NOT NULL,
	path_prefix    TEXT NOT NULL DEFAULT '',
	pattern_type   TEXT NOT NULL DEFAULT 'glob',
	relationship   TEXT NOT NULL,
	auth_profile   TEXT NOT NULL DEFAULT '',
	UNIQUE(capability_id, scope, auth_profile)
);

CREATE VIRTUAL TABLE IF NOT EXISTS mcp_capability_search USING fts5(
	capability_id UNINDEXED,
	server,
	name,
	title,
	description,
	schema_summary,
	binding
);

CREATE INDEX IF NOT EXISTS idx_mcp_server_versions_identity ON mcp_server_versions(server_id, version);
CREATE INDEX IF NOT EXISTS idx_mcp_capabilities_identity ON mcp_capabilities(server_version_id, kind, name);
CREATE INDEX IF NOT EXISTS idx_mcp_bindings_scope ON mcp_capability_bindings(scope);
`

// CapabilityBinding is one contextual relationship between a capability and a
// project scope. It may name a trusted auth profile, but never contains secrets.
type CapabilityBinding struct {
	Scope        string `json:"scope"`
	Relationship string `json:"relationship"`
	AuthProfile  string `json:"auth_profile,omitempty"`
}

// CapabilityFacet is a candidate-scoped capability filter value.
type CapabilityFacet struct {
	Value string
	Count int
}

// CapabilityFacets contains narrowing values calculated across every match.
type CapabilityFacets struct {
	Servers      []CapabilityFacet
	Kinds        []CapabilityFacet
	Availability []CapabilityFacet
}

// CapabilityQuery bounds and filters an offline capability search.
type CapabilityQuery struct {
	Search       string
	Path         string
	Server       string
	Kind         capability.CapabilityKind
	Availability capability.AvailabilityStatus
	View         string
	Limit        int
	Offset       int
	AllViews     bool
}

// CapabilityPage contains one hydrated page plus SQL-derived result metadata.
type CapabilityPage struct {
	Records    []CapabilityRecord
	MatchCount int
	Facets     CapabilityFacets
}

// ApplyCapabilitySuggestions overlays pack-provided relevance onto catalog
// metadata and creates bounded placeholder entries for explicitly preferred
// capabilities that have not yet been cataloged. Suggestions never add a
// transport, credential, endpoint, or executable configuration.
func ApplyCapabilitySuggestions(records []CapabilityRecord, suggestions []scanner.CapabilitySuggestion) ([]CapabilityRecord, error) {
	for _, suggestion := range suggestions {
		preferred := make(map[string]bool, len(suggestion.Capabilities))
		for _, name := range suggestion.Capabilities {
			preferred[name] = true
		}
		matched := map[string]bool{}
		for i := range records {
			selected := &records[i]
			if selected.Capability.Server.Identity.CanonicalName != suggestion.ServerRef || selected.Capability.Server.Version != suggestion.Version {
				continue
			}
			if len(preferred) > 0 && !preferred[selected.Capability.Name] {
				continue
			}
			matched[selected.Capability.Name] = true
			selected.Capability.Availability = capability.AvailabilitySuggested
			for _, scope := range suggestion.Scopes {
				selected.Bindings = appendUniqueCapabilityBinding(selected.Bindings, CapabilityBinding{
					Scope: scope, Relationship: suggestion.Relationship,
				})
			}
		}

		for _, name := range suggestion.Capabilities {
			if matched[name] {
				continue
			}
			selected, err := (capability.Capability{
				Server: capability.ServerVersion{
					Identity: capability.ServerIdentity{CanonicalName: suggestion.ServerRef},
					Version:  suggestion.Version,
				},
				Kind:         capability.CapabilityTool,
				Name:         name,
				Description:  "Suggested by " + suggestion.SourceRef + "; full metadata is not yet indexed.",
				Availability: capability.AvailabilitySuggested,
			}).WithComputedSchemaDigest()
			if err != nil {
				return nil, err
			}
			raw, err := json.Marshal(selected)
			if err != nil {
				return nil, err
			}
			record := CapabilityRecord{
				Capability: selected, Visibility: "public", AuthPartitionHash: "public", CacheScope: "public",
				SourceType: suggestion.SourceType, SourceRef: suggestion.SourceRef, RawDefinition: raw,
			}
			for _, scope := range suggestion.Scopes {
				record.Bindings = append(record.Bindings, CapabilityBinding{Scope: scope, Relationship: suggestion.Relationship})
			}
			records = append(records, record)
		}
	}
	return records, nil
}

func appendUniqueCapabilityBinding(bindings []CapabilityBinding, candidate CapabilityBinding) []CapabilityBinding {
	for _, existing := range bindings {
		if existing == candidate {
			return bindings
		}
	}
	return append(bindings, candidate)
}

// CapabilityRecord is the persisted capability plus its source, view, and
// contextual binding overlays.
type CapabilityRecord struct {
	ID                int64
	Capability        capability.Capability
	Visibility        string
	AuthPartitionHash string
	CacheScope        string
	SourceType        string
	SourceRef         string
	Risk              string
	Bindings          []CapabilityBinding
	RawDefinition     json.RawMessage
	Score             float64
	Transports        []TransportRecord
}

type TransportRecord struct {
	Server          string          `json:"server"`
	Version         string          `json:"version"`
	Kind            string          `json:"kind"`
	PackageRegistry string          `json:"package_registry,omitempty"`
	PackageID       string          `json:"package_id,omitempty"`
	PackageVersion  string          `json:"package_version,omitempty"`
	PackageDigest   string          `json:"package_digest,omitempty"`
	Endpoint        string          `json:"endpoint,omitempty"`
	CommandTemplate json.RawMessage `json:"command_template,omitempty"`
	Origin          string          `json:"origin,omitempty"`
}

type staticCapabilityCatalog struct {
	Capabilities []capability.Capability `json:"capabilities"`
	Transports   []TransportRecord       `json:"transports,omitempty"`
}

func createCapabilitySchema(db interface {
	Exec(query string, args ...any) (sql.Result, error)
}) error {
	_, err := db.Exec(capabilitySchema)
	return err
}

// LoadStaticCapabilityCatalog parses metadata from a bounded project-local
// catalog. It never starts or connects to a downstream MCP server.
func LoadStaticCapabilityCatalog(root string, source config.MCPCatalog, bindings []config.MCPBinding) ([]CapabilityRecord, error) {
	path, err := resolveCatalogPath(root, source.Path)
	if err != nil {
		return nil, err
	}
	file, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("opening static MCP catalog %s: %w", source.Path, err)
	}
	defer file.Close()

	limited := io.LimitReader(file, maxStaticCatalogBytes+1)
	data, err := io.ReadAll(limited)
	if err != nil {
		return nil, fmt.Errorf("reading static MCP catalog %s: %w", source.Path, err)
	}
	if len(data) > maxStaticCatalogBytes {
		return nil, fmt.Errorf("static MCP catalog %s exceeds %d bytes", source.Path, maxStaticCatalogBytes)
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	var document staticCapabilityCatalog
	if err := decoder.Decode(&document); err != nil {
		return nil, fmt.Errorf("parsing static MCP catalog %s: %w", source.Path, err)
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		if err == nil {
			return nil, fmt.Errorf("parsing static MCP catalog %s: multiple JSON values", source.Path)
		}
		return nil, fmt.Errorf("parsing static MCP catalog %s: %w", source.Path, err)
	}

	records := make([]CapabilityRecord, 0, len(document.Capabilities))
	for _, selected := range document.Capabilities {
		selected, err = selected.WithComputedSchemaDigest()
		if err != nil {
			return nil, fmt.Errorf("static MCP catalog %s capability %q: %w", source.Path, selected.Name, err)
		}
		selected.Availability = capability.AvailabilityDiscovered
		record := CapabilityRecord{
			Capability:        selected,
			Visibility:        "public",
			AuthPartitionHash: "public",
			CacheScope:        "public",
			SourceType:        "static",
			SourceRef:         filepath.ToSlash(source.Path),
		}
		for _, transport := range document.Transports {
			if transport.Server == selected.Server.Identity.CanonicalName && transport.Version == selected.Server.Version {
				record.Transports = append(record.Transports, transport)
			}
		}
		record.RawDefinition, err = json.Marshal(selected)
		if err != nil {
			return nil, err
		}
		for _, binding := range bindings {
			if binding.Server != selected.Server.Identity.CanonicalName || binding.Version != selected.Server.Version {
				continue
			}
			record.Bindings = append(record.Bindings, CapabilityBinding{
				Scope:        binding.Scope,
				Relationship: "available",
				AuthProfile:  binding.AuthProfile,
			})
		}
		if len(record.Bindings) == 0 {
			record.Bindings = []CapabilityBinding{{Scope: "**", Relationship: "discovered"}}
		} else {
			record.Capability.Availability = capability.AvailabilitySetupRequired
		}
		records = append(records, record)
	}
	return records, nil
}

func resolveCatalogPath(root, configured string) (string, error) {
	rootAbs, err := filepath.Abs(root)
	if err != nil {
		return "", err
	}
	rootAbs, err = filepath.EvalSymlinks(rootAbs)
	if err != nil {
		return "", fmt.Errorf("resolving project root: %w", err)
	}
	candidate := filepath.Join(rootAbs, filepath.Clean(configured))
	resolved, err := filepath.EvalSymlinks(candidate)
	if err != nil {
		return "", fmt.Errorf("resolving static MCP catalog %s: %w", configured, err)
	}
	rel, err := filepath.Rel(rootAbs, resolved)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("static MCP catalog %s resolves outside the project", configured)
	}
	return resolved, nil
}

// InsertCapability upserts one canonical capability and replaces its search
// document and contextual bindings.
func (r *Registry) InsertCapability(record CapabilityRecord) (int64, error) {
	selected, err := record.Capability.WithComputedSchemaDigest()
	if err != nil {
		return 0, err
	}
	record.Capability = selected
	if record.Visibility == "" {
		record.Visibility = "public"
	}
	if record.AuthPartitionHash == "" {
		record.AuthPartitionHash = "public"
	}
	if record.CacheScope == "" {
		record.CacheScope = record.Visibility
	}
	if record.SourceType == "" || record.SourceRef == "" {
		return 0, errors.New("capability source type and reference are required")
	}
	if len(record.RawDefinition) == 0 {
		record.RawDefinition, err = json.Marshal(selected)
		if err != nil {
			return 0, err
		}
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)

	tx, err := r.db.Begin()
	if err != nil {
		return 0, err
	}
	defer tx.Rollback() //nolint:errcheck
	if _, err := tx.Exec(`INSERT INTO mcp_servers
		(canonical_name, publisher, source_type, source_ref, indexed_at)
		VALUES (?, ?, ?, ?, ?)
		ON CONFLICT(canonical_name) DO UPDATE SET
		publisher=excluded.publisher, source_type=excluded.source_type,
		source_ref=excluded.source_ref, indexed_at=excluded.indexed_at`,
		selected.Server.Identity.CanonicalName, selected.Server.Identity.Publisher,
		record.SourceType, record.SourceRef, now); err != nil {
		return 0, fmt.Errorf("upserting MCP server: %w", err)
	}
	var serverID int64
	if err := tx.QueryRow(`SELECT id FROM mcp_servers WHERE canonical_name = ?`, selected.Server.Identity.CanonicalName).Scan(&serverID); err != nil {
		return 0, err
	}
	serverMetadata, _ := json.Marshal(selected.Server)
	if _, err := tx.Exec(`INSERT INTO mcp_server_versions
		(server_id, version, package_digest, status, raw_metadata, indexed_at)
		VALUES (?, ?, ?, ?, ?, ?)
		ON CONFLICT(server_id, version) DO UPDATE SET
		package_digest=excluded.package_digest, status=excluded.status,
		raw_metadata=excluded.raw_metadata, indexed_at=excluded.indexed_at`,
		serverID, selected.Server.Version, selected.Server.PackageDigest,
		defaultString(selected.Server.Status, "active"), serverMetadata, now); err != nil {
		return 0, fmt.Errorf("upserting MCP server version: %w", err)
	}
	var serverVersionID int64
	if err := tx.QueryRow(`SELECT id FROM mcp_server_versions WHERE server_id = ? AND version = ?`, serverID, selected.Server.Version).Scan(&serverVersionID); err != nil {
		return 0, err
	}
	for _, transport := range record.Transports {
		if transport.Server != selected.Server.Identity.CanonicalName || transport.Version != selected.Server.Version || transport.Kind == "" {
			return 0, errors.New("MCP transport identity does not match capability server version")
		}
		if _, err := tx.Exec(`INSERT INTO mcp_transports
			(server_version_id, kind, package_registry, package_id, package_version,
			 package_digest, endpoint, command_template, origin)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
			ON CONFLICT(server_version_id, kind, package_id, endpoint) DO UPDATE SET
			package_registry=excluded.package_registry, package_version=excluded.package_version,
			package_digest=excluded.package_digest, command_template=excluded.command_template,
			origin=excluded.origin`, serverVersionID, transport.Kind, transport.PackageRegistry,
			transport.PackageID, transport.PackageVersion, transport.PackageDigest,
			transport.Endpoint, []byte(transport.CommandTemplate), transport.Origin); err != nil {
			return 0, fmt.Errorf("upserting MCP transport: %w", err)
		}
	}
	provenance, _ := json.Marshal(map[string]string{"source_type": record.SourceType, "source_ref": record.SourceRef})
	if _, err := tx.Exec(`INSERT INTO mcp_capability_views
		(server_version_id, visibility, auth_partition_hash, cache_scope, observed_at, expires_at, provenance)
		VALUES (?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(server_version_id, auth_partition_hash) DO UPDATE SET
		visibility=excluded.visibility, cache_scope=excluded.cache_scope, observed_at=excluded.observed_at,
		expires_at=excluded.expires_at, provenance=excluded.provenance`,
		serverVersionID, record.Visibility, record.AuthPartitionHash, record.CacheScope,
		formatOptionalTime(selected.ObservedAt), formatOptionalTime(selected.ExpiresAt), provenance); err != nil {
		return 0, fmt.Errorf("upserting MCP capability view: %w", err)
	}
	var viewID int64
	if err := tx.QueryRow(`SELECT id FROM mcp_capability_views WHERE server_version_id = ? AND auth_partition_hash = ?`, serverVersionID, record.AuthPartitionHash).Scan(&viewID); err != nil {
		return 0, err
	}
	if _, err := tx.Exec(`INSERT INTO mcp_capabilities
		(server_version_id, view_id, kind, name, title, description, input_schema,
		 output_schema, schema_digest, risk, availability, raw_definition, indexed_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(view_id, kind, name) DO UPDATE SET
		title=excluded.title, description=excluded.description,
		input_schema=excluded.input_schema, output_schema=excluded.output_schema,
		schema_digest=excluded.schema_digest, risk=excluded.risk,
		availability=excluded.availability, raw_definition=excluded.raw_definition,
		indexed_at=excluded.indexed_at`,
		serverVersionID, viewID, selected.Kind, selected.Name, selected.Title,
		selected.Description, string(selected.InputSchemaJSON), string(selected.OutputSchemaJSON),
		selected.SchemaDigest, record.Risk, selected.Availability, []byte(record.RawDefinition), now); err != nil {
		return 0, fmt.Errorf("upserting MCP capability: %w", err)
	}
	var capabilityID int64
	if err := tx.QueryRow(`SELECT id FROM mcp_capabilities WHERE view_id = ? AND kind = ? AND name = ?`, viewID, selected.Kind, selected.Name).Scan(&capabilityID); err != nil {
		return 0, err
	}
	if _, err := tx.Exec(`DELETE FROM mcp_capability_bindings WHERE capability_id = ?`, capabilityID); err != nil {
		return 0, err
	}
	var bindingSearch []string
	for _, binding := range record.Bindings {
		pt, pp := classifyScope(binding.Scope)
		if _, err := tx.Exec(`INSERT INTO mcp_capability_bindings
			(capability_id, scope, path_prefix, pattern_type, relationship, auth_profile)
			VALUES (?, ?, ?, ?, ?, ?)`, capabilityID, binding.Scope, pp, pt,
			binding.Relationship, binding.AuthProfile); err != nil {
			return 0, err
		}
		bindingSearch = append(bindingSearch, binding.Relationship, binding.Scope, binding.AuthProfile)
	}
	if _, err := tx.Exec(`DELETE FROM mcp_capability_search WHERE rowid = ?`, capabilityID); err != nil {
		return 0, err
	}
	if _, err := tx.Exec(`INSERT INTO mcp_capability_search
		(rowid, capability_id, server, name, title, description, schema_summary, binding)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)`, capabilityID, capabilityID,
		selected.Server.Identity.CanonicalName, selected.Name, selected.Title,
		selected.Description, schemaSearchText(selected), strings.Join(bindingSearch, " ")); err != nil {
		return 0, err
	}
	if err := tx.Commit(); err != nil {
		return 0, err
	}
	return capabilityID, nil
}

func schemaSearchText(selected capability.Capability) string {
	return string(selected.InputSchemaJSON) + " " + string(selected.OutputSchemaJSON)
}

func defaultString(value, fallback string) string {
	if value == "" {
		return fallback
	}
	return value
}

// CapabilityCount returns the number of indexed capability views.
func (r *Registry) CapabilityCount() (int, error) {
	var count int
	err := r.db.QueryRow(`SELECT COUNT(*) FROM mcp_capabilities`).Scan(&count)
	return count, err
}

// AllCapabilities returns capabilities in stable canonical order.
func (r *Registry) AllCapabilities() ([]CapabilityRecord, error) {
	return r.queryCapabilities(capabilitySelect + ` ORDER BY s.canonical_name, sv.version, c.kind, c.name`)
}

// QueryCapabilitiesBySearch performs capability-granular FTS discovery.
func (r *Registry) QueryCapabilitiesBySearch(search string) ([]CapabilityRecord, error) {
	page, err := r.QueryCapabilityPage(CapabilityQuery{Search: search, AllViews: true})
	return page.Records, err
}

// QueryCapabilityPage lets SQLite filter, count, facet, rank, and page before
// full capability definitions and bindings are hydrated.
func (r *Registry) QueryCapabilityPage(query CapabilityQuery) (CapabilityPage, error) {
	if query.Offset < 0 || query.Limit < 0 {
		return CapabilityPage{}, errors.New("capability query limit and offset cannot be negative")
	}
	from := capabilityFrom
	var where []string
	var args []any
	tokens := searchTokens(query.Search)
	if len(tokens) > 0 {
		terms := make([]string, len(tokens))
		for i, token := range tokens {
			terms[i] = `"` + strings.ReplaceAll(token, `"`, `""`) + `"`
		}
		from += ` JOIN mcp_capability_search f ON f.rowid = c.id`
		where = append(where, `mcp_capability_search MATCH ?`, `rank MATCH 'bm25(8.0, 8.0, 6.0, 4.0, 2.0, 1.0)'`)
		args = append(args, strings.Join(terms, " OR "))
	}
	if !query.AllViews {
		where = append(where, `(v.auth_partition_hash = 'public' OR v.auth_partition_hash = ?)`)
		args = append(args, query.View)
	}
	if query.Server != "" {
		where = append(where, `s.canonical_name = ?`)
		args = append(args, query.Server)
	}
	if query.Kind != "" {
		where = append(where, `c.kind = ?`)
		args = append(args, query.Kind)
	}
	availabilityExpression := `CASE WHEN v.expires_at <> '' AND datetime(v.expires_at) < datetime('now') THEN 'stale' ELSE c.availability END`
	if query.Availability != "" {
		where = append(where, availabilityExpression+` = ?`)
		args = append(args, query.Availability)
	}
	if query.Path != "" {
		path := filepath.ToSlash(strings.TrimPrefix(query.Path, "./"))
		globIDs, err := r.matchingCapabilityGlobIDs(path)
		if err != nil {
			return CapabilityPage{}, err
		}
		bindingConditions := []string{
			`b.pattern_type = 'universal'`,
			`(b.pattern_type = 'exact' AND b.path_prefix = ?)`,
			`(b.pattern_type = 'prefix' AND ? LIKE b.path_prefix || '%')`,
		}
		args = append(args, path, path)
		if len(globIDs) > 0 {
			placeholders := make([]string, len(globIDs))
			for i, id := range globIDs {
				placeholders[i] = "?"
				args = append(args, id)
			}
			bindingConditions = append(bindingConditions, `(b.pattern_type = 'glob' AND b.capability_id IN (`+strings.Join(placeholders, ",")+`))`)
		}
		where = append(where, `EXISTS (SELECT 1 FROM mcp_capability_bindings b WHERE b.capability_id = c.id AND (`+strings.Join(bindingConditions, " OR ")+`))`)
	}
	whereSQL := ""
	if len(where) > 0 {
		whereSQL = ` WHERE ` + strings.Join(where, ` AND `)
	}

	matchCount, facets, err := r.queryCapabilityMetadata(from, whereSQL, availabilityExpression, args)
	if err != nil {
		return CapabilityPage{}, err
	}
	page := CapabilityPage{MatchCount: matchCount, Facets: facets}
	if query.Offset >= page.MatchCount {
		return page, nil
	}

	order := ` ORDER BY s.canonical_name, sv.version, c.kind, c.name`
	if len(tokens) > 0 {
		order = ` ORDER BY rank, s.canonical_name, sv.version, c.kind, c.name`
	}
	pageArgs := append([]any(nil), args...)
	limit := ""
	if query.Limit > 0 {
		limit = ` LIMIT ? OFFSET ?`
		pageArgs = append(pageArgs, query.Limit, query.Offset)
	} else if query.Offset > 0 {
		limit = ` LIMIT -1 OFFSET ?`
		pageArgs = append(pageArgs, query.Offset)
	}
	page.Records, err = r.queryCapabilities(capabilityColumns+from+whereSQL+order+limit, pageArgs...)
	return page, err
}

func (r *Registry) queryCapabilityMetadata(from, whereSQL, availabilityExpression string, args []any) (int, CapabilityFacets, error) {
	query := `WITH candidates AS MATERIALIZED (
		SELECT s.canonical_name AS server, c.kind AS kind, ` + availabilityExpression + ` AS availability ` + from + whereSQL + `
	)
	SELECT
		(SELECT COUNT(*) FROM candidates),
		COALESCE((SELECT json_group_array(json_object('value', value, 'count', count)) FROM
			(SELECT server AS value, COUNT(*) AS count FROM candidates GROUP BY server ORDER BY server)), '[]'),
		COALESCE((SELECT json_group_array(json_object('value', value, 'count', count)) FROM
			(SELECT kind AS value, COUNT(*) AS count FROM candidates GROUP BY kind ORDER BY kind)), '[]'),
		COALESCE((SELECT json_group_array(json_object('value', value, 'count', count)) FROM
			(SELECT availability AS value, COUNT(*) AS count FROM candidates GROUP BY availability ORDER BY availability)), '[]')`
	var count int
	var serversJSON, kindsJSON, availabilityJSON string
	if err := r.db.QueryRow(query, args...).Scan(&count, &serversJSON, &kindsJSON, &availabilityJSON); err != nil {
		return 0, CapabilityFacets{}, err
	}
	var facets CapabilityFacets
	if err := json.Unmarshal([]byte(serversJSON), &facets.Servers); err != nil {
		return 0, CapabilityFacets{}, err
	}
	if err := json.Unmarshal([]byte(kindsJSON), &facets.Kinds); err != nil {
		return 0, CapabilityFacets{}, err
	}
	if err := json.Unmarshal([]byte(availabilityJSON), &facets.Availability); err != nil {
		return 0, CapabilityFacets{}, err
	}
	return count, facets, nil
}

// ResolveCapability retrieves one exact current capability definition.
func (r *Registry) ResolveCapability(server, version string, kind capability.CapabilityKind, name string) (*CapabilityRecord, error) {
	records, err := r.queryCapabilities(capabilitySelect+`
		WHERE s.canonical_name = ? AND sv.version = ? AND c.kind = ? AND c.name = ?
		ORDER BY v.visibility, v.auth_partition_hash LIMIT 1`, server, version, kind, name)
	if err != nil || len(records) == 0 {
		return nil, err
	}
	return &records[0], nil
}

// ResolveCapabilityView retrieves a public capability or the exact private
// auth partition. It prevents hosted callers from resolving another tenant's
// cached capability view.
func (r *Registry) ResolveCapabilityView(server, version string, kind capability.CapabilityKind, name, view string) (*CapabilityRecord, error) {
	records, err := r.queryCapabilities(capabilitySelect+`
		WHERE s.canonical_name = ? AND sv.version = ? AND c.kind = ? AND c.name = ?
		AND (v.auth_partition_hash = 'public' OR v.auth_partition_hash = ?)
		ORDER BY CASE WHEN v.auth_partition_hash = ? THEN 0 ELSE 1 END LIMIT 1`, server, version, kind, name, view, view)
	if err != nil || len(records) == 0 {
		return nil, err
	}
	return &records[0], nil
}

const capabilityColumns = `SELECT c.id, s.canonical_name, s.publisher, sv.version,
	sv.package_digest, sv.status, c.kind, c.name, c.title, c.description,
	c.input_schema, c.output_schema, c.schema_digest, c.availability,
	v.visibility, v.auth_partition_hash, v.cache_scope, v.observed_at, v.expires_at, s.source_type,
	s.source_ref, c.risk, c.raw_definition `

const capabilityFrom = `FROM mcp_capabilities c
	JOIN mcp_server_versions sv ON sv.id = c.server_version_id
	JOIN mcp_servers s ON s.id = sv.server_id
	JOIN mcp_capability_views v ON v.id = c.view_id`

const capabilitySelect = capabilityColumns + capabilityFrom

func (r *Registry) queryCapabilities(query string, args ...any) ([]CapabilityRecord, error) {
	rows, err := r.db.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var records []CapabilityRecord
	for rows.Next() {
		var record CapabilityRecord
		var input, output, raw []byte
		var observedAt, expiresAt string
		if err := rows.Scan(&record.ID,
			&record.Capability.Server.Identity.CanonicalName,
			&record.Capability.Server.Identity.Publisher,
			&record.Capability.Server.Version,
			&record.Capability.Server.PackageDigest,
			&record.Capability.Server.Status,
			&record.Capability.Kind, &record.Capability.Name,
			&record.Capability.Title, &record.Capability.Description,
			&input, &output, &record.Capability.SchemaDigest,
			&record.Capability.Availability, &record.Visibility,
			&record.AuthPartitionHash, &record.CacheScope, &observedAt, &expiresAt,
			&record.SourceType, &record.SourceRef, &record.Risk, &raw); err != nil {
			return nil, err
		}
		record.Capability.InputSchemaJSON = append(json.RawMessage(nil), input...)
		record.Capability.OutputSchemaJSON = append(json.RawMessage(nil), output...)
		record.RawDefinition = append(json.RawMessage(nil), raw...)
		record.Capability.ObservedAt = parseOptionalTime(observedAt)
		record.Capability.ExpiresAt = parseOptionalTime(expiresAt)
		if !record.Capability.ExpiresAt.IsZero() && time.Now().After(record.Capability.ExpiresAt) {
			record.Capability.Availability = capability.AvailabilityStale
		}
		records = append(records, record)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	for i := range records {
		bindings, err := r.capabilityBindings(records[i].ID)
		if err != nil {
			return nil, err
		}
		records[i].Bindings = bindings
	}
	return records, nil
}

func formatOptionalTime(value time.Time) string {
	if value.IsZero() {
		return ""
	}
	return value.UTC().Format(time.RFC3339Nano)
}

func parseOptionalTime(value string) time.Time {
	parsed, err := time.Parse(time.RFC3339Nano, value)
	if err != nil {
		return time.Time{}
	}
	return parsed
}

func (r *Registry) capabilityBindings(capabilityID int64) ([]CapabilityBinding, error) {
	rows, err := r.db.Query(`SELECT scope, relationship, auth_profile
		FROM mcp_capability_bindings WHERE capability_id = ?
		ORDER BY scope, relationship, auth_profile`, capabilityID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var bindings []CapabilityBinding
	for rows.Next() {
		var binding CapabilityBinding
		if err := rows.Scan(&binding.Scope, &binding.Relationship, &binding.AuthProfile); err != nil {
			return nil, err
		}
		bindings = append(bindings, binding)
	}
	return bindings, rows.Err()
}
