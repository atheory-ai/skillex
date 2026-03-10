---
topics:
  - architecture
  - internals
---
# UI Package Architecture

The @test/ui package uses a layered architecture with three tiers: primitives, composites, and layouts.

Primitives are unstyled base components. Composites combine primitives with styling. Layouts arrange composites in common patterns.

Internal state management uses a context provider at the application root. Do not access internal contexts directly from consuming packages.
