VERSION ?= 0.6.0-dev
LDFLAGS := -ldflags "-X github.com/skillex/skillex/cli.Version=$(VERSION)"

.PHONY: build install test lint clean dist npm-stage npm-pack npm-publish refresh doctor

build:
	go build $(LDFLAGS) -o skillex ./cmd/skillex

install:
	go install $(LDFLAGS) ./cmd/skillex

test:
	go test ./...

lint:
	go vet ./...

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

# Stage: copy dist/ binaries into each platform package's bin/ directory.
# Run `make dist` first, then `make npm-stage`.
npm-stage: dist
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

# Publish all packages to npm (requires npm login and correct VERSION).
# Publish platform packages first, then the main wrapper.
npm-publish: npm-stage
	cd npm/darwin-arm64 && npm publish --access public
	cd npm/darwin-x64   && npm publish --access public
	cd npm/linux-x64    && npm publish --access public
	cd npm/linux-arm64  && npm publish --access public
	cd npm/win32-x64    && npm publish --access public
	cd npm/skillex      && npm publish --access public
	@echo "Published @skillex/skillex@$(VERSION) and all platform packages."

# ── Repo workflow ────────────────────────────────────────────────────────────

refresh:
	go run $(LDFLAGS) ./cmd/skillex refresh

doctor:
	go run $(LDFLAGS) ./cmd/skillex doctor
