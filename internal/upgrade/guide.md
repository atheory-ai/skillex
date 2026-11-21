# Upgrading an existing Skillex project

Record the installed version before updating. Read each entry newer than that
version through the target, in order. Check all prerequisites before installing
the target once using the project's existing installation method. Use the target
executable for subsequent commands. A single initialization or refresh with
that executable can satisfy multiple entries. Reconnect any running Skillex MCP
server, confirm the installed version, and inspect the project diff. Preserve
the project's choices for optional features.

## 0.6.1

No project changes required.

## 0.6.2

No project changes required.

## 0.6.3

No project changes required.

## 0.6.4

No project changes required.

## 0.7.0

No project changes required. Optional skill packs can be indexed with
`skillex refresh` if the project adopts them.

## 0.7.1

Projects using the Claude Code MCP setup should check the project-root
`.mcp.json` entry and reconnect Claude Code to the project server.

## 0.7.2

Run `skillex refresh` to rebuild the registry with corrected skill counts.

## 0.8.0

Update scripts consuming query output to use bounded discovery followed by
`skillex read --ref <ref>`. Refresh the registry and verify a query and
selected read against the project.

## 0.8.1

No project changes required.

## 0.8.2

No project changes required.

## 0.8.3

Review agent instructions that request bulk skill content. Use bounded
discovery and selected reads.

## 0.9.0

No project changes required for skills-only configurations. The experimental
downstream MCP broker is a separate opt-in.

## 0.9.1

Run the project's `skillex init --yes` to regenerate its managed agent
instructions. Review the diff. From this release onward, `refresh` only
rebuilds the index.

## 0.10.0

Before installing the npm wrapper, ensure Node.js 18+ and system tar are
available. For an offline deployment, seed the target archive and checksum
cache before first use. Run the project's Skillex version command to verify
the wrapper can resolve the binary.

## 0.11.0

Before building from source, check the target release's Go toolchain
requirements. Run `skillex refresh` to index supported in-repo agent skill
directories and verify a relevant path query.

## 0.11.1

Use the newly built binary for the security fixes. Verify the version and
reconnect any long-running MCP server.

## 0.12.0

Run the upgraded project's `skillex init --yes` to regenerate the managed
bootstrap and refresh the bundled catalog. Review the diff, including any
starter skill created by init. Reconnect MCP and run an unfiltered
`skillex query` or `skillex_query` to verify setup.

Optional features include task bindings and the `minimal-comments`,
`verify-behavior`, and `recheck-assumptions` presets. Keep existing choices
unless the project owner requests a change.

## 0.12.1

**Before installing from 0.12.0**, open the target release using the
`release_url` in the `skillex_update_available` notice. That client predates
upgrade-plan refs. Tell the user this patch adds version-specific upgrade
guidance and offer to update Skillex. Once approved, use the notice's
`update_command` if present, or the project's existing installation method.
For a pinned project, update the dependency or installer pin to 0.12.1.

**After installing**, use the project's Skillex executable to run
`skillex version` and confirm it reports `0.12.1`. Reconnect any running
Skillex MCP server. Run the upgraded project’s `skillex doctor` and follow
its setup diagnostic if the managed block is missing or stale. To verify the
upgrade plan, run
`skillex read --ref upgrade:0.12.0:0.12.1` or call `skillex_read` with
`{"ref":"upgrade:0.12.0:0.12.1"}` after reconnecting. The plan should contain
only the 0.12.1 entry.

A project already set up for 0.12.0 needs no changes to its managed
`AGENTS.md` block, task bindings, or preset selections for this patch.

## 0.13.0

Update the CLI or project dependency to 0.13.0 using the existing installation
method. No project changes required. Verify with the project's `skillex version`
and reconnect running Skillex MCP sessions to load the new pack discovery tools.

Optional for projects using registry packs: run `skillex pack list --json` to
inspect installed versions and their saved evidence. Run `skillex pack check`
to fetch signed revocations and refresh the index. To review available upgrades,
run `skillex pack update --preview --json` before approving any changes.
