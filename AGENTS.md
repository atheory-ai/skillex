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

At the start of each conversation, call `skillex_query` with no filters if Skillex setup context is missing. Follow the setup guidance from Skillex and use `skillex_read` for returned refs.

### CLI (fallback)

If MCP is not available, query skills via the command line. If the repository documents a local development binary, use it instead of a globally installed release:

Run the unfiltered query at the start of each conversation if setup context is missing. If it returns a core ref, use `read --ref` when that revision's guidance is missing or the revision changes. When a core cue applies, read its linked skill if needed. Match tasks using their cues and apply the linked skill before each occurrence. Use `read --ref` when a linked skill is missing from context.

Use filtered queries for additional context. Refine broad results before reading a selected skill or section. Treat update notices as advisory.

```
  ./.skillex/bin/skillex query
  ./.skillex/bin/skillex query --search "<concepts>" --json
  ./.skillex/bin/skillex query --path <filepath> --limit 8 --json
  ./.skillex/bin/skillex query --topic <topic> --tags <tags> --json
  ./.skillex/bin/skillex query --source skillex --json
  ./.skillex/bin/skillex read --ref <ref-from-query> --section <optional-section-id>
```

<!-- skillex:end -->
