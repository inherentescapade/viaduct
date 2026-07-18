# Viaduct — top-level build helpers.
#
# CLI/TUI targets produce a single static `viaduct` binary (no CGO required).
# Desktop targets delegate to the Wails build in ./desktop.
#
# Version stamping: `viaduct --version` reports $(VERSION), which defaults to
# `git describe` and can be overridden, e.g. `make build VERSION=v1.2.3`.

VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
LDFLAGS := -s -w -X viaduct/cmd.version=$(VERSION)

.PHONY: help build install test vet desktop desktop-windows clean

help: ## Show this help
	@grep -E '^[a-zA-Z_-]+:.*?## .*$$' $(MAKEFILE_LIST) \
		| awk 'BEGIN{FS=":.*?## "}{printf "  \033[36m%-16s\033[0m %s\n", $$1, $$2}'

build: ## Compile the CLI/TUI binary for the host platform -> ./viaduct
	CGO_ENABLED=0 go build -trimpath -ldflags "$(LDFLAGS)" -o viaduct .

install: ## Install the CLI/TUI binary into $(GOPATH)/bin
	CGO_ENABLED=0 go install -trimpath -ldflags "$(LDFLAGS)" .

test: ## Run the full test suite (desktop included, via the WebKit 4.1 tag)
	go test -tags webkit2_41 ./...

vet: ## Static analysis across all packages
	go vet -tags webkit2_41 ./...

desktop: ## Build the desktop GUI for the host platform (see desktop/Makefile)
	$(MAKE) -C desktop build

desktop-windows: ## Cross-build the Windows desktop portable exe + NSIS installer
	cd desktop && wails build -platform windows/amd64 -nsis -trimpath

clean: ## Remove build artifacts
	rm -f viaduct viaduct.exe
	rm -rf dist desktop/build/bin
