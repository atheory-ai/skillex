# Changelog

All notable changes to this project should be documented in this file.

The format is based on Keep a Changelog and the project uses Semantic Versioning.

## [Unreleased]

## [0.6.1]

- Added an npm-facing README and richer package metadata for `@atheory-ai/skillex` so the npm package page explains what Skillex is and how to use it.
- Added a guarded `make release-tag` helper to create and push the `v*` tag that triggers the GitHub Actions release workflow.
- Initial open source project scaffolding for contribution, security, and release process documentation.
- **Query:** `name` and `description` frontmatter fields indexed in the registry; `--search` (CLI) and `search` (MCP) for keyword discovery over those fields (multi-token OR). Schema migration v3 adds columns on existing databases.
