# Technical Design: Contextual MCP Capability Broker

Status: experimental implementation; open-source milestones 0–4 complete

Last updated: 2026-08-25

## 1. Purpose

This document defines the implementation design for the product requirements and
high-level architecture in this directory. Open-source milestones 0–4 are
implemented behind the experimental version 5 opt-in gate. Sections describing
managed operations remain deployment design rather than bundled local behavior;
the implementation-status document is the source of truth for that boundary.

## 2. Existing system constraints

The current implementation provides:

- a Go 1.23 module with a Go 1.24.3 toolchain;
- SQLite through `modernc.org/sqlite`;
- a schema at `PRAGMA user_version = 4`;
- FTS5 search for skill name, description, headings, and body;
- full refresh through `internal/registry.Refresh`;
- path, package, topic, tag, and text retrieval in `internal/query`;
- project and dependency pack activation in `internal/packs` and
  `internal/scanner`;
- CLI and stdio MCP interfaces over shared query/registry behavior;
- `mark3labs/mcp-go v0.17.0` as the current MCP implementation dependency.

The capability broker must preserve:

- deterministic local queries;
- bounded discovery followed by selected reads;
- CLI and MCP parity;
- versioned, idempotent SQLite migrations;
- repository scope and dependency-boundary behavior;
- source provenance and public/private visibility;
- a reproducible local index where all non-runtime inputs are fixed.

The current MCP dependency predates the target MCP `2026-07-28` feature set. A
protocol compatibility spike must determine whether to upgrade it, contribute
missing support, wrap it behind adapters, or use another implementation. Core
domain packages must not depend directly on one SDK's request/response types.

## 3. Proposed package layout

The final names may change, but responsibilities should remain separate:

```text
internal/
  capability/
    model.go          canonical server/capability domain types
    normalize.go      source-to-canonical normalization
    summarize.go      bounded schema summaries and search documents
    ref.go            capability reference issue/validation
  catalog/
    source.go         catalog source interface
    registry_api.go   standard MCP Registry synchronizer
    static.go         trusted static source
    snapshot.go       signed/publisher capability snapshots
  binding/
    model.go          MCP binding and relationship types
    activate.go       reuse pack activation/scoping semantics
    link.go           code context to canonical capabilities
  broker/
    broker.go         invocation orchestration
    describe.go       selected capability definition retrieval
    result.go         attributed downstream results/errors
  connector/
    connector.go      protocol-neutral downstream interface
    stdio.go          local process transport
    http.go           Streamable HTTP transport
    manager.go        lifecycle, pooling, timeout, circuit breaking
  auth/
    principal.go      user/workload/service principal
    inbound.go        hosted Skillex authentication interface
    outbound.go       downstream auth provider interface
    credentials.go    logical slots and non-exportable handles
    policy.go         auth strategy selection
    providers/        env, dotenv, keychain, helper, OAuth, EMA, etc.
  policy/
    model.go          allow/deny/approval rules
    evaluate.go       layered policy evaluator
  telemetry/
    events.go         stable event/span attributes
    redact.go         centralized redaction
```

Existing packages evolve as follows:

- `internal/packs` parses MCP declarations in addition to skills.
- `internal/scanner` discovers MCP declarations and trusted server facts.
- `internal/linker` may either link both domains or delegate MCP linking to
  `internal/binding`; shared scope classification must not be duplicated.
- `internal/registry` owns capability tables, migrations, persistence, and
  refresh transactions.
- `internal/query` returns skill and capability results through one compatible
  response.
- `mcp` exposes query, read, describe, and call tools over core packages.
- `cli` exposes equivalent commands and structured JSON.

## 4. Domain model

### 4.1 Canonical server identity

```go
type ServerIdentity struct {
    CanonicalName string // reverse-DNS MCP registry name
    Publisher     string // verified namespace/signer identity
}

type ServerVersion struct {
    Identity      ServerIdentity
    Version       string
    PackageDigest string
    Status        string // active, deprecated, deleted
}
```

Friendly names and self-reported `serverInfo` are display metadata only. They are
not used as security identities.

### 4.2 Transport/install variants

```go
type TransportKind string

const (
    TransportStdio          TransportKind = "stdio"
    TransportStreamableHTTP TransportKind = "streamable-http"
)

type Transport struct {
    Kind            TransportKind
    PackageRegistry string
    PackageID       string
    PackageVersion  string
    PackageDigest   string
    CommandTemplate []string
    Endpoint        string
    Origin          string
}
```

Legacy SSE may be represented for compatibility but must not be preferred for
new configurations.

### 4.3 Capability

```go
type CapabilityKind string

const (
    CapabilityTool             CapabilityKind = "tool"
    CapabilityPrompt           CapabilityKind = "prompt"
    CapabilityResourceTemplate CapabilityKind = "resource-template"
)

type Capability struct {
    ID               int64
    ServerVersionID  int64
    ViewID            int64
    Kind              CapabilityKind
    Name              string
    Title             string
    Description       string
    InputSchemaJSON   []byte
    OutputSchemaJSON  []byte
    SchemaDigest      string
    ArgumentSummary   []ArgumentSummary
    Risk              RiskClassification
    DeclaredMetadata  Provenance
    ObservedMetadata  *Provenance
    IndexedAt         time.Time
}
```

`SchemaDigest` is computed over a canonical JSON representation of the fields
needed to validate invocation compatibility.

### 4.4 Capability view

```go
type CapabilityView struct {
    ID                int64
    ServerVersionID   int64
    Visibility        string // public, private
    AuthPartitionHash string
    CacheScope        string // public, private
    TTL               time.Duration
    ObservedAt        time.Time
    ExpiresAt         time.Time
}
```

`AuthPartitionHash` is a non-reversible identifier derived from tenant,
principal/profile, resource, and issuer. It must not contain a token or raw user
identifier. Public views use a fixed public partition.

### 4.5 Binding

```go
type Relationship string

const (
    RelationshipSuggested Relationship = "suggested"
    RelationshipAvailable Relationship = "available"
    RelationshipPreferred Relationship = "preferred"
)

type CapabilityBinding struct {
    ServerIdentity  ServerIdentity
    CapabilityNames []string
    Relationship    Relationship
    Activation      packs.ActivateWhen
    ScopeMode       string
    Scopes          []string
    Source          Provenance
}
```

Package-origin bindings are forced to `suggested` unless a higher-trust layer
promotes them.

### 4.6 Availability

```go
type AvailabilityStatus string

const (
    AvailabilityDiscovered       AvailabilityStatus = "discovered"
    AvailabilitySuggested        AvailabilityStatus = "suggested"
    AvailabilitySetupRequired    AvailabilityStatus = "setup-required"
    AvailabilityCredentialMissing AvailabilityStatus = "credential-missing"
    AvailabilityLoginRequired    AvailabilityStatus = "login-required"
    AvailabilityScopeRequired    AvailabilityStatus = "scope-required"
    AvailabilityReady            AvailabilityStatus = "ready"
    AvailabilityUnsupportedAuth  AvailabilityStatus = "unsupported-authentication"
    AvailabilityPolicyDenied     AvailabilityStatus = "policy-denied"
    AvailabilityUnreachable      AvailabilityStatus = "unreachable"
    AvailabilityStale            AvailabilityStatus = "stale"
)
```

Availability is an overlay computed from indexed facts, trusted configuration,
credential-source presence, prior observations, and policy. It must not be
treated as a permanent property of a capability record.

## 5. Configuration design

### 5.1 Versioning

Root configuration version 5 now provides the explicit project-level MCP gate.
Version 4 remains skills-only. Version 5 also remains skills-only when `MCP` is
absent or `MCP.Enabled` is false. Packs still require a separately versioned
schema change coordinated with `atheory-ai/skillex-packs` before canonical
content is published.

Unknown fields should be rejected in the new security-sensitive configuration
sections. Silent typos in service identity or secret mapping are unsafe.

### 5.2 Pack schema

Proposed lower-case pack fields:

```yaml
name: prisma
version: 1.0.0

mcp-servers:
  - ref: io.github.example/postgres-mcp
    version: 2.1.0
    relationship: suggested
    activate-when:
      detector: prisma
    scope: boundary
    capabilities:
      prefer:
        - schema.inspect
        - query.explain
```

Proposed Go types:

```go
type Manifest struct {
    Name        string         `yaml:"name"`
    Version     string         `yaml:"version"`
    Description string         `yaml:"description"`
    Source      string         `yaml:"source"`
    Detectors   Detectors      `yaml:"detectors"`
    Skills      []SkillRef     `yaml:"skills"`
    MCPServers  []MCPServerRef `yaml:"mcp-servers"`
}

type MCPServerRef struct {
    Ref           string          `yaml:"ref"`
    Version       string          `yaml:"version"`
    Relationship  Relationship    `yaml:"relationship"`
    ActivateWhen  ActivateWhen    `yaml:"activate-when"`
    Scope         string          `yaml:"scope"`
    Files         []string        `yaml:"files"`
    Capabilities  CapabilityHints `yaml:"capabilities"`
}
```

Validation requirements:

- `ref` must be a canonical registry identity or approved private namespace;
- versions must be exact in the initial implementation;
- package-provided relationships may not exceed `suggested`;
- auth profiles, secret sources, raw headers, executable overrides, and
  credentials are forbidden in package-provided entries;
- activation and scope semantics reuse existing pack code;
- conflicts preserve provenance and are resolved by policy, not load order.

### 5.3 Trusted user/enterprise config

Trusted auth configuration should live outside code-shipped packs. A proposed
shape is:

```yaml
auth-providers:
  corporate-sso:
    type: enterprise-managed
    issuer: https://login.example.com
    client-metadata: https://skillex.example/client.json
    token-store: system-keychain

credential-profiles:
  github-work:
    service: io.github.github/github-mcp-server
    credentials:
      access-token:
        sources:
          - env:
              key: GH_TOKEN
          - dotenv:
              path: "${projectRoot}/.env.mcp"
              key: GITHUB_TOKEN
        inject:
          stdio-env: GITHUB_PERSONAL_ACCESS_TOKEN
          http-header:
            name: Authorization
            format: "Bearer ${value}"

service-policy:
  io.github.github/github-mcp-server:
    allowed-versions: ["1.8.0"]
    allowed-origins: ["https://api.githubcopilot.com"]
    auth-profiles: [github-work, corporate-sso]
```

The parser must preserve which layer supplied every value. Enterprise-managed
configuration may make selected fields non-overridable.

### 5.4 Project opt-in and profile selection

A repository may request an existing trusted profile only if policy allows:

```yaml
Version: 5
MCP:
  Enabled: true
  Bindings:
    - Server: io.github.github/github-mcp-server
      Version: 1.8.0
      AuthProfile: github-work
      Scope: "**"
```

The user sees and approves the first repository-to-profile association unless an
enterprise policy preauthorizes it. Repository configuration cannot define or
modify `github-work`. `Server`, exact `Version`, and `Scope` are required. An
auth profile is optional for servers that support unauthenticated operation.
Bindings are invalid unless `Enabled` is true, and version 5 rejects unknown
fields.

## 6. Registry schema

The proposed schema is additive to the existing skill tables. Exact migration
numbers are assigned when implementation begins.

```sql
CREATE TABLE mcp_servers (
    id              INTEGER PRIMARY KEY,
    canonical_name  TEXT NOT NULL UNIQUE,
    publisher       TEXT NOT NULL,
    title           TEXT,
    description     TEXT,
    repository_url  TEXT,
    source_type     TEXT NOT NULL,
    source_ref      TEXT NOT NULL,
    indexed_at      TEXT NOT NULL
);

CREATE TABLE mcp_server_versions (
    id              INTEGER PRIMARY KEY,
    server_id       INTEGER NOT NULL REFERENCES mcp_servers(id) ON DELETE CASCADE,
    version         TEXT NOT NULL,
    package_digest  TEXT,
    status          TEXT NOT NULL,
    raw_metadata    BLOB NOT NULL,
    indexed_at      TEXT NOT NULL,
    UNIQUE(server_id, version)
);

CREATE TABLE mcp_transports (
    id                  INTEGER PRIMARY KEY,
    server_version_id   INTEGER NOT NULL REFERENCES mcp_server_versions(id) ON DELETE CASCADE,
    kind                TEXT NOT NULL,
    package_registry    TEXT,
    package_id          TEXT,
    package_version     TEXT,
    package_digest      TEXT,
    endpoint            TEXT,
    command_template    BLOB,
    origin              TEXT,
    UNIQUE(server_version_id, kind, package_id, endpoint)
);

CREATE TABLE mcp_capability_views (
    id                  INTEGER PRIMARY KEY,
    server_version_id   INTEGER NOT NULL REFERENCES mcp_server_versions(id) ON DELETE CASCADE,
    visibility          TEXT NOT NULL,
    auth_partition_hash TEXT NOT NULL,
    cache_scope         TEXT NOT NULL,
    ttl_ms              INTEGER NOT NULL,
    observed_at         TEXT NOT NULL,
    expires_at          TEXT NOT NULL,
    provenance          BLOB NOT NULL,
    UNIQUE(server_version_id, auth_partition_hash)
);

CREATE TABLE mcp_capabilities (
    id                  INTEGER PRIMARY KEY,
    view_id             INTEGER NOT NULL REFERENCES mcp_capability_views(id) ON DELETE CASCADE,
    kind                TEXT NOT NULL,
    name                TEXT NOT NULL,
    title               TEXT,
    description         TEXT,
    input_schema        BLOB,
    output_schema       BLOB,
    schema_digest       TEXT NOT NULL,
    argument_summary    BLOB,
    risk                TEXT NOT NULL,
    declared_provenance BLOB,
    observed_provenance BLOB,
    indexed_at          TEXT NOT NULL,
    UNIQUE(view_id, kind, name)
);

CREATE VIRTUAL TABLE mcp_capability_search USING fts5(
    capability_id UNINDEXED,
    capability_name,
    capability_title,
    capability_description,
    argument_text,
    server_name,
    server_description,
    binding_text
);

CREATE TABLE mcp_bindings (
    id                  INTEGER PRIMARY KEY,
    server_id           INTEGER NOT NULL REFERENCES mcp_servers(id) ON DELETE CASCADE,
    capability_pattern  TEXT,
    relationship        TEXT NOT NULL,
    scope               TEXT NOT NULL,
    path_prefix         TEXT NOT NULL DEFAULT '',
    pattern_type        TEXT NOT NULL DEFAULT 'glob',
    activation          BLOB NOT NULL,
    source_type         TEXT NOT NULL,
    source_ref          TEXT NOT NULL,
    trust_level         TEXT NOT NULL
);

CREATE TABLE mcp_runtime_state (
    server_version_id   INTEGER NOT NULL REFERENCES mcp_server_versions(id) ON DELETE CASCADE,
    profile_id          TEXT NOT NULL,
    status              TEXT NOT NULL,
    auth_method         TEXT,
    last_success_at     TEXT,
    last_failure_at     TEXT,
    error_class         TEXT,
    schema_digest       TEXT,
    PRIMARY KEY(server_version_id, profile_id)
);
```

Indexes should cover canonical identity, version, scope prefix, relationship,
status, view partition, capability kind/name, and runtime state.

### 6.1 Secret exclusion

Database insertion APIs must reject known secret-bearing fields. Raw registry and
capability metadata must be structurally scrubbed before storage. No token,
cookie, authorization header, private key, password, or resolved dotenv value may
enter these tables.

### 6.2 Refresh behavior

Phase 1 may continue full local rebuilds for repository-derived bindings while
maintaining a separately synchronized catalog. A later migration can make all
sources incremental. Catalog synchronization must use source cursors or
`updated_since` where available and apply updates transactionally.

`Registry.Clear` must be split so a repository refresh does not erase globally
synchronized server metadata or private hosted catalog data.

## 7. Capability ingestion

### 7.1 Catalog source interface

```go
type CatalogSource interface {
    Name() string
    Sync(ctx context.Context, cursor string, limit int) (CatalogPage, error)
}

type CatalogPage struct {
    Servers    []DeclaredServer
    NextCursor string
    Complete   bool
}
```

The standard Registry API adapter stores exact versions and transport metadata.
Registry search is not used at query time.

### 7.2 Capability source interface

```go
type CapabilitySource interface {
    Capabilities(ctx context.Context, server ServerVersion, view ViewRequest) (CapabilitySnapshot, error)
}
```

Implementations:

- publisher snapshot source;
- trusted remote introspection source;
- trusted stdio introspection source;
- prior runtime observation source.

### 7.3 Introspection safety

Remote introspection:

- only for approved origins;
- uses no credential for the public view;
- uses a named profile for a private view;
- applies timeouts, response-size limits, schema-depth limits, and pagination
  limits;
- respects `ttlMs` and `cacheScope`;
- never follows an endpoint redirect to an unapproved origin with credentials.

Stdio introspection:

- only after package identity/version/digest trust;
- uses an explicit executable and argument template;
- uses a minimal environment;
- starts in a controlled working directory;
- applies sandbox and egress policy where supported;
- enforces startup and list timeouts;
- captures logs only from stderr with redaction;
- terminates the process after introspection unless a managed idle pool retains
  it safely.

### 7.4 Search-document construction

Searchable fields and initial relative weights:

| Field | Relative weight |
|---|---:|
| Exact capability name/title | 10 |
| Capability description | 8 |
| Explicit pack intended use/preference | 7 |
| Argument names/descriptions | 5 |
| Output summary | 3 |
| Server title/description | 2 |
| Server instructions | 1 |

SQLite FTS5 BM25 column weights should implement the first version. Embeddings
are explicitly not required for MVP. Ranking tests must cover natural-language
synonyms supplied through descriptions and bindings rather than hidden model
inference.

Readiness is applied as a modest post-search boost or annotation. It must not
promote an unrelated ready tool over a strongly relevant setup-required tool.

## 8. Query contract

### 8.1 Backward-compatible response

The existing response uses `results` for skill results. The first additive
version preserves that field and adds `capabilities`:

```go
type Response struct {
    Type          ResponseType      `json:"type"`
    Results       []Result          `json:"results,omitempty"` // existing skills
    Capabilities  []CapabilityResult `json:"capabilities,omitempty"`
    Vocabulary    *Vocabulary       `json:"vocabulary,omitempty"`
    Query         *Echo             `json:"query,omitempty"`
    MatchCount    int               `json:"match_count,omitempty"`
    ReturnedCount int               `json:"returned_count,omitempty"`
    TooBroad      bool              `json:"too_broad,omitempty"`
    NextCursor    string            `json:"next_cursor,omitempty"`
    NarrowWith    *NarrowWith       `json:"narrow_with,omitempty"`
}
```

Existing clients ignore the additive field. A later major response version may
rename `results` to `skills` after an explicit compatibility period.

Count semantics must be unambiguous. The additive version should add category
counts while preserving existing skill count fields:

```json
{
  "skill_match_count": 3,
  "capability_match_count": 7,
  "match_count": 3
}
```

`match_count` retains its existing meaning until a versioned response changes it.

### 8.2 Query parameters

Existing filters apply to skills. The following additive parameters are
proposed:

```go
type Params struct {
    // existing fields...
    Include       []ResultDomain // skills, capabilities; default both for MCP after rollout
    CapabilityKind []CapabilityKind
    Server         string
    Availability   []AvailabilityStatus
    Principal      auth.Principal // derived, not supplied by an untrusted tool argument
}
```

CLI flags may include:

```text
--include skills,capabilities
--capability-kind tool
--server io.github.example/postgres-mcp
--availability ready,login-required
```

The MCP handler derives the principal from connection/request context and never
accepts arbitrary tenant, subject, or group claims in `skillex_query` arguments.

### 8.3 Capability result

```go
type CapabilityResult struct {
    Ref             string             `json:"ref"`
    Kind            CapabilityKind     `json:"kind"`
    Name            string             `json:"name"`
    Title           string             `json:"title,omitempty"`
    Description     string             `json:"description,omitempty"`
    Server          ServerSummary      `json:"server"`
    Arguments       []ArgumentSummary  `json:"arguments,omitempty"`
    Risk            RiskClassification `json:"risk"`
    Availability    Availability       `json:"availability"`
    Relationship    Relationship       `json:"relationship,omitempty"`
    Scopes          []string           `json:"scopes,omitempty"`
    Score           float64            `json:"score,omitempty"`
    MatchedIn       []string           `json:"matched_in,omitempty"`
    SchemaBytes     int                `json:"schema_bytes,omitempty"`
    DescribeRequired bool              `json:"describe_required,omitempty"`
}
```

The query response never contains auth tokens, raw headers, executable command
overrides, or secret-source locations.

### 8.4 Pagination

Skills and capabilities should initially use independent cursors so adding
capabilities cannot reorder existing skill pages. A composite cursor may contain
both offsets/version fingerprints but must preserve deterministic replay against
an unchanged index.

### 8.5 No-match and narrowing

Vocabulary expands with server names, capability kinds, readiness states, and
top contextual relationships. Candidate-scoped facets must not reveal hidden or
private capabilities.

## 9. Capability references

### 9.1 Format

Capability references are integrity-protected opaque strings:

```text
mcp-tool:v1:<base64url(payload)>.<base64url(mac)>
```

Payload fields:

```json
{
  "server": "io.github.example/postgres-mcp",
  "version": "2.1.0",
  "capability": "schema.inspect",
  "kind": "tool",
  "schema_digest": "sha256:...",
  "view": "private:7c4...",
  "context": "sha256:...",
  "issued_at": 1787160000,
  "expires_at": 1787160600,
  "nonce": "..."
}
```

Local mode uses an instance key stored with restrictive filesystem permissions.
Hosted mode uses a rotating tenant-aware signing service. References contain no
secret or raw identity.

### 9.2 Validation

Invocation validates:

- prefix and version;
- signature using constant-time comparison;
- expiry and optional single-use policy;
- current context fingerprint;
- server identity/version trust;
- current capability existence and schema digest;
- current visibility for the principal;
- current policy and readiness.

Reference validity never bypasses authorization.

### 9.3 Write-capable references

Policy may issue shorter-lived or single-use references for destructive tools.
The first version should at least support a `max_age` policy and nonce replay
tracking in hosted mode.

## 10. Describe contract

`skillex_mcp_describe` accepts a capability reference and a byte budget. It
returns:

- canonical server/version and transport class;
- exact tool name/title/description;
- bounded input and output schemas;
- annotations and Skillex-derived risk classification, clearly distinguished;
- required scopes and selected auth method when known;
- current availability and policy state;
- reference refresh action if stale.

Large schemas are truncated only on schema boundaries; otherwise the response
returns a clear error and next action rather than invalid JSON.

## 11. Invocation contract

### 11.1 MCP tool

```text
skillex_mcp_call
```

Input:

```json
{
  "ref": "mcp-tool:v1:...",
  "arguments": {
    "database": "development"
  }
}
```

The tool schema for `arguments` is a generic JSON object because the selected
downstream schema is dynamic. Skillex performs the authoritative validation.

### 11.2 Result envelope

```json
{
  "server": {
    "name": "io.github.example/postgres-mcp",
    "version": "2.1.0"
  },
  "capability": {
    "kind": "tool",
    "name": "schema.inspect"
  },
  "result": {
    "content": [],
    "structuredContent": {}
  },
  "telemetry": {
    "trace_id": "..."
  }
}
```

The broker preserves downstream content types where the host-facing SDK allows
it. It must not silently convert an error result into successful text.

### 11.3 Broker algorithm

```text
1. Parse and verify the capability reference.
2. Load the current indexed capability and raw schema.
3. Resolve repository context and authenticated principal.
4. Re-evaluate trust and policy.
5. Compare the referenced and current schema digests.
6. Validate arguments using bounded JSON Schema 2020-12 evaluation.
7. Classify the actual operation for approval and telemetry.
8. Resolve the allowed auth strategy and credential profile.
9. Acquire a non-exportable credential handle or initiate auth interaction.
10. Resolve and verify the permitted transport.
11. Connect/start through the connector manager.
12. Re-discover/re-list if required by freshness or write policy.
13. Invoke the downstream tool with timeout and cancellation propagation.
14. Handle input-required interactions and retry state.
15. Validate output when a schema is present.
16. Record runtime state and emit redacted telemetry.
17. Return an attributed result.
```

### 11.4 Schema evaluation limits

JSON Schema validation must enforce configurable maximums for:

- schema bytes;
- nesting depth;
- number of `$ref` resolutions;
- validation duration;
- argument bytes;
- output bytes.

External `$ref` retrieval is forbidden during query and invocation. Definitions
must be self-contained or rejected/marked unsupported.

### 11.5 Approval

The policy decision includes:

```go
type Decision struct {
    Effect          string // allow, deny, approval-required
    ReasonCodes     []string
    DownstreamLabel string
    OperationLabel  string
    Risk            RiskClassification
}
```

Approval UI content names the real server/tool, arguments after redaction, data
destination, and derived effects. If the host supports MCP multi-round-trip input
requirements, use that mechanism. Otherwise the CLI/MCP adapter returns a typed
interaction-required error and retry instructions.

## 12. Authentication design

### 12.1 Separate inbound and outbound roles

```go
type InboundAuthenticator interface {
    Authenticate(ctx context.Context, request RequestIdentity) (Principal, error)
}

type OutboundAuthProvider interface {
    Name() string
    Supports(requirement AuthRequirement, principal Principal) bool
    Acquire(ctx context.Context, req GrantRequest) (CredentialHandle, error)
}
```

Inbound authenticators verify callers of hosted Skillex. Outbound providers
obtain credentials for downstream resources. A Skillex access token is never
automatically an outbound credential.

### 12.2 Credential handle

```go
type CredentialHandle interface {
    Method() string
    ExpiresAt() time.Time
    Inject(ctx context.Context, sink CredentialSink) error
    Close() error
}
```

There is intentionally no `Value() string` method. Providers inject into an exact
approved sink. Connector code receives the sink output, not general access to
the credential store.

### 12.3 Static source confinement

```go
type SecretSource interface {
    ResolveExact(ctx context.Context, service ServerIdentity, slot CredentialSlot) (SecretHandle, Presence, error)
}
```

Environment provider:

- receives exactly one configured key;
- calls `LookupEnv` for that key only;
- does not expose `os.Environ`;
- reports presence separately from value resolution.

Dotenv provider:

- resolves the configured template against an approved project root;
- rejects traversal and unsafe symlink escape;
- reads with a size limit;
- parses only enough to return the exact configured key;
- never merges the file into process environment;
- never records values in errors or telemetry.

Stdio injection constructs a new allowlisted environment rather than inheriting
the broker process environment. A minimal platform baseline such as `PATH`, temp
directory, and locale must be explicit and policy-controlled.

### 12.4 OAuth provider

The standard OAuth provider must support:

- OAuth Protected Resource Metadata discovery;
- authorization server metadata and OIDC discovery;
- authorization code with PKCE;
- Client ID Metadata Documents as the preferred modern client registration;
- Dynamic Client Registration only as a compatibility fallback;
- issuer validation;
- resource indicators and audience binding;
- incremental scopes;
- refresh tokens where supported;
- secure state, nonce, and redirect handling;
- encrypted/token-store persistence.

Redirect and browser interaction belong to the interface layer. Core auth code
returns typed actions rather than launching a browser itself.

### 12.5 Enterprise-Managed Authorization

The EMA provider:

1. obtains the authenticated user's identity assertion from the configured
   enterprise session provider;
2. requests an ID-JAG through token exchange at the enterprise IdP;
3. binds ID-JAG `aud` to the downstream authorization server issuer and
   `resource` to the downstream MCP resource;
4. exchanges the ID-JAG at the downstream authorization server for an access
   token;
5. validates returned token metadata and stores it under the target partition.

ID-JAGs are short-lived grants, not general downstream bearer tokens. The
provider must prevent replay and cross-resource reuse.

### 12.6 Workload and service principals

Client credentials, workload OIDC, JWT bearer, cloud identity, and mTLS providers
produce service-principal credentials. Policy must distinguish them from user
delegation. A service identity must never be silently substituted for a user when
the downstream operation requires user attribution.

### 12.7 Authentication state machine

```text
unknown
  -> public
  -> configured
  -> credential-missing
  -> login-required
  -> exchanging
  -> ready
  -> scope-required
  -> expired
  -> policy-denied
  -> unsupported
  -> error
```

State transitions are recorded without tokens. `ready` means a usable credential
or provider session is currently available, not that future authorization is
guaranteed.

## 13. Connector design

### 13.1 Protocol-neutral interface

```go
type Connector interface {
    Discover(ctx context.Context) (DiscoverResult, error)
    ListTools(ctx context.Context, cursor string) (ToolPage, error)
    DescribeTool(ctx context.Context, name string) (ToolDefinition, error)
    CallTool(ctx context.Context, name string, arguments any, opts CallOptions) (CallResult, error)
    Close() error
}

type ConnectorFactory interface {
    Open(ctx context.Context, transport Transport, credential CredentialHandle, policy ConnectorPolicy) (Connector, error)
}
```

The adapter layer converts SDK-specific structures to canonical domain types.

### 13.2 Version negotiation

Target `2026-07-28` first and support configured fallback to older protocol
revisions. Cache negotiation per concrete endpoint/process instance. Protocol
version is part of runtime state and telemetry.

The compatibility spike must cover:

- stateless per-request metadata;
- `server/discover`;
- caching fields;
- multi-round-trip input requirements;
- list-change subscriptions;
- Streamable HTTP routing headers;
- trace-context propagation;
- legacy server fallback.

### 13.3 Connection lifecycle

Remote HTTP connectors may pool transport resources but must attach credentials
per request and partition caches correctly. Stdio connectors are keyed by exact
server version, command, working directory, profile, sandbox policy, and project
context. Idle shutdown defaults should be short until server behavior is proven.

### 13.4 Redirect and origin policy

Credentials are never attached across an unapproved redirect. Remote origins are
canonicalized and matched against the trust decision. DNS rebinding and local
network access require explicit threat-model tests.

## 14. Policy design

### 14.1 Inputs

Policy receives:

- principal and tenant;
- repository identity and path;
- server identity/version/digest/transport;
- capability name/kind/schema digest;
- declared and derived risk;
- requested arguments after redaction classification;
- selected auth method/profile/scopes;
- source provenance and relationship;
- deployment environment;
- runtime health and freshness.

### 14.2 Precedence

```text
hard enterprise deny
  > enterprise requirement/allow
    > user deny/allow
      > approved repository binding
        > project suggestion
          > dependency suggestion
```

No lower layer can weaken a higher-layer restriction. Equivalent additive allows
may merge only when the schema explicitly defines merging.

### 14.3 Decision caching

Read-only query visibility decisions may be cached by a policy-input digest.
Invocation decisions for write/destructive actions are re-evaluated and not
reused beyond a short configured horizon.

## 15. Telemetry design

### 15.1 Span model

```text
skillex.query
  skillex.capability.rank

skillex.mcp.invoke
  skillex.policy.evaluate
  skillex.auth.acquire
  skillex.connector.open
  mcp.server.discover
  mcp.tools.call
```

Use W3C trace context supported by MCP where possible so downstream spans can
join the same trace.

### 15.2 Default attributes

- canonical server and version;
- capability kind and name;
- source and relationship;
- availability and policy decision;
- auth method, never credential identity/value;
- pseudonymous tenant/principal/workspace identifiers;
- protocol and transport;
- outcome and error class;
- total and downstream latency;
- bytes, with content excluded;
- cache and schema-refresh outcome.

### 15.3 Payload policy

Prompt text, search text, arguments, results, URIs, code, and resource contents
are excluded by default. Optional content inspection is a distinct enterprise
feature with explicit policy, redaction, retention, residency, and access
controls. It is not enabled by a generic debug flag.

### 15.4 Local defaults

Local open source mode defaults to no remote telemetry export. Operators may
enable an OpenTelemetry exporter. Basic local operational counters may remain
ephemeral unless explicitly persisted.

## 16. CLI and MCP interfaces

### 16.1 MCP tools

- `skillex_query`: additive capability results.
- `skillex_read`: unchanged selected skill read.
- `skillex_mcp_describe`: selected capability definition.
- `skillex_mcp_call`: selected downstream invocation.

MCP tool handlers remain thin adapters over shared packages. They derive
principal and request context from the authenticated transport, not model-supplied
arguments.

### 16.2 CLI commands

Proposed commands:

```text
skillex query [existing flags] [capability flags]
skillex capability describe --ref <ref>
skillex capability call --ref <ref> --arguments <json-or-stdin>
skillex auth status [--server <identity>] [--profile <name>]
skillex auth login --server <identity> [--profile <name>]
skillex catalog sync [--source <name>]
```

The existing `skillex mcp` command remains the command that starts the host-facing
Skillex MCP server.

Interactive browser/login actions must be explicit in CLI mode. `--json` emits
typed actions and errors suitable for automation.

## 17. Error model

Define stable machine-readable codes:

```text
CAPABILITY_REF_INVALID
CAPABILITY_REF_EXPIRED
CAPABILITY_SCHEMA_CHANGED
CAPABILITY_NOT_VISIBLE
CAPABILITY_POLICY_DENIED
CAPABILITY_APPROVAL_REQUIRED
AUTH_CREDENTIAL_MISSING
AUTH_LOGIN_REQUIRED
AUTH_SCOPE_REQUIRED
AUTH_METHOD_UNSUPPORTED
AUTH_TOKEN_EXPIRED
AUTH_EXCHANGE_FAILED
SERVER_UNTRUSTED
SERVER_IDENTITY_CHANGED
SERVER_UNREACHABLE
SERVER_PROTOCOL_UNSUPPORTED
TOOL_ARGUMENT_INVALID
TOOL_RESULT_INVALID
TOOL_CALL_FAILED
```

Errors contain safe server/tool display identities, reason codes, retryability,
and next actions. They never contain secret values, raw tokens, authorization
headers, or unredacted downstream payloads.

## 18. Caching and freshness

### 18.1 Catalog cache

Registry metadata is synchronized using cursors/incremental timestamps and stored
locally. Query never calls the registry.

### 18.2 Capability cache

Respect MCP `ttlMs` and `cacheScope`:

- public capability views may be shared under the same server/version/schema;
- private views are partitioned by authorization context;
- expired views remain searchable only with `stale` status if policy permits;
- write-capable invocations revalidate expired schemas before execution;
- list-change events invalidate affected views.

### 18.3 Credential cache

Credential providers own token caching. Cache keys include issuer, subject or
service principal, client identity, resource, scopes, tenant, and auth method.
Tokens are refreshed before expiry with jitter and never placed in SQLite.

## 19. Concurrency and transactions

- SQLite remains single-writer with WAL and busy timeout.
- Repository refresh builds into a transaction or temporary database and swaps
  only after validation.
- Catalog synchronization applies a page atomically and persists the cursor only
  after commit.
- Query uses read snapshots and does not block on network work.
- Connector manager deduplicates simultaneous opens for the same safe key.
- Auth provider refresh uses single-flight behavior per token cache key.
- Invocation cancellation propagates to auth, connector, and downstream call.
- Hosted mode requires cross-process coordination for reference replay, token
  refresh, circuit breaking, and change-event publication.

## 20. Testing strategy

### 20.1 Unit tests

Pack/configuration:

- valid MCP declaration parsing;
- package relationship forced to `suggested`;
- forbidden secret/auth fields rejected;
- canonical identity and exact-version validation;
- activation and scope parity with skills.

Index and search:

- normalization and provenance retention;
- canonical JSON/schema digest stability;
- FTS field weighting;
- structured filtering and candidate-scoped facets;
- private-view exclusion;
- deterministic ordering and pagination;
- schema migration and fresh-index behavior.

References:

- signature, expiry, context, digest, and version validation;
- tampering and replay;
- key rotation;
- no secret/identity leakage.

Credential sources:

- exact environment-key lookup only;
- dotenv exact-key lookup, size limit, traversal, and symlink escape;
- minimal stdio environment;
- redacted errors;
- server/profile/source binding;
- enterprise precedence.

Policy:

- deny precedence;
- user/workload distinctions;
- version/origin drift;
- approval classifications;
- cached-decision invalidation.

### 20.2 Fake MCP servers

Test fixtures should include deterministic servers for:

- public tools;
- OAuth authorization code and PKCE;
- Client ID Metadata Documents;
- Dynamic Client Registration compatibility;
- Enterprise-Managed Authorization/ID-JAG;
- client credentials/workload identity;
- static bearer header;
- stdio environment credential;
- changing tool lists and TTL expiry;
- public and private cache scopes;
- multi-round-trip input requirements;
- invalid/malicious schemas;
- malicious descriptions/annotations;
- redirects to an unapproved origin;
- timeouts, crashes, malformed JSON-RPC, and oversized results.

### 20.3 Integration tests

- registry sync -> index -> query;
- pack suggestion -> user trust -> capability readiness;
- query -> describe -> invoke;
- missing auth -> login/action -> retry;
- identity token -> ID-JAG -> downstream access token -> call;
- service principal -> workload/client credential -> call;
- schema change between query and invoke;
- policy change between query and invoke;
- downstream failure isolation;
- trace propagation across broker and fake downstream.

### 20.4 Acceptance and golden tests

Observable CLI and MCP behavior requires:

- additive query JSON golden fixtures;
- human-readable skill/capability summaries;
- broad query `too_broad` and narrowing behavior;
- exact-match and natural-language capability searches;
- no-match vocabulary without hidden capability leakage;
- capability describe byte budgets;
- invocation result and typed errors;
- CLI/MCP parity;
- generated `AGENTS.md` guidance if the recommended retrieval flow changes.

Use the repository's existing acceptance/golden conventions. Query/MCP behavior
changes require `make test-acceptance`; concurrent registry and connector work
requires race coverage.

### 20.5 Security tests

Required adversarial cases:

- dependency suggests attacker server and requests a known sensitive env key;
- server identity changes endpoint after approval;
- dotenv path escapes through traversal or symlink;
- remote redirect attempts credential forwarding;
- private capability view appears to another principal;
- reference payload or MAC is modified;
- stale write reference is replayed;
- tool schema attempts external `$ref` retrieval or excessive recursion;
- downstream error contains credential material;
- telemetry exporter receives arguments/results despite default exclusion;
- stdio child enumerates inherited environment;
- hosted tenant/cache key collision;
- host token is passed through as downstream token.

## 21. Performance validation

Add benchmarks for:

- refresh and migration with 10,000 skills and 50,000 capabilities;
- FTS search latency and top-k ranking;
- path/scope filtering across monorepo-shaped bindings;
- capability reference issue/validation;
- JSON Schema validation at configured limits;
- concurrent queries during refresh;
- connector reuse and cold start;
- auth single-flight and cache lookup;
- telemetry overhead with export disabled and enabled.

Queries must be benchmarked with both exact and broad search terms. A fast broad
query is not useful unless bounded results and narrowing remain relevant.

## 22. Migration and compatibility plan

### 22.1 Database

- Add tables through an idempotent migration.
- Preserve skill tables and refs unchanged.
- Backfill no capability data during migration; explicit refresh/sync populates
  the new domain.
- Bump `currentSchemaVersion` only with migration and fresh-index tests.
- Update registry signatures to include relevant capability inputs without
  including runtime tokens or volatile health timestamps.

### 22.2 Configuration

- Existing Version 4 configurations continue to work with no MCP capability
  behavior and cannot contain MCP fields.
- Version 5 is required for MCP root fields and still defaults to skills-only.
- Broker construction requires `MCP.Enabled: true` plus at least one exact
  server-version binding.
- Existing packs with only skills remain valid.
- Packs using `mcp-servers` require the new pack schema/version.

### 22.3 Query

- Preserve existing `results` skill output.
- Add `capabilities` and category counts.
- Existing queries may default to skill-only for one compatibility release in
  CLI human output while MCP structured output opts into both; the final default
  must be documented and tested.
- No existing caller is forced to invoke or configure MCP capabilities.

### 22.4 MCP

- Existing `skillex_query` and `skillex_read` names remain.
- New tools are additive.
- Upgrade/fallback behavior for protocol versions is isolated behind adapters.

## 23. Implementation sequence

### Milestone 0: Protocol and threat-model spike

- Build a minimal Skillex-as-server/Skillex-as-client prototype against two fake
  downstream servers.
- Confirm current Go SDK gaps for MCP `2026-07-28`.
- Validate generic `skillex_mcp_call` behavior in at least two harnesses.
- Review threat model for code-shipped suggestions and credential mapping.
- Decide reference signing and local key storage.

Exit criteria:

- no downstream host registration;
- one query can return a dynamic capability ref;
- one call can invoke a fake downstream tool;
- the approval result identifies the real tool;
- a malicious pack cannot access an unmapped env variable.

### Milestone 1: Static catalog and capability search

- Add domain types and schema migration.
- Add trusted static and Registry API sources.
- Add capability snapshot ingestion without process execution.
- Extend pack parsing/linking for suggestions.
- Extend query response and CLI/MCP goldens.

Exit criteria:

- offline contextual search returns tool-level summaries and readiness;
- current skill-only behavior remains compatible;
- broad discovery remains bounded.

### Milestone 2: Local introspection and invocation

- Add protocol-neutral connectors.
- Add trusted HTTP and stdio introspection.
- Add describe and call.
- Add no-auth, env, dotenv, keychain, and helper providers.
- Add policy and reference validation.
- Add local telemetry.

Exit criteria:

- trusted local and remote tools invoke end to end;
- secrets remain confined and redacted;
- query performs no network access;
- schema and policy changes invalidate stale calls.

### Milestone 3: OAuth

- Add protected-resource/auth-server discovery.
- Add PKCE, CIMD, secure token storage, refresh, and scopes.
- Add compatibility DCR.
- Add typed login and retry interactions.

Exit criteria:

- OAuth fake-server conformance tests pass;
- tokens are resource/issuer bound;
- login is lazy and resumable.

### Milestone 4: Enterprise identity and hosted foundations

- Add inbound principal authentication.
- Add tenant isolation.
- Add EMA/ID-JAG.
- Add workload/client credential providers.
- Add hosted reference signing, audit, and secure stores.

Exit criteria:

- user and workload flows retain correct attribution;
- no cross-tenant index, cache, token, or telemetry exposure;
- enterprise policy is enforced at query and invocation.

### Milestone 5: Managed operations

- Add managed registry sources and background synchronization.
- Add connector fleet controls and regional egress.
- Add dashboards, usage analytics, and operational alerting.
- Publish provider and subregistry integration documentation.

## 24. Documentation and release work

Each externally visible milestone must update:

- `README.md` product and configuration documentation;
- generated `AGENTS.md` retrieval instructions where appropriate;
- CLI help and examples;
- MCP tool descriptions;
- manual testing scenarios;
- schema references and migration guidance;
- security documentation and threat model;
- `atheory-ai/skillex-packs` contracts when pack schema changes.

Version bumps and release actions remain separate from feature implementation.

## 25. Implemented decisions

1. Protocol-neutral connector interfaces isolate a direct MCP `2026-07-28`
   stateless adapter from the older host-facing SDK.
2. Capability results are additive; version 4 and version 5 with MCP disabled
   retain skills-only behavior. Skills and capabilities have independent offsets
   inside one backward-compatible cursor envelope.
3. Project schema version 5 and trusted schema version 1 are strict. Packs may
   suggest exact server versions and capability names but cannot define execution
   or authentication.
4. Canonical name plus exact server version is the routing identity. Registry
   namespace, package/remote transport provenance, and package hashes are retained;
   trusted execution configuration remains a separate allowlist.
5. References are short-lived HMAC envelopes bound to context, view, routing
   scope, auth profile, schema digest, issue/expiry time, and nonce. Every call
   reauthorizes and revalidates live schema.
6. Local OAuth records are AES-GCM encrypted with an owner-only key. Browser
   interaction is represented as typed CLI continuation rather than launched by core.
7. The provider set is env, dotenv, keychain, helper, mTLS, authorization-code
   PKCE/CIMD/DCR, client credentials, private-key JWT, workload token exchange,
   and EMA/ID-JAG.
8. Stdio receives an empty allowlisted environment and an explicitly trusted
   absolute executable. Platform process sandboxing remains the launcher's policy.
9. Telemetry is disabled by default and excludes arguments, results, paths,
   tokens, credential identities, and headers. Local JSONL and aggregate summary
   are the open-source sinks.
10. Hosted foundations accept verified JWT/IAP-style or exact mTLS principals and
    derive opaque tenant partitions and tenant-specific signing keys. Deployment
    frameworks, KMS/HSMs, regional connector fleets, and dashboards remain
    operator choices.
