---
name: Skillex packs boundary
description: Work at the boundary between the Skillex engine and the atheory-ai/skillex-packs registry. Use when a change may affect pack manifests, activation, resolver behavior, verification, installation, canonical pack content, registry publication, or cross-repository ownership.
topics: [packs, integration]
tags: [skillex-packs, boundaries, cross-repository]
---

# Skillex Packs Boundary

## Assign ownership correctly

- This repository owns engine behavior: pack discovery, activation, resolution, verification, installation, and consumer-facing CLI/MCP behavior.
- [`atheory-ai/skillex-packs`](https://github.com/atheory-ai/skillex-packs) owns canonical pack content, pack authoring and tests, naming and tiers, registry policy, signing, and pack publication.
- Do not duplicate the registry's authoring or release lifecycle here. Consult its skills and documentation when the work belongs to that repository.

## Coordinate contract changes

- Make a linked change in both repositories when a pack manifest, schema, activation condition, signer or verification contract, or registry-consumer interface changes.
- State the compatibility direction and rollout order in both change descriptions. Keep engine acceptance coverage for the consumer behavior.
- Use this repository's `internal/packs/` and `test/acceptance/pack_test.go` for engine-side behavior; use `skillex-packs` for canonical content and registry validation.

## Choose the right repository

- Work here for an engine defect or capability that affects how packs are detected, resolved, trusted, installed, or exposed to agents.
- Work in `skillex-packs` for creating, editing, testing, naming, signing, or releasing a pack, and for registry governance.
