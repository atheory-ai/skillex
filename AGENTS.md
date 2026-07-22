# AGENTS

This file documents how to work in this repository.

## Local development

This checkout dogfoods the current source, not a globally installed Skillex release.

1. Run `make dev-binary` after checkout. It builds `.skillex/bin/skillex` from this source tree.
2. Run `make dev-binary` again after changing Go source.
3. Use `./.skillex/bin/skillex` for local Skillex commands (`.exe` on Windows). `make refresh` and the acceptance suite rebuild or select it automatically.


<!-- skillex:start -->
## Skillex

This project uses Skillex for skill management. Use the skillex MCP server
if available (preferred), otherwise use the CLI commands below.

### MCP (preferred)

If the `skillex` MCP server is connected, use it directly:

- Start with `skillex_query` (path, topic, tags, package, search, limit, cursor). It returns bounded discovery summaries, not whole skill files.
- If `too_broad` is true, use its candidate-scoped `narrow_with` facets and suggestions to refine the query.
- Use `skillex_read` with a selected result `ref` and optional section id only after discovery; keep reads bounded.
- MCP resources provide a skill table of contents. Do not bulk-load skill content.

### CLI (fallback)

If MCP is not available, query skills via the command line. If the repository documents a local development binary, use it instead of a globally installed release:

```
  skillex query --search "<concepts>"
  skillex query --path <filepath> --limit 8
  skillex query --topic <topic> --tags <tags>
  skillex read --ref <ref-from-query> --section <optional-section-id>
```

### Available scopes

  - **
  - .github/workflows/release.yml
  - .goreleaser.yaml
  - CHANGELOG.md
  - Makefile
  - VERSION
  - cli/query.go
  - cli/read.go
  - docs/manual-testing.md
  - internal/agents/**
  - internal/packs/**
  - internal/query/**
  - internal/registry/**
  - mcp/server.go
  - test/**
  - test/acceptance/pack_test.go

### Available topics

  golden-tests, integration, mcp, packs, registry, release-recovery, releases, repo-conventions, retrieval, testing

### Available tags

  acceptance, boundaries, compatibility, cross-repository, discovery, getting-started, github-actions, golden-tests, publishing, skillex-packs, verification, versioning, workflow

<!-- skillex:end -->
