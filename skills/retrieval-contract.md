---
name: Retrieval contract
description: Change or review Skillex discovery and retrieval behavior. Use when modifying query or read commands, MCP query or read tools, result summaries, cursors, section references, full-text ranking, registry migrations, or generated instructions that teach agents how to retrieve skills.
topics: [retrieval, mcp, registry]
tags: [discovery, compatibility, golden-tests]
---

# Retrieval Contract

## Preserve progressive retrieval

- Make discovery return bounded summaries: selected skill metadata, relevance signals, and narrowing information rather than complete skill bodies.
- Require a selected result reference for content reads. Support reading a chosen section when that is enough context.
- Treat result references, section identifiers, cursors, `too_broad` signals, and narrowing facets as interface contracts. Keep their behavior stable or document and test an intentional change.

## Change all interface layers together

- Keep CLI and MCP behavior aligned over the shared query and registry implementation.
- When changing search, headings, body indexing, or ranking, account for registry schema migration and fresh-index behavior.
- Update generated `AGENTS.md` guidance when the recommended discovery or read flow changes.

## Prove the change

- Add focused unit coverage for parsing, ranking, pagination, or migration details.
- Add acceptance and golden coverage for observable summaries, narrowing, pagination, selected reads, and MCP parity.
- Test broad queries as well as exact matches; bounded retrieval is successful only when it helps an agent narrow safely.
