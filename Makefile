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

# Release targets take V=X.Y.Z (the leading v is optional; VERSION= works too)
# and tag vX.Y.Z.
TAG     := v$(patsubst v%,%,$(or $(V),$(VERSION)))
REPO    := $(patsubst github.com/%,%,$(MODULE))
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

.PHONY: test-race
test-race: ## Run the tests under the race detector
	go test -race ./...

.PHONY: cover
cover: ## Run the tests and report coverage per package
	go test -cover ./...

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

.PHONY: bundle
bundle: build ## Build the macOS application bundle in bin/kbswitcher.app
	@test "$(GOOS)" = "darwin" || (echo "bundle is only meaningful on macOS"; exit 1)
	@rm -rf $(APP)
	@mkdir -p $(APP)/Contents/MacOS $(APP)/Contents/Resources
	@cp $(BINDIR)/$(BINARY) $(APP)/Contents/MacOS/kbswitcher
	@cp internal/assets/icon.png $(APP)/Contents/Resources/icon.png
	@printf '%s\n' \
		'<?xml version="1.0" encoding="UTF-8"?>' \
		'<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">' \
		'<plist version="1.0">' \
		'<dict>' \
		'	<key>CFBundleName</key><string>kbswitcher</string>' \
		'	<key>CFBundleDisplayName</key><string>kbswitcher</string>' \
		'	<key>CFBundleExecutable</key><string>kbswitcher</string>' \
		'	<key>CFBundleIdentifier</key><string>$(APPID)</string>' \
		'	<key>CFBundlePackageType</key><string>APPL</string>' \
		'	<key>CFBundleShortVersionString</key><string>$(VERSION)</string>' \
		'	<key>CFBundleVersion</key><string>$(VERSION)</string>' \
		'	<key>LSMinimumSystemVersion</key><string>11.0</string>' \
		'	<key>LSUIElement</key><true/>' \
		'	<key>NSHighResolutionCapable</key><true/>' \
		'</dict>' \
		'</plist>' > $(APP)/Contents/Info.plist
	@echo "built $(APP)"

# A repeated dist replaces the archive's line in checksums.txt: the updater
# trusts the first one.
.PHONY: dist
dist: build ## Package the build and record its checksum in checksums.txt, as a release expects
	@mkdir -p $(BINDIR)/dist
	@cp $(BINDIR)/$(BINARY) $(BINDIR)/dist/$(BINARY)
ifeq ($(GOOS),windows)
	cd $(BINDIR)/dist && "$(ZIPTAR)" -a -c -f ../$(PACKAGE) $(BINARY)
else
	cd $(BINDIR)/dist && tar -czf ../$(PACKAGE) $(BINARY)
endif
	@rm -rf $(BINDIR)/dist
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

# release undoes whatever stopped short of the push, so the version can be cut
# again; once origin has the tag, a fix needs the next version. YES=1 skips
# the question before the push.
.PHONY: release
release: ## Release in one go: check, tag, package, confirm, push; undone if it stops before the push (make release V=1.2.3 [YES=1])
	$(need-version)
	$(need-clean-main)
	"$(MAKE)" release-check
	"$(MAKE)" tag V=$(TAG)
	@rm -f $(BINDIR)/checksums.txt
	@"$(MAKE)" dist VERSION=$(TAG) || { echo "Packaging failed: deleting the tag."; "$(MAKE)" untag V=$(TAG) >/dev/null; exit 1; }
	@if [ "$(YES)" != 1 ]; then \
		printf 'Push %s as %s to origin? [y/N] ' "$$(git log -1 --format='%h %s')" "$(TAG)"; \
		read answer || answer=; \
		case "$$answer" in y|Y|yes) ;; *) echo "Not pushed: deleting the tag."; "$(MAKE)" untag V=$(TAG) >/dev/null; exit 1;; esac; \
	fi
	@"$(MAKE)" push V=$(TAG) || { echo "The push failed and, being atomic, sent nothing: deleting the tag."; "$(MAKE)" untag V=$(TAG) >/dev/null; exit 1; }
	@echo
	@echo "$(TAG) is pushed. Publish it on GitHub:"
	@echo "  1. Open https://$(MODULE)/releases/new?tag=$(TAG)"
	@echo "  2. Title: $(TAG). Attach $(BINDIR)/$(call package,$(TAG)) and $(BINDIR)/checksums.txt"
	@echo "  3. Leave 'Set as a pre-release' off and press 'Publish release':"
	@echo "     the updater sees neither drafts nor pre-releases."
	@echo "  4. Then: make release-verify V=$(patsubst v%,%,$(TAG))"

# The updater installs the latest release only: its archive for this machine
# and checksums.txt, which must vouch for that archive.
.PHONY: release-verify
release-verify: ## After publishing: check GitHub serves vV as the latest release, with the files the updater needs (make release-verify V=1.2.3)
	$(need-version)
	@json=$$(curl -fsS https://api.github.com/repos/$(REPO)/releases/latest) || { \
		echo "Error: GitHub has no published release yet (drafts and pre-releases do not count)."; exit 1; }; \
	echo "$$json" | grep -q '"tag_name": *"$(TAG)"' || { \
		echo "Error: the latest release is $$(echo "$$json" | grep -o '"tag_name": *"[^"]*"'), not $(TAG)."; exit 1; }; \
	for f in $(call package,$(TAG)) checksums.txt; do \
		echo "$$json" | grep -q "\"name\": *\"$$f\"" || { echo "Error: $(TAG) has no $$f attached."; exit 1; }; \
	done; \
	sums=$$(curl -fsSL https://github.com/$(REPO)/releases/download/$(TAG)/checksums.txt) || { echo "Error: cannot download checksums.txt."; exit 1; }; \
	echo "$$sums" | grep -q "$$($(SHA256) $(BINDIR)/$(call package,$(TAG)) | cut -d' ' -f1)" || { \
		echo "Error: the published checksums.txt does not vouch for $(BINDIR)/$(call package,$(TAG))."; exit 1; }; \
	echo "$(TAG) is the latest release, and its checksums.txt vouches for the archive built here."

.PHONY: clean
clean: ## Remove the build output
	rm -rf $(BINDIR)
