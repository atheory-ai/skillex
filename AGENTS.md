# AGENTS

This file documents how to work in this repository.


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

If MCP is not available, query skills via the command line:

```
  skillex query --search "<concepts>"
  skillex query --path <filepath> --limit 8
  skillex query --topic <topic> --tags <tags>
  skillex read --ref <ref-from-query> --section <optional-section-id>
```

### Available scopes

  - **

### Available topics

  repo-conventions

### Available tags

  getting-started

<!-- skillex:end -->
