# Skillex Manual Testing Strategy

## 1. Purpose

This document defines the manual testing approach for skillex. It complements the automated acceptance suite by covering what automation cannot: user experience, workflow coherence, error message clarity, documentation accuracy, and real-world integration with agent harnesses.

The automated suite answers "does it work?" This strategy answers "does it work *well*?"

## 2. When to Run Manual Tests

Manual testing happens at three points:

- **Before a release.** Run all user journeys and the exploratory checklist against the release candidate.
- **After major feature work.** When a new command or workflow is added, run the relevant journey plus exploratory testing around the new feature.
- **During dogfooding.** Using skillex on a real project surfaces issues that structured testing misses. Keep a log.

## 3. Test Environment

Manual tests run against real projects, not the golden fixtures. Prepare two environments:

**Environment A: Fresh monorepo.** A new pnpm workspace with 3-4 packages. No skillex configuration. This is the "new user" environment.

**Environment B: Established monorepo.** A real or realistic project with existing dependencies, some of which have skillex exports. This is the "adopter" environment. Ideally, use the skillex repo itself (dogfooding).

Both environments should have at least one agent harness available (Cursor, Claude Code, or similar) for testing the MCP and agent integration journeys.

## 4. User Journeys

Each journey represents a real workflow a user would perform. Test them end-to-end, in order, noting anything that feels wrong — not just things that break.

---

### Journey 1: First-Time Setup

**Persona:** Developer who just heard about skillex and wants to try it.

**Starting state:** Environment A (fresh monorepo, no skillex).

**Steps:**

1. Install skillex (npm wrapper or binary download).
   - Is the installation documented clearly?
   - Does `skillex version` work immediately?

2. Run `skillex init` in the repo root.
   - Does the interactive flow make sense? Are the questions clear?
   - Does it detect the monorepo structure correctly?
   - Does it suggest sensible scope rules?
   - Read the generated `skillex.yaml`. Does it make sense without reading the docs?

3. Read the generated `AGENTS.md` section.
   - Is the MCP-first guidance clear?
   - Would an agent understand how to use skillex from this section alone?

4. Run `skillex doctor`.
   - Does the output make sense for a fresh project?
   - Are the suggestions actionable?

5. Create a skill file manually in `skills/getting-started.md` with frontmatter.
   - Is the expected format documented well enough to write one from memory?
   - What happens if you forget the frontmatter?

6. Create a corresponding `skills/getting-started.test.md`.
   - Is the test format intuitive?
   - Can you write one without constantly referencing the docs?

7. Run `skillex refresh`.
   - Does the output tell you what happened?
   - Run `skillex query --topic getting-started`. Does it return your skill?

8. Run `skillex test validate`.
   - Does it pass? If not, is the error message clear enough to fix without docs?

**What to look for:**
- Time from install to first successful query (target: under 5 minutes).
- Number of times you had to look at docs vs. figure it out from CLI output.
- Any point where you felt confused about what to do next.

---

### Journey 2: Adding Skills to a Package

**Persona:** Library maintainer who wants to export skills for consumers.

**Starting state:** Environment A or B with skillex initialized.

**Steps:**

1. Pick a workspace package. Run `skillex init --package` in its directory.
   - Does it create the right directories?
   - Does it update package.json correctly?

2. Write a public skill in `skillex/public/usage.md` explaining how to use the package.
   - Add frontmatter with topics and tags.

3. Write the corresponding test file.

4. Write a private skill in `skillex/private/dev-setup.md` for contributors.

5. Run `skillex refresh` from the repo root.
   - Does the output mention the new package?

6. Query from a consumer package's path. Verify you see the public skill.

7. Query from inside the package's own source tree. Verify you see the private skill but not the public one.

8. Query from a different package that doesn't depend on this one. Verify you see neither.

**What to look for:**
- Is the public/private distinction intuitive?
- Does the developer understand *why* certain skills appear or don't?
- Is there enough feedback during refresh to know the skills were picked up?

---

### Journey 3: Agent Integration (MCP)

**Persona:** Developer using Cursor (or similar) who wants the agent to use skillex.

**Starting state:** Environment B with skillex initialized, skills populated, registry built.

**Steps:**

1. Configure the MCP server in your harness (e.g., `.cursor/mcp.json`).
   - Was this covered by `skillex init --harness cursor`? If not, is the manual setup documented?

2. Start a new agent session. Check that the agent discovers the MCP server.
   - Does the agent see skillex_query as an available tool?
   - Does the agent see skills as resources?

3. Ask the agent a question that should trigger a skillex query (e.g., "how do I use the @test/ui components?").
   - Does the agent call skillex_query?
   - Is the returned context relevant?
   - Does the agent use the skill content effectively in its response?

4. Ask a question that crosses visibility boundaries (e.g., ask about internal architecture while working in a consumer package).
   - Does the agent correctly receive only public skills?

5. Ask the agent to validate a skill (using the meta-skill if available).
   - Does it understand the .test.md format?
   - Does it run the validation scenarios?
   - Is the report useful?

6. Disconnect the MCP server. Ask the same questions.
   - Does the agent fall back to CLI commands via AGENTS.md guidance?
   - Is the experience degraded but functional?

**What to look for:**
- Does the MCP integration "just work" or does it require fiddling?
- Does the agent make good use of the query parameters (topics, tags, path)?
- Quality of agent responses with vs. without skillex.

---

### Journey 4: Agent Integration (CLI Fallback)

**Persona:** Developer using an agent harness that doesn't support MCP.

**Starting state:** Environment B with skillex initialized, no MCP configured.

**Steps:**

1. Start an agent session. The agent reads AGENTS.md.
   - Does the agent understand the skillex section?
   - Does it know to run `skillex query` commands?

2. Ask a question that should trigger a query.
   - Does the agent construct the right CLI command?
   - Does it parse the output correctly?

3. Ask follow-up questions requiring different query parameters.
   - Does the agent vary its queries (different topics, tags, paths)?
   - Does it use --format summary to decide what to load, then --format content?

**What to look for:**
- Whether the AGENTS.md instructions are sufficient for the agent to self-direct.
- Whether the CLI output format is easy for the agent to parse.
- Common mistakes the agent makes when constructing queries.

---

### Journey 5: Importing External Skills

**Persona:** Developer who found useful skills online and wants to adopt them.

**Starting state:** Environment B with skillex initialized.

**Steps:**

1. Run `skillex get <url>` with a real GitHub URL containing markdown guidance.
   - Does the fetch work?
   - Is the review process clear?
   - Are the flagged items (if any) explained well?

2. Approve the import. Check `skillex/vendor/`.
   - Are the files in the expected location?
   - Does the frontmatter have the source URL?
   - Were test stubs generated?

3. Run `skillex refresh`. Query for the imported skill.
   - Does it appear in results?

4. Run `skillex import` with a local markdown file.
   - Same review process?
   - Correct destination?

5. Run `skillex doctor`. Does it report vendor skill provenance?

**What to look for:**
- Trust in the review process — do you feel confident the skill is safe?
- Clarity of risk assessment output.
- Whether the vendor directory organization makes sense.

---

### Journey 6: CI Integration

**Persona:** Team lead adding skillex checks to the CI pipeline.

**Starting state:** Environment B with skillex populated and tests passing locally.

**Steps:**

1. Add `skillex refresh --check` to the CI pipeline.
   - Run it locally when the registry is fresh. Does it pass?
   - Modify a skill file. Does it fail with a clear message?

2. Add `skillex test validate --check` to the CI pipeline.
   - Remove a test file. Does it fail with a clear message?
   - Is the output CI-friendly (parseable, no ANSI codes with --quiet)?

3. Simulate a PR workflow:
   - Branch, add a new skill, forget the test file, push.
   - Does CI catch it? Is the error message enough to fix without asking someone?

4. Fix the issue, push again. Does CI pass?

**What to look for:**
- Are CI error messages actionable without context?
- Is --check fast enough for CI (under 10 seconds)?
- Does --quiet + --json produce clean, parseable output?

---

### Journey 7: Day-Two Maintenance

**Persona:** Developer returning to the project after weeks away.

**Starting state:** Environment B, stale registry (skills have changed since last refresh).

**Steps:**

1. Pull latest code. Run `skillex refresh --check`.
   - Does it tell you the registry is stale?
   - Is the remedy obvious (run `skillex refresh`)?

2. Run `skillex refresh`. Check the AGENTS.md diff.
   - Is the manifest update sensible?
   - Any surprising additions or removals?

3. Run `skillex doctor`.
   - Does it surface any new issues (skills without tests, missing frontmatter)?
   - Are the suggestions prioritized?

4. A dependency upgraded and changed its skill exports. Run `skillex refresh`.
   - Does the registry reflect the new version's skills?
   - Can you tell what changed?

**What to look for:**
- Whether the tool helps you understand what changed.
- Whether `--dry-run` is useful for previewing changes.
- Whether doctor is a reliable "health check" after time away.

---

## 5. Exploratory Testing Checklist

Run through these scenarios looking for anything unexpected. No specific expected outcome — just exercise the tool and note what feels wrong.

### Error Handling

- [ ] Run `skillex refresh` with no `skillex.yaml` present.
- [ ] Run `skillex query` before any refresh.
- [ ] Run `skillex refresh` with a corrupted `skillex.yaml` (invalid YAML).
- [ ] Run `skillex refresh` with a `skillex.yaml` that references nonexistent skill files.
- [ ] Run `skillex query` with an invalid --path (nonexistent directory).
- [ ] Run `skillex query` with contradictory filters that match nothing.
- [ ] Run `skillex init` when already initialized.
- [ ] Run `skillex init --package` in the repo root (wrong context).
- [ ] Run `skillex test validate` when there are no skill files at all.
- [ ] Run `skillex get` with an invalid URL.
- [ ] Run `skillex get` with a URL that returns non-markdown content.
- [ ] Run `skillex import` with a binary file.
- [ ] Run `skillex mcp` when no registry exists.
- [ ] Kill `skillex refresh` mid-execution. Is the database left in a corrupt state?
- [ ] Run two `skillex refresh` processes simultaneously.

### Output Quality

- [ ] Is every error message actionable? (Does it say what to do, not just what's wrong?)
- [ ] Are warnings distinguishable from errors?
- [ ] Does `--json` output valid JSON in every error case?
- [ ] Does `--quiet` truly suppress all stderr in every command?
- [ ] Does `--help` for every command accurately describe all flags?
- [ ] Are Lipgloss-styled outputs readable on light and dark terminal backgrounds?
- [ ] Do Bubbletea interactive flows work correctly when piped (non-TTY)?

### Boundary Conditions

- [ ] Skill file with only frontmatter, no body content.
- [ ] Skill file that is empty (0 bytes).
- [ ] Skill file with frontmatter containing unusual YAML (nested objects, anchors, multi-line strings).
- [ ] Test file with 0 validation sections.
- [ ] Test file with 50+ validation sections.
- [ ] Package name with unicode characters.
- [ ] Skill file path with spaces.
- [ ] Very deep nesting: `skillex/public/a/b/c/d/skill.md` — does the scanner find it?
- [ ] `skillex.yaml` with 100+ rules — does refresh remain fast?
- [ ] Dependency boundary pointing to a non-package directory.

### Cross-Platform (if testing on multiple OSes)

- [ ] Path separators in query results (forward slash everywhere, even on Windows?).
- [ ] SQLite database portable between OSes (built on Mac, read on Linux?).
- [ ] Bubbletea rendering in Windows Terminal vs CMD vs PowerShell.
- [ ] Line endings in skill files (CRLF vs LF) — does frontmatter parse correctly?

## 6. Dogfooding Protocol

The most valuable manual testing is using skillex on a real project — ideally the skillex project itself. This protocol structures that usage.

### Setup

1. Run `skillex init` in the skillex repo.
2. Create skills for:
   - Contributing to skillex (`skillex/private/contributing.md`)
   - Architecture overview (`skillex/private/architecture.md`)
   - Release process (`skillex/private/release.md`)
   - Using the CLI (`skillex/public/cli-usage.md`)
   - Using the MCP server (`skillex/public/mcp-usage.md`)
3. Write test files for each.
4. Configure MCP in your primary agent harness.

### Daily Use

When working on skillex, pay attention to:

- Does the agent use skillex to answer questions about the project?
- When you ask "how do I add a new command?", does it query the architecture skill?
- When you ask "how do I release?", does it query the release process skill?
- Are the skills actually helping, or are they too vague / too detailed / missing key information?

### Log Template

Keep a running log of issues discovered through dogfooding:

```markdown
## Dogfooding Log

### [Date]

**Scenario:** What I was trying to do.
**Expected:** What I expected to happen.
**Actual:** What actually happened.
**Severity:** Blocking / Annoying / Cosmetic / Idea
**Notes:** Additional context.
```

## 7. Recording Results

### For User Journeys

For each journey, record:

- **Pass / Fail / Partial** — did the journey complete successfully?
- **Time taken** — how long end-to-end?
- **Friction points** — moments of confusion, unclear messages, unexpected behavior.
- **Improvement ideas** — things that could be better even if they technically work.

### For Exploratory Testing

For each checklist item, record:

- **Observed behavior** — what happened.
- **Acceptable?** — yes / no / needs improvement.
- **Issue filed?** — link to Linear issue if applicable.

### For Dogfooding

Use the log template above. Review the log weekly and batch file issues.

## 8. Release Checklist

Before any release, verify:

- [ ] All 7 user journeys completed with no blocking issues.
- [ ] Exploratory checklist fully exercised.
- [ ] No open "blocking" or "annoying" issues from dogfooding log.
- [ ] Automated acceptance suite is green on all platforms.
- [ ] README quickstart tested by someone who hasn't used skillex before.
- [ ] `skillex version` reports the correct version number.
