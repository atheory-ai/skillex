---
topics:
  - workflow
  - testing
---
# Development Workflow for @test/ui

To develop @test/ui locally, use the workspace dev server:

```bash
pnpm --filter @test/ui dev
```

Run tests with:
```bash
pnpm --filter @test/ui test
```

The Storybook environment is available at `http://localhost:6006` when running the dev server.
