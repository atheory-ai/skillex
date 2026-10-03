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

The workflow verifies the tag, builds canonical archives, signs them, generates an SBOM, attests provenance, uploads assets, and publishes the GitHub Release. Independent downstream jobs publish Homebrew and build/publish the thin npm wrapper after environment approval.

- Keep npm packaging and publication downstream of GitHub Release publication. An npm build or registry failure must not block canonical archives. npm contains only the version-pinned wrapper; never stage or publish copied platform binaries.
- Render the Homebrew formula from the checksums of the exact archives already uploaded to the GitHub release. Never rebuild archives in the Homebrew publication job.
- Use the separate tap token only to check out and push `atheory-ai/homebrew-tap`; the default workflow token cannot write across repositories.
- Keep the minimal GitHub Actions permissions required by each release step, including provenance attestation.
- Keep release-only credentials in GitHub secrets; never put their values in code, skills, logs, or issue text.

## Recover deliberately

- Inspect the failed job and its logs before changing code. A tag run can expose paths ordinary PR CI does not execute.
- If verification fails before publishing, fix the workflow or product defect, increment the patch version, merge it, and tag the new version.
- If npm and the GitHub release have already succeeded, that version is released. Do not create another version or attempt to republish npm solely to repair a downstream Homebrew failure.
- Repair Homebrew with the `Recover Homebrew Publication` workflow against the existing published version. It downloads `checksums.txt`, renders the formula, and idempotently updates the tap without rebuilding release assets.
