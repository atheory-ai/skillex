# Tests: testing-and-golden-harness.md

## Validation: retrieval regression

Prompt: I changed the query summary fields and the MCP response. What tests should I run and update?
Success criteria:
  - Recommends targeted acceptance and golden coverage
  - Mentions CLI and MCP parity
  - Distinguishes intentional fixture updates from incidental rewrites

## Validation: skill change

Prompt: I added a repository skill. How do I validate it and update agent instructions?
Success criteria:
  - Requires a co-located .test.md file
  - Uses skillex test validate --check
  - Uses refresh to reindex skills and verifies agent instruction files remain unchanged
  - Uses init to regenerate and inspect the managed AGENTS.md guidance when its generator changes

## Validation: capability query optimization

Prompt: I moved broad capability pagination into SQLite. How should I verify the optimization?
Success criteria:
  - Runs the 50,000-capability broad and exact benchmark with allocation reporting
  - Requires correctness coverage for totals, facets, filters, pagination, and migration
  - Includes acceptance or golden coverage for observable cursor and narrowing behavior
  - Includes race and lint gates for release readiness
