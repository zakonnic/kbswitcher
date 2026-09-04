MODULE  := github.com/zakonnic/kbswitcher
BINDIR  := bin
GOOS    ?= $(shell go env GOOS)
GOARCH  ?= $(shell go env GOARCH)

VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
COMMIT  ?= $(shell git rev-parse --short HEAD 2>/dev/null || echo unknown)

BINARY  := kbswitcher
PACKAGE := $(BINARY)_$(VERSION)_$(GOOS)_$(GOARCH).tar.gz
# -H windowsgui keeps the program from opening a console window.
ifeq ($(GOOS),windows)
	BINARY   := kbswitcher.exe
	PACKAGE  := kbswitcher_$(VERSION)_$(GOOS)_$(GOARCH).zip
	GUIFLAGS := -H windowsgui
endif

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

.PHONY: icon
icon: ## Redraw the tray icon from tools/icongen
	go run ./tools/icongen

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

.PHONY: dist
dist: build ## Package the build and write checksums.txt, as a release expects
	@mkdir -p $(BINDIR)/dist
	@cp $(BINDIR)/$(BINARY) $(BINDIR)/dist/$(BINARY)
ifeq ($(GOOS),windows)
	cd $(BINDIR)/dist && tar -a -c -f ../$(PACKAGE) $(BINARY)
else
	cd $(BINDIR)/dist && tar -czf ../$(PACKAGE) $(BINARY)
endif
	@rm -rf $(BINDIR)/dist
	cd $(BINDIR) && $(SHA256) $(PACKAGE) >> checksums.txt
	@echo "packaged $(BINDIR)/$(PACKAGE)"

.PHONY: clean
clean: ## Remove the build output
	rm -rf $(BINDIR)
