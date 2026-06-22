# Changelog

All notable changes to this project should be documented in this file.

The format is based on Keep a Changelog and the project uses Semantic Versioning.

## [Unreleased]

## [0.7.1]

### Fixed

- **Init:** write Claude Code MCP configuration to `.mcp.json` at the project root.
- **Release:** check out the repository before publishing GitHub release assets.

## [0.7.0]

### Added

- **Packs:** added `skillex/pack.yaml` as a new skill distribution and activation unit for project-local, package-shipped, and module-shipped skills.
- **Project-local packs:** repositories can commit packs at `skillex/pack.yaml` or `skillex/packs/*/pack.yaml`; activated skills are indexed individually with `source_type: pack`.
- **Pack activation:** packs can activate skills from `files-present`, `files-matching`, `dependency-declared`, and `detector` conditions.
- **Pack scopes:** packs support `repo`, `boundary`, `subtree`, `directory`, `matching-files`, and `nearest-ancestor` scope strategies.
- **Detector registry:** Skillex now has refresh-time detectors. Core provides a small baseline (`docker`, `go`, `javascript`, `typescript`), while loaded packs can register ecosystem, framework, or library detectors such as `gin`, `rails`, or `nextjs`.
- **Package-shipped packs:** Node packages can ship `skillex/pack.yaml` alongside existing `skillex/public` and `skillex/private` skill exports.
- **Go resolver:** added the first non-Node resolver. Skillex detects `go.mod` boundaries, reads Go module dependencies, resolves local `replace` and `vendor` module roots, and discovers Go module-shipped packs without network access or module mutation.
- **Go fixture coverage:** added an end-to-end Go fixture proving project-local Go packs and module-shipped Go packs are queryable by path and package.

### Changed

- Generalized scanner behavior so dependency ecosystems map into a shared resolver model rather than Node-specific package scanning.
- Documentation now presents Skillex as a polyglot project utility rather than only a Node package skill indexer.

## [0.6.1]

- Added an npm-facing README and richer package metadata for `@atheory-ai/skillex` so the npm package page explains what Skillex is and how to use it.
- Added a guarded `make release-tag` helper to create and push the `v*` tag that triggers the GitHub Actions release workflow.
- Initial open source project scaffolding for contribution, security, and release process documentation.
- **Query:** `name` and `description` frontmatter fields indexed in the registry; `--search` (CLI) and `search` (MCP) for keyword discovery over those fields (multi-token OR). Schema migration v3 adds columns on existing databases.
