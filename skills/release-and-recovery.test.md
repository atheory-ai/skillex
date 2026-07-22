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
