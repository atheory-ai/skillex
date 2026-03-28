# Skillex Specification (v0.6)

## 1. Problem Statement

Modern agent workflows need "skills" (repo rules, package usage guidelines, migration notes, dev workflows) to develop safely and efficiently.

In monorepos and dependency-heavy projects, skill discovery becomes hard because:

- Skills differ by package version.
- A monorepo may have different versions of the same dependency installed in different workspaces.
- We must avoid polluting the LLM context window by loading irrelevant docs.
- We want deterministic, reproducible skill resolution, not install-time mutation or runtime crawling.
- Even well-organized skill files require the LLM to browse multiple documents, which is slow and wastes context.

Current approaches leave agents to hunt through layered documentation. We need a system that covers the full lifecycle: authoring skills, testing them, registering them, indexing them, and retrieving exactly what's needed on demand — in a single query, in microseconds.

## 2. Goals

- **Deterministic:** same repo state produces the same index.
- **Scalable:** thousands of packages without context bloat.
- **Version-correct:** skill exports are read from the resolved package install for a given boundary.
- **Non-invasive:** dependencies never modify the consumer repo.
- **Instant retrieval:** agents query a structured index by path, topic, and tags — no document browsing.
- **Testable:** every skill can have a co-located test file that agents use for self-evaluation.
- **Self-improving:** skills for writing skills, skills for testing skills, and analysis tooling create a closed feedback loop.
- **Multi-platform:** CLI shipped for macOS, Linux, and Windows as a small Go binary.
- **Node-first** (pnpm/yarn/npm), but architecture allows additional resolvers later.

### Non-goals (v0.6)

- Full multi-ecosystem dependency graph support (Python, Java, etc.).
- Agent runtime dependency resolution.
- Embedding an LLM in the CLI — agents are the test runtime, the CLI validates structure and serves queries.
- Natural language search or vector embeddings — retrieval is structured, not semantic.

## 3. Architecture Overview

Skillex has a core engine with three interface layers:

```
              ┌─────────────────┐
              │   AGENTS.md     │  Fallback: agents read
              │   (manifest)    │  available skills at
              └────────┬────────┘  session start
                       │
              ┌────────┴────────┐
              │   MCP Server    │  Native: skills as
              │  (resources +   │  resources, query as
              │    tools)       │  typed tool calls
              └────────┬────────┘
                       │
              ┌────────┴────────┐
              │   CLI           │  Foundation: cobra,
              │  (stdout/stderr)│  bubbletea, lipgloss
              └────────┬────────┘
                       │
┌──────────────────────┴──────────────────────────────┐
│                   skillex core                        │
│                                                      │
│  ┌───────────┐  ┌───────────┐  ┌──────────────────┐ │
│  │  Scanner   │  │  Linker   │  │     Registry     │ │
│  │           │  │           │  │    (SQLite)      │ │
│  │ Finds     │  │ Resolves  │  │                  │ │
│  │ skillex   │  │ public/   │  │ Stores skills,   │ │
│  │ exports & │─▶│ private   │─▶│ topics, tags,    │ │
│  │ boundaries│  │ per scope │  │ scopes, sources  │ │
│  │           │  │           │  │                  │ │
│  └───────────┘  └───────────┘  └────────┬─────────┘ │
│                                          │           │
│  ┌───────────┐                 ┌────────▼─────────┐ │
│  │ Validator  │                 │     Query       │ │
│  │           │                 │    Engine        │ │
│  │ Checks    │                 │                  │ │
│  │ test file │                 │ path, topic,     │ │
│  │ structure │                 │ tags, package    │ │
│  │           │                 │                  │ │
│  └───────────┘                 └──────────────────┘ │
└─────────────────────────────────────────────────────┘
```

**Core engine (Go library):**

1. **Scanner** — discovers skillex exports in dependencies and workspace packages.
2. **Linker** — resolves which skills apply to which scopes, enforcing public/private visibility.
3. **Registry** — stores everything in a SQLite database with structured metadata.
4. **Query engine** — retrieves relevant skill content by path, topic, tags, and package.
5. **Validator** — checks that skill and test files are well-formed.

**Interface layers (all backed by the same core):**

1. **CLI** — the foundation. Built with Cobra for command structure, Bubbletea for interactive TUI (e.g., `skillex doctor`), and Lipgloss for styled terminal output. Structured data goes to stdout (for piping), human-readable styled output goes to stderr. Every agent harness can call this.
2. **MCP Server** — the native integration layer for agents that support Model Context Protocol. Skills are exposed as MCP resources that agents discover through the protocol. Query is exposed as a typed tool with parameters for path, topic, tags, and package. No `AGENTS.md` parsing or shell command construction needed.
3. **AGENTS.md manifest** — the fallback for agents that just read files. Auto-generated on every refresh. Lists available scopes, topics, tags, packages, and query examples. The agent reads it once at session start and knows how to call the CLI.

## 4. Core Concepts

### 4.1 Skills

A "skill" is a Markdown file with YAML frontmatter containing rules, patterns, examples, or workflow guidance.

```markdown
---
topics: [error-handling, validation]
tags: [v2, breaking-change]
---

# Error Handling in @acme/foo

When using the FooClient, all API calls return...
```

The frontmatter provides structured metadata for indexing. The body is the skill content served to agents.

### 4.2 Public and Private Skills

Every skill belongs to one of two categories:

- **Public** — outward-facing skills that tell consumers how to use a package. Linked when the package appears as a dependency of the current scope.
- **Private** — inward-facing skills that tell contributors how to work on a package. Linked when the agent's working path is inside the package's source tree.

This distinction is declared by directory convention (`skillex/public/`, `skillex/private/`) and enforced by the linker. The relationship between the working path and the package determines which set applies.

### 4.3 Skill Tests

Every skill file can have a co-located test file following the convention `<name>.test.md` alongside `<name>.md`. Test files contain structured validation scenarios that agents use to self-evaluate whether a skill produces correct guidance.

The agent is the test runtime. The CLI only validates structure.

### 4.4 Dependency Boundary

A dependency boundary defines which dependency universe applies.

In Node, the boundary's `package.json` defines the direct dependency set. The scanner reads the boundary to enumerate dependencies and discover their skillex exports.

### 4.5 The Registry

The registry is a SQLite database at `.skillex/index.db`. It replaces the YAML index file from earlier versions. All skill metadata, content, scope assignments, and source information are stored here and queryable via the CLI.

The database is the single source of truth after a refresh. Agents never read skill files directly — they query the registry through the CLI.

## 5. File Formats

### 5.1 Root Configuration: `skillex.yaml`

Location: repository root.

```yaml
Version: 4

Rules:
  - Scope: "**"
    Skills:
      - skills/repo.md

  - Scope: "packages/*/**"
    Skills:
      - skills/package-dev.md

  - Scope: "packages/app-a/**"
    DependencyBoundary: packages/app-a
```

Notes:

- `Skills` reference repo-local skill files attached to a scope.
- `DependencyBoundary` tells the scanner to resolve dependencies at that path and feed their exports to the linker.
- Rules are processed in order and are **additive** — a path matching multiple rules accumulates all applicable skills.

### 5.2 Package Export Declaration

A package declares itself as a skillex project through its `package.json`:

```json
{
  "skillex": true
}
```

Or with an explicit path if the skillex directory is non-standard:

```json
{
  "skillex": {
    "path": "docs/skillex"
  }
}
```

When `"skillex": true`, the scanner looks for `skillex/public/` and `skillex/private/` in the package root.

This leverages the existing `package.json` export convention — no separate YAML config file is needed inside the package.

### 5.3 Package Directory Layout

```
skillex/
  public/
    consumer.md
    consumer.test.md
    migrations.md
    migrations.test.md
  private/
    architecture.md
    architecture.test.md
    dev-workflow.md
    dev-workflow.test.md
  vendor/
    github.com/someone/react-patterns/
      hooks.md
      hooks.test.md
    local/
      imported-guide.md
      imported-guide.test.md
```

Conventions:

- Skill files use `.md` extension with YAML frontmatter for topics and tags.
- Test files use `.test.md` extension and sit alongside their corresponding skill.
- The `public/` and `private/` directories map directly to the visibility model.
- The `vendor/` directory contains external skills imported via `skillex get` or `skillex import`. Vendor skills are organized by source and committed to the repo for auditability.

### 5.4 Skill Frontmatter

Every skill file includes YAML frontmatter for structured metadata:

```markdown
---
topics: [configuration, initialization]
tags: [getting-started]
---

# Configuring @acme/foo

To initialize the client...
```

Fields:

- `topics` — semantic categories describing what the skill covers. Used for `--topic` queries.
- `tags` — freeform labels for filtering. Used for `--tags` queries.
- `source` (vendor skills only) — the URL or path the skill was imported from, for provenance tracking.
- `reviewed` (vendor skills only) — timestamp of the last agent review.

Both `topics` and `tags` are optional. Skills without frontmatter are still indexed and queryable by path, scope, and package — they just won't appear in topic or tag queries.

### 5.5 Skill Test File Format

Test files are structured Markdown that agents read to self-evaluate a skill's effectiveness.

```markdown
# Tests: consumer.md

## Validation: API initialization
Prompt: "How do I initialize the @acme/foo client?"
Success criteria:
  - Response references the FooClient constructor
  - Response includes the required config object
  - Response does not expose internal implementation details

## Validation: Error handling
Prompt: "How should I handle errors from @acme/foo?"
Success criteria:
  - Response covers the FooError type
  - Response shows try/catch pattern with specific error codes

## Validation: Migration from v1
Prompt: "I'm upgrading @acme/foo from v1 to v2"
Skills: consumer.md, migrations.md
Success criteria:
  - Response mentions the breaking change in auth flow
  - Response provides the v2 config shape
  - Response does not suggest deprecated v1 patterns
```

Format rules:

- H1 references the skill being tested: `Tests: <filename>`.
- Each `## Validation:` section defines one scenario.
- `Prompt:` is the input the agent evaluates against the skill.
- `Skills:` optionally lists additional skill files for the scenario.
- `Success criteria:` is a list of semantic expectations the agent self-checks against.

## 6. The Registry (SQLite)

### 6.1 Purpose

The registry is the internal index that makes structured queries fast. It stores everything the scanner and linker produce in a queryable format.

### 6.2 Schema (Conceptual)

```
skills
  id            INTEGER PRIMARY KEY
  path          TEXT        -- file path relative to repo root
  content       TEXT        -- full skill content (body without frontmatter)
  package_name  TEXT NULL   -- source package, null for repo-level skills
  package_ver   TEXT NULL   -- resolved version
  visibility    TEXT        -- 'public', 'private', or 'repo'
  source_type   TEXT        -- 'repo' or 'dependency'

skill_topics
  skill_id      INTEGER
  topic         TEXT

skill_tags
  skill_id      INTEGER
  tag           TEXT

skill_scopes
  skill_id      INTEGER
  scope         TEXT        -- glob pattern this skill applies to

skill_tests
  id            INTEGER PRIMARY KEY
  skill_id      INTEGER
  name          TEXT        -- validation name
  prompt        TEXT
  extra_skills  TEXT NULL   -- comma-separated additional skill filenames
  criteria      TEXT        -- newline-separated success criteria
```

### 6.3 Refresh Behavior

`skillex refresh` rebuilds the registry from scratch:

1. Parse root `skillex.yaml`.
2. Scan for repo-level skill files.
3. For each dependency boundary, enumerate dependencies and discover exports via `package.json` `skillex` field.
4. For each discovered export, read `skillex/public/` and `skillex/private/` directories.
5. Parse frontmatter from every `.md` file for topics and tags.
6. Parse every `.test.md` file for validation scenarios.
7. Apply linking rules to determine scope assignments and visibility.
8. Write everything to `.skillex/index.db`.
9. Auto-generate the `AGENTS.md` skillex manifest section.

The database is always rebuilt entirely — no incremental updates in v0.4. This keeps the refresh deterministic and simple.

## 7. CLI: `skillex`

### Design

The CLI is built with:

- **Cobra** — command and subcommand structure, flag parsing, help generation.
- **Bubbletea** — interactive TUI for commands that benefit from it (e.g., `skillex doctor` walking through issues, `skillex get` review prompts).
- **Lipgloss** — styled terminal output for human-readable diagnostics and reports.

Output conventions:

- **stdout** — structured data (query results, JSON output). This is what agents and scripts consume via piping.
- **stderr** — human-readable progress, diagnostics, and styled output. This is what developers see in the terminal.
- **`--json` flag** — available on all commands, forces stdout to JSON for programmatic consumption.
- **`--quiet` flag** — suppresses stderr output for CI and scripting.

### Init

```
skillex init
```

Bootstraps a repository for skillex. This is the first command a user runs. It:

1. Creates `skillex.yaml` with sensible defaults based on the detected project structure (monorepo vs single package, workspace layout).
2. Creates the `skills/` directory with a starter repo-level skill.
3. Writes the initial skillex section into `AGENTS.md`, including instructions for the agent on how to use the MCP server and CLI fallback.
4. Creates a `.skillex/` directory for the registry.
5. Optionally sets up the MCP server configuration for detected agent harnesses (e.g., writes `.cursor/mcp.json` if Cursor is detected).
6. Runs an initial `skillex refresh` to build the registry.

```
skillex init --harness cursor
```

Explicitly configures MCP integration for a specific agent harness.

```
skillex init --package
```

Initializes a package (as opposed to a repo root) for skillex exports. Creates the `skillex/public/` and `skillex/private/` directories and adds the `"skillex": true` field to `package.json`.

The init command is interactive by default (Bubbletea) — it walks the user through choices about project structure, scope rules, and harness configuration. Use `--yes` to accept all defaults for scripted setup.

### Query

```
skillex query --path packages/app-a/src/auth.ts
```

Returns all skills in scope for that file path.

```
skillex query --topic error-handling
```

Returns all skills tagged with the given topic.

```
skillex query --tags migration,breaking-change
```

Returns skills matching all specified tags.

```
skillex query --package @acme/foo
```

Returns all skills exported by a package (respecting visibility for the current scope).

```
skillex query --path packages/app-a/** --topic auth,errors --tags v2
```

Flags compose as intersection — this returns skills that are in scope for `packages/app-a`, cover auth or errors, and are tagged v2.

```
skillex query --path packages/app-a/src/auth.ts --format content
```

Returns the full skill content, concatenated and ready to pipe into an agent's context.

```
skillex query --path packages/app-a/src/auth.ts --format summary
```

Returns metadata only — skill names, topics, tags, sources. Useful for agents deciding what to load.

#### Response types

The `--json` output always includes a `type` field that distinguishes three response kinds:

| `type`       | When                                 | Contents                                              |
| ------------ | ------------------------------------ | ----------------------------------------------------- |
| `results`    | Filters matched one or more skills   | `results` array of skill objects                      |
| `vocabulary` | No filters provided                  | `vocabulary` with topics, tags, packages, skill count |
| `no_match`   | Filters provided but nothing matched | `query` echo + `vocabulary` hint                      |

**No code path returns all skill content as a fallback.** A bare `skillex query` returns vocabulary metadata to help agents discover valid filter values, not a dump of all skills.

When a query matches nothing, the `no_match` response includes the full vocabulary so agents can self-correct by picking a valid topic, tag, or package name from the hint and retrying.

Example vocabulary response:

```json
{
  "type": "vocabulary",
  "vocabulary": {
    "topics": [
      { "name": "api", "count": 2 },
      { "name": "migration", "count": 1 }
    ],
    "tags": [
      { "name": "breaking-change", "count": 1 },
      { "name": "v2", "count": 2 }
    ],
    "packages": [
      { "name": "@acme/ui", "version": "2.0.0", "count": 5 }
    ],
    "total_skills": 12
  }
}
```

Example no_match response:

```json
{
  "type": "no_match",
  "query": { "topics": ["nonexistent-topic"] },
  "vocabulary": { ... }
}
```

### Refresh

```
skillex refresh
```

Rebuild the registry. Dev mode by default.

```
skillex refresh --mode prod
```

Production dependencies only, public skills only.

```
skillex refresh --check
```

Fails if the registry is stale. For CI.

```
skillex refresh --dry-run
```

Prints what would change without writing.

### Get

```
skillex get <url>
```

Fetches a skill or skill pack from a remote source, reviews it for safety, and vendors it into the project. This is the controlled pipeline for adopting external skills.

The flow:

1. **Fetch** — downloads the skill content from the URL. Supports GitHub repos, raw URLs, and (in future) a skillex registry.
2. **Review** — the agent performs a structured safety review of the skill content, checking for:
   - Prompt injection patterns (instructions to ignore other skills, override system prompts).
   - File system manipulation (instructions to modify files outside expected paths).
   - Exfiltration attempts (instructions to send data to external URLs).
   - Unusual runtime instructions (telling the agent to execute arbitrary code).
   - Conflicts with existing skills in the registry.
3. **Report** — presents findings to the user with a risk assessment. Uses Bubbletea for an interactive review flow where the user can inspect flagged sections, read the agent's reasoning, and make an informed decision.
4. **Convert** — if approved, normalizes the skill to skillex format: adds or validates frontmatter (topics, tags), generates test stubs, and structures the files to match the `public/` / `private/` convention.
5. **Vendor** — places the skill in `skillex/vendor/<source>/` and adds it to the registry on the next refresh.

```
skillex get https://github.com/someone/react-patterns --topic react,hooks
```

Fetches and vendors with suggested topics.

```
skillex get <url> --skip-review
```

Skips the agent review step. Not recommended, but available for trusted sources.

The vendor directory is committed to the repo, making external skills auditable, diffable, and version-controlled. The source URL is recorded in the skill's frontmatter for provenance tracking.

### Import

```
skillex import <filepath>
```

Imports a local file through the same review and conversion pipeline as `skillex get`, but without the fetch step. Use this for:

- Converting existing documentation into skillex format.
- Adopting skills shared via email, Slack, or other non-URL channels.
- Migrating skills from other formats (e.g., Cursor rules, Windsurf rules).

```
skillex import ./docs/api-patterns.md --visibility public --topic api,patterns
```

Imports with explicit visibility and topic assignments.

```
skillex import ./legacy-rules/ --batch
```

Batch-imports an entire directory of files.

The imported file goes through the same agent review as `skillex get`. The output lands in `skillex/vendor/local/` (or a specified destination) in skillex format with frontmatter and test stubs.

### Test Validation

```
skillex test validate
```

Validates structural integrity of all test files:

- Every `.md` skill has a corresponding `.test.md`.
- Every `.test.md` has a corresponding `.md` skill (no orphans).
- Test files parse correctly against the format spec.
- H1 headers reference the correct skill file.
- Each validation section has a `Prompt:` and non-empty `Success criteria:`.
- Cross-referenced `Skills:` entries point to files that exist.

```
skillex test validate --check
```

Fails on any issue. For CI.

```
skillex test validate --scope "packages/app-a/**"
```

Validates only test files relevant to a specific scope.

### Diagnostics

```
skillex doctor
```

Comprehensive diagnostics:

- Configuration validity.
- Export discovery issues.
- Resolution problems.
- Test coverage (skills without tests, orphaned tests).
- Topic and tag distribution.
- Skills with no frontmatter.
- Vendor skill provenance and staleness.

```
skillex version
```

Show CLI version.

## 8. MCP Server

### Purpose

For agent harnesses that support Model Context Protocol (Cursor, Claude Code, and others), skillex can run as an MCP server. This provides native integration without shell command construction or stdout parsing.

The MCP server is a thin adapter over the same core library that powers the CLI. It exposes two MCP primitives:

### Resources

Skills are exposed as MCP resources. Each skill in the registry becomes a discoverable resource with:

- A URI following the pattern `skillex://skills/{scope}/{package}/{filename}`
- Metadata including topics, tags, visibility, and source package
- The skill content as the resource body

Agents discover available resources through the MCP protocol's resource listing. This replaces the `AGENTS.md` manifest for MCP-capable agents — the agent sees what's available through the protocol itself without reading a file.

### Tools

The query engine is exposed as an MCP tool:

```
Tool: skillex_query
Parameters:
  path:    string (optional) — file path or glob
  topic:   string[] (optional) — topic filters
  tags:    string[] (optional) — tag filters
  package: string (optional) — package name filter
  format:  "content" | "summary" (default: "content")
```

This is functionally identical to `skillex query` but with typed parameters and structured responses. The agent doesn't need to know CLI syntax — it calls a typed tool.

### Running the MCP Server

```
skillex mcp
```

Starts the MCP server using stdio transport (the standard for local MCP servers).

Configuration in agent harnesses follows their standard MCP setup. For example, in a `.cursor/mcp.json` or equivalent:

```json
{
  "mcpServers": {
    "skillex": {
      "command": "skillex",
      "args": ["mcp"]
    }
  }
}
```

### When to Use MCP vs CLI

- **MCP** — preferred when the agent harness supports it. Typed parameters, structured responses, resource discovery. No shell escaping, no stdout parsing.
- **CLI** — universal fallback. Works with every agent harness. Also the right choice for CI, scripting, and human use.
- **AGENTS.md** — last resort for agents that neither support MCP nor can reliably execute shell commands. Also serves as human-readable documentation of what's available.

## 9. AGENTS.md Manifest

On every refresh, skillex auto-generates (or updates) a section in the repo's `AGENTS.md`. This section serves two purposes: it teaches the agent how to interact with skillex, and it provides a manifest of what's available.

The generated section is MCP-first — it tells the agent to prefer the MCP server when available and fall back to CLI commands otherwise.

```markdown
## Skillex

This project uses Skillex for skill management. Use the skillex MCP server
if available (preferred), otherwise use the CLI commands below.

### MCP (preferred)

If the `skillex` MCP server is connected, use it directly:

- Use the `skillex_query` tool with parameters: path, topic, tags, package, format.
- Browse available skills through MCP resource discovery.

### CLI (fallback)

If MCP is not available, query skills via the command line:

  skillex query --path <filepath>
  skillex query --topic <topic> --tags <tags>
  skillex query --package <package>
  skillex query --path <glob> --topic <topic> --format content

### Available scopes

  - */** (repo-wide)
  - packages/app-a/**
  - packages/app-b/**

### Available topics

  error-handling, configuration, migration,
  authentication, testing, deployment

### Available tags

  v2, breaking-change, deprecated, security,
  getting-started

### Packages with skills

  @acme/foo (2.3.1) — 3 public, 2 private
  @acme/bar (1.0.0) — 1 public
```

This section is generated from the registry. The LLM reads it once at session start and knows exactly what's available and how to ask for it.

Skillex manages only its own section in `AGENTS.md`, delimited by markers. It does not modify other content.

## 10. Skill Testing Model

### 10.1 Philosophy

The agent is the test runtime, not the CLI. Skillex validates structure; agents validate behavior.

Skill quality is inherently a semantic question — whether a skill produces correct guidance can only be evaluated by the same kind of system that consumes the skill. The CLI checks that test files exist and are well-formed. The agent assesses whether the skill's guidance is actually correct and useful.

### 10.2 How Agents Use Test Files

When a developer asks an agent to validate a skill, the agent:

1. Queries the registry for the skill and its test scenarios.
2. For each scenario, evaluates the prompt with the skill loaded.
3. Self-assesses the output against the success criteria.
4. Reports which validations passed, which failed, and why.

### 10.3 The Skill-Testing Meta-Skill

A repo can include a meta-skill that teaches agents how to work with test files:

- How to interpret `.test.md` files and run self-evaluations.
- How to report results (pass/fail with reasoning).
- How to run scenarios multiple times to detect nondeterminism.
- How to propose improvements to skills based on test failures.
- How to suggest structural changes (splitting large skills, reducing overlap).
- How to evaluate context efficiency (whether loaded skills influence outputs).

This is a regular skill file with its own tests:

```
skills/
  skill-testing.md
  skill-testing.test.md
```

The meta-skill is not required. Agents with sufficient capability will understand the test format directly. The meta-skill provides more structured guidance for consistent results.

### 10.4 Skill Analysis

The meta-skill can also guide agents to perform analysis on the skill corpus:

- **Overlap detection** — are two skills covering the same ground for the same scope?
- **Context pollution** — is a skill being loaded into scopes where its content isn't relevant?
- **Split recommendations** — would a large skill be more effective as two focused skills?
- **Coverage gaps** — are there scopes or packages with no skills at all?
- **Tag hygiene** — are topics and tags used consistently across the corpus?

These analyses are performed by the agent reading skill metadata from the registry. The CLI could provide supporting queries (e.g., `skillex doctor` could flag potential overlap), but the deeper analysis is agent-driven.

## 11. Package Registration (Node)

### Discovery

The scanner finds skillex exports through `package.json`:

```json
{
  "skillex": true
}
```

This means: "this package exports skills at `skillex/public/` and `skillex/private/`."

Or with a custom path:

```json
{
  "skillex": {
    "path": "docs/skillex"
  }
}
```

### Resolution

For each dependency boundary:

1. Read the boundary's `package.json`.
2. Enumerate direct dependencies (and devDependencies in dev mode).
3. Resolve installed package roots.
4. Check each package's `package.json` for a `skillex` field.
5. If present, scan the declared skill directories.
6. Parse frontmatter and test files.
7. Feed results to the linker for scope and visibility assignment.

## 12. Platform Distribution

Skillex is written in Go and shipped as prebuilt binaries. Key library choices:

- **Cobra** — command structure and flag parsing.
- **Bubbletea** — interactive TUI for diagnostic and exploratory commands.
- **Lipgloss** — styled terminal output to stderr.
- **modernc.org/sqlite** (or similar pure-Go SQLite) — embedded database with no CGo dependency, simplifying cross-compilation.
- **MCP SDK** — stdio-based MCP server implementation.

SQLite is embedded in the binary — no external database dependency.

Supported targets:

- macOS (amd64 / arm64)
- Linux (amd64 / arm64)
- Windows (amd64)

### npm Wrapper Package

An optional npm package provides:

- The Go binaries
- A platform selector script
- A `skillex` command exposed via `bin`

This allows repos to install skillex as a dev dependency.

## 13. Repository Integration

### Getting Started

```
skillex init
```

This creates all required files and configuration. See §7 (Init) for details.

### Required Files (after init)

```
skillex.yaml
.skillex/index.db
AGENTS.md (skillex-managed section)
```

Recommended scripts:

```json
{
  "skillex:refresh": "skillex refresh",
  "skillex:test": "skillex test validate",
  "skillex:doctor": "skillex doctor"
}
```

Optional:

```json
{
  "postinstall": "skillex refresh"
}
```

CI:

```
skillex refresh --check
skillex test validate --check
```

## 14. Rule Evaluation

Rules are evaluated in order and are additive. When determining the skills for a given working path:

1. Walk the rules list top to bottom.
2. For each rule whose `Scope` glob matches the working path, add its `Skills` to the result set.
3. If the rule declares a `DependencyBoundary`, resolve the boundary's dependencies and add their public skills.
4. If the working path is inside a package that has skillex exports, add that package's private skills.

The registry pre-computes scope assignments so queries are index lookups, not rule evaluations.

## 15. Security and Determinism

- Dependencies must not modify the consumer repository.
- Output is derived only from:
  - Root `skillex.yaml`
  - Dependency `package.json` metadata
  - Dependency skillex exports
- No network access required at any stage.
- No lockfile mutation.
- Test validation is purely structural — no LLM calls, no side effects.
- The SQLite database is a build artifact, fully reproducible from inputs.

## 16. Future Extensions

Potential future improvements:

- Additional resolvers (Python, Rust, Go, Java)
- Git-change based scopes
- Cached incremental refresh
- Richer scope activation metadata
- Conditional skill inclusion (e.g., only link migration skills when upgrading)
- Table-of-contents skills for two-phase retrieval
- Test coverage reporting and nondeterminism tracking
- Semantic search as an optional query mode alongside structured queries
- Remote skill registries for cross-repo skill sharing
- Skill analysis tooling: automated detection of overlapping skills, context pollution measurement, and split recommendations
- Vendor skill update checking: `skillex get --update` to re-fetch and re-review updated versions of vendored skills

These are intentionally excluded from v0.6 to keep the system simple and deterministic.
