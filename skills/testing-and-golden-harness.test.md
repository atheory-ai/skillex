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
  - Uses refresh and inspection of generated AGENTS.md guidance
