# Product Requirements: Contextual MCP Capability Broker

Status: experimental implementation; open-source scope delivered

Owners: Skillex maintainers

Last updated: 2026-08-25

## Executive summary

Skillex currently externalizes skill discovery from agent harnesses. It scans
repositories and dependencies, resolves scope and version, builds a deterministic
local index, and gives an agent only the knowledge relevant to its query.

This proposal extends the same model to MCP capabilities:

- Skills answer: **what does the agent need to know?**
- MCP capabilities answer: **what can the agent do?**

An agent harness registers one MCP server: Skillex. It does not register the
downstream MCP servers known to Skillex. On every `skillex_query`, Skillex
evaluates repository path, task intent, dependencies, versions, trust, policy,
authentication readiness, and capability metadata. The response contains
bounded skill summaries and bounded callable capability summaries. When the
agent selects a capability, it invokes it through a stable Skillex tool. Skillex
then acts as an MCP client to the downstream server.

The result is a harness- and vendor-agnostic capability runtime whose open
source engine can run locally or be deployed as a hosted enterprise service.

## Product thesis

Agent harnesses should not each need to solve:

- project- and path-specific integration discovery;
- package- and version-specific integration recommendations;
- MCP registry aggregation;
- tool-level capability search;
- downstream transport and authentication differences;
- enterprise allow/deny policy;
- credential-source mapping;
- cross-surface usage telemetry.

Code and packages should be able to ship contextual knowledge and suggested
capabilities. Users and organizations should control trust, credentials, and
policy. Agents should receive only the knowledge and actions that apply to the
current query.

The stable harness-facing contract is progressive:

```text
skillex_query(context, intent)
    -> relevant skill summaries
    -> relevant MCP capability summaries

skillex_read(skill_ref)
    -> bounded selected knowledge

skillex_mcp_describe(capability_ref)
    -> bounded selected tool definition, when the query summary is insufficient

skillex_mcp_call(capability_ref, arguments)
    -> policy-checked downstream invocation
```

## Problem statement

### Static host registration is the wrong abstraction

Most harnesses configure MCP servers globally or per workspace. Once registered,
the harness owns tool discovery and relevance selection. That produces several
problems:

- global servers appear in unrelated repositories;
- workspace configuration is too coarse for monorepos and dependency
  boundaries;
- every harness needs separate installation and configuration;
- large tool catalogs compete for model context or host-side search;
- a repository cannot declare which integrations are useful without also
  appearing to grant those integrations trust;
- authentication and policy state are fragmented by harness;
- enterprises lack a consistent inventory and usage view across surfaces.

Merely writing discovered servers into VS Code, Cursor, or another host does not
solve this problem. It only moves configuration generation. This proposal does
not write or dynamically register downstream MCP servers with a harness.

### Server descriptions are not capability discovery

MCP registry metadata answers which servers exist and how to install or connect
to them. A server description is usually too broad to answer whether a concrete
action is useful. Search should operate primarily over tools, prompts, and
resource templates, with server descriptions as supporting evidence.

For example, a task phrased as "find the incident related to this failed
migration" should be able to match a Linear `issues.search` tool even if the
server description only says "Work with Linear."

### Authentication is part of availability

A relevant capability may be:

- public and ready;
- trusted but missing a credential;
- configured but awaiting interactive login;
- authorized for one user and forbidden for another;
- available to a service identity but not a personal identity;
- blocked by organization policy;
- unsupported because Skillex lacks the required authentication method.

Search must present relevance and readiness separately so the agent can make a
useful decision without exposing credentials.

## Goals

1. Extend `skillex_query` to return relevant knowledge and actions in one bounded
   response.
2. Keep downstream MCP servers invisible to and unregistered with the harness.
3. Resolve MCP capabilities using the same path, dependency, version, detector,
   and scope concepts used for skills.
4. Allow projects and packages to ship MCP suggestions without granting trust or
   credentials.
5. Search at tool, prompt, and resource-template granularity rather than only at
   server granularity.
6. Invoke selected downstream capabilities through stable, tamper-resistant
   references.
7. Support local, hosted, and self-hosted deployment using the same open
   contracts.
8. Support the authentication mechanisms required by current MCP servers,
   including standard OAuth and Enterprise-Managed Authorization.
9. Bind every credential to a canonical service identity, logical credential
   slot, permitted source, and exact injection destination.
10. Preserve deterministic, offline query behavior: ordinary queries must not
    contact registries or downstream servers.
11. Provide useful, privacy-preserving telemetry from resolution through
    invocation.
12. Preserve CLI and MCP parity over shared core behavior.

## Non-goals

The initial product will not:

- automatically install or execute every server in the public MCP Registry;
- allow a package or repository to grant itself access to credentials;
- expose arbitrary environment variables or environment files to a downstream
  process;
- place secrets, resource contents, tool results, or prior arguments in the
  searchable index;
- make the host register, restart, or refresh downstream MCP servers;
- forward a host access token to a downstream resource with a different
  audience;
- replace a downstream server's authorization decisions;
- guarantee that a self-reported tool description or annotation is truthful;
- become a general-purpose agent planner in the first release;
- remove the harness's responsibility to decide whether to call Skillex or to
  present required user approvals.

## Terminology

### Server

A canonical MCP server identity and version, obtained from an MCP registry,
trusted configuration, or a signed package declaration.

### Capability

A concrete tool, prompt, or resource template offered by an MCP server. Tools are
the first invokable capability type in scope.

### Binding

A rule connecting a server or capability to code context through scope,
activation conditions, dependency boundaries, and relationship strength.

### Suggestion

An untrusted or not-yet-approved declaration that a server may be useful in a
context. A suggestion never grants execution or credential access.

### Credential slot

A logical secret required by a service, such as `github.access-token`. It is not
the environment variable, file key, or header name used to source or inject the
secret.

### Credential profile

Trusted user or enterprise configuration mapping a canonical service's
credential slots to narrowly permitted sources and injection destinations.

### Capability view

The set of capabilities visible under a specific public or authenticated
authorization context. Private views are isolated by tenant, principal, and
credential profile.

### Broker

Skillex acting as an MCP server to the harness and an MCP client to downstream
servers. The broker resolves, authorizes, invokes, and returns results; it does
not register downstream servers with the harness.

## Users and primary journeys

### Individual developer in a repository

1. The developer registers the local Skillex MCP once.
2. A package in the repository suggests a database MCP for migration work.
3. The user has approved that server and mapped its token in global Skillex
   configuration.
4. The agent queries Skillex while editing a migration.
5. Skillex returns migration guidance and a read-only schema inspection tool.
6. The agent calls the capability through Skillex.
7. Skillex loads only the configured credential, invokes the downstream server,
   and returns the result.

### Package author

1. The author ships a pack containing skills and suggested MCP bindings.
2. Activation rules tie those suggestions to dependencies, files, and scopes.
3. Consumers see suggestions only when relevant.
4. No server is trusted, installed, started, or given credentials solely because
   the dependency declared it.

### Enterprise developer

1. The organization operates hosted or self-hosted Skillex.
2. Public, private, and internal MCP registries synchronize into its catalog.
3. Organization policy approves server identities and allowed capabilities.
4. The employee authenticates to Skillex through corporate SSO.
5. Skillex obtains resource-specific downstream credentials through
   Enterprise-Managed Authorization or another approved method.
6. Resolution and invocation telemetry is available to authorized operators.

### CI or autonomous workload

1. The workload authenticates to Skillex with workload identity or client
   credentials.
2. Policy exposes only capabilities approved for that service principal.
3. Downstream credentials are obtained with workload identity, token exchange,
   or narrowly scoped service credentials.
4. No interactive personal credential is used.

## Product principles

### One registered MCP

The harness registers Skillex. Downstream servers are never written into host
configuration and never dynamically registered with the host.

### Query before content or execution

Discovery returns bounded summaries. Full skill content, full capability
definitions, and execution require a selected reference.

### Relevance is not authorization

A high-relevance result may be unavailable. A ready capability may be irrelevant.
The response must report both dimensions.

### Suggestions do not confer trust

Package and repository declarations may influence discovery but cannot release
credentials or bypass user and enterprise policy.

### Query is offline; invocation is lazy

Registry synchronization and capability introspection occur during explicit
refresh or controlled background work. Downstream connection and authentication
occur only when needed. A normal query performs no network access.

### Credentials are capabilities, not ambient state

Downstream servers receive only explicitly mapped credential values through
explicit destinations. They never inherit the full Skillex environment.

### Identity is exchanged, not forwarded

Tokens are issuer-, audience-, resource-, and scope-bound. A credential used to
call Skillex must not be passed through to an unrelated downstream resource.

### Provenance is visible

Every server, capability, binding, policy decision, and readiness state carries
its source and trust level.

### Open contracts before hosted features

Pack schemas, configuration schemas, capability references, query responses,
auth provider interfaces, policy decisions, invocation semantics, and telemetry
events belong to the open source engine.

## Functional requirements

### FR1: Unified contextual query

`skillex_query` must be able to return both `skills` and `capabilities` for the
same path and intent. Results must be bounded, ranked, paginated, and accompanied
by narrowing information when broad.

The query must consider:

- repository path or glob;
- task search terms;
- topics and tags;
- package or module identity and version;
- active detectors;
- user and organization policy;
- server trust;
- capability view visibility;
- readiness and authentication state;
- configured relationship such as suggested or preferred.

### FR2: Capability-granular indexing

The index must represent:

- server metadata and versions;
- transports and installation metadata;
- tools, prompts, and resource templates;
- compact input and output schema summaries;
- risk annotations and policy classifications;
- capability provenance and schema digest;
- public or private authorization view;
- refresh time, TTL, and cache scope;
- local binding and readiness overlays.

The system must store raw definitions separately from searchable text.

### FR3: Safe capability ingestion

Skillex must ingest capabilities from:

- standard MCP registries;
- trusted user and enterprise configuration;
- project-local and dependency-shipped pack suggestions;
- publisher-provided capability metadata;
- unauthenticated introspection of approved remote servers;
- authenticated introspection under a named profile;
- prior successful runtime discovery.

Skillex must not execute an arbitrary public registry package solely to learn its
tools.

### FR4: Contextual MCP declarations in packs

Packs may declare suggested servers, preferred capabilities, activation rules,
and scopes. Engine-side schema and resolution behavior belongs in this
repository. Canonical pack content, publication, naming, signing, and registry
policy belong in `atheory-ai/skillex-packs`.

Package-provided declarations default to `suggested` and cannot select an actual
credential source.

### FR5: Progressive capability selection

A query result must include enough information to choose a capability without
including arbitrarily large schemas. It must include a stable capability
reference and compact argument summary. A separate describe operation must
return the complete bounded tool definition when required.

### FR6: Downstream invocation

`skillex_mcp_call` must:

1. validate and decode the selected reference;
2. re-evaluate current context and policy;
3. verify the server identity and capability schema digest;
4. validate arguments against the indexed input schema;
5. resolve the required principal and credential profile;
6. obtain or refresh a resource-specific credential;
7. start or connect to the downstream server lazily;
8. perform the MCP call;
9. propagate elicitation, approval, cancellation, and errors safely;
10. validate structured output when an output schema is available;
11. emit telemetry without recording secrets or payloads by default;
12. return a result identifying the actual server and tool used.

### FR7: Authentication providers

The architecture must support:

- no authentication;
- static environment and dotenv secrets;
- OS keychain or credential store;
- credential helper commands;
- standard MCP OAuth authorization code with PKCE;
- Client ID Metadata Documents;
- legacy Dynamic Client Registration for compatibility;
- Enterprise-Managed Authorization and ID-JAG;
- client credentials;
- workload identity and token exchange;
- service-specific OAuth/OIDC providers;
- mTLS and `private_key_jwt` where required.

Support may ship in phases, but unsupported required methods must produce an
explicit readiness status rather than a generic failure.

### FR8: Bounded credential sources

Trusted configuration must map:

```text
canonical service identity
  -> logical credential slot
    -> ordered permitted sources
      -> exact source key
        -> exact injection destination
```

A downstream connector must not enumerate environment variables or environment
files. A repository-controlled declaration must not map arbitrary host secrets to
arbitrary servers.

### FR9: Inbound hosted authentication

A hosted Skillex deployment must support configurable inbound authentication for
human and workload principals. The token used to access Skillex must be distinct
from downstream resource tokens.

### FR10: Readiness reporting

Search must distinguish at least:

- discovered;
- suggested;
- trusted;
- configured;
- credential missing;
- login required;
- scope required;
- ready;
- unsupported authentication;
- policy denied;
- unreachable;
- stale or schema changed.

Readiness checks must never expose the credential value.

### FR11: Policy and approval

Policy must be able to allow, deny, or require approval by:

- canonical server and publisher;
- version or digest;
- endpoint or package source;
- capability kind and name;
- read/write/destructive classification;
- repository scope;
- user, group, tenant, or workload identity;
- credential profile;
- data classification and deployment environment.

Deny takes precedence. Every invocation must re-evaluate policy; a query result is
not an authorization grant.

### FR12: Telemetry

Skillex must emit OpenTelemetry-compatible events spanning:

```text
available -> eligible -> returned -> described -> invoked -> completed
```

Default telemetry may include server and tool identity, pseudonymous principal
and workspace identifiers, policy decision, authorization method, outcome,
latency, and error class. Prompt text, arguments, results, source code, resource
contents, and credentials are excluded by default.

### FR13: CLI and MCP parity

Core query, describe, invoke, auth status, and policy behavior must be shared.
The CLI must remain a usable fallback for harnesses that can run commands but do
not implement MCP.

## Query response requirements

A proposed response shape is:

```json
{
  "type": "results",
  "skills": [
    {
      "ref": "skill:...",
      "name": "Database migrations",
      "description": "Repository migration conventions",
      "score": 0.91
    }
  ],
  "capabilities": [
    {
      "ref": "mcp-tool:v1:...",
      "server": "com.example/postgres-mcp",
      "name": "schema.inspect",
      "description": "Inspect the development database schema",
      "arguments": [
        {"name": "database", "type": "string", "required": true}
      ],
      "risk": "read-only",
      "availability": {
        "status": "ready",
        "auth_method": "static-bearer",
        "profile": "postgres-development"
      },
      "score": 0.96,
      "matched_in": ["tool_description", "binding"]
    }
  ],
  "match_count": 2,
  "returned_count": 2
}
```

Compatibility requirements for the existing query response are defined in the
technical design. The initial rollout should add fields without removing the
existing `results` skill array.

## Configuration requirements

### Project opt-in

The implemented project-level boundary uses configuration version 5 and is
default-off. Version 4 remains skills-only, as does version 5 without the
explicit enabled flag:

```yaml
Version: 5
MCP:
  Enabled: true
  Bindings:
    - Server: io.github.example/postgres-mcp
      Version: 2.1.0
      AuthProfile: postgres-development
      Scope: "services/api/**"
```

The repository may select only the name of a separately trusted auth profile.
It cannot define credential sources. This configuration gate is implemented;
catalog persistence, profile resolution, and public host-facing capability
tools remain rollout work tracked in the implementation status.

### Pack declaration

The proposed pack syntax is illustrative and requires versioned schema review:

```yaml
name: prisma
version: 1.0.0

skills:
  - file: migrations.md
    activate-when:
      files-matching:
        - "prisma/migrations/**"
    scope: subtree

mcp-servers:
  - ref: io.github.example/postgres-mcp
    relationship: suggested
    activate-when:
      detector: prisma
    scope: boundary
    capabilities:
      prefer:
        - schema.inspect
        - query.explain
```

### Trusted credential configuration

Actual secret sources belong to user or enterprise configuration:

```yaml
credential-profiles:
  postgres-development:
    service: io.github.example/postgres-mcp
    credentials:
      database-url:
        sources:
          - dotenv:
              path: "${projectRoot}/.env.mcp"
              key: DATABASE_URL
        inject:
          stdio-env: DATABASE_URL
```

Project configuration may select an already trusted profile but must not invent a
new source mapping unless the user explicitly promotes and approves that
configuration into a trusted layer.

## Security and privacy requirements

1. Treat all registry, pack, server, tool, prompt, resource, annotation, and
   instruction metadata as untrusted until provenance and policy establish
   otherwise.
2. Bind server trust to canonical identity, publisher, version or digest,
   transport, and endpoint/package source. Identity drift requires reapproval.
3. Never store secret values in SQLite, query results, capability references,
   logs, traces, or error messages.
4. Pass a minimal constructed environment to stdio servers, never the complete
   Skillex environment.
5. Resolve dotenv paths within approved roots, reject traversal and unsafe
   symlink escapes, and read only the configured key.
6. Partition private capability views and caches by tenant, principal, and
   authorization context.
7. Validate token issuer, audience, resource, expiry, scopes, and authorized
   party as required by the selected method.
8. Do not pass the token used to access Skillex to a downstream server unless a
   standards-compliant token exchange produces a new target-bound token.
9. Re-evaluate policy and readiness at invocation time.
10. Make write and destructive behavior visible before approval. A generic
    `skillex_mcp_call` must not obscure the downstream server and operation.
11. Sandbox untrusted local servers where supported and apply network egress
    policy.
12. Provide retention, export, deletion, and tenant controls for hosted
    telemetry.

## Performance and reliability targets

Initial targets, to be validated with benchmarks:

- local query p95 under 50 ms with 10,000 skills and 50,000 capability records;
- no query-time network dependency;
- incremental registry synchronization without a full rebuild;
- broker overhead p95 under 25 ms excluding authentication and downstream
  latency;
- deterministic ordering for unchanged query inputs and index state;
- bounded query results of at most 20 items per page by default contract;
- stale capability metadata may be shown with an explicit status but must be
  revalidated before a write-capable invocation;
- a downstream outage must not prevent skill queries or unrelated capability
  invocations.

## Success metrics

### Product usefulness

- percentage of capability queries producing at least one relevant candidate;
- selected capabilities as a percentage of returned candidates;
- successful invocations as a percentage of selected capabilities;
- reduction in per-harness MCP configuration entries;
- reduction in irrelevant tools exposed to the model;
- time from repository checkout to first useful external capability.

### Search quality

- precision at the first three capability results;
- no-result and reformulation rate;
- frequency of server-level matches with no useful tool-level match;
- stale or invalid capability-reference rate.

### Reliability and security

- invocation success and latency by server/tool/version;
- authentication completion and failure rates by method;
- policy-denied and approval-denied rates;
- credential leakage incidents: zero;
- cross-tenant data or cache exposure incidents: zero;
- arbitrary environment or filesystem secret access incidents: zero.

## Rollout plan

### Phase 0: Contracts and fixtures

- Finalize pack and trusted configuration schemas.
- Define canonical server identity and capability references.
- Add fake MCP servers and auth providers for tests.
- Add query response compatibility fixtures.
- Record dependency gaps against MCP 2026-07-28.

### Phase 1: Catalog and declared capability discovery

- Synchronize standard MCP registry metadata.
- Accept signed or trusted publisher capability summaries.
- Index server and capability summaries in SQLite/FTS.
- Return capabilities from `skillex_query` with non-invokable or setup-required
  status.
- Add pack suggestions and trust overlays.

### Phase 2: Trusted local invocation

- Introspect explicitly trusted stdio and HTTP servers.
- Add `skillex_mcp_describe` and `skillex_mcp_call`.
- Implement no-auth, environment, dotenv, keychain, and credential-helper
  providers.
- Enforce reference integrity, schema validation, policy, and approval metadata.
- Emit local OpenTelemetry spans.

### Phase 3: Standard OAuth

- Implement protected-resource and authorization-server metadata discovery.
- Support authorization code with PKCE and Client ID Metadata Documents.
- Retain Dynamic Client Registration only for compatibility.
- Add secure token storage, refresh, incremental scopes, and auth status.

### Phase 4: Enterprise identity

- Implement inbound hosted identity and multi-tenant principals.
- Implement Enterprise-Managed Authorization and ID-JAG.
- Implement client credentials and workload identity as their specifications and
  deployment requirements stabilize.
- Add enterprise policy, audit, and credential-provider integrations.

### Phase 5: Hosted service and ecosystem

- Operate synchronized public and private catalogs.
- Add managed indexing and private capability views.
- Add organization dashboards and usage analytics.
- Publish open provider and registry integration contracts.
- Coordinate canonical pack content and publication with
  `atheory-ai/skillex-packs`.

## Risks and mitigations

### Generic broker tool obscures the real action

Mitigation: every query, describe, approval, result, audit event, and error names
the downstream server and tool. Write-capable operations require explicit policy
or approval.

### Capability metadata is malicious or inaccurate

Mitigation: retain provenance, distinguish declared from observed metadata,
validate schemas, prefer trusted observations, and never treat annotations as
authorization facts.

### Capability catalogs become too large

Mitigation: FTS and structured filtering, bounded results, candidate-scoped
narrowing, deterministic pagination, local readiness boosts, and progressive
describe.

### The broker becomes a high-value credential target

Mitigation: secret handles instead of raw values, encrypted storage, short-lived
tokens, process isolation, least privilege, redaction, audit, tenant isolation,
and support for external secret managers.

### Server schemas change after indexing

Mitigation: include schema digests in references, respect MCP TTL/cache scope,
revalidate before invocation, invalidate stale references, and require a new
query when compatibility changes.

### Authentication support fragments

Mitigation: provider interfaces, explicit unsupported states, conformance
fixtures, phased delivery, and separation of inbound and outbound roles.

### Host behavior varies

Mitigation: expose a small stable MCP surface and keep all downstream discovery
and invocation behind Skillex. Maintain CLI parity as a fallback.

## Open questions

1. Should a single unified result ranking interleave skills and capabilities, or
   should each category be ranked independently within one response?
2. How much of an input schema belongs in the query summary before a separate
   describe call is required?
3. Which publisher-provided capability metadata format should Skillex accept
   before authenticated introspection?
4. Should repository-owned credential-profile selection require one-time user
   approval, or be prohibited outside global configuration?
5. Which risk annotations are derived by Skillex versus trusted from the server?
6. How should local stdio sandbox and network policy vary by platform?
7. What retention and aggregation defaults are appropriate for local telemetry?
8. Which MCP 2026-07-28 features require replacing or upgrading the current
   `mark3labs/mcp-go` dependency?
9. Should an invocation reference be single-use for write-capable tools?
10. How should a hosted service expose private registry and provider extension
    points without coupling them to hosted-only implementation details?

## Decision gate

This proposal should proceed beyond Phase 0 only if a prototype proves all of
the following:

- tool-level search materially outperforms server-description search;
- a fixed Skillex query/describe/call interface works in multiple harnesses;
- capability references can remain bounded and safe across schema refreshes;
- credential-source confinement can be enforced for stdio and HTTP connectors;
- downstream invocation overhead is acceptable;
- the approval experience clearly identifies the real downstream action.
