# [PROJECT_NAME] Makefile
# Self-documenting: run `make help` to see all targets.

.PHONY: help test lint build run docker-build docker-run migrate seed clean \
        test-coverage test-race init check-env

# Default target
.DEFAULT_GOAL := help

# ============================================================
# Help (self-documenting)
# ============================================================

help: ## Print all available targets with descriptions
	@grep -E '^[a-zA-Z_-]+:.*?## .*$$' $(MAKEFILE_LIST) | \
		awk 'BEGIN {FS = ":.*?## "}; {printf "\033[36m%-20s\033[0m %s\n", $$1, $$2}'

# ============================================================
# Development
# ============================================================

run: check-env ## Run the service locally
	# Replace with your run command, e.g.:
	# go run ./cmd/server
	# python manage.py runserver
	# npm run dev
	@echo "Replace this target with your run command"

init: ## Initialise the project (first-time setup)
	cp -n .env.example .env || true
	@echo "Copied .env.example to .env — fill in your values before running"
	# Add any other first-time setup: npm install, go mod download, pip install, etc.

check-env: ## Verify required environment variables are set
	@test -f .env || (echo "ERROR: .env not found. Run: cp .env.example .env" && exit 1)

# ============================================================
# Testing
# ============================================================

test: ## Run the full test suite
	# Replace with your test command, e.g.:
	# go test ./... -count=1
	# pytest
	# npm test
	@echo "Replace this target with your test command"

test-race: ## Run tests with race detector (Go projects)
	# go test ./... -count=1 -race
	@echo "Replace this target with your race test command"

test-coverage: ## Run tests and generate coverage report
	# go test ./... -coverprofile=coverage.out && go tool cover -html=coverage.out
	# pytest --cov=. --cov-report=html
	@echo "Replace this target with your coverage command"

test-scenario: ## Run holdout scenario suite (Evaluator use only)
	@test -d scenarios || (echo "ERROR: scenarios/ directory not found" && exit 1)
	# python scripts/run_scenarios.py
	@echo "Replace this target with your scenario runner command"

# ============================================================
# Code Quality
# ============================================================

lint: ## Run linters and formatters
	# Go: golangci-lint run
	# Python: ruff check . && ruff format --check .
	# Node: eslint . && prettier --check .
	@echo "Replace this target with your lint command"

lint-fix: ## Run linters and auto-fix where possible
	# Go: golangci-lint run --fix && gofmt -w .
	# Python: ruff check --fix . && ruff format .
	# Node: eslint --fix . && prettier --write .
	@echo "Replace this target with your lint-fix command"

vuln-scan: ## Scan dependencies for vulnerabilities
	# Go: govulncheck ./...
	# Python: pip-audit
	# Node: npm audit
	@echo "Replace this target with your vulnerability scan command"

# ============================================================
# Build
# ============================================================

build: ## Build the service binary / bundle
	# Go: go build -o bin/server ./cmd/server
	# Python: (no build step usually needed)
	# Node: npm run build
	@echo "Replace this target with your build command"

build-lambda: ## Build Lambda deployment package (if applicable)
	# GOOS=linux GOARCH=arm64 go build -o bootstrap ./cmd/lambda
	# zip -j function.zip bootstrap
	@echo "Replace this target if this project deploys to Lambda"

# ============================================================
# Docker
# ============================================================

docker-build: ## Build Docker image
	docker build -t [PROJECT_NAME]:local .

docker-run: ## Run service and all dependencies via Docker Compose
	docker compose up -d
	@echo "Service running. Check docker compose logs -f for output."

docker-stop: ## Stop all Docker Compose services
	docker compose down

docker-logs: ## Tail Docker Compose logs
	docker compose logs -f

docker-clean: ## Remove Docker image and volumes
	docker compose down -v
	docker rmi [PROJECT_NAME]:local 2>/dev/null || true

# ============================================================
# Database
# ============================================================

migrate: ## Run database migrations
	# Go (golang-migrate): migrate -path db/migrations -database "$$DATABASE_URL" up
	# Django: python manage.py migrate
	# Prisma: npx prisma migrate deploy
	@echo "Replace this target with your migration command"

migrate-create: ## Create a new migration (NAME=migration_name required)
	@test -n "$(NAME)" || (echo "ERROR: NAME is required. Usage: make migrate-create NAME=add_users_table" && exit 1)
	# migrate create -ext sql -dir db/migrations -seq $(NAME)
	@echo "Replace this target with your migration create command"

migrate-down: ## Rollback last migration
	@echo "Replace this target with your rollback command"

seed: ## Seed the development database with test data
	@echo "Replace this target with your seed command"

# ============================================================
# Harness
# ============================================================

harness-init: ## Run harness initialisation (first session setup)
	@bash scripts/init.sh

harness-status: ## Print current harness state from session_state.json and progress.json
	@echo "=== Session State ==="
	@cat session_state.json | python3 -m json.tool 2>/dev/null || cat session_state.json
	@echo ""
	@echo "=== Progress ==="
	@cat progress.json | python3 -m json.tool 2>/dev/null || cat progress.json

# ============================================================
# Cleanup
# ============================================================

clean: ## Remove build artifacts and temporary files
	rm -rf bin/ dist/ build/ out/
	rm -f coverage.out coverage.html
	find . -name "*.tmp" -delete
	find . -name "*.log" -delete
