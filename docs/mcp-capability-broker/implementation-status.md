# MCP Capability Broker Implementation Status

Last updated: 2026-08-19

## Current milestone

Milestone 0: protocol and threat-model spike.

The first schema-free foundation is implemented in:

- `internal/capability`: canonical capability identities, stable schema
  digests, and integrity-protected short-lived references;
- `internal/broker`: offline discovery, selected describe, context and schema
  revalidation, policy-before-connect behavior, lazy connectors, and attributed
  downstream results.

Unit tests use two fake downstream servers to prove that discovery opens no
connection and a selected reference opens only the selected server.

No registry, pack, root configuration, CLI, or host-facing MCP response schema
has changed yet.

## Protocol compatibility finding

The repository currently depends on `github.com/mark3labs/mcp-go v0.17.0`.
Inspection of that dependency confirms:

- its latest protocol constant is `2024-11-05`;
- its client and server lifecycles require `initialize`;
- it does not expose MCP `2026-07-28` `server/discover`, per-request protocol
  metadata, `resultType`, list cache fields, MRTR, or header-routing contracts.

Therefore the existing SDK cannot be the capability broker's core contract.
Protocol-neutral domain and connector interfaces are now the accepted boundary.
The next compatibility spike must compare an upgrade or replacement against the
fake connector contract before changing the production dependency.

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
3. Root configuration and pack schema versions.
4. Complete publisher/package/endpoint trust binding rules.
5. Production MCP SDK selection and older-protocol fallback policy.

Registry migrations and pack schema changes remain blocked until these decisions
are resolved and represented in acceptance fixtures.

## Verification

Passing gates:

```text
go test ./internal/capability ./internal/broker
make verify-unit
make dev-binary
```

The next implementation slice should create the real MCP connector adapter and
run the same broker contract tests against two process or HTTP fake servers,
without exposing the new behavior through CLI or MCP interfaces yet.
