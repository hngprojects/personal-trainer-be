.PHONY: help run build test test-cover lint fmt tidy clean \
        install-tools sqlc \
        migrate-up migrate-down migrate-create \
        migrate-version migrate-force migrate-drop

# ----------------------------------------------------------------------
# Variables
# ----------------------------------------------------------------------
BINARY     := bin/server
PKG        := ./...
MIGRATIONS := migrations
DB_URL     ?= $(DATABASE_URL)

MIGRATE_VERSION := v4.17.1

MIGRATE ?= $(shell command -v migrate 2>/dev/null || echo $(shell go env GOPATH 2>/dev/null)/bin/migrate)

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
install-tools: ## Install golang-migrate CLI
	go install -tags 'postgres' github.com/golang-migrate/migrate/v4/cmd/migrate@$(MIGRATE_VERSION)

# ----------------------------------------------------------------------
# Database migrations (golang-migrate)
# ----------------------------------------------------------------------
_check-migrate:
	@if [ ! -x "$(MIGRATE)" ]; then \
	  echo "ERROR: migrate CLI not found. Run 'make install-tools' first."; \
	  exit 1; \
	fi

_check-db: _check-migrate
	@if [ -z "$(DB_URL)" ]; then \
	  echo "ERROR: DATABASE_URL is not set. Copy .env.example to .env and export it."; \
	  exit 1; \
	fi

migrate-up: _check-db ## Apply all pending migrations
	$(MIGRATE) -path $(MIGRATIONS) -database "$(DB_URL)" up

migrate-down: _check-db ## Rollback the most recent migration
	$(MIGRATE) -path $(MIGRATIONS) -database "$(DB_URL)" down 1

migrate-create: _check-migrate ## Create new migration files: make migrate-create NAME=add_trainers
	@if [ -z "$(NAME)" ]; then \
	  echo "ERROR: NAME is required, e.g. make migrate-create NAME=add_trainers"; \
	  exit 1; \
	fi
	$(MIGRATE) create -ext sql -dir $(MIGRATIONS) -seq $(NAME)

migrate-version: _check-db ## Print current migration version
	$(MIGRATE) -path $(MIGRATIONS) -database "$(DB_URL)" version

migrate-force: _check-db ## Force a version to fix a dirty state: make migrate-force VERSION=1
	@if [ -z "$(VERSION)" ]; then \
	  echo "ERROR: VERSION is required, e.g. make migrate-force VERSION=1"; \
	  exit 1; \
	fi
	$(MIGRATE) -path $(MIGRATIONS) -database "$(DB_URL)" force $(VERSION)

migrate-drop: _check-db ## Drop EVERYTHING in the database (destructive!)
	$(MIGRATE) -path $(MIGRATIONS) -database "$(DB_URL)" drop -f
