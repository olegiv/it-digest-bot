SHELL           := /bin/bash
GO              ?= go
BINARY          := digest
BIN_DIR         := bin
PKG             := github.com/olegiv/it-digest-bot
VERSION_PKG     := $(PKG)/internal/version
VERSION         ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo "dev")
COMMIT          ?= $(shell git rev-parse --short HEAD 2>/dev/null || echo "none")
BUILD_DATE      ?= $(shell date -u +%Y-%m-%dT%H:%M:%SZ)
VERSION_LDFLAGS := \
                   -X '$(VERSION_PKG).Version=$(VERSION)' \
                   -X '$(VERSION_PKG).Commit=$(COMMIT)' \
                   -X '$(VERSION_PKG).Date=$(BUILD_DATE)'
LDFLAGS         := -s -w $(VERSION_LDFLAGS)

GOLANGCI_LINT_VERSION := v2.11.4
GOFUMPT_VERSION       := v0.9.2

.DEFAULT_GOAL := help

.PHONY: all help build build-prod build-linux-amd64 build-darwin-arm64 build-all-platforms \
        test test-race coverage coverage-html fmt fmt-check vet lint lint-go check deps tidy clean install-tools \
        run-watch run-dry deploy rollback backup status uninstall dry-watch dry-daily

all: build ## Build the default local/dev binary

build: ## Build fast local/dev binary for host platform
	@mkdir -p $(BIN_DIR)
	$(GO) build -ldflags="$(VERSION_LDFLAGS)" -o $(BIN_DIR)/$(BINARY) ./cmd/digest

build-prod: ## Build optimized host production binary
	@mkdir -p $(BIN_DIR)
	$(GO) build -trimpath -ldflags="$(LDFLAGS)" -o $(BIN_DIR)/$(BINARY) ./cmd/digest

build-linux-amd64: ## Build optimized static Linux AMD64 production binary
	@mkdir -p $(BIN_DIR)
	GOOS=linux GOARCH=amd64 GOAMD64=v3 CGO_ENABLED=0 \
	    $(GO) build -trimpath -ldflags="$(LDFLAGS)" \
	    -o $(BIN_DIR)/$(BINARY)-linux-amd64 ./cmd/digest

build-darwin-arm64: ## Build optimized Darwin ARM64 production binary
	@mkdir -p $(BIN_DIR)
	GOOS=darwin GOARCH=arm64 \
	    $(GO) build -trimpath -ldflags="$(LDFLAGS)" \
	    -o $(BIN_DIR)/$(BINARY)-darwin-arm64 ./cmd/digest

build-all-platforms: build-linux-amd64 build-darwin-arm64 ## Build all production platform binaries

test: ## Run all tests
	$(GO) test ./...

test-race: ## Run tests with race detector
	$(GO) test -race ./...

coverage: ## Run tests with coverage summary
	$(GO) test -cover ./...

coverage-html: ## Generate HTML coverage report
	$(GO) test -coverprofile=coverage.out ./...
	$(GO) tool cover -html=coverage.out -o coverage.html
	@echo "Coverage report: coverage.html"

fmt: ## Format code with gofumpt
	gofumpt -w .

fmt-check: ## Fail if gofumpt would reformat files
	@out=$$(gofumpt -l .); \
	if [ -n "$$out" ]; then \
		echo "gofumpt would reformat:"; \
		echo "$$out"; \
		exit 1; \
	fi

vet: ## Run go vet
	$(GO) vet ./...

lint-go: ## Run golangci-lint
	golangci-lint run ./...

lint: lint-go ## Run all linters

check: fmt-check vet lint test ## Run the full local quality gate

deps: ## Download Go module dependencies
	$(GO) mod download

tidy: ## Tidy Go modules
	$(GO) mod tidy

run-watch: ## Run the watch subcommand locally
	$(GO) run ./cmd/digest watch --config config.toml

run-dry: ## Render a dry-run post to stdout
	$(GO) run ./cmd/digest post --dry-run --config config.toml

clean: ## Remove build artifacts
	rm -rf $(BIN_DIR) coverage.out coverage.html

install-tools: ## Install pinned developer tools
	$(GO) install github.com/golangci/golangci-lint/v2/cmd/golangci-lint@$(GOLANGCI_LINT_VERSION)
	$(GO) install mvdan.cc/gofumpt@$(GOFUMPT_VERSION)

deploy: ## Build + ship binary to server (HOST= overrides deploy/deploy.env)
	HOST=$(HOST) ./deploy/deploy.sh

rollback: ## Swap /usr/local/bin/digest with .prev backup if present
	HOST=$(HOST) ./deploy/rollback.sh

backup: ## Snapshot state.db → backups/ (HOST= overrides deploy/deploy.env)
	HOST=$(HOST) ./deploy/backup.sh

status: ## Show timers + recent journal (HOST= overrides deploy/deploy.env)
	HOST=$(HOST) ./deploy/status.sh

dry-watch: ## Dry-run 'digest watch' on the server
	HOST=$(HOST) ./deploy/dry-run.sh watch

dry-daily: ## Dry-run 'digest daily' on the server
	HOST=$(HOST) ./deploy/dry-run.sh daily

uninstall: ## Stop + remove it-digest-bot from server
	HOST=$(HOST) ./deploy/uninstall.sh

help: ## Show this help
	@awk 'BEGIN {FS = ":.*##"; printf "Usage: make \033[36m<target>\033[0m\n\nTargets:\n"} /^[a-zA-Z0-9_-]+:.*##/ {printf "  \033[36m%-22s\033[0m %s\n", $$1, $$2}' $(MAKEFILE_LIST)
