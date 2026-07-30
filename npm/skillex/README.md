# Skillex

Skill management for AI agents in polyglot projects.

Skillex helps agents load the right guidance for the code they are working on without dumping an entire repo's docs into context. It indexes repo skills, package skills, packs, scope rules, and installed package versions, then answers targeted queries in microseconds.

## What Skillex does

- Resolves the right skills for a file path, package, topic, or tag
- Handles monorepos and multiple installed versions of the same package
- Separates public consumer skills from private maintainer skills
- Activates pack-shipped skills from project files, dependencies, and detectors
- Lets packages and Go modules ship `skillex/pack.yaml` with their code
- Exposes the index through both MCP and a CLI fallback
- Generates `AGENTS.md` instructions for agents that cannot use MCP directly

## Recommended install

For cross-ecosystem use, install Skillex as a global platform utility. See the
main project README for the universal installer.

## Node dev dependency

Use this npm package when a Node.js project wants Skillex pinned as local dev
tooling:

```bash
npm install --save-dev @atheory-ai/skillex
# or
pnpm add -D @atheory-ai/skillex
# or
yarn add -D @atheory-ai/skillex
```

The wrapper package installs the correct native binary for your platform through
npm `optionalDependencies`.

## Quick start

Initialize Skillex in your repository:

```bash
skillex init
```

Rebuild the local skill index:

```bash
skillex refresh
```

Query the skills that apply to a file:

```bash
skillex query --path packages/app-a/src/auth.ts
```

Query by topic, tag, or package:

```bash
skillex query --topic auth
skillex query --tags migration,breaking-change
skillex query --package @acme/foo
```

Use a selected result to read only the guidance needed:

```bash
skillex query --path packages/app-a/src/auth.ts --search "session handling"
skillex read --ref <ref-from-query> --section <optional-section-id>
```

`query` returns bounded discovery summaries. Narrow broad results before reading a
selected skill or section. `--format content` is retained for backward compatibility,
but is not the recommended agent workflow.

## Example workflow

1. Add repo skills in `skills/` and configure scopes in `skillex.json`
2. Run `skillex refresh` after changing skills, configuration, or installed dependencies
3. Let your agent query, narrow, and read only the skills relevant to its current task

## Packs

Packs are Skillex's ecosystem extension mechanism. A pack bundles skill files
with activation rules and optional detectors:

```text
skillex/
  pack.yaml
  usage.md
```

```yaml
name: my-framework
version: 1.0.0
detectors:
  my-framework:
    matches:
      - dependency:
          source: npm-package
          name: my-framework
skills:
  - file: usage.md
    activate-when:
      detector: my-framework
    scope: boundary
```

Projects can commit packs locally, and libraries can ship packs with their
package/module source. Go modules are supported through `go.mod`, local
`replace`, and `vendor` module roots; Skillex does not download dependencies
during refresh or query.

## Why this exists

Without scoped skill retrieval, agents either get too little context or far too much of it. Skillex moves scope resolution into deterministic indexing so the model receives the small, correct slice of guidance for the current path and dependency boundary.

## Repository

- Source: https://github.com/atheory-ai/skillex
- Documentation: https://github.com/atheory-ai/skillex/blob/main/README.md
