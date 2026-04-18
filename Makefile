SHELL           := /bin/bash
BINARY          := digest
BIN_DIR         := bin
PKG             := github.com/olegiv/it-digest-bot
VERSION_PKG     := $(PKG)/internal/version
VERSION         ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo "dev")
COMMIT          ?= $(shell git rev-parse --short HEAD 2>/dev/null || echo "none")
BUILD_DATE      ?= $(shell date -u +%Y-%m-%dT%H:%M:%SZ)
LDFLAGS         := -s -w \
                   -X '$(VERSION_PKG).Version=$(VERSION)' \
                   -X '$(VERSION_PKG).Commit=$(COMMIT)' \
                   -X '$(VERSION_PKG).Date=$(BUILD_DATE)'

GOLANGCI_LINT_VERSION := v2.9.0
GOFUMPT_VERSION       := v0.7.0

.PHONY: all build build-linux test test-race lint fmt run-watch run-dry clean install-tools help \
        deploy rollback backup status uninstall dry-watch dry-daily

all: build

build: ## Build binary for host platform
	@mkdir -p $(BIN_DIR)
	CGO_ENABLED=0 go build -trimpath -ldflags="$(LDFLAGS)" -o $(BIN_DIR)/$(BINARY) ./cmd/digest

build-linux: ## Build static PIE binary for Linux amd64 targeting x86-64-v3
	@mkdir -p $(BIN_DIR)
	GOOS=linux GOARCH=amd64 GOAMD64=v3 CGO_ENABLED=0 \
	    go build -trimpath -buildmode=pie -ldflags="$(LDFLAGS)" \
	    -o $(BIN_DIR)/$(BINARY)-linux-amd64 ./cmd/digest

test: ## Run all tests
	go test ./...

test-race: ## Run tests with race detector
	go test -race ./...

lint: ## Run golangci-lint
	golangci-lint run ./...

fmt: ## Format code with gofumpt
	gofumpt -w .

run-watch: ## Run the watch subcommand locally
	go run ./cmd/digest watch --config config.toml

run-dry: ## Render a dry-run post to stdout
	go run ./cmd/digest post --dry-run --config config.toml

clean: ## Remove build artifacts
	rm -rf $(BIN_DIR)

install-tools: ## Install pinned developer tools
	go install github.com/golangci/golangci-lint/v2/cmd/golangci-lint@$(GOLANGCI_LINT_VERSION)
	go install mvdan.cc/gofumpt@$(GOFUMPT_VERSION)

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
	@awk 'BEGIN {FS = ":.*##"} /^[a-zA-Z_-]+:.*##/ {printf "  \033[36m%-14s\033[0m %s\n", $$1, $$2}' $(MAKEFILE_LIST)
