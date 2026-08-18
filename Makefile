BINARY_NAME=romp
VERSION?=$(shell git describe --tags --always --dirty 2>/dev/null || echo "dev")
COMMIT=$(shell git rev-parse --short HEAD 2>/dev/null || echo "none")
BUILD_TIME=$(shell date -u +"%Y-%m-%dT%H:%M:%SZ")
LDFLAGS=-ldflags "-X main.version=$(VERSION) -X main.commit=$(COMMIT) -X main.buildTime=$(BUILD_TIME)"

.PHONY: build test release clean help

build: ## Build the binary
	@mkdir -p bin
	go build $(LDFLAGS) -o bin/$(BINARY_NAME) ./cmd/romp/

test: ## Run tests
	go test ./...

release: ## Build release tarballs (darwin arm64 + amd64)
	@mkdir -p bin
	@for arch in arm64 amd64; do \
		echo "Building romp-darwin-$$arch..."; \
		GOARCH=$$arch go build $(LDFLAGS) -o bin/romp ./cmd/romp/; \
		chmod +x bin/romp; \
		tar -czf bin/romp-darwin-$$arch.tar.gz -C bin romp; \
		rm bin/romp; \
	done
	@echo "Built bin/romp-darwin-{arm64,amd64}.tar.gz"

clean: ## Remove built binaries
	rm -rf bin/

help: ## Show this help
	@grep -E '^[a-zA-Z_-]+:.*?## .*$$' $(MAKEFILE_LIST) | sort | awk 'BEGIN {FS = ":.*?## "}; {printf "\033[36m%-10s\033[0m %s\n", $$1, $$2}'

.DEFAULT_GOAL := help
