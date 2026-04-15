# Releasing Skillex

This document is for maintainers.

## Release Model

- `main` is the release branch.
- Releases are created from a tagged commit on `main`.
- The tag must match the root `VERSION` file exactly, for example `v0.6.1`.
- GitHub Actions performs the verified build and publishes to npm after approval through the `npm-release` environment.

## Release Steps

1. Make sure the intended release changes are merged to `main`.
2. Update `VERSION`.
3. Update `CHANGELOG.md`.
4. Open and merge the release PR.
5. Pull the merged `main` branch locally.
6. Create and push the release tag:

```bash
git checkout main
git pull --ff-only
make release-tag
```

7. Wait for the `Release` workflow to complete verification.
8. Approve the `npm-release` environment when prompted.
9. Confirm the npm packages were published successfully.
10. Create GitHub release notes if needed.

`make release-tag` is the intended path. It refuses to run unless:

- the current branch is `main`
- the worktree is clean
- `HEAD` matches `origin/main`
- the target tag does not already exist locally or on `origin`

The GitHub release workflow starts only when the `v*` tag is pushed.

## Local Packaging Check

If you need to inspect release tarballs before tagging:

```bash
make npm-pack
```

This is for inspection only. The normal release path is GitHub Actions, not local `npm publish`.
