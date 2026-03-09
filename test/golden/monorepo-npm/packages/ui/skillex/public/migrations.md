---
topics:
  - migration
tags:
  - v2
  - breaking-change
---
# Migration Guide: v1 to v2

Breaking changes in @test/ui v2 require updating component usage.

**Renamed props:**
- `type` → `variant` (Button, Badge)
- `color` → `intent` (Alert)

**Removed components:**
- `LegacyButton` — use `Button` with `variant="legacy"` instead.
