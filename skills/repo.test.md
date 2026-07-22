# Tests: repo.md

## Validation: orient a contributor

Prompt: I need to change query behavior and update this repository's own skills. Where should I work and how should I validate it?
Success criteria:
  - Identifies the query, registry, CLI/MCP, acceptance, golden, and skills areas appropriately
  - Recommends bounded Skillex discovery before reading content
  - Recommends refreshing generated agent instructions after skill changes
  - Does not recommend editing the generated AGENTS.md block by hand

## Validation: run from source

Prompt: The skillex command is not installed on PATH. How do I use the repository's implementation while developing it?
Success criteria:
  - Uses go run ./cmd/skillex as the source fallback
  - Keeps the normal query-then-read retrieval sequence
