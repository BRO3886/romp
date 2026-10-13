BINARY_NAME=romp
VERSION?=$(shell git describe --tags --always --dirty 2>/dev/null || echo "dev")
COMMIT=$(shell git rev-parse --short HEAD 2>/dev/null || echo "none")
BUILD_TIME=$(shell date -u +"%Y-%m-%dT%H:%M:%SZ")
LDFLAGS=-ldflags "-X main.version=$(VERSION) -X main.commit=$(COMMIT) -X main.buildTime=$(BUILD_TIME)"

.PHONY: build test release clean help generate-protocol smoke

generate-protocol: ## Generate Go and Swift wire types
	python3 scripts/generate_protocol.py

smoke: build ## Check real daemon and Go/Swift clients in temporary state
	python3 scripts/smoke_daemon.py

build: ## Build the binary
	@mkdir -p bin
	go build $(LDFLAGS) -o bin/$(BINARY_NAME) ./cmd/romp/
	go build $(LDFLAGS) -o bin/rompd ./cmd/rompd/

test: ## Run tests
	go test ./...

release: ## Build release tarballs (darwin arm64 + amd64)
	@mkdir -p bin
	@set -e; for arch in arm64 amd64; do \
		echo "Building romp-darwin-$$arch..."; \
		GOOS=darwin GOARCH=$$arch go build $(LDFLAGS) -o bin/romp ./cmd/romp/; \
		GOOS=darwin GOARCH=$$arch go build $(LDFLAGS) -o bin/rompd ./cmd/rompd/; \
		chmod +x bin/romp bin/rompd; \
		COPYFILE_DISABLE=1 tar -czf bin/romp-darwin-$$arch.tar.gz -C bin romp rompd; \
		rm bin/romp bin/rompd; \
	done
	@echo "Built bin/romp-darwin-{arm64,amd64}.tar.gz"

clean: ## Remove built binaries
	rm -rf bin/

help: ## Show this help
	@grep -E '^[a-zA-Z_-]+:.*?## .*$$' $(MAKEFILE_LIST) | sort | awk 'BEGIN {FS = ":.*?## "}; {printf "\033[36m%-10s\033[0m %s\n", $$1, $$2}'

.DEFAULT_GOAL := help
