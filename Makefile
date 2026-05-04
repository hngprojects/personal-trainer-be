.PHONY: help run build test test-cover lint fmt tidy clean \
        install-tools sqlc \
        migrate-up migrate-down migrate-create \
        migrate-version migrate-status migrate-reset

# Auto-load .env if present (so DATABASE_URL etc. are available without manual export)
-include .env
export

# ----------------------------------------------------------------------
# Variables
# ----------------------------------------------------------------------
BINARY     := bin/server
PKG        := ./...
MIGRATIONS := migrations
DB_URL     ?= $(DATABASE_URL)

GOOSE_VERSION := v3.24.3

GOOSE ?= $(shell command -v goose 2>/dev/null || echo $(shell go env GOPATH 2>/dev/null)/bin/goose)

export CGO_ENABLED ?= 0

# ----------------------------------------------------------------------
# Help
# ----------------------------------------------------------------------
help: ## Show this help
	@grep -E '^[a-zA-Z_-]+:.*?## .*$$' $(MAKEFILE_LIST) | \
	  awk 'BEGIN {FS = ":.*?## "}; {printf "  \033[36m%-20s\033[0m %s\n", $$1, $$2}'

# ----------------------------------------------------------------------
# Build / run
# ----------------------------------------------------------------------
run: ## Run the server
	go run ./cmd/server

build: ## Build binary to bin/server
	mkdir -p bin
	go build -o $(BINARY) ./cmd/server

# ----------------------------------------------------------------------
# Test / quality
# ----------------------------------------------------------------------
test: ## Run tests with race detector + coverage
	go test -race -cover $(PKG)

test-cover: ## Generate HTML coverage report (coverage.html)
	go test -coverprofile=coverage.out $(PKG)
	go tool cover -html=coverage.out -o coverage.html
	@echo "open coverage.html"

lint: ## Run go vet
	go vet $(PKG)

fmt: ## Format the codebase
	gofmt -s -w .

tidy: ## Tidy go.mod
	go mod tidy

clean: ## Remove build / coverage artifacts
	rm -rf bin tmp dist coverage.out coverage.html

# ----------------------------------------------------------------------
# Code generation
# ----------------------------------------------------------------------
sqlc: ## Regenerate sqlc DB layer from SQL files
	sqlc generate

# ----------------------------------------------------------------------
# Tooling
# ----------------------------------------------------------------------
install-tools: ## Install goose CLI
	go install github.com/pressly/goose/v3/cmd/goose@$(GOOSE_VERSION)

# ----------------------------------------------------------------------
# Database migrations (goose)
# ----------------------------------------------------------------------
_check-goose:
	@if [ ! -x "$(GOOSE)" ]; then \
	  echo "ERROR: goose CLI not found. Run 'make install-tools' first."; \
	  exit 1; \
	fi

_check-db: _check-goose
	@if [ -z "$(DB_URL)" ]; then \
	  echo "ERROR: DATABASE_URL is not set. Copy .env.example to .env and export it."; \
	  exit 1; \
	fi

migrate-up: _check-db ## Apply all pending migrations
	$(GOOSE) -dir $(MIGRATIONS) postgres "$(DB_URL)" up

migrate-down: _check-db ## Rollback the most recent migration
	$(GOOSE) -dir $(MIGRATIONS) postgres "$(DB_URL)" down

migrate-create: _check-goose ## Create a new migration file: make migrate-create NAME=add_trainers
	@if [ -z "$(NAME)" ]; then \
	  echo "ERROR: NAME is required, e.g. make migrate-create NAME=add_trainers"; \
	  exit 1; \
	fi
	$(GOOSE) -dir $(MIGRATIONS) -s create $(NAME) sql

migrate-version: _check-db ## Print current migration version
	$(GOOSE) -dir $(MIGRATIONS) postgres "$(DB_URL)" version

migrate-status: _check-db ## Show applied/pending migration status
	$(GOOSE) -dir $(MIGRATIONS) postgres "$(DB_URL)" status

migrate-reset: _check-db ## Rollback ALL migrations (destructive!)
	$(GOOSE) -dir $(MIGRATIONS) postgres "$(DB_URL)" reset
