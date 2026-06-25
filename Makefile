BASE_VERSION := $(shell cat VERSION)
VERSION ?= $(BASE_VERSION)-dev
PACKAGE_VERSION ?= $(BASE_VERSION)
LDFLAGS := -ldflags "-X github.com/atheory-ai/skillex/cli.Version=$(VERSION)"
GO ?= go
GORELEASER ?= goreleaser

UNIT_PACKAGES = $(shell $(GO) list ./... | grep -v '/test/acceptance$$')

.PHONY: build install test test-unit test-race \
        fmt fmt-check vet lint vuln \
        verify verify-unit \
        dist release-snapshot \
        npm-stage npm-pack npm-publish version-sync \
        refresh doctor \
        release-tag clean \
        test-setup test-acceptance test-perf test-clean \
        help

# ── Build ─────────────────────────────────────────────────────────────

build:
	$(GO) build $(LDFLAGS) -o skillex ./cmd/skillex

install:
	$(GO) install $(LDFLAGS) ./cmd/skillex

# ── Test ──────────────────────────────────────────────────────────────

test:
	$(GO) test $(UNIT_PACKAGES)

test-unit:
	$(GO) test $(UNIT_PACKAGES)

test-race:
	$(GO) test -race $(UNIT_PACKAGES)

# ── Lint / static analysis ────────────────────────────────────────────

fmt:
	@files=$$(git ls-files '*.go'); \
	if [ -n "$$files" ]; then gofmt -w $$files; fi

fmt-check:
	@files=$$(git ls-files '*.go'); \
	if [ -z "$$files" ]; then exit 0; fi; \
	unformatted=$$(gofmt -l $$files); \
	if [ -n "$$unformatted" ]; then \
		echo "These files need gofmt:"; \
		echo "$$unformatted"; \
		exit 1; \
	fi

vet:
	$(GO) vet ./...

# Requires .golangci.yml at repo root (from release-template).
lint:
	@command -v golangci-lint >/dev/null 2>&1 || { \
		echo "golangci-lint not installed; see https://golangci-lint.run/usage/install/"; exit 1; }
	golangci-lint run

vuln:
	@command -v govulncheck >/dev/null 2>&1 || $(GO) install golang.org/x/vuln/cmd/govulncheck@latest
	govulncheck ./...

# ── Verify aggregates ─────────────────────────────────────────────────

# Faster pre-PR / CI-unit gate.
verify-unit: fmt-check vet test-unit build

# Full release gate (includes acceptance + lint).
verify: fmt-check vet lint test-unit test-acceptance build

# ── Cross-compilation (for npm packaging) ─────────────────────────────

# Local cross-compile; produces binaries at dist/skillex-<os>-<arch>{.exe}
# for use by npm-stage. Goreleaser handles release archive production
# separately (see release-snapshot).
dist: clean
	GOOS=darwin  GOARCH=amd64 $(GO) build $(LDFLAGS) -o dist/skillex-darwin-x64      ./cmd/skillex
	GOOS=darwin  GOARCH=arm64 $(GO) build $(LDFLAGS) -o dist/skillex-darwin-arm64    ./cmd/skillex
	GOOS=linux   GOARCH=amd64 $(GO) build $(LDFLAGS) -o dist/skillex-linux-x64       ./cmd/skillex
	GOOS=linux   GOARCH=arm64 $(GO) build $(LDFLAGS) -o dist/skillex-linux-arm64     ./cmd/skillex
	GOOS=windows GOARCH=amd64 $(GO) build $(LDFLAGS) -o dist/skillex-win32-x64.exe   ./cmd/skillex

# ── Release ───────────────────────────────────────────────────────────

# Local goreleaser snapshot (no publish). Produces archives + checksums
# in dist/. Replaces the old hand-rolled scripts/package-release-assets.sh.
release-snapshot:
	$(GORELEASER) release --snapshot --clean --skip=publish

# ── npm packaging ─────────────────────────────────────────────────────

version-sync:
	node scripts/set-npm-version.mjs $(PACKAGE_VERSION)

# Stage: copy dist/ binaries (from `make dist`) into each platform
# package's bin/ directory.
npm-stage: version-sync dist
	cp dist/skillex-darwin-arm64   npm/darwin-arm64/bin/skillex
	cp dist/skillex-darwin-x64     npm/darwin-x64/bin/skillex
	cp dist/skillex-linux-x64      npm/linux-x64/bin/skillex
	cp dist/skillex-linux-arm64    npm/linux-arm64/bin/skillex
	cp dist/skillex-win32-x64.exe  npm/win32-x64/bin/skillex.exe
	chmod +x npm/darwin-arm64/bin/skillex \
	         npm/darwin-x64/bin/skillex   \
	         npm/linux-x64/bin/skillex    \
	         npm/linux-arm64/bin/skillex
	@echo "Binaries staged. Run 'make npm-pack' to create tarballs."

# Pack all packages into dist/ as .tgz files (dry-run publish).
npm-pack: npm-stage
	cd npm/darwin-arm64 && npm pack --pack-destination ../../dist
	cd npm/darwin-x64   && npm pack --pack-destination ../../dist
	cd npm/linux-x64    && npm pack --pack-destination ../../dist
	cd npm/linux-arm64  && npm pack --pack-destination ../../dist
	cd npm/win32-x64    && npm pack --pack-destination ../../dist
	cd npm/skillex      && npm pack --pack-destination ../../dist
	@echo "Tarballs written to dist/. Inspect before publishing."

# Manual fallback. Normal release path is the GitHub Actions release
# workflow with cosign signing.
npm-publish: npm-stage
	cd npm/darwin-arm64 && npm publish --access public --provenance
	cd npm/darwin-x64   && npm publish --access public --provenance
	cd npm/linux-x64    && npm publish --access public --provenance
	cd npm/linux-arm64  && npm publish --access public --provenance
	cd npm/win32-x64    && npm publish --access public --provenance
	cd npm/skillex      && npm publish --access public --provenance

# ── Repo workflow ─────────────────────────────────────────────────────

refresh:
	$(GO) run $(LDFLAGS) ./cmd/skillex refresh

doctor:
	$(GO) run $(LDFLAGS) ./cmd/skillex doctor

release-tag:
	@version=$$(cat VERSION); \
	tag="v$$version"; \
	branch=$$(git rev-parse --abbrev-ref HEAD); \
	if [ "$$branch" != "main" ]; then \
		echo "release-tag must be run from the main branch."; \
		exit 1; \
	fi; \
	if [ -n "$$(git status --short)" ]; then \
		echo "release-tag requires a clean worktree."; \
		exit 1; \
	fi; \
	git fetch origin main --tags; \
	if [ "$$(git rev-parse HEAD)" != "$$(git rev-parse origin/main)" ]; then \
		echo "release-tag requires HEAD to match origin/main."; \
		exit 1; \
	fi; \
	if git rev-parse -q --verify "refs/tags/$$tag" >/dev/null; then \
		echo "Tag $$tag already exists locally."; \
		exit 1; \
	fi; \
	if git ls-remote --tags --exit-code origin "refs/tags/$$tag" >/dev/null 2>&1; then \
		echo "Tag $$tag already exists on origin."; \
		exit 1; \
	fi; \
	git tag "$$tag"; \
	git push origin "$$tag"; \
	echo "Pushed $$tag. GitHub Actions will run the release workflow."

# ── Acceptance tests ──────────────────────────────────────────────────

test-setup:
	./test/setup.sh

test-acceptance: test-setup build
	$(GO) test ./test/acceptance/... -v -timeout 300s

test-perf: build
	./test/setup.sh --perf
	$(GO) test ./test/acceptance/ -run "TestPerformance" -v -timeout 600s

test-clean:
	./test/setup.sh --clean

# ── Cleanup ───────────────────────────────────────────────────────────

clean:
	rm -f skillex skillex.exe
	rm -rf dist/

# ── Help ──────────────────────────────────────────────────────────────

help:
	@echo "Common targets:"
	@echo "  make build             Build ./skillex"
	@echo "  make verify-unit       fmt-check + vet + unit tests + build"
	@echo "  make verify            full pre-PR gate (+ lint + acceptance)"
	@echo "  make test-race         Run tests with the race detector"
	@echo "  make lint              golangci-lint"
	@echo "  make vuln              govulncheck"
	@echo "  make release-snapshot  Local goreleaser snapshot"
	@echo "  make npm-pack          Stage + pack npm wrappers"
	@echo "  make test-acceptance   Run fixture acceptance suite"
	@echo "  make clean             Remove build artifacts"
