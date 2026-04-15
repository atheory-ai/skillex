# Skillex

Skill management for AI agents in Node.js projects.

Skillex helps agents load the right guidance for the code they are working on without dumping an entire repo's docs into context. It indexes repo skills, package skills, scope rules, and installed package versions, then answers targeted queries in microseconds.

## What Skillex does

- Resolves the right skills for a file path, package, topic, or tag
- Handles monorepos and multiple installed versions of the same package
- Separates public consumer skills from private maintainer skills
- Exposes the index through both MCP and a CLI fallback
- Generates `AGENTS.md` instructions for agents that cannot use MCP directly

## Install

```bash
npm install --save-dev @atheory-ai/skillex
```

The wrapper package installs the correct native binary for your platform through npm `optionalDependencies`.

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

Return full skill content for an agent:

```bash
skillex query --path packages/app-a/** --format content
```

## Example workflow

1. Add repo skills in `skills/` and configure scopes in `skillex.json`
2. Run `skillex refresh` to rebuild `.skillex/index.db`
3. Let your agent query only the skills relevant to its current task

## Why this exists

Without scoped skill retrieval, agents either get too little context or far too much of it. Skillex moves scope resolution into deterministic indexing so the model receives the small, correct slice of guidance for the current path and dependency boundary.

## Repository

- Source: https://github.com/atheory-ai/skillex
- Documentation: https://github.com/atheory-ai/skillex/blob/main/README.md
