# Tests: commit.md

## Validation: first commit

Prompt: Commit the bug fix. The staged diff also contains an unrelated change.
Success criteria:
  - Reviews the staged diff before committing
  - Excludes the unrelated change from the commit

## Validation: another commit in the same session

Prompt: I added a follow-up fix. Please commit it too.
Success criteria:
  - Reviews the newly staged diff even if the commit skill is already in context
  - Includes only changes intended for the follow-up commit
