# Periodic version checks and agent-visible update notices

Exploration dated 2026-10-22. Based on `main` at
`67324d7374aca404e45fb7bf3007e21398291384`, after `git pull --ff-only`.
This document proposes behavior; it does not implement a checker or updater.

Implementation follow-up: the foreground approach is now implemented; see
[the README](../README.md#update-notices) for its supported behavior and controls.
The explicit check command, doctor diagnostics, and dismissal controls discussed
below remain possible follow-ups. Cross-process scheduling uses OS file locks,
which release automatically when a process exits or crashes.

## Recommendation

Build one Go update-check service shared by the CLI and MCP interface layers.
Keep release metadata in a user cache, refresh successful checks at most once
per 24 hours per release channel, back off failed attempts, and report available
updates independently of that schedule.
An invocation can show an already-known update without making a network call.

For an initial implementation, use a bounded foreground check when the cache
is due: at most 750 ms total for an automatic check. This works for short-lived
CLI commands and can deliver a newly discovered update on the same invocation.
Make the checker replaceable with background refresh later. The main product
decision is whether occasional network activity on a query invocation is
acceptable: today's README explicitly promises no network calls at query time.

Put the notice in the JSON text returned by `skillex_query` and `skillex_read`,
and in CLI JSON responses for those commands. MCP initialization instructions
and human CLI stderr are additional delivery channels. A terminal-only banner
will miss the primary audience.

## What exists today

| Area | Finding and consequence |
| --- | --- |
| Version | `cli/version.go` defaults to `dev`; Makefile and GoReleaser inject a version. Resolve tagged `go install` builds through `runtime/debug.ReadBuildInfo` when the injected value is absent. Otherwise those installations could be mistaken for development builds. |
| CLI | `cli/root.go` has no shared execution hooks. Query and read write their own JSON; an update feature needs both lifecycle integration and response decoration. A post-run hook alone cannot change JSON already written. |
| MCP | `cli/mcp.go` passes the version into `mcp/server.go`. Query/read responses already contain serialized JSON in their first text block. Request callbacks support long-lived skill refreshes and provide an integration point for checking cached update state. |
| SDK | Pinned `mcp-go v0.17.0` supports `server.WithInstructions`, multiple text blocks, and hooks. Its `CallToolResult` has no native `structuredContent` field. An optional field inside the existing JSON text needs no SDK upgrade. |
| Consumers | `test/helpers/mcp_client.go:CallToolText` returns only `Content[0].Text`. A second text block alone would be invisible to that helper; other hosts must be tested rather than assumed to concatenate blocks. |
| Installation | `Install` records global, local dev dependency, or source invocation. It is project configuration, not conclusive evidence of how the running binary was installed. The npm wrapper extracts a temporary executable on every run, so executable-path detection alone is insufficient. |
| Releases | `.github/workflows/release.yml` publishes the canonical GitHub release, then runs npm and Homebrew publication as independent downstream jobs. A GitHub release does not guarantee that either downstream channel is ready. Some release guidance in the repository still describes an older publication order; the workflow is the evidence used here. |
| Offline use | The npm wrapper already honors `SKILLEX_OFFLINE=1` and `SKILLEX_CACHE_DIR`. Extend the offline promise into the Go checker. Keep version-check state out of the deterministic project index. |

## Patterns worth borrowing

- **Codex CLI:** the inspected TUI implementation reads cached metadata, refreshes
  it in the background after 20 hours, and uses the previous cache for the current
  run. It skips source builds, supports configuration opt-out and per-version
  dismissal, and accounts for package-channel availability. This is a useful
  lifecycle pattern, not evidence that its MCP server exposes update messages.
  [Source](https://github.com/openai/codex/blob/main/codex-rs/tui/src/updates.rs),
  [cache](https://github.com/openai/codex/blob/main/codex-rs/tui/src/updates_cache.rs).
- **update-notifier:** defaults to a one-day interval and uses a detached child
  process so checking can survive a short CLI invocation. Its default display
  requires a TTY and it disables checks in CI. Borrow the persisted schedule and
  worker lifecycle, but design delivery for agents explicitly.
  [Documentation](https://github.com/sindresorhus/update-notifier),
  [implementation](https://github.com/sindresorhus/update-notifier/blob/main/update-notifier.js).
- **Claude Code:** documents startup and periodic checks, background installation,
  restart activation, explicit update controls, and last-attempt diagnostics.
  Borrow the visibility and controls; automatic installation is a separate
  feature from the notification requested here. Its documented interval is not
  precise enough to infer a TTL.
  [Documentation](https://code.claude.com/docs/en/setup#auto-updates).
- **Tailwind:** inspecting its current CLI entry point did not establish a
  periodic self-update checker. Do not assign it a checking interval based on
  recollection; the verified implementations above provide firmer comparisons.
  [Inspected entry point](https://github.com/tailwindlabs/tailwindcss/blob/main/packages/@tailwindcss-cli/src/index.ts).

## Two ways to schedule checks

| | A. Bounded foreground check | B. Cached output with background refresh |
| --- | --- | --- |
| Normal invocation | Read local cache; no network while fresh. | Read local cache; no network on the response path. |
| Due or missing cache | One process obtains a lease and makes a request within a short total deadline. Others use cached state. | One process obtains a lease and schedules a worker. The current invocation uses existing cache. |
| First-run notice | Can appear immediately if the request succeeds within the deadline. | Usually appears on a later CLI invocation or MCP call. |
| Short CLI process | Straightforward; process lifetime covers the check. | Requires a detached helper. A Go goroutine dies when the CLI exits. |
| Long MCP process | Check on the next request after the TTL; no periodic traffic while idle. | Background startup check and TTL timer while running; subsequent tool responses see the refreshed state. |
| Main tradeoff | Adds up to 750 ms on a due invocation and changes the network-at-query-time promise. | More process management and eventual delivery; background traffic must also be disclosed. |

**A is the smaller first implementation** if occasional automatic checks on query
invocations are acceptable. Check after a command has parsed and validated its
inputs, before rendering its result. Exclude help, completion, and plain version
printing. Preserve command errors and exit status regardless of checker failure.
For MCP, never put network work ahead of the initialization handshake; a due
check can run on the first tool request and subsequent requests after expiry.

**B is preferable when response latency is the overriding requirement.** A native
helper must run with stdin/stdout/stderr disconnected, a hard timeout, and no
recursive checks. On Windows, account for process creation and executable file
locking. The npm wrapper's temporary executable is cleaned up when the main
child exits: launching a worker from it needs explicit ownership and cleanup,
or a wrapper-managed worker. Do not copy the Codex TUI's in-process background
task into a short-lived Go CLI and expect it to finish.

If the existing no-network-at-query-time contract must be preserved literally,
use a stricter variant: query/read only consume cache; `mcp` startup, `refresh`,
`doctor`, or an explicit check command refresh it. This means CLI-only users who
never run those commands will not discover updates automatically. That is a
real coverage tradeoff, not equivalent behavior.

## Delivering the notice to the agent

### Primary: optional data in existing tool output

Add a small optional `notices` array to the existing top-level query/read JSON,
without wrapping or renaming the current response fields. For example, with
hypothetical versions and a confirmed pnpm local installation:

```json
{
  "type": "results",
  "results": [],
  "notices": [{
    "code": "skillex_update_available",
    "current_version": "0.11.0",
    "latest_version": "0.12.0",
    "checked_at": "2026-10-22T12:00:00Z",
    "message": "Skillex 0.12.0 is available; running 0.11.0. Suggest updating this project's pinned dev dependency when appropriate.",
    "update_command": "pnpm add -D @atheory-ai/skillex@0.12.0",
    "restart_required": true
  }]
}
```

Include a compact notice on every discovery query while an update is known to
be available. Discovery is where new tasks usually enter; once per server
lifetime can miss later tasks that reuse the same MCP process. For read-first
and broker-first callers, include it on the first eligible response per MCP
session and again after 24 hours or a newly discovered version. Do not mark an
update notice as a tool error. Preserve actual errors and never replace them
with an update message.

Do not globally consume a cached notice when one process displays it: another
repository or agent still needs to see it. Network throttling is shared;
presentation suppression is local to the relevant process/session. A deliberate
user dismissal can target a specific version.

Keep the notice under a fixed byte budget, such as 1 KiB, outside retrieved skill
content. Retrieval bytes, content hashes, refs, cursors, and ranking must remain
unchanged. Include the notice budget in the documented total response bound.

The SDK currently serializes JSON inside `content[0].text`; that is sufficient.
Native `structuredContent` can be added in a separate SDK upgrade while retaining
the JSON text for compatibility. The protocol documents both forms.
[Tool result specification](https://modelcontextprotocol.io/specification/2025-06-18/server/tools#structured-content).

### Secondary: initialization instructions and human output

- Add a short cached update notice to MCP `initialize.result.instructions` using
  the existing SDK support. Keep `serverInfo.version` truthful. Initialization
  is useful for early exposure, but host behavior determines whether instructions
  reach the model, and the text will become stale in a long-running server.
- Print one line on ordinary CLI stderr when a known update exists, even without
  a TTY. Honor `--quiet`. Keep `--json` stdout as one valid JSON document and
  include the structured notice there so stdout-only agents can see it.
- Raw `mcp` stdout must remain exclusively JSON-RPC. Stderr and logging
  notifications alone are insufficient: clients may ignore them.
  [Initialization](https://modelcontextprotocol.io/specification/2025-06-18/basic/lifecycle#initialization),
  [stdio rules](https://modelcontextprotocol.io/specification/2025-06-18/basic/transports#stdio).

An alternative is to preserve the first JSON block exactly and append a second
text block. This avoids adding a JSON field, but requires host compatibility
tests and changes to the current test helper. It is a weaker default for this
repository. A dedicated status tool or resource is useful for diagnostics, but
cannot be the only notice mechanism because the agent may never request it.

No protocol field guarantees that an agent will act on the notice. The goal is
to place clear, actionable data in the normal model-visible result and verify
that the intended harnesses actually expose it.

## Cache, controls, and release discovery

- Use a versioned JSON cache below `SKILLEX_CACHE_DIR` when set, otherwise
  `os.UserCacheDir()/skillex`; keep it outside `.skillex/index.db`. Partition by
  release source/channel, not by project or installed version. Compare cached
  latest metadata against the running version on every invocation.
- Store `latest_version`, `last_success_at`, `last_attempt_at`, `next_check_at`,
  and optional ETag. Default to 24 hours after success; respect server retry
  hints and use bounded failure backoff, initially one hour. Record failed
  attempts so offline/rate-limited runs do not retry on every invocation.
- Use an atomic, cross-process lease before any automatic request; do not wait
  for another process's lease. Expire abandoned leases. Use a temporary file and
  atomic replacement for cache updates. With an unwritable cache/lease location,
  skip automatic network checks rather than issuing uncached requests every run.
- Bound cache and HTTP response sizes, validate timestamps and semantic versions,
  and handle corrupt state and clock rollback without retry loops. Stop showing
  unrefreshed notices after a proposed seven-day stale window. Use semantic
  comparison, including `v` normalization and prereleases; `golang.org/x/mod`
  is already a direct dependency. Skip unknown, source, and `-dev` builds by
  default, after resolving tagged Go-install build information.
- Start with GitHub's public `/repos/atheory-ai/skillex/releases/latest` endpoint
  for standalone binaries. It excludes drafts and prereleases. Use a short total
  deadline, bounded redirects, and conditional requests when supported. Do not
  require a GitHub token. Failure is silent during normal work; an explicit
  diagnostic can report it.
  [GitHub release API](https://docs.github.com/en/rest/releases/releases#get-the-latest-release).
- For npm and Homebrew, determine availability from that installation's channel,
  or verify its availability before suggesting a specific target version. For
  the custom Homebrew tap, inspect the published tap metadata; the core-formula
  API may not cover it. Cache each source separately. A release-generated static
  manifest could later reduce endpoint complexity, but must account for these
  independently completing publication jobs.
- Honor `SKILLEX_OFFLINE=1` for all automatic/manual checker network paths. Add
  `SKILLEX_NO_UPDATE_CHECK=1` for automatic checks and notices; optionally honor
  the common `NO_UPDATE_NOTIFIER` convention. Disable automatic checks in CI and
  tests, with an explicit opt-in for testing. `--quiet` only controls human
  output, leaving JSON notice data available. Document automatic metadata traffic
  separately from telemetry; no project names, paths, or skill content are sent.
- Add a proposed `skillex version --check` for an explicit bounded refresh and
  `doctor` fields for last attempt/success, next scheduled check, source, and
  disabled reason. A manual check can bypass TTL, but must still respect offline
  mode and have a finite timeout. These commands do not exist yet.

## Make upgrade advice match the installation

Use `Install` as context, supplemented by actual invocation provenance. Have the
npm wrapper pass an installation hint to Go, because its executable lives in a
temporary directory. Use `debug.ReadBuildInfo` for tagged Go installations. For
ambiguous global installations, link to upgrade instructions rather than guess.

| Confirmed installation | Suggested action |
| --- | --- |
| Local npm / pnpm / Yarn | Offer the corresponding dev-dependency update to the available version. This is a project dependency and lockfile change; a global install will not update what the agent runs. |
| Homebrew tap | Offer `brew upgrade atheory-ai/tap/skillex` once that tap has the release. |
| Tagged Go install | Offer `go install github.com/atheory-ai/skillex/cmd/skillex@v<available-version>`. |
| Installer-managed global binary | Point to installer/upgrade guidance, preserving any custom install directory. |
| Source / development checkout | Suppress automatic release notices by default; rebuilding the checkout is a different workflow. |
| Unknown global binary | Provide the release link and ask the user to use their installation method. |

Generate commands from local known templates and validated versions. Do not
turn fetched release prose into agent instructions. The notice should suggest
the next action without granting authority to modify dependencies. Updating a
binary also does not update an already-running MCP process: tell the agent/user
to restart or reconnect after the upgrade.

## Implementation and validation outline

1. Add `internal/updatecheck` for version resolution, release sources, scheduling,
   cache/lease handling, and compact notices. Inject clock, transport, and cache
   directory so tests use no public network.
2. Add CLI lifecycle integration and response decoration in `cli/`; pass a shared
   notice provider into `mcp/`. Keep network/cache state out of the query engine.
   Cover query results, vocabulary, no-match, and read responses, plus broker
   entry points and resource-only callers where supported.
3. Add initialization instructions, human stderr, and optional JSON notices;
   define diagnostics and controls. Update the README network promise according
   to the chosen scheduling policy. Update generated agent guidance to explain
   that notices are advisory and pinned installations need project updates.
4. Test fresh/missing/expired/corrupt cache, simultaneous processes, crash recovery,
   timeout/offline/rate-limit failure, unwritable cache, version normalization,
   tagged Go builds, channel lag, and suppression after upgrading. Prove failure
   never changes the original command outcome.
5. Add CLI/MCP acceptance and golden coverage with a seeded cache and local HTTP
   fixture. Assert valid JSON stdout, clean MCP framing, a notice in the first
   text block, unchanged retrieval data/budgets, and discovery notices visible
   across tasks sharing one server. Check actual Codex and Claude Code harness
   model context, rather than only terminal display. Add latency measurements
   for fresh-cache runs and the due-check deadline.

Exploration verification: rebuilt the development binary, exercised bounded
skill discovery/read, inspected the pinned SDK and release workflow, and probed
the current MCP initialize/query exchange. No runtime implementation was changed;
future host-visible notice behavior and checker performance remain to be tested
when implemented.
