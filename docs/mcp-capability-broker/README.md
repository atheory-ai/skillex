# Skillex MCP Capability Broker

Status: proposed

Last updated: 2026-08-19

This design package describes a proposed extension of Skillex from contextual
skill retrieval into contextual MCP capability discovery and invocation.

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
- [Technical design](./technical-design.md) defines proposed schemas, Go
  interfaces, query and invocation contracts, authentication providers,
  storage, testing, migrations, and implementation phases.

These documents are a design proposal, not a statement that the described
functionality already exists.
