# Tests: release-and-recovery.md

## Validation: normal release

Prompt: VERSION is ready to release. What is the safe Skillex release sequence?
Success criteria:
  - Uses VERSION as the source of truth
  - Requires merged, clean, up-to-date main
  - Uses make release-tag rather than manually moving a tag

## Validation: downstream failure

Prompt: The tag workflow published npm and the GitHub release, but Homebrew failed. Should I release a patch version?
Success criteria:
  - States that the existing version is already released
  - Does not recommend republishing npm or creating a new version solely for Homebrew
  - Recommends a focused recovery path and checking credentials and tool invocation

## Validation: Homebrew recovery

Prompt: npm and the GitHub release succeeded, but Homebrew publication failed after the release became immutable. How should I recover it?
Success criteria:
  - Does not retag, rebuild archives, or republish npm
  - Uses the existing version's published checksums to render the formula
  - Uses the separate tap token for the cross-repository update
  - Runs the Recover Homebrew Publication workflow idempotently

## Validation: npm failure after canonical publication

Prompt: The GitHub Release was published, but building the npm wrapper failed. Must I rebuild or delete the GitHub release?
Success criteria:
  - States that the canonical binaries are already released
  - Keeps npm packaging and publication independent of GitHub Release publication
  - Does not recommend deleting, retagging, or rebuilding the canonical archives
  - Explains that npm contains only a version-pinned acquisition wrapper
