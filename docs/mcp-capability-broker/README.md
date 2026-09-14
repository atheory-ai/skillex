# Skillex MCP Capability Broker

Status: experimental implementation; open-source milestones 0–4 complete

Last updated: 2026-08-25

This package describes Skillex's experimental extension from contextual skill
retrieval into contextual MCP capability discovery and invocation.

The downstream capability broker is opt-in and experimental in Skillex 0.9.0.
Core discovery, routing, isolation, and protocol flows have automated coverage.
Authentication implementations have protocol conformance coverage, but have not
been certified against every MCP server, identity provider, or enterprise
deployment. Existing skill retrieval and Skillex's skill-facing MCP server remain
stable.

The central idea is simple: an agent harness registers only Skillex. Each
`skillex_query` returns the knowledge and external capabilities relevant to the
current code and task. When the agent selects a capability, Skillex invokes the
downstream MCP server itself. Downstream servers are never registered with the
harness.

Documents:

- [Product requirements](./prd.md) defines the vision, users, requirements,
  safety boundaries, success criteria, and rollout.
- [High-level architecture](./high-level-architecture.md) defines the system
  boundaries, components, flows, deployment modes, and trust model.
- [Technical design](./technical-design.md) defines schemas, Go
  interfaces, query and invocation contracts, authentication providers,
  storage, testing, migrations, and implementation phases.
- [Implementation status](./implementation-status.md) records implemented
  milestones, verification, compatibility findings, and deployment boundaries.

Open-source milestones 0–4 are implemented: offline capability indexing,
contextual discovery, lazy trusted stdio and Streamable HTTP invocation,
credential-source isolation, OAuth and enterprise authorization foundations,
telemetry, tenant isolation, and additive host-facing capability tools. Managed
background synchronization, regional connector fleets, identity-provider key
operations, dashboards, and service SLOs remain deployment responsibilities.
The implementation-status document is the source of truth for that boundary.
