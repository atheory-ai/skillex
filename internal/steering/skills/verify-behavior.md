# Verify behavior

Choose a check that exercises the same entry point and inputs a user or caller would use. For a bug fix, capture the original failure on those inputs when practical, then run the check again against the fix. For a new feature, check one representative use and any failure path the change introduces.

Reuse an existing test or run the actual command or feature. A build is useful evidence for compilation. If you cannot exercise the behavior, state which check you ran and what is still unverified.
