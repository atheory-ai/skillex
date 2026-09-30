---
name: Registry and migrations
description: Change Skillex's SQLite registry or add a schema migration. Use for connection settings, PRAGMA user_version upgrades, backfills, nullable field scans, and registry migration tests.
topics: [registry, migration]
tags: [sqlite, compatibility, testing]
---

# Registry and Migrations

## Keep connections predictable

- `internal/registry/registry.go` uses the pure-Go `modernc.org/sqlite` driver. Preserve `SetMaxOpenConns(1)`, `busy_timeout = 5000`, foreign keys, and WAL mode in `Open`.
- With one connection, close query rows before issuing another query or write. Buffer backfill rows, check `rows.Err()`, then close them before inserting; deferred closure alone can deadlock nested database work.
- Scan nullable text into `sql.NullString` or use `COALESCE(column, '')` for string destinations. Keep base-field scan order aligned with SQL projections, then load topics, tags, and scopes through `populateMeta`.
- On skill upsert, resolve the authoritative ID by unique path. `LastInsertId` is unreliable after `ON CONFLICT ... DO UPDATE`; child rows and FTS must use the resolved ID.

## Add a migration

1. Update the fresh-database schema in `registry.go` (or `createCapabilitySchema` for capability tables), then increment `currentSchemaVersion` in `migrate.go`.
2. Add the upgrade in `migrateSchema`. Read `PRAGMA user_version`, acquire `BEGIN IMMEDIATE`, and re-read the version inside that transaction so a competing opener can finish first.
3. Make the change recoverable and idempotent. Use `CREATE ... IF NOT EXISTS`; for SQLite `ALTER TABLE ADD COLUMN`, inspect `columnNames` first. Give existing rows safe defaults or backfill them explicitly. Gate destructive/rebuilding work on the previous version.
4. Roll back each failing migration path. Advance `user_version` only after all upgrade steps succeed, then commit. Keep fresh and upgraded databases equivalent.
5. Cover fresh creation, an old schema with existing data, repeat open, partial migration recovery, and concurrent openers in `internal/registry/migrate_test.go`. Exercise affected query/FTS behavior, not just column existence. Run `go test ./internal/registry ./internal/query` and the relevant CLI/MCP acceptance tests.

## Distinguish upgrades from refresh

- `Open` applies structural upgrades. Existing migration steps may backfill search data, but configured capability metadata is loaded by explicit refresh; opening a database must not introspect downstream MCP servers.
- Refresh scans and links source skills, clears the index, and reinserts skills, tests, and capability metadata. Source skills and configuration are authoritative; `.skillex/index.db` is generated state.
- After changing skills, rebuild the checkout binary with `make dev-binary` when needed, then `make refresh` and `./.skillex/bin/skillex test validate --check`. Refresh does not own generated agent instructions; use `init --yes` for those.
