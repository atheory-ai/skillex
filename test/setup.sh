#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "$SCRIPT_DIR/.." && pwd)"
GOLDEN_DIR="$SCRIPT_DIR/golden"
FIXTURES_DIR="$SCRIPT_DIR/fixtures"

# ── flags ─────────────────────────────────────────────────────────────────────
PERF=false
CLEAN=false
for arg in "$@"; do
  case "$arg" in
    --perf)  PERF=true ;;
    --clean) CLEAN=true ;;
  esac
done

# ── clean ─────────────────────────────────────────────────────────────────────
if $CLEAN; then
  echo "Removing $FIXTURES_DIR"
  rm -rf "$FIXTURES_DIR"
  exit 0
fi

# ── prerequisites ──────────────────────────────────────────────────────────────
check_prereq() {
  if ! command -v "$1" &>/dev/null; then
    echo "ERROR: '$1' is required but not found on PATH." >&2
    exit 1
  fi
}

check_prereq node
check_prereq pnpm
check_prereq yarn
check_prereq npm

# ── helpers ────────────────────────────────────────────────────────────────────
copy_golden() {
  local name="$1"
  local src="$GOLDEN_DIR/$name"
  local dst="$FIXTURES_DIR/$name"
  echo "Setting up fixture: $name"
  rm -rf "$dst"
  cp -r "$src" "$dst"
}

# ── standard fixtures ─────────────────────────────────────────────────────────
rm -rf "$FIXTURES_DIR"
mkdir -p "$FIXTURES_DIR"

# Fixture A: pnpm monorepo
copy_golden monorepo-pnpm
(
  cd "$FIXTURES_DIR/monorepo-pnpm"
  pnpm install --silent 2>/dev/null || pnpm install
)

# Fixture B: yarn monorepo
copy_golden monorepo-yarn
(
  cd "$FIXTURES_DIR/monorepo-yarn"
  yarn install --silent 2>/dev/null || yarn install
)

# Fixture C: npm monorepo
copy_golden monorepo-npm
(
  cd "$FIXTURES_DIR/monorepo-npm"
  npm install --silent 2>/dev/null || npm install
)

# Fixture D: single package (simulated deps, no package manager install)
copy_golden single-package
(
  dst="$FIXTURES_DIR/single-package"
  # Place the simulated external package in node_modules
  mkdir -p "$dst/node_modules"
  cp -r "$dst/_simulated_deps/external-with-skillex" "$dst/node_modules/"
  rm -rf "$dst/_simulated_deps"
)

echo "Standard fixtures ready in $FIXTURES_DIR"

# ── performance fixture ────────────────────────────────────────────────────────
if $PERF; then
  echo "Generating performance fixture..."
  PERF_DIR="$FIXTURES_DIR/perf"
  rm -rf "$PERF_DIR"
  mkdir -p "$PERF_DIR"

  # Generate performance fixture via Go helper
  # (or inline generation here)
  cat > "$PERF_DIR/package.json" <<'EOF'
{
  "name": "perf-fixture",
  "private": true,
  "version": "1.0.0"
}
EOF
  cat > "$PERF_DIR/skillex.yaml" <<'EOF'
Version: 4
Rules:
  - Scope: "**"
    Skills:
      - skills/repo.md
EOF
  mkdir -p "$PERF_DIR/skills"
  echo "# Repo" > "$PERF_DIR/skills/repo.md"
  cat > "$PERF_DIR/skills/repo.test.md" <<'EOF'
# Tests: repo.md

## Validation: Basic

Prompt: Test
Success criteria:
  - Works
EOF

  TOPICS=(api auth billing caching config data events http infra jobs logging metrics queue routing security sessions storage tasks testing webhooks auth2 billing2 caching2 config2 data2 events2 http2 infra2 jobs2 logging2 metrics2 queue2 routing2 security2 sessions2 storage2 tasks2 testing2 webhooks2 analytics auditing batching circuit-breaker compression database discovery encoding filtering indexing)
  TAGS=(v1 v2 v3 stable beta deprecated internal external breaking-change experimental legacy production preview alpha)

  # 100 packages, 50 skills each
  mkdir -p "$PERF_DIR/node_modules"
  for i in $(seq 1 100); do
    pkg="mock-pkg-$i"
    pkgdir="$PERF_DIR/node_modules/$pkg"
    mkdir -p "$pkgdir/skillex/public" "$pkgdir/skillex/private"
    cat > "$pkgdir/package.json" <<EOF
{"name":"$pkg","version":"1.0.0","skillex":true}
EOF
    for j in $(seq 1 25); do
      t1="${TOPICS[$((RANDOM % ${#TOPICS[@]}))]}"
      t2="${TOPICS[$((RANDOM % ${#TOPICS[@]}))]}"
      tag="${TAGS[$((RANDOM % ${#TAGS[@]}))]}"
      cat > "$pkgdir/skillex/public/skill-$j.md" <<EOF
---
topics: [$t1, $t2]
tags: [$tag]
---
# Skill $j from $pkg

Public skill content for $pkg skill $j.
EOF
      cat > "$pkgdir/skillex/public/skill-$j.test.md" <<EOF
# Tests: skill-$j.md

## Validation: Basic

Prompt: How does skill $j work?
Success criteria:
  - Provides useful information
EOF
      cat > "$pkgdir/skillex/private/skill-$j.md" <<EOF
---
topics: [$t1]
tags: [$tag]
---
# Private Skill $j from $pkg

Private skill content for $pkg skill $j.
EOF
      cat > "$pkgdir/skillex/private/skill-$j.test.md" <<EOF
# Tests: skill-$j.md

## Validation: Basic

Prompt: Internal details for skill $j?
Success criteria:
  - Provides internal details
EOF
    done
  done

  # 10 app packages
  for i in $(seq 1 10); do
    appdir="$PERF_DIR/packages/pkg-$i"
    mkdir -p "$appdir/src"
    echo "export {};" > "$appdir/src/index.ts"
    # Each app depends on 30 random packages
    deps=""
    for d in $(shuf -n 30 -e $(seq 1 100)  2>/dev/null || seq 1 30); do
      deps="$deps\"mock-pkg-$d\":\"1.0.0\","
    done
    cat > "$appdir/package.json" <<EOF
{"name":"@perf/app-$i","version":"1.0.0","private":true,"dependencies":{${deps%,}}}
EOF
  done

  # Build pnpm-workspace.yaml
  cat > "$PERF_DIR/pnpm-workspace.yaml" <<'EOF'
packages:
  - 'packages/*'
EOF

  # Add rules for each app
  {
    echo "Version: 4"
    echo ""
    echo "Rules:"
    echo "  - Scope: \"**\""
    echo "    Skills:"
    echo "      - skills/repo.md"
    for i in $(seq 1 10); do
      echo "  - Scope: \"packages/pkg-$i/**\""
      echo "    DependencyBoundary: packages/pkg-$i"
    done
  } > "$PERF_DIR/skillex.yaml"

  echo "Performance fixture ready."
fi

echo "Done."
