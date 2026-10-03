# Tests: registry-and-migrations.md

## Validation: add a compatible migration

Prompt: How do I add a new SQLite registry column without breaking an existing checkout or two agents opening it at once?
Success criteria:
  - Updates the fresh schema and increments currentSchemaVersion
  - Uses the existing BEGIN IMMEDIATE transaction and rechecks user_version inside it
  - Checks columnNames before ALTER TABLE and provides safe existing-row defaults or backfill
  - Rolls back failures and advances user_version only after successful migration work
  - Tests fresh, old populated, repeated, partial, and concurrent opens plus affected query behavior

## Validation: backfill with one connection

Prompt: My registry migration reads old rows and writes a search index, but hangs when executing an insert inside rows.Next. What should I change?
Success criteria:
  - Preserves the single-connection registry setting
  - Buffers source rows, checks rows.Err, and closes rows before issuing writes
  - Does not rely on deferred row closure to release the connection before nested database work
  - Handles nullable text using COALESCE or sql.NullString

## Validation: recover generated state

Prompt: I changed a skill and its scope but query still returns the old content. Should I manually update index.db or regenerate AGENTS.md?
Success criteria:
  - Treats source skills and configuration as authoritative
  - Rebuilds the checkout binary after Go changes and refreshes the registry from source
  - Validates co-located skill test structure
  - Distinguishes refresh ownership from init ownership of generated instructions
