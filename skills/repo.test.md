# Tests: repo.md

## Validation: orient a contributor

Prompt: I need to change query behavior and update this repository's own skills. Where should I work and how should I validate it?
Success criteria:
  - Identifies the query, registry, CLI/MCP, acceptance, golden, and skills areas appropriately
  - Recommends bounded Skillex discovery before reading content
  - Uses refresh to reindex skills/ changes without modifying agent instruction files
  - Does not treat an MCP project-skill watch as a substitute for refreshing skills/, configuration, or dependencies
  - Uses init to regenerate agent instructions when their generator changes
  - Does not recommend editing the generated AGENTS.md block by hand

## Validation: run from source

Prompt: The skillex command is not installed on PATH. How do I use the repository's implementation while developing it?
Success criteria:
  - Uses make dev-binary to build the local development binary
  - Uses .skillex/bin/skillex rather than a globally installed release
  - Keeps the normal query-then-read retrieval sequence
