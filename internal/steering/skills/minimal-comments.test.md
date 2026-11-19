# Tests: minimal-comments.md

## Validation: review comments in a changed file

Prompt: Review three new comments in this diff. One narrates a simple loop, one documents a public API, and one explains why an old data format still has to be read.
Success criteria:
  - Removes or flags the comment that repeats the loop
  - Preserves required API documentation
  - Preserves the explanation of the compatibility constraint
