---
name: Testing and golden harness
description: Validate Skillex changes that affect Go behavior, CLI or MCP responses, registry indexing, generated instructions, skill files, acceptance tests, or golden fixtures. Use when selecting tests or deciding whether a behavior change needs contract coverage.
topics: [testing, golden-tests]
tags: [verification, acceptance]
---

# Testing and Golden Harness

## Choose the smallest sufficient gate

- Use `make verify-unit` for formatting, vetting, unit tests, and a build.
- Use `go test ./...` for the full Go suite, including acceptance packages.
- Use `make test-acceptance` when command output, fixtures, discovery, query, MCP, or end-to-end behavior changes.
- Use `make test-race` for concurrent registry, scanner, or server changes. Run `make lint` when touching code governed by the repository lint configuration.

## Preserve product contracts

- Treat `test/golden/` as a user-facing behavior corpus. Change fixtures and assertions only when the intended behavior has changed.
- Keep CLI and MCP tests aligned when they expose the same retrieval or indexing contract.
- Add targeted acceptance coverage for a regression; do not rely only on a unit test when the observable workflow crosses layers.

## Validate skills and agent instructions

- Give each substantive skill a co-located `.test.md` containing realistic prompts and falsifiable success criteria.
- Run `go run ./cmd/skillex test validate --check` after adding or editing skill tests.
- Run `make refresh` after skill edits, then inspect the generated `AGENTS.md` discovery guidance.
- Use `docs/manual-testing.md` for exploratory and release-candidate journeys that automated tests cannot cover.
