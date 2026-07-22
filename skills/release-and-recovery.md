---
name: Release and recovery
description: Prepare, tag, monitor, or recover a Skillex release. Use when changing VERSION, release.yml, GoReleaser or npm packaging, GitHub release assets, provenance, npm publishing, Homebrew publication, or diagnosing a tag-triggered release failure.
topics: [releases, release-recovery]
tags: [versioning, github-actions, publishing]
---

# Release and Recovery

## Prepare and tag

- Treat the root `VERSION` file as the release version source of truth. Include its changelog entry in the release change.
- Merge the release commit to `main`, start from a clean checkout that matches `origin/main`, and run `make release-tag`.
- Let the guarded target create the matching immutable `v<version>` tag. Do not create or move a release tag manually.
- Run the relevant local gate before tagging. The tag workflow is the authoritative release gate and also exercises release-only packaging and publishing paths.

## Understand the publication order

The workflow verifies the tag, builds archives, signs them, generates an SBOM, attests provenance, uploads release assets, packages npm tarballs, publishes npm after environment approval, publishes the GitHub release, then publishes Homebrew.

- Preserve that ordering: npm packaging recreates `dist/`, so archive assets must be captured before npm packaging.
- Keep the minimal GitHub Actions permissions required by each release step, including provenance attestation.
- Keep release-only credentials in GitHub secrets; never put their values in code, skills, logs, or issue text.

## Recover deliberately

- Inspect the failed job and its logs before changing code. A tag run can expose paths ordinary PR CI does not execute.
- If verification fails before publishing, fix the workflow or product defect, increment the patch version, merge it, and tag the new version.
- If npm and the GitHub release have already succeeded, that version is released. Do not create another version or attempt to republish npm solely to repair a downstream Homebrew failure.
- Repair a downstream publication with a purpose-built recovery path against the existing tag. Confirm the required token and current tool invocation before retrying.
