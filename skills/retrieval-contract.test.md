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
