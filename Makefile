# Hyperspace Makefile
# Run `make help` to see all targets.

.PHONY: help test test-cover bench lint vuln build build-hsd build-stat run-hsd docker-build \
        harness-init harness-status clean

.DEFAULT_GOAL := help

# ============================================================
# Help (self-documenting)
# ============================================================

help: ## Print this help
	@grep -E '^[a-zA-Z_-]+:.*?## .*$$' $(MAKEFILE_LIST) | \
		awk 'BEGIN {FS = ":.*?## "}; {printf "\033[36m%-20s\033[0m %s\n", $$1, $$2}'

# ============================================================
# Testing
# ============================================================

test: ## go test -race ./...
	go test -race ./...

test-cover: ## go test -race -coverprofile=coverage.out ./... && go tool cover -func=coverage.out
	go test -race -coverprofile=coverage.out ./...
	go tool cover -func=coverage.out

bench: ## go test -bench=. -benchmem ./...
	go test -bench=. -benchmem ./...

# ============================================================
# Code Quality
# ============================================================

lint: ## golangci-lint run ./...
	golangci-lint run ./...

vuln: ## govulncheck ./...
	govulncheck ./...

# ============================================================
# Build
# ============================================================

build: build-hsd build-stat ## Build all binaries

build-hsd: ## Build hsd daemon
	go build -o hsd ./cmd/hsd/

build-stat: ## Build hyperspace-stat CLI
	go build -o hyperspace-stat ./cmd/hyperspace-stat/

# ============================================================
# Run
# ============================================================

run-hsd: ## Run hsd daemon (embedded mode for local dev)
	go run ./cmd/hsd/

# ============================================================
# Docker
# ============================================================

docker-build: ## Build Docker image for hsd
	docker build -t hyperspace-hsd:local .

# ============================================================
# Harness
# ============================================================

harness-init: ## Run harness initialisation (first session setup)
	@bash scripts/init.sh

harness-status: ## Print current harness state from session_state.json and progress.json
	@echo "=== Session State ==="
	@python3 -m json.tool session_state.json 2>/dev/null || cat session_state.json
	@echo ""
	@echo "=== Progress ==="
	@python3 -m json.tool progress.json 2>/dev/null || cat progress.json

# ============================================================
# Cleanup
# ============================================================

clean: ## Remove build artifacts and temporary files
	rm -f hsd hyperspace-stat hyperspace-probe coverage.out coverage.html
	find . -name "*.tmp" -delete
