# Tests: verify-behavior.md

## Validation: verify a reproducible fix

Prompt: The CLI used to exit successfully for an invalid config. I fixed the parser and the package builds. Check whether this is ready to hand back.
Success criteria:
  - Runs the normal CLI entry point with the invalid config
  - Checks the exit status and the error the user sees
  - Reports any behavior it could not verify
