# Tests: retrieval-contract.md

## Validation: summary-first change

Prompt: I want query to return a new relevance signal. What else must I review besides the query function?
Success criteria:
  - Covers CLI and MCP parity
  - Covers registry/index and migration implications when relevant
  - Requires acceptance and golden contract coverage
  - Mentions generated AGENTS.md guidance

## Validation: broad query

Prompt: A search matches many skills. What should the agent receive and do next?
Success criteria:
  - Recommends bounded summaries rather than full skill bodies
  - Uses too_broad, narrowing, cursor, or selected reads appropriately

## Validation: capability pagination performance

Prompt: A capability query matches 50,000 records but returns 20. Where should filtering, facets, counts, pagination, hydration, and reference signing happen?
Success criteria:
  - Requires SQLite to filter, count, facet, rank, and paginate the complete candidate set
  - Hydrates and signs only the requested page
  - Preserves candidate-scoped narrowing facets and stable cursor behavior
  - Requires a broad 50,000-capability benchmark with allocation reporting
