# Pack registry federation

Design proposal for [engine issue #93](https://github.com/atheory-ai/skillex/issues/93). Skillex 0.15.0 accepts one registry URL with bundled trust. This proposal extends the publisher's [registry precedence and handle ownership model](https://github.com/atheory-ai/skillex-packs/blob/6d7f915d13eb1433e02aac4661e9bd0b11360d0b/.specs/06-registries-and-federation.md).

## When two registries offer the same name

For a fresh install, visit the configured registries in order. Authenticate the catalog and check that its signer is authorized for its declared registry identity. Within each trusted source, apply the global and source-specific filters to individual releases, then select the newest eligible stable version using the existing Skillex compatibility rules. The first source with an eligible release wins. For example, if the preferred mirror offers compatible 1.4.0 and upstream offers compatible 2.0.0, select 1.4.0. If 1.4.0 requires a newer engine and the mirror has no eligible older release, try upstream.

Global restrictions apply at every source. A source can further restrict tiers, licenses, or scopes. Use the handle's authorized tier, the release's authenticated license metadata and its qualified pack name for these checks. A release missing a license cannot satisfy a license allowlist. An unknown policy key or ambiguous trust binding is a configuration error before fetching.

An unavailable source, an unauthenticated catalog or a candidate with invalid producer evidence can be skipped. Identify the rejected source and reason in the result, even when a later source succeeds. Preserve the withdrawal rules across endpoints representing the same publisher. A stale mirror cannot supply a version already known to be revoked, and a known withdrawal of the latest release still blocks fallback within that publisher. An authority's signed withdrawal applies only to packs it is authorized to publish. On a first lookup the client can only enforce withdrawals it has observed.

## Who may sign

A source URL identifies a transport. Evaluate two independent authorizations:

- The catalog must be signed by the workflow authorized to publish that logical registry. A mirror serving identical signed catalog bytes can use a different URL.
- The pack handle determines the allowed archive signer and build provenance. Pin the issuer, trust root, producer repository and release workflow. Bind the workflow to the exact tag for the selected name and version. Preserve verification of archive size, digest and attested subject before extraction.

Keep the bundled authority for the reserved `atheory-ai` handle. For existing community packs the bundled registry's release workflow signs the published archives too. By default, that workflow remains the only authorized producer for unassigned handles. An operator can explicitly bind a different, nonreserved handle to a private producer in user- or administrator-managed trust settings outside the checkout. Project configuration may select a previously authorized registry and narrow its policy. A project-supplied path, catalog entry or mirror cannot grant signing authority. Reject conflicting explicit bindings and require a separate trust decision to change an existing binding.

A mirror can serve an upstream release under the original signature. A separately signed index that republishes upstream entries also needs verifiable upstream catalog evidence covering metadata used for selection, including compatibility, license, rename declarations and pins. The archive signature alone authenticates the archive bytes. Start with mirrors of original signed catalogs and defer re-signed composite indexes until that metadata proof is specified.

## Review and installed state

Search can report a catalog-authenticated candidate without fetching its archive. CLI and MCP previews must run the same resolution and producer verification. Show the selected endpoint, logical registry, publisher identity, pack name, version and digest. Bind approval to the reviewed selection. If the source or evidence changes before apply, return a fresh preview.

Record logical registry and producer identity in new installation evidence. On update or withdrawal checks, a different endpoint can serve evidence from that same authority. Reordering endpoints cannot silently transfer an installed pack to a new publisher. Preserve explicit consent for each rename. Existing locks can recover the bundled authority from their verified signed catalog and retain offline behavior.

## First implementation and golden cases

Start with two endpoints serving bundled-authority catalogs. This exercises ordered lookup, source diagnostics and CLI/MCP parity before introducing custom roots. Follow with out-of-band handle bindings and per-release policy filters. Keep unsupported configuration rejected until its behavior is implemented.

| Signed fixture | Expected result |
|---|---|
| Both sources offer compatible versions of the same name | Select the preferred source's highest eligible version regardless of the other source's version. |
| First source has no eligible release | Select the second source and identify it in both previews. |
| A mirror claims the reserved handle with a different signer | Reject it, report the reason, and accept only an independently verified eligible source. |
| A trusted source offers an explicitly bound private handle | Require its configured signer and exact release-tag provenance. |
| Global policy excludes all candidates, or a saved withdrawal is replayed through a stale mirror | Reject the excluded release before project writes. |
| An endpoint changes after preview, or an installed identity would move to another publisher | Require review of the changed selection and preserve the existing installation. |
