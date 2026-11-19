# Tests: recheck-assumptions.md

## Validation: repeated fixes share an assumption

Prompt: Two cache invalidation changes failed the same test. Both assume the reader gets its value from the cache. Continue investigating.
Success criteria:
  - States the assumption shared by the failed fixes
  - Uses an observation that distinguishes the cache from another data source
  - Gathers evidence before editing the invalidation logic again
