# Make on Windows runs recipes with the first sh.exe on PATH. Cygwin's sets
# HOME to its own home when none is set, as from PowerShell, and git then
# misses ~/.gitconfig: no identity for the tag, no credentials for the push.
export HOME := $(or $(HOME),$(USERPROFILE))

MODULE  := github.com/zakonnic/kbswitcher
BINDIR  := bin
GOOS    ?= $(shell go env GOOS)
GOARCH  ?= $(shell go env GOARCH)

VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
COMMIT  ?= $(shell git rev-parse --short HEAD 2>/dev/null || echo unknown)

BINARY  := kbswitcher
ARCHIVE := tar.gz
# -H windowsgui keeps the program from opening a console window.
ifeq ($(GOOS),windows)
	BINARY   := kbswitcher.exe
	ARCHIVE  := zip
	GUIFLAGS := -H windowsgui
	# bsdtar writes zip, GNU tar cannot; on Windows Git's sh finds GNU tar
	# first, so Windows' own tar is named.
	WINROOT  := $(or $(SystemRoot),$(SYSTEMROOT))
	ZIPTAR   := $(if $(WINROOT),$(subst \,/,$(WINROOT))/System32/tar.exe,tar)
endif
package = kbswitcher_$(1)_$(GOOS)_$(GOARCH).$(ARCHIVE)
PACKAGE = $(call package,$(VERSION))

# What a release publishes, one archive per platform; the build job of
# .github/workflows/ci.yml lists the same.
PLATFORMS := windows_amd64 darwin_amd64 darwin_arm64 linux_amd64 linux_arm64
archives   = $(foreach p,$(PLATFORMS),kbswitcher_$(1)_$(p).$(if $(filter windows_%,$(p)),zip,tar.gz))

# Release targets take V=X.Y.Z (the leading v is optional; VERSION= works too)
# and tag vX.Y.Z.
TAG     := v$(patsubst v%,%,$(or $(V),$(VERSION)))
define need-version
	@test -n "$(V)" || test "$(origin VERSION)" != file || { echo "V is required, e.g. make $@ V=1.2.3"; exit 1; }
	@echo "$(TAG)" | grep -Eq '^v[0-9]+\.[0-9]+\.[0-9]+$$' || { echo "V must look like 1.2.3, not $(or $(V),$(VERSION))"; exit 1; }
endef
define need-clean-main
	@test "$$(git rev-parse --abbrev-ref HEAD)" = main || { echo "Error: release from main, not $$(git rev-parse --abbrev-ref HEAD)."; exit 1; }
	@test -z "$$(git status --porcelain --untracked-files=no)" || { \
		echo "Error: uncommitted changes. The tag would point at the last commit, not at what you see."; \
		exit 1; \
	}
endef

LDFLAGS := -s -w $(GUIFLAGS) \
	-X $(MODULE)/internal/buildinfo.Version=$(VERSION) \
	-X $(MODULE)/internal/buildinfo.Commit=$(COMMIT)

# LSUIElement keeps the bundle out of the Dock and the application switcher.
APP     := $(BINDIR)/kbswitcher.app
APPID   := com.zakonnic.kbswitcher
SHA256  := $(shell command -v sha256sum >/dev/null 2>&1 && echo sha256sum || echo shasum -a 256)

.DEFAULT_GOAL := help

.PHONY: help
help: ## Show this help
	@grep -hE '^[a-zA-Z_-]+:.*?## .*$$' $(MAKEFILE_LIST) | \
		awk 'BEGIN {FS = ":.*?## "}; {printf "\033[36m%-16s\033[0m %s\n", $$1, $$2}'

.PHONY: build
build: ## Build the application into bin/
	@mkdir -p $(BINDIR)
	go build -trimpath -ldflags "$(LDFLAGS)" -o $(BINDIR)/$(BINARY) ./cmd/kbswitcher
	@echo "built $(BINDIR)/$(BINARY) ($(VERSION))"

.PHONY: run
run: build ## Build and run
	$(BINDIR)/$(BINARY)

.PHONY: test
test: ## Run the tests
	go test ./...

# Linking race builds in parallel can exhaust the commit limit of a busy
# Windows machine; the linker then fails or hangs, not a test.
.PHONY: test-race
test-race: ## Run the tests under the race detector
	go test -race -p 1 ./...

.PHONY: cover
cover: ## Run the tests and report coverage per package
	go test -cover ./...

# tools/ is test equipment: it stays out of the number, as in CI.
.PHONY: cover-gen
cover-gen: ## Measure the coverage of the application's code into var/coverage.out, the number the README badge shows
	@mkdir -p var
	go test -count=1 -coverpkg=./cmd/...,./internal/... -coverprofile=var/coverage.out ./...
	go tool cover -func=var/coverage.out | tail -1

.PHONY: vet
vet: ## Run go vet
	go vet ./...

.PHONY: lint
lint: ## Run golangci-lint (see .golangci.yml)
	golangci-lint run

.PHONY: fmt
fmt: ## Format the Go sources
	gofmt -l -w .

.PHONY: tidy
tidy: ## Tidy go.mod and go.sum
	go mod tidy

.PHONY: check
check: fmt vet test ## Format, vet and test

# The linker takes the .syso next to main.go into every windows/amd64 build;
# it is committed, so only a change of icon.ico calls for this.
.PHONY: syso
syso: ## Rebuild the Windows icon resource from internal/assets/icon.ico (needs windres)
	echo '1 ICON "internal/assets/icon.ico"' | windres -O coff -F pe-x86-64 -o cmd/kbswitcher/rsrc_windows_amd64.syso

# The icon is made with sips and iconutil, which every Mac has.
.PHONY: bundle
bundle: build ## Build the macOS application bundle in bin/kbswitcher.app
	@test "$(GOOS)" = "darwin" || (echo "bundle is only meaningful on macOS"; exit 1)
	@rm -rf $(APP) $(BINDIR)/kbswitcher.iconset
	@mkdir -p $(APP)/Contents/MacOS $(APP)/Contents/Resources $(BINDIR)/kbswitcher.iconset
	@cp $(BINDIR)/$(BINARY) $(APP)/Contents/MacOS/kbswitcher
	@for s in 16 32 128 256; do \
		sips -z $$s $$s internal/assets/icon.png --out $(BINDIR)/kbswitcher.iconset/icon_$${s}x$${s}.png >/dev/null && \
		sips -z $$((s * 2)) $$((s * 2)) internal/assets/icon.png --out $(BINDIR)/kbswitcher.iconset/icon_$${s}x$${s}@2x.png >/dev/null || exit 1; \
	done
	@iconutil -c icns -o $(APP)/Contents/Resources/kbswitcher.icns $(BINDIR)/kbswitcher.iconset
	@rm -rf $(BINDIR)/kbswitcher.iconset
	@printf '%s\n' \
		'<?xml version="1.0" encoding="UTF-8"?>' \
		'<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">' \
		'<plist version="1.0">' \
		'<dict>' \
		'	<key>CFBundleName</key><string>kbswitcher</string>' \
		'	<key>CFBundleDisplayName</key><string>kbswitcher</string>' \
		'	<key>CFBundleExecutable</key><string>kbswitcher</string>' \
		'	<key>CFBundleIdentifier</key><string>$(APPID)</string>' \
		'	<key>CFBundleIconFile</key><string>kbswitcher</string>' \
		'	<key>CFBundlePackageType</key><string>APPL</string>' \
		'	<key>CFBundleShortVersionString</key><string>$(patsubst v%,%,$(VERSION))</string>' \
		'	<key>CFBundleVersion</key><string>$(patsubst v%,%,$(VERSION))</string>' \
		'	<key>LSMinimumSystemVersion</key><string>12.0</string>' \
		'	<key>LSUIElement</key><true/>' \
		'	<key>NSHighResolutionCapable</key><true/>' \
		'</dict>' \
		'</plist>' > $(APP)/Contents/Info.plist
	@echo "built $(APP)"

# A repeated dist replaces the archive's line in checksums.txt: the updater
# trusts the first one. On macOS the archive holds the application bundle,
# without the ._ files macOS's tar would add; the updater finds the program in
# it by name.
.PHONY: dist
dist: $(if $(filter darwin,$(GOOS)),bundle,build) ## Package the build and record its checksum in checksums.txt, as a release expects
ifeq ($(GOOS),windows)
	@mkdir -p $(BINDIR)/dist
	@cp $(BINDIR)/$(BINARY) $(BINDIR)/dist/$(BINARY)
	cd $(BINDIR)/dist && "$(ZIPTAR)" -a -c -f ../$(PACKAGE) $(BINARY)
	@rm -rf $(BINDIR)/dist
else ifeq ($(GOOS),darwin)
	COPYFILE_DISABLE=1 tar -C $(BINDIR) -czf $(BINDIR)/$(PACKAGE) kbswitcher.app
else
	tar -C $(BINDIR) -czf $(BINDIR)/$(PACKAGE) $(BINARY)
endif
	cd $(BINDIR) && touch checksums.txt && \
		{ awk -v f=$(PACKAGE) '{ n = $$2; sub(/^\*/, "", n) } n != f' checksums.txt; $(SHA256) $(PACKAGE); } > checksums.new && \
		mv checksums.new checksums.txt
	@echo "packaged $(BINDIR)/$(PACKAGE)"

# The updater only offers a newer version, so tag refuses an older one.
.PHONY: tag
tag: ## Tag HEAD of main as vV (make tag V=1.2.3); 'make release' does it for you
	$(need-version)
	$(need-clean-main)
	@! git rev-parse -q --verify "refs/tags/$(TAG)" >/dev/null || { echo "Error: tag $(TAG) already exists."; exit 1; }
	@last=$$(git tag --list 'v*' --sort=-v:refname | head -1); \
	if [ -n "$$last" ] && [ "$$(printf '%s\n%s\n' "$$last" "$(TAG)" | sort -V | tail -1)" != "$(TAG)" ]; then \
		echo "Error: $(TAG) is not newer than $$last."; \
		exit 1; \
	fi
	git tag -a $(TAG) -m "$(TAG)"

.PHONY: untag
untag: ## Delete the local tag vV (make untag V=1.2.3)
	$(need-version)
	git tag -d $(TAG)

.PHONY: push
push: ## Push main and the tag vV to origin, both or neither (make push V=1.2.3)
	$(need-version)
	@test "$$(git rev-parse --abbrev-ref HEAD)" = main || { echo "Error: push from main, not $$(git rev-parse --abbrev-ref HEAD)."; exit 1; }
	git push --atomic origin main $(TAG)

.PHONY: release-check
release-check: ## What a release must pass first: formatting, vet, lint and the tests under the race detector
	@test -z "$$(gofmt -l .)" || { echo "Error: unformatted files:"; gofmt -l .; exit 1; }
	"$(MAKE)" vet lint test-race

# The tag's push has CI build every platform and publish the release (the
# release job of .github/workflows/ci.yml); release then waits for it and
# checks what the updater will download. Whatever stops short of the push is
# undone, so the version can be cut again; so can a tag CI did not publish,
# once it is deleted from origin as tools/release says. YES=1 skips the
# question before the push.
.PHONY: release
release: ## Release in one go: check, tag, confirm, push, wait for CI to publish every platform, verify (make release V=1.2.3 [YES=1])
	$(need-version)
	$(need-clean-main)
	"$(MAKE)" release-check
	"$(MAKE)" tag V=$(TAG)
	@if [ "$(YES)" != 1 ]; then \
		printf 'Push %s as %s to origin? [y/N] ' "$$(git log -1 --format='%h %s')" "$(TAG)"; \
		read answer || answer=; \
		case "$$answer" in y|Y|yes) ;; *) echo "Not pushed: deleting the tag."; "$(MAKE)" untag V=$(TAG) >/dev/null; exit 1;; esac; \
	fi
	@"$(MAKE)" push V=$(TAG) || { echo "The push failed and, being atomic, sent nothing: deleting the tag."; "$(MAKE)" untag V=$(TAG) >/dev/null; exit 1; }
	go run ./tools/release wait $(TAG) $(call archives,$(TAG))

# The updater installs the latest release only: its archive for the machine and
# checksums.txt, which must vouch for that archive.
.PHONY: release-verify
release-verify: ## Check GitHub serves vV as the latest release, every platform's archive vouched for by checksums.txt (make release-verify V=1.2.3)
	$(need-version)
	go run ./tools/release verify $(TAG) $(call archives,$(TAG))

.PHONY: clean
clean: ## Remove the build output
	rm -rf $(BINDIR)
