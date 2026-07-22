---
name: Repository operations
description: Work effectively in the Skillex engine repository. Use for repository-wide conventions, source orientation, local command selection, generated agent instructions, or when adding and maintaining this repository's own skills.
topics: [repo-conventions]
tags: [getting-started, workflow]
---

# Repository Operations

## Orient

- Treat `cli/` and `mcp/` as interface layers over shared behavior in `internal/`.
- Keep query and retrieval behavior in `internal/query/`; indexing and schema migrations in `internal/registry/`; discovery in `internal/scanner/`; visibility and scope linking in `internal/linker/`; and generated agent instructions in `internal/agents/`.
- Treat `test/acceptance/` and `test/golden/` as product-contract coverage, not incidental fixtures.
- Keep repository-wide agent knowledge in `skills/`. Add a co-located `<name>.test.md` for every substantive skill.

## Work locally

- Prefer the connected Skillex MCP server. Otherwise run `./.skillex/bin/skillex` (`.exe` on Windows); bootstrap and rebuild it with `make dev-binary` after changing Go source. Do not substitute a globally installed release for this checkout.
- Start with bounded discovery (`query`), then read a selected result or section. Do not bulk-read skill content.
- Run `make verify-unit` for a fast Go gate. Run `go test ./...` or `make test-acceptance` when a change affects acceptance behavior.
- Run `make refresh` after changing skills or generated agent instructions. Do not hand-edit the generated Skillex block in `AGENTS.md`.

## Make changes complete

- Preserve CLI and MCP parity because both expose the same core behavior.
- Add or update acceptance and golden coverage when a visible command, response shape, index behavior, or generated instruction changes.
- Keep commits focused. Version bumps and release-only changes belong in the release workflow, not unrelated feature work.
