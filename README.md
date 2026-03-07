# Skillex

**Skill management for AI agents in Node.js projects.**

Skillex solves the problem of agent skill discovery in monorepos and dependency-heavy projects. It gives agents exactly the skills they need — versioned, scoped, instantly queryable — without polluting their context window with irrelevant documentation.

---

## The problem

Modern agent workflows depend on "skills": Markdown documents that teach agents how to use packages, follow repo conventions, handle migrations, and work safely in a codebase. As projects grow, skill management breaks down:

- Skills differ by **package version** — the right guidance for `@acme/foo@2` is wrong for v1.
- **Monorepos** may have different versions of the same package in different workspaces.
- Loading all skills upfront **wastes context window**. Agents get everything or nothing.
- Skills scattered across docs folders require agents to **browse multiple files** to find what they need.

Skillex covers the full lifecycle: **authoring, indexing, and retrieving** exactly the right skills on demand — in a single query, in microseconds.

---

## How it works

```
Your repo                        Agent
─────────────────────────────    ──────────────────────────────
skillex.yaml                     1. "What skills apply to
skills/repo.md                      packages/app-a/src/auth.ts?"
packages/app-a/
  package.json                   2. skillex query
  node_modules/                     --path packages/app-a/src/auth.ts
    @acme/foo/
      skillex/                   3. Returns: repo.md + @acme/foo
        public/consumer.md          consumer.md + auth.md
        public/auth.md              (correct version, scoped)
        private/internals.md
                                 4. Agent reads skills, proceeds
.skillex/index.db  ◀── rebuilt      with accurate context
                       on refresh
```

Skillex scans your dependencies for skill exports, links them to the right scopes, stores everything in a local SQLite registry, and serves queries in microseconds. The registry is a deterministic build artifact — same repo state always produces the same index.

---

## Features

- **Deterministic** — same repo state produces the same index, every time.
- **Version-correct** — skills are read from the resolved package install, not from the internet.
- **Scope-aware** — skills are linked to the paths where they apply. A query for `packages/app-a/**` never returns skills for `packages/app-b`.
- **Public and private** — packages export consumer-facing skills (public) and contributor-facing skills (private). Visibility is enforced automatically.
- **Instant retrieval** — SQLite index with structured queries. No document browsing, no semantic search overhead.
- **MCP native** — first-class Model Context Protocol server. Agents with MCP support get typed tool calls and resource discovery.
- **CLI fallback** — every agent harness can call the CLI. Works in CI, scripts, and terminals.
- **AGENTS.md manifest** — auto-generated fallback for agents that can't run MCP or shell commands.
- **Testable** — every skill can have a co-located `.test.md` file with structured validation scenarios.
- **Non-invasive** — dependencies never modify your repo. No lockfile mutation, no network calls at query time.

---

## Installation

### npm (recommended for Node.js projects)

```bash
npm install --save-dev @skillex/skillex
# or
pnpm add -D @skillex/skillex
# or
yarn add -D @skillex/skillex
```

The package automatically installs the correct binary for your platform (macOS arm64/x64, Linux arm64/x64, Windows x64) via npm's `optionalDependencies` mechanism — only the binary for your OS is downloaded.

### go install

```bash
go install github.com/ladyhunterbear/skillex/cmd/skillex@latest
```

### Build from source

```bash
git clone https://github.com/ladyhunterbear/skillex
cd skillex
make build          # produces ./skillex
make install        # installs to $GOPATH/bin
```

---

## Quick start

### Initialize your repo

```bash
skillex init
```

This creates:
- `skillex.yaml` — configuration file
- `skills/repo.md` — a starter repo-wide skill
- `AGENTS.md` — auto-generated agent instructions (MCP + CLI)
- `.skillex/index.db` — the registry (rebuilt on each refresh)

To also configure MCP for your agent harness:

```bash
skillex init --harness cursor       # writes .cursor/mcp.json
skillex init --harness claude-code  # writes .claude/mcp.json
skillex init --harness windsurf     # writes .windsurf/mcp.json
```

### Write your first skill

Edit `skills/repo.md`:

```markdown
---
topics: [conventions, git]
tags: [getting-started]
---

# Repository Conventions

## Commit messages
We use conventional commits: feat:, fix:, chore:, docs:

## Branch naming
feature/<ticket>-<short-description>
```

### Rebuild the index

```bash
skillex refresh
```

### Query skills

```bash
# All skills for a file path
skillex query --path packages/app-a/src/auth.ts

# By topic
skillex query --topic error-handling

# By tag
skillex query --tags migration,breaking-change

# By package
skillex query --package @acme/foo

# Compound query — intersection of all filters
skillex query --path packages/app-a/** --topic auth --tags v2

# Full content, ready to pipe to an agent
skillex query --path packages/app-a/** --format content
```

---

## Configuration

### `skillex.yaml`

```yaml
Version: 4

Rules:
  # Repo-wide skill — applies everywhere
  - Scope: "**"
    Skills:
      - skills/repo.md

  # Skills for everyone working in packages/
  - Scope: "packages/*/**"
    Skills:
      - skills/package-dev.md

  # Resolve npm dependencies for app-a and link their skill exports
  - Scope: "packages/app-a/**"
    DependencyBoundary: packages/app-a
```

**Rules are additive.** A path matching multiple rules accumulates skills from all of them.

| Field | Description |
|---|---|
| `Scope` | Glob pattern. Skills in this rule apply when the working path matches. |
| `Skills` | Repo-local skill files to attach to this scope. |
| `DependencyBoundary` | Path to a `package.json`. The scanner reads its dependencies and links any that export skills. |

---

## Skills

A skill is a Markdown file with optional YAML frontmatter.

```markdown
---
topics: [error-handling, validation]
tags: [v2, breaking-change]
---

# Error Handling in @acme/foo

When using FooClient, all API calls return a Result type...
```

### Frontmatter fields

| Field | Description |
|---|---|
| `topics` | Semantic categories. Used for `--topic` queries. |
| `tags` | Freeform labels. Used for `--tags` queries. |
| `source` | (Vendor skills) URL the skill was imported from. |
| `reviewed` | (Vendor skills) Timestamp of last agent review. |

Both fields are optional. Skills without frontmatter are still indexed and queryable by path, scope, and package.

---

## Exporting skills from a package

Any npm package can export skills by adding a `skillex` field to its `package.json`:

```json
{
  "name": "@acme/foo",
  "skillex": true
}
```

Then create the skill directories:

```
skillex/
  public/         ← skills for consumers of the package
    consumer.md
    consumer.test.md
    migrations.md
    migrations.test.md
  private/        ← skills for contributors to the package
    architecture.md
    dev-workflow.md
```

To initialize a package for skill exports:

```bash
skillex init --package
```

**Public skills** are linked when the package appears as a dependency of the current scope.
**Private skills** are linked when the agent's working path is inside the package's source tree.

### Custom skill directory

```json
{
  "skillex": {
    "path": "docs/skillex"
  }
}
```

---

## Skill tests

Every skill can have a co-located test file (`<name>.test.md`). Tests are structured scenarios that agents use to self-evaluate whether a skill produces correct guidance.

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

**The agent is the test runtime.** Skillex validates structure; agents validate behavior. To check structural integrity:

```bash
skillex test validate
skillex test validate --check   # exit non-zero on errors (CI)
```

---

## MCP server

Skillex runs as a Model Context Protocol server, providing native integration for MCP-capable agent harnesses (Cursor, Claude Code, Windsurf, and others).

```bash
skillex mcp
```

### MCP configuration

Add to your harness's MCP config (e.g. `.cursor/mcp.json`):

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

Or let `skillex init --harness <name>` write this for you.

### MCP primitives

**Tool: `skillex_query`**

```
Parameters:
  path    string    File path or glob pattern
  topic   string    Comma-separated topic filters
  tags    string    Comma-separated tag filters
  package string    Package name filter
  format  string    "content" (default) or "summary"
```

**Resources**

Each skill in the registry is exposed as a discoverable MCP resource at:
```
skillex://skills/{scope}/{package}/{filename}
```

Agents discover available resources through the MCP protocol's resource listing — no `AGENTS.md` parsing required.

---

## CLI reference

All commands support `--json` (structured stdout) and `--quiet` (suppress stderr).

### `skillex init`

```bash
skillex init                      # Interactive setup for a repo
skillex init --yes                # Accept all defaults
skillex init --package            # Initialize a package for skill exports
skillex init --harness cursor     # Also configure MCP for Cursor
```

### `skillex refresh`

```bash
skillex refresh                   # Rebuild the registry (dev mode)
skillex refresh --mode prod       # Production deps + public skills only
skillex refresh --check           # Fail if registry is stale (CI)
skillex refresh --dry-run         # Preview without writing
```

### `skillex query`

```bash
skillex query --path <filepath>
skillex query --topic <topic>
skillex query --tags <tag1,tag2>
skillex query --package <name>
skillex query --path <glob> --topic <topic> --format content
skillex query --format summary --json
```

### `skillex test validate`

```bash
skillex test validate             # Check all test files
skillex test validate --check     # Exit non-zero on errors (CI)
skillex test validate --scope "packages/app-a/**"
```

### `skillex doctor`

```bash
skillex doctor                    # Full diagnostics report
skillex doctor --json             # Machine-readable report
```

Checks: configuration validity, registry health, test coverage, topic/tag distribution, AGENTS.md presence, vendor skill provenance.

### `skillex get`

```bash
skillex get <url>                         # Fetch and vendor a remote skill
skillex get <url> --topic react,hooks     # Assign topics on import
skillex get <url> --skip-review           # Skip safety review
```

Fetches a skill from a URL, runs a structural safety review (checking for prompt injection patterns, exfiltration attempts, and dangerous commands), converts it to skillex format, and vendors it to `skillex/vendor/<source>/`.

### `skillex import`

```bash
skillex import ./docs/api-patterns.md
skillex import ./docs/api-patterns.md --visibility public --topic api,patterns
skillex import ./legacy-rules/ --batch
```

Imports a local file through the same review and conversion pipeline as `skillex get`. Use this to migrate Cursor rules, Windsurf rules, or any existing Markdown documentation.

### `skillex mcp`

```bash
skillex mcp                       # Start MCP server on stdio
```

### `skillex version`

```bash
skillex version
skillex version --json
```

---

## CI integration

Add to your CI pipeline:

```bash
# Fail if the registry is out of date with the source files
skillex refresh --check

# Fail if any test files are malformed
skillex test validate --check
```

Recommended `package.json` scripts:

```json
{
  "scripts": {
    "skillex:refresh": "skillex refresh",
    "skillex:test":    "skillex test validate",
    "skillex:doctor":  "skillex doctor",
    "postinstall":     "skillex refresh"
  }
}
```

---

## Vendoring external skills

Skillex provides a controlled pipeline for adopting skills from external sources. Vendor skills are committed to your repo, making them auditable, diffable, and version-controlled.

```bash
# Fetch from a URL
skillex get https://raw.githubusercontent.com/someone/react-patterns/main/hooks.md

# Import from a local file
skillex import ./cursor-rules.md --visibility public --topic react

# Batch import a directory
skillex import ./legacy-docs/ --batch
```

Vendored skills land in `skillex/vendor/<source>/` with:
- Normalized frontmatter (topics, tags)
- Source URL recorded for provenance
- Auto-generated test stubs

---

## AGENTS.md

On every `refresh`, Skillex auto-generates (or updates) a section in `AGENTS.md`. This serves as a fallback for agents that support neither MCP nor shell execution.

```markdown
<!-- skillex:start -->
## Skillex

This project uses Skillex for skill management. Use the skillex MCP server
if available (preferred), otherwise use the CLI commands below.

### MCP (preferred)
...

### CLI (fallback)
...

### Available scopes
  - **
  - packages/app-a/**

### Available topics
  error-handling, configuration, migration, authentication

### Available tags
  v2, breaking-change, deprecated, getting-started

### Packages with skills
  @acme/foo (2.3.1) — 3 public, 2 private
<!-- skillex:end -->
```

Skillex manages only its own section, delimited by markers. It never modifies other content in the file.

---

## Project structure

```
.
├── skillex.yaml              # Configuration
├── AGENTS.md                 # Agent instructions (auto-updated)
├── skills/                   # Repo-level skills
│   ├── repo.md
│   └── repo.test.md
└── .skillex/
    └── index.db              # Registry (build artifact, not committed)
```

For packages exporting skills:

```
my-package/
├── package.json              # "skillex": true
└── skillex/
    ├── public/               # Consumer-facing skills
    │   ├── consumer.md
    │   └── consumer.test.md
    ├── private/              # Contributor-facing skills
    │   ├── architecture.md
    │   └── dev-workflow.md
    └── vendor/               # External skills (committed)
        ├── github.com/someone/react-patterns/
        │   └── hooks.md
        └── local/
            └── imported-guide.md
```

---

## How agents use Skillex

### With MCP (preferred)

The agent discovers available skills through MCP resource listing and calls the `skillex_query` tool directly — no shell commands, no file parsing.

### With CLI (fallback)

The agent reads `AGENTS.md` at session start to learn what's available and how to query, then calls `skillex query` when it needs skills for a specific path or topic.

### Skill testing model

When validating a skill, the agent:

1. Queries the registry for the skill and its test scenarios
2. For each scenario, evaluates the prompt with the skill loaded
3. Self-assesses the output against the success criteria
4. Reports which validations passed, failed, and why

The CLI validates structure. The agent validates behavior.

---

## Architecture

```
              ┌─────────────────┐
              │   AGENTS.md     │  Fallback
              └────────┬────────┘
                       │
              ┌────────┴────────┐
              │   MCP Server    │  Native (resources + tools)
              └────────┬────────┘
                       │
              ┌────────┴────────┐
              │   CLI           │  Foundation (Cobra + Lipgloss)
              └────────┬────────┘
                       │
┌──────────────────────┴──────────────────────────────┐
│                    skillex core                       │
│                                                      │
│  Scanner → Linker → Registry (SQLite) → Query engine │
│  Validator                                           │
└─────────────────────────────────────────────────────┘
```

**Core engine** (Go library):

| Component | Responsibility |
|---|---|
| Scanner | Discovers skill files in the repo and in installed npm packages |
| Linker | Resolves public/private visibility and scope assignments |
| Registry | SQLite database storing skills, topics, tags, scopes, and test scenarios |
| Query engine | Structured retrieval by path, topic, tags, and package |
| Validator | Checks that skill and test files are well-formed |

**Interface layers** (all backed by the same core):

| Layer | Use case |
|---|---|
| CLI | Universal. CI, scripts, terminals, any agent harness |
| MCP server | Native integration for MCP-capable harnesses |
| AGENTS.md | Last resort for agents that can't run MCP or shell commands |

---

## Building from source

**Requirements:** Go 1.22+

```bash
git clone https://github.com/ladyhunterbear/skillex
cd skillex

make build      # ./skillex
make install    # $GOPATH/bin/skillex
make test       # go test ./...
make lint       # go vet ./...
make dist       # cross-compile for all platforms → dist/
```

**Cross-compiled targets:**

| File | Platform |
|---|---|
| `dist/skillex-darwin-arm64` | macOS Apple Silicon |
| `dist/skillex-darwin-x64` | macOS Intel |
| `dist/skillex-linux-x64` | Linux x64 |
| `dist/skillex-linux-arm64` | Linux arm64 |
| `dist/skillex-win32-x64.exe` | Windows x64 |

### Publishing the npm package

```bash
VERSION=0.6.0 make npm-pack     # produces tarballs in dist/ for inspection
VERSION=0.6.0 make npm-publish  # publishes all 6 packages to npm
```

---

## Security

- Dependencies **never modify** the consumer repository.
- No network access at query time — all data comes from the local registry.
- No lockfile mutation.
- `skillex get` and `skillex import` run a structural safety review before vendoring any external skill, checking for prompt injection patterns, exfiltration attempts, and dangerous commands.
- The SQLite database is fully reproducible from source inputs.

---

## License

MIT
