# MCP Capability Broker Implementation Status

Last updated: 2026-08-20

## Current milestone

The open-source capability-broker scope in milestones 0–4 is implemented.
Milestone 5 is an operating model (managed synchronization, regional connector
fleets, dashboards, and alerting) rather than a prerequisite for the local or
self-hosted broker.

The implemented foundation includes:

- `internal/capability`: canonical capability identities, stable schema
  digests, and integrity-protected short-lived references;
- `internal/broker`: offline discovery, selected describe, context and schema
  revalidation, policy-before-connect behavior, lazy connectors, and attributed
  downstream results.

- SQLite capability/server/version/view/binding persistence and FTS search;
- offline static catalog ingestion with project-root confinement;
- additive shared CLI/MCP query behavior with signed capability refs;
- offline describe and selected invocation through the one registered Skillex MCP;
- project and dependency pack `mcp-servers` suggestions, constrained to exact
  versions, `suggested` relationships, and non-executable metadata;
- a persistent owner-only local reference-signing key;
- trusted user/enterprise transport and exact credential-source configuration;
- env and bounded dotenv sources with exact-key resolution and symlink confinement;
- trusted stdio and stateless MCP `2026-07-28` Streamable HTTP connectors;
- JSON Schema 2020-12 argument validation and live input/output schema checks;
- optional privacy-safe local usage telemetry, disabled by default;
- CLI capability describe/call and auth readiness commands.
- explicit standard Registry API synchronization with opaque cursor handling,
  namespace allowlists, package/remote transport provenance, and artifact hashes;
- explicit trusted-server introspection that indexes tools, prompts, and resource
  templates into a freshness-bounded offline snapshot;
- capability-only continuation offsets plus server, kind, and readiness facets;
- optional `server/discover`, paginated lists, public/private TTL caches, and
  MCP `2026-07-28` multi-round-trip input retry fields;
- stable machine-readable broker error codes;
- env, dotenv, OS keychain, absolute credential-helper, and mTLS sources;
- protected-resource/authorization-server discovery, PKCE, CIMD-first client
  registration, explicitly enabled DCR fallback, encrypted token storage and refresh;
- client credentials, `private_key_jwt`, workload token exchange, and stable
  Enterprise-Managed Authorization/ID-JAG exchanges;
- hosted inbound RS256 bearer/IAP-style verification, exact mTLS principal
  mapping, opaque tenant partitions, tenant-specific signing-key derivation,
  and private-view isolation.

The process-level acceptance harness adds:

- a strict MCP `2026-07-28` stdio fake server;
- a production connector adapter that performs `server/discover`, paginated
  `tools/list`, live schema verification, and `tools/call`;
- a deterministic golden catalog and normalized query result;
- two independently launchable downstream server fixtures;
- proof that query and describe start no downstream process;
- proof that invocation starts only the selected server;
- proof that arguments and structured results cross the real MCP wire;
- proof that live schema drift prevents `tools/call`;
- proof that the parent environment and host MCP configuration are unchanged.

The project configuration boundary includes:

- configuration version 5 with a strict, explicit `MCP.Enabled` gate;
- exact server-version, scope, and optional trusted auth-profile bindings;
- strict unknown-field rejection for version 5;
- a project-facing broker constructor that refuses skills-only configuration;
- golden-corpus coverage proving every existing version 4 fixture remains
  skills-only and cannot initialize broker dependencies;
- `skillex doctor --json` reporting of MCP enablement and binding count.

Existing version 4 configuration and skill-only output remain unchanged. MCP
fields and additive host tools only exist after explicit version 5 opt-in.

## Protocol compatibility finding

The repository currently depends on `github.com/mark3labs/mcp-go v0.17.0`.
Inspection of that dependency confirms:

- its latest protocol constant is `2024-11-05`;
- its client and server lifecycles require `initialize`;
- it does not expose MCP `2026-07-28` `server/discover`, per-request protocol
  metadata, `resultType`, list cache fields, MRTR, or header-routing contracts.

Therefore the existing SDK cannot be the capability broker's core contract.
Protocol-neutral domain and connector interfaces are now the accepted boundary.
The first connector speaks the required modern stdio wire contract directly,
which provides a conformance target for evaluating an SDK upgrade or
replacement before changing the production dependency.

## Decisions recorded

### Accepted for the spike

1. The host registers only Skillex. Downstream servers are opened lazily behind
   broker connectors and never written to host configuration.
2. Canonical routing identity is server canonical name, exact version,
   capability kind, and capability name. Publisher and package digest remain
   additional trust inputs.
3. Schema compatibility uses a SHA-256 digest over the capability kind, name,
   and canonicalized input/output JSON schemas.
4. Capability references use the versioned `mcp-tool:v1` envelope with an
   HMAC-SHA256 integrity tag, context digest, view, schema digest, issue/expiry
   times, and random nonce.
5. Reference validation does not authorize a call. Context, view, current
   capability schema, readiness, and policy are re-evaluated before a connector
   opens.

### Deliberate deployment boundaries

- Skillex targets the final MCP `2026-07-28` downstream protocol. Omission of
  `resultType` is accepted as a compatibility behavior and `server/discover` is
  optional, but pre-stateless MCP transports that require `initialize` are not
  silently downgraded. They must be fronted by a compatible adapter or selected
  through an explicitly added legacy connector.
- The repository supplies hosted authentication, tenant partition, signing,
  cache, and audit contracts. A particular hosted product still chooses its HTTP
  framework, KMS/HSM, identity-provider key refresh, regional egress, durable
  event sink, dashboards, and operational SLOs.
- Registry synchronization is explicit and atomically replaces an offline
  snapshot. It intentionally does not make network calls during user queries.
- Credential helpers and keychains are exact trusted sources. Skillex does not
  enumerate ambient secrets or let project/pack configuration name executable,
  header, certificate, or credential locations.

## Verification

Passing gates:

```text
go test ./internal/capability ./internal/broker
go test ./internal/config
go test ./internal/connector/stdio ./internal/connector/streamhttp
go test ./internal/auth ./internal/trust ./internal/registryapi
go test ./internal/hostedauth ./internal/tenant
go test ./test/acceptance -run TestMCPBroker_GoldenDiscoveryDescribeAndRealStdioCall
make verify-unit
make test-acceptance
make test-race
make lint
make dev-binary
```

The automated gates include golden query compatibility, real subprocess MCP
calls, explicit introspection, exact credential injection, telemetry redaction,
OAuth/EMA conformance, MRTR retry behavior, schema drift, cache partitioning,
and cross-tenant view rejection.
