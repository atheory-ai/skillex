BASE_VERSION := $(shell cat VERSION)
VERSION ?= $(BASE_VERSION)-dev
PACKAGE_VERSION ?= $(BASE_VERSION)
LDFLAGS := -ldflags "-X github.com/atheory-ai/skillex/cli.Version=$(VERSION)"

.PHONY: build install test lint clean dist npm-stage npm-pack npm-publish refresh doctor version-sync verify release-tag

verify:
	go test $$(go list ./... | grep -v '/test/acceptance$$')
	go vet ./...
	@files=$$(git ls-files '*.go'); \
	if [ -n "$$files" ]; then \
		unformatted=$$(gofmt -l $$files); \
		if [ -n "$$unformatted" ]; then \
			echo "These files need gofmt:"; \
			echo "$$unformatted"; \
			exit 1; \
		fi; \
	fi
	$(MAKE) build

build:
	go build $(LDFLAGS) -o skillex ./cmd/skillex

install:
	go install $(LDFLAGS) ./cmd/skillex

test:
	go test $$(go list ./... | grep -v '/test/acceptance$$')

lint:
	go vet ./...

lint-fix:
	gofmt -w .

clean:
	rm -f skillex skillex.exe
	rm -rf dist/

# ── Cross-compilation ────────────────────────────────────────────────────────

# Build all platform binaries into dist/
dist: clean
	GOOS=darwin  GOARCH=amd64 go build $(LDFLAGS) -o dist/skillex-darwin-x64      ./cmd/skillex
	GOOS=darwin  GOARCH=arm64 go build $(LDFLAGS) -o dist/skillex-darwin-arm64    ./cmd/skillex
	GOOS=linux   GOARCH=amd64 go build $(LDFLAGS) -o dist/skillex-linux-x64       ./cmd/skillex
	GOOS=linux   GOARCH=arm64 go build $(LDFLAGS) -o dist/skillex-linux-arm64     ./cmd/skillex
	GOOS=windows GOARCH=amd64 go build $(LDFLAGS) -o dist/skillex-win32-x64.exe   ./cmd/skillex

# ── npm packaging ────────────────────────────────────────────────────────────

version-sync:
	node scripts/set-npm-version.mjs $(PACKAGE_VERSION)

# Stage: copy dist/ binaries into each platform package's bin/ directory.
# Run `make dist` first, then `make npm-stage`.
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

# Publish all packages to npm (manual fallback only).
# The normal release path is the GitHub Actions release workflow.
npm-publish: npm-stage
	cd npm/darwin-arm64 && npm publish --access public
	cd npm/darwin-x64   && npm publish --access public
	cd npm/linux-x64    && npm publish --access public
	cd npm/linux-arm64  && npm publish --access public
	cd npm/win32-x64    && npm publish --access public
	cd npm/skillex      && npm publish --access public
	@echo "Published @atheory-ai/skillex@$(VERSION) and all platform packages."

# ── Repo workflow ────────────────────────────────────────────────────────────

refresh:
	go run $(LDFLAGS) ./cmd/skillex refresh

doctor:
	go run $(LDFLAGS) ./cmd/skillex doctor

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

# ── Acceptance tests ──────────────────────────────────────────────────────────

.PHONY: test-setup test-acceptance test-perf test-clean

test-setup:
	./test/setup.sh

test-acceptance: test-setup build
	go test ./test/acceptance/... -v -timeout 300s

test-perf: build
	./test/setup.sh --perf
	go test ./test/acceptance/ -run "TestPerformance" -v -timeout 600s

test-clean:
	./test/setup.sh --clean
