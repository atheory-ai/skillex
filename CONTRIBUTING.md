# Contributing to Skillex

Thanks for contributing to Skillex.

## Workflow

- Create a short-lived branch from `main`.
- Open a pull request back to `main`.
- Keep changes focused. Separate feature work from release/version bumps unless they belong together.
- Releases are created from tagged commits on `main` by the core maintaners. You cannot create release tags from feature branches.

## Local Setup

Requirements:

- Go 1.23+
- Node.js 22+
- `npm`
- `pnpm`
- `yarn`

Install dependencies and build locally:

```bash
make build
```

## Verification

Before opening a PR, run:

```bash
make verify
make test-acceptance
```

`make verify` checks formatting, runs `go vet`, runs the Go test suite, and builds the CLI.

`make test-acceptance` prepares the fixture repositories under `test/fixtures` and runs the acceptance suite.

If you are changing npm packaging or release-related code, you should also run:

```bash
make npm-pack
```

## Versioning

Versioning and releases are managed by the core maintainers.

## Pull Requests

Please include:

- a clear description of the change
- tests for behavior changes where practical
- documentation updates when commands, workflows, or user-facing behavior changed

If your change affects release packaging, versioning, or installation behavior, call that out explicitly in the PR.

## Code of Conduct

By participating in this project, you agree to follow the rules in [CODE_OF_CONDUCT.md](./CODE_OF_CONDUCT.md).
