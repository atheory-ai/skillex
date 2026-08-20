# MCP Capability Broker Implementation Status

Last updated: 2026-08-20

## Current milestone

Milestone 0: protocol and threat-model spike.

The first schema-free foundation is implemented in:

- `internal/capability`: canonical capability identities, stable schema
  digests, and integrity-protected short-lived references;
- `internal/broker`: offline discovery, selected describe, context and schema
  revalidation, policy-before-connect behavior, lazy connectors, and attributed
  downstream results.

Unit tests use two in-process fake downstream servers to prove that discovery
opens no connection and a selected reference opens only the selected server.

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

The project configuration boundary now adds:

- configuration version 5 with a strict, explicit `MCP.Enabled` gate;
- exact server-version, scope, and optional trusted auth-profile bindings;
- strict unknown-field rejection for version 5;
- a project-facing broker constructor that refuses skills-only configuration;
- golden-corpus coverage proving every existing version 4 fixture remains
  skills-only and cannot initialize broker dependencies;
- `skillex doctor --json` reporting of MCP enablement and binding count.

No registry or pack schema has changed yet. The CLI only reports the
configuration state; query/read output and the host-facing MCP response schema
have not changed.

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

### Still pending before persistence work

1. Local signing-key storage and rotation; hosted signing-service interface.
2. Additive query response defaults and cursor compatibility.
3. Trusted user/enterprise auth configuration and pack schema versions.
4. Complete publisher/package/endpoint trust binding rules.
5. Production MCP SDK selection and older-protocol fallback policy.

Registry migrations and pack schema changes remain blocked until these decisions
are resolved and represented in acceptance fixtures.

## Verification

Passing gates:

```text
go test ./internal/capability ./internal/broker
go test ./internal/config
go test ./internal/connector/stdio
go test ./test/acceptance -run TestMCPBroker_GoldenDiscoveryDescribeAndRealStdioCall
make verify-unit
make test-acceptance
make test-race
make lint
make dev-binary
```

The next implementation slice should add static catalog ingestion and the
capability persistence migration behind the same tested broker contract. Public
query and host-facing MCP response changes remain gated on the additive query
compatibility decision above. Streamable HTTP, trusted auth resolution, and
transport fallback/error fixtures follow without weakening the configuration
gate.
