# Tests: open-pr.md

## Validation: prepare a pull request after committing

Prompt: Prepare to open a PR for the fix we just committed. The unit tests passed and the integration tests were skipped.
Success criteria:
  - Loads the PR skill through Skillex before preparing the PR
  - Summarizes the change and reports the unit test result
  - Identifies the integration tests as unverified
