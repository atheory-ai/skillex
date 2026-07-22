# Tests: packs-boundary.md

## Validation: author a canonical pack

Prompt: I want to add a new canonical framework pack and publish it. Which repository owns that work?
Success criteria:
  - Directs pack content, validation, signing, and publication to skillex-packs
  - Does not duplicate the pack release lifecycle in the engine repository

## Validation: change pack activation

Prompt: A new manifest activation condition needs engine support. How should I divide and coordinate the work?
Success criteria:
  - Assigns engine resolution and acceptance coverage to this repository
  - Assigns manifest or canonical-pack changes to skillex-packs
  - Requires linked changes with compatibility and rollout order
