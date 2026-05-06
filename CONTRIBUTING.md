- use make's `install-tools`
- check `.env.example` to setup environment variables
-
- use make's `code-gen` to generate boilerplate for routes
- routes any feature being added (routes) should use `make codegen`
- new features should include tests
- all PRs should be made to the dev branch.
- use the github commit message styles feat: whatever
- go models should use `make sqlc`
- if there are issues, feel free to reach out to the lead dev, any of your teammates or any mentor

# Contributing Guide

This document outlines how to get started with the codebase and the required development workflow for contributors.

---

## 1. Project Setup

Before running the project, install all required development tools:

```bash
make install-tools
```

Claim offer
how do teams usually setup goose commands in makefile?

Most teams treat Goose like a thin CLI wrapper and standardize it through a Makefile so nobody remembers long commands or environment flags.

Here’s how it’s usually set up in real Go projects.

1. Typical folder + config layout
   Most common setup:

/db/migrations
00001_init.sql
00002_add_users.sql

/makefile
/goose.yml (or config embedded in Makefile/env vars)
Teams usually don’t rely heavily on goose.yml; instead they pass flags via Makefile.

2. Environment variables (almost always used)
   Instead of hardcoding DB URLs:

DATABASE_URL=postgres://user:pass@localhost:5432/app?sslmode=disable 3. Standard Makefile pattern
This is what you’ll see in many Go repos:

DB_URL=postgres://user:password@localhost:5432/app?sslmode=disable
MIGRATIONS_DIR=./db/migrations

GOOSE=goose -dir $(MIGRATIONS_DIR) postgres "$(DB_URL)"
Then expose simple commands:

Run migrations
migrate-up:
$(GOOSE) up
Rollback one step
migrate-down:
	$(GOOSE) down
Create a new migration
migrate-create:
	@read -p "Name: " name; \
	goose -dir $(MIGRATIONS_DIR) create $$name sql
Check status
migrate-status:
$(GOOSE) status 4. More “production-style” setup (cleaner)
Teams often separate DB connection:

include .env
export

GOOSE=goose -dir $(MIGRATIONS_DIR) postgres "$(DATABASE_URL)"
So you don’t repeat URLs everywhere.

5. Some teams wrap DB driver too
   Instead of hardcoding postgres, they do:

DB_DRIVER=postgres

GOOSE=goose -dir $(MIGRATIONS_DIR) $(DB_DRIVER) "$(DATABASE_URL)" 6. Why Makefile instead of raw goose?
Because it gives:

consistent commands across team

no remembering long CLI flags

CI can just run make migrate-up

easier onboarding

7. Common CI usage
   In GitHub Actions or similar:

make migrate-up
or

make migrate-status 8. Real-world pattern summary
Most teams end up with only these commands:

make migrate-up

make migrate-down

make migrate-status

make migrate-create

That’s it. Everything else is hidden.

If you want, I can show you a clean production-grade Makefile template that also handles:

multiple environments (dev/staging/prod)

Docker-based DB migrations

CI safety checks (prevent accidental down migrations)

make keyword works in normal cli but not vscode cli, why?

This is almost always a PATH/environment mismatch between VSCode’s terminal and your system terminal.

make works in one place but not VSCode because VSCode is not seeing the same environment variables.

Here’s what’s actually happening.

1. VSCode terminal vs system terminal use different PATHs
   Your normal terminal (CMD / PowerShell / Git Bash) → has make in PATH

VSCode terminal → might not have that PATH loaded

So VSCode literally cannot find make.exe.

2. Most common causes on Windows
   ✅ A. You installed Make after opening VSCode
   VSCode does NOT auto-refresh PATH.

Fix:

Close VSCode completely

Reopen it

✅ B. Make is installed but not in system PATH
Check in VSCode terminal:

where make
If nothing shows → PATH problem.

C. You installed "Make for Windows" but it’s not globally exposed
Some installs put it here:

C:\Program Files\GnuWin32\bin
or

C:\mingw64\bin
If that folder is not in PATH → VSCode won’t see it.

3. Fix PATH properly (most important step)
   Add Make to System PATH, not just user PATH.

Steps:
Open:

System Properties → Environment Variables
Under System variables:

Find Path

Click Edit

Add folder containing make.exe

Example:

C:\Program Files\GnuWin32\bin
Restart:

VSCode

Terminal

4. Confirm inside VSCode
   Run:

make --version
If it works → fixed.

5. Another hidden cause: VSCode terminal type
   Check bottom right / terminal profile:

PowerShell ❌ sometimes misses tools

CMD ✔ usually consistent

Git Bash ✔ best for Unix tools

Try switching terminal:

Terminal → Select Default Profile → Git Bash or CMD 6. Rare case: WSL confusion
If you're using WSL:

make works in WSL terminal

but not Windows terminal (VSCode default)

Fix:

install make in WSL OR

use WSL terminal inside VSCode

Quick diagnosis flow
If you want to pinpoint it fast:

where make
❌ nothing → PATH issue

✔ path shown → terminal mismatch or VSCode restart needed

If you want, tell me how you installed make (GnuWin32, chocolatey, mingw, WSL), and I’ll give you the exact fix for your setup.

how to actually restart vscode

There are a few levels of “restart VSCode”, and people often do the wrong one.

1. Proper full restart (what you usually need)
   Windows:
   Close all VSCode windows

Make sure it’s not still running in background:

Open Task Manager

End task for Code.exe

Reopen VSCode

This ensures PATH changes are picked up.

2. Quick restart inside VSCode (not always enough)
   Use Command Palette:

Ctrl + Shift + P
Then run:

Developer: Reload Window
This reloads the UI, but does NOT fully refresh environment variables reliably.

3. Restart terminal only (least effective for your case)
   If you only want to refresh shell:

Click trash icon in terminal

Open new terminal

or:

Ctrl + Shift + `
This still won’t fix PATH issues if VSCode itself hasn’t been restarted.

4. When you MUST do full restart
   You specifically need full restart when:

You installed make, go, node, etc.

You changed system PATH

A CLI works outside VSCode but not inside

That’s your case.

5. Quick rule
   Terminal reload → refresh shell

Window reload → refresh UI

Full close/reopen → refresh environment variables (PATH)

If make still doesn’t work after a full restart, the issue is definitely PATH—not VSCode.

.PHONY: help run build test test-cover lint fmt tidy clean \
 install-tools sqlc \
 migrate-up migrate-down migrate-create \
 migrate-version migrate-force migrate-drop

# ----------------------------------------------------------------------

# Variables

# ----------------------------------------------------------------------

BINARY := bin/server
PKG := ./...
MIGRATIONS := migrations
DB_URL ?= $(DATABASE_URL)

MIGRATE_VERSION := v4.17.1

MIGRATE ?= $(shell command -v migrate 2>/dev/null || echo $(shell go env GOPATH 2>/dev/null)/bin/migrate)

export CGO_ENABLED ?= 0

# ----------------------------------------------------------------------

# Help

# ----------------------------------------------------------------------

help: ## Show this help
@grep -E '^[a-zA-Z_-]+:._?## ._$$' $(MAKEFILE_LIST) | \
 awk 'BEGIN {FS = ":.\*?## "}; {printf " \033[36m%-20s\033[0m %s\n", $$1, $$2}'

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

\_check-migrate:
@if [ ! -x "$(MIGRATE)" ]; then \
 echo "ERROR: migrate CLI not found. Run 'make install-tools' first."; \
 exit 1; \
 fi

\_check-db: \_check-migrate
@if [ -z "$(DB_URL)" ]; then \
 echo "ERROR: DATABASE_URL is not set. Copy .env.example to .env and export it."; \
 exit 1; \
 fi

migrate-up: \_check-db ## Apply all pending migrations
$(MIGRATE) -path $(MIGRATIONS) -database "$(DB_URL)" up

migrate-down: \_check-db ## Rollback the most recent migration
$(MIGRATE) -path $(MIGRATIONS) -database "$(DB_URL)" down 1

migrate-create: \_check-migrate ## Create new migration files: make migrate-create NAME=add_trainers
@if [ -z "$(NAME)" ]; then \
 echo "ERROR: NAME is required, e.g. make migrate-create NAME=add_trainers"; \
 exit 1; \
 fi
$(MIGRATE) create -ext sql -dir $(MIGRATIONS) -seq $(NAME)

migrate-version: \_check-db ## Print current migration version
$(MIGRATE) -path $(MIGRATIONS) -database "$(DB_URL)" version

migrate-force: \_check-db ## Force a version to fix a dirty state: make migrate-force VERSION=1
@if [ -z "$(VERSION)" ]; then \
 echo "ERROR: VERSION is required, e.g. make migrate-force VERSION=1"; \
 exit 1; \
 fi
$(MIGRATE) -path $(MIGRATIONS) -database "$(DB_URL)" force $(VERSION)

migrate-drop: \_check-db ## Drop EVERYTHING in the database (destructive!)
$(MIGRATE) -path $(MIGRATIONS) -database "$(DB_URL)" drop -f

codegen:
oapi-codegen -config oapi-codegen.yaml api.yaml

explain how this works, but only the migration parts. What are the steps to it working

Here’s how the migration part of your Makefile actually works, step by step, without fluff.

1. What tool you are using
   You’re using:

golang-migrate (migrate CLI)
This is a binary tool that:

reads SQL files in a folder (migrations/)

applies them in order

tracks applied versions in your database

2. What this line is doing
   MIGRATIONS := migrations
   DB_URL ?= $(DATABASE_URL)
   Meaning:
   MIGRATIONS → folder where your .sql migration files live

DB_URL → database connection string from environment variable

So everything depends on:

DATABASE_URL=postgres://... 3. How Make finds the migrate CLI
MIGRATE ?= $(shell command -v migrate 2>/dev/null || echo $(shell go env GOPATH 2>/dev/null)/bin/migrate)
This means:
Try to find migrate in your system PATH:

command -v migrate
If not found:

fall back to:

$(go env GOPATH)/bin/migrate
👉 So Make always tries to locate the binary automatically.

4. The safety checks (VERY important)
   Check 1: migrate exists
   \_check-migrate:
   @if [ ! -x "$(MIGRATE)" ]; then \
    echo "ERROR: migrate CLI not found. Run 'make install-tools' first."; \
    exit 1; \
    fi
   What this does:
   Before any migration command runs:

it checks if migrate exists

if not → stops immediately

Check 2: DB URL exists
\_check-db: \_check-migrate
@if [ -z "$(DB_URL)" ]; then \
 echo "ERROR: DATABASE_URL is not set..."; \
 exit 1; \
 fi
What this does:
ensures migration tool exists

ensures database connection string exists

prevents accidental broken runs

5. Install step (first thing you must run)
   install-tools:
   go install -tags 'postgres' github.com/golang-migrate/migrate/v4/cmd/migrate@v4.17.1
   What happens:
   This installs:

migrate CLI binary → $GOPATH/bin/migrate
So after this:

migrate -version
should work.

6. Creating migration files
   migrate-create:
   $(MIGRATE) create -ext sql -dir $(MIGRATIONS) -seq $(NAME)
   Command you run:
   make migrate-create NAME=add_users
   What it does:
   Creates files like:

migrations/
000001_add_users.up.sql
000001_add_users.down.sql
.up.sql → apply changes

.down.sql → rollback changes

7. Running migrations (core operation)
   Apply migrations
   migrate-up:
   $(MIGRATE) -path $(MIGRATIONS) -database "$(DB_URL)" up
   What happens:
   Connects to DB using DB_URL

Reads all migration files in migrations/

Checks what has NOT been applied

Runs missing .up.sql files in order

Records versions in DB table:

schema_migrations
Rollback 1 migration
migrate-down:
$(MIGRATE) -path $(MIGRATIONS) -database "$(DB_URL)" down 1
What happens:
finds latest applied migration

runs its .down.sql

removes version from tracking table

8. Checking current version
   migrate-version:
   $(MIGRATE) -path $(MIGRATIONS) -database "$(DB_URL)" version
   Output example:
   12
   Meaning:

migration version 12 is currently applied

9. Fixing broken migration state (force)
   migrate-force:
   $(MIGRATE) ... force $(VERSION)
   When used:
   Only when DB is stuck in “dirty state”.

Example:

make migrate-force VERSION=5
What it does:
manually sets migration version in DB

does NOT run SQL files

just fixes tracking state

10. Dropping database (dangerous)
    migrate-drop:
    $(MIGRATE) ... drop -f
    What it does:
    deletes ALL tables

wipes schema completely

bypasses confirmation due to -f

11. Full execution flow (how everything works together)
    Step-by-step lifecycle:
    Step 1 — Install tool
    make install-tools
    → installs migrate binary

Step 2 — Set environment
DATABASE_URL=postgres://...
Step 3 — Create migrations
make migrate-create NAME=create_users
→ generates .up.sql + .down.sql

Step 4 — Write SQL
You manually edit:

CREATE TABLE users (...);
Step 5 — Apply migrations
make migrate-up
→ runs pending SQL in order

Step 6 — Rollback if needed
make migrate-down 12. Mental model (important)
Think of it like this:

migrations/ = source of truth

DB table = memory of what has been applied

migrate CLI = engine that compares both

If you want, I can break down:

how schema_migrations table works internally

or how this compares to Goose (since you asked earlier)

or how sqlc + migrations + API layer fit together in a real system architecture

how would i set this up for goose?

Switching this Makefile setup from golang-migrate → Goose is pretty straightforward, but the mental model changes slightly:

golang-migrate = versioned up/down execution engine

Goose = migration runner with a simpler CLI + state tracking table (goose_db_version)

1. Install Goose
   Replace your install step:

install-tools:
go install github.com/pressly/goose/v3/cmd/goose@latest
That’s it. No tags needed like migrate.

2. Replace core variables
   You can simplify your Makefile a bit:

MIGRATIONS := migrations
DB_URL ?= $(DATABASE_URL)

GOOSE := goose -dir $(MIGRATIONS) postgres "$(DB_URL)" 3. Important difference vs migrate
Goose does NOT need:

-ext sql

-seq

separate binary flags per command

Instead:

file naming controls order

state tracked automatically in DB

4. Migration file format changes
   OLD (migrate):
   000001_create_users.up.sql
   000001_create_users.down.sql
   NEW (goose):
   Goose uses single file with annotations OR separate SQL files depending on style.

Most common modern approach (recommended):
migrations/
202601011200_create_users.sql
Inside file:

-- +goose Up
CREATE TABLE users (
id SERIAL PRIMARY KEY,
name TEXT NOT NULL
);

-- +goose Down
DROP TABLE users;
👉 This is the standard Goose pattern.

5. Replace Makefile commands
   Check DB dependency (same idea as before)
   \_check-db:
   @if [ -z "$(DB_URL)" ]; then \
    echo "ERROR: DATABASE_URL not set"; \
    exit 1; \
    fi
6. Migration commands (Goose version)
   UP migrations
   migrate-up: \_check-db
   $(GOOSE) up
   DOWN (1 step rollback)
   migrate-down: \_check-db
   $(GOOSE) down
   UP to a specific version
   migrate-version: \_check-db
   $(GOOSE) status
   or:

migrate-to: \_check-db
$(GOOSE) up-to $(VERSION)
Create migration
Goose is simpler:

migrate-create:
@if [ -z "$(NAME)" ]; then \
 echo "ERROR: NAME required"; exit 1; \
 fi
goose -dir $(MIGRATIONS) create $(NAME) sql
This generates:

migrations/
20260506120000_add_users.sql 7. Optional: reset / reset database
migrate-reset: \_check-db
$(GOOSE) reset
⚠️ Drops everything and re-runs migrations.

8. Full Goose Makefile migration section
   Here’s the clean version teams actually use:

MIGRATIONS := migrations
DB_URL ?= $(DATABASE_URL)

GOOSE := goose -dir $(MIGRATIONS) postgres "$(DB_URL)"

\_check-db:
@if [ -z "$(DB_URL)" ]; then \
 echo "DATABASE_URL not set"; exit 1; \
 fi

migrate-up: \_check-db
$(GOOSE) up

migrate-down: \_check-db
$(GOOSE) down

migrate-status: \_check-db
$(GOOSE) status

migrate-create:
@if [ -z "$(NAME)" ]; then \
 echo "NAME required"; exit 1; \
 fi
$(GOOSE) create $(NAME) sql

migrate-reset: \_check-db
$(GOOSE) reset 9. How Goose actually works internally
When you run:

make migrate-up
Goose does this:

Step 1: Connect DB
Uses your DATABASE_URL

Step 2: Checks tracking table
Creates automatically:

goose_db_version
Stores:

version number

applied_at timestamp

Step 3: Reads migration folder
Sorts files by:

timestamp prefix OR

internal version ordering

Step 4: Applies pending migrations
Executes:

-- +goose Up
blocks only

Step 5: Updates DB state
Records version in goose_db_version

10. Key differences vs migrate (important)
    Feature migrate goose
    File format up/down separate files single file with annotations
    Tracking table schema_migrations goose_db_version
    Commands verbose flags simple commands
    Rollback explicit down 1 down (or version-based)
    Creation CLI generates pairs CLI generates single file
11. What most teams prefer (real-world)
    Goose is usually chosen when:

you want simpler workflow

fewer CLI flags

Go-native feel

faster iteration

migrate is chosen when:

strict version control systems

complex multi-step rollback strategies

12. If you want a clean modern setup
    A very common production combo is:

goose → migrations

sqlc → query generation

oapi-codegen → API types

Makefile → orchestration

If you want, I can convert your entire Makefile (not just migrations) into a clean Goose + sqlc + oapi-codegen production template so everything matches properly.

the make install-tools command, is that usually for just one cli tool?

No — it’s not limited to one tool. In real projects, install-tools is just a convenience target for installing all dev dependencies, not a rule about “one CLI only”.

1. What install-tools actually is
   It’s just a Makefile convention for:

“Install everything this project needs to work (but isn’t part of the Go module build).”

So it often includes multiple tools.

2. Common real-world pattern
   Instead of one tool:

install-tools:
go install github.com/pressly/goose/v3/cmd/goose@latest
Teams usually do:

install-tools:
go install github.com/pressly/goose/v3/cmd/goose@latest
go install github.com/sqlc-dev/sqlc/cmd/sqlc@latest
go install github.com/oapi-codegen/oapi-codegen/v2/cmd/oapi-codegen@latest
go install github.com/golangci/golangci-lint/cmd/golangci-lint@latest
So one command sets up the entire dev environment.

3. Why teams do it this way
   Because onboarding becomes:

git clone repo
make install-tools
make run
That’s it.

No “install 5 CLIs manually”.

4. What actually belongs in install-tools
   Good rule:

Include:
CLI tools not managed by go.mod

code generators

migration tools

linters / formatters

Don’t include:
your app dependencies (those are in go.mod)

runtime stuff (Postgres, Redis, etc.)

5. Important detail (people miss this)
   go install ...@version installs binaries into:

$(go env GOPATH)/bin
So install-tools implicitly assumes:

GOPATH/bin is in PATH

Otherwise tools won’t be found even after install.

6. Better modern pattern (cleaner teams use this)
   Instead of one big target, they sometimes split:

install-tools: install-goose install-sqlc install-lint

install-goose:
go install github.com/pressly/goose/v3/cmd/goose@latest

install-sqlc:
go install github.com/sqlc-dev/sqlc/cmd/sqlc@latest

install-lint:
go install github.com/golangci/golangci-lint/cmd/golangci-lint@latest
This is cleaner for debugging.

7. Even more advanced (version-pinned dev tools)
   Serious production teams avoid @latest:

GOOSE_VERSION := v3.18.0

install-tools:
go install github.com/pressly/goose/v3/cmd/goose@$(GOOSE_VERSION)
Why:

avoids CI breaking when tool updates

ensures reproducible builds

8. Mental model
   Think of it like:

go mod tidy → installs libraries your code imports

install-tools → installs developer utilities your repo depends on

9. Simple answer
   ✔ No, it’s not “one CLI tool”
   ✔ It’s usually a bundle installer for all dev tools in the project
   ✔ The name is just convention, not a rule

If you want, I can show you a clean production Makefile layout used in backend teams (Go + Goose + sqlc + CI) so you can see how everything fits together in a real system.

MIGRATIONS := migrations

and this part? "migrations" looks like it doesn't point to anything, is it just a string?

Yes — in this line:

MIGRATIONS := migrations
migrations is just a string value, not something special by itself.

1. What it actually means
   This is a Makefile variable assignment:

MIGRATIONS = variable name

migrations = plain text string

So later when you write:

$(MIGRATIONS)
it literally becomes:

migrations 2. So does it “point to something”?
Only indirectly.

It “points” to something by convention, not by magic.

In your project structure, you likely have:

project/
migrations/
000001_init.sql
000002_users.sql
So the string:

migrations
is interpreted as a folder path relative to where you run make.

3. How it is used in your commands
   Example:

GOOSE := goose -dir $(MIGRATIONS) postgres "$(DB_URL)"
becomes:

goose -dir migrations postgres "your_db_url"
So Goose now knows:

“Look inside the migrations/ folder for migration files.”

4. Important concept: Make doesn’t validate paths
   Make does NOT check if:

folder exists

files exist

path is valid

It just substitutes text.

So this:

MIGRATIONS := migrations
is the same as:

MIGRATIONS := ./migrations
or even:

MIGRATIONS := /home/user/project/migrations
depending on how you want to structure it.

5. Why teams use a variable instead of hardcoding
   Instead of writing:

goose -dir migrations up
goose -dir migrations down
they do:

GOOSE := goose -dir $(MIGRATIONS)
Because now you can change it in one place:

MIGRATIONS := db/migrations
and everything still works.

6. Mental model (important)
   Think of it like this:

MIGRATIONS := migrations
means:

“Whenever I say $(MIGRATIONS), replace it with the word 'migrations'”

NOT:

“Go find a special object called migrations”

7. When it actually becomes “real”
   It only becomes meaningful when combined with tools:

Goose → reads folder path

migrate → reads folder path

sqlc → reads folder path

So the tool gives meaning to the string, not Make.

8. One-liner summary
   Yes — it’s just a string.
   It becomes meaningful only because Goose interprets it as a directory path.

If you want, I can break down your full Makefile and show you exactly what every line expands to when you run make migrate-up (that’s where it usually clicks fully).

- use make's install-tools
- check .env.example to setup environment variables
-
- use make's code-gen to generate boilerplate for routes
- routes any feature being added (routes) should use make codegen
- new features should include tests
- all PRs should be made to the dev branch.
- use the github commit message styles feat: whatever
- go models should use make sqlc
- if there are issues, feel free to reach out to the lead dev, any of your teammates or any mentor

From the info here (and this chat we're having) generate a content for the contributing.md. It'll help people the team on how to get started with the codebase.
Generate it in a single markdown

# Contributing Guide

This document outlines how to get started with the codebase and the required development workflow for contributors.

---

## 1. Project Setup

Before running the project, install all required development tools:

```bash
make install-tools
```

This will install all CLI dependencies required for development (e.g., migration tools, sqlc, code generators).

---

## 2. Environment Variables

The project depends on environment variables for configuration.

1. Copy the example environment file:

```bash
# for linux machines
cp .env.example .env
```

2. Update .env with your local configuration values.
   Make sure all required variables are set before running the application.

## 3. Running the project

To start the server:

```bash
make run
```

## 4. Code Generation

### Generate API routes / boilerplate

Whenever new routes or API features are added, run:

```bash
make codegen
```

This ensures all generated boilerplate (e.g., API handlers, types) is up to date.

> ⚠️ Any changes to routes MUST be followed by `make codegen`.

### Generate database models (SQLC)

All database-related models and queries are managed using SQLC.

Run:

```bash
make sqlc
```

This generates Go models and query methods from SQL definitions.

> ⚠️ Do not manually edit generated SQLC files.

## 5. Database Migrations

If working with database schema changes:

- Use migration tooling via Makefile commands (Goose-based or configured migration tool)
- Ensure migrations are created and applied correctly before pushing changes

Example:

```bash
make migrate-create NAME=your_migration_name
make migrate-up
```

## 6. Testing

All new features must include tests.
Run tests with:

``make test`

To generate coverage reports:

`make test-cover`

## 7. Branching Strategy

- All new work should be based off the `dev` branch
- Pull requests must target `dev` (not `main`)

## 8. Commit Message Style

All commits must follow conventional commit style:

```
feat: add user authentication
fix: resolve login bug
refactor: simplify auth middleware
test: add unit tests for user service
```

Format:
`type: short description`

Common types:

- `feat` → new feature
- `fix` → bug fix
- `refactor` → code change that doesn't add features or fix bugs
- `test` → adding or updating tests
- `chore` → maintenance tasks

## 9. Code Quality Rules

- Keep code formatted and clean
- Run linters before pushing if available
- Ensure `make test` passes before submitting a PR
- Keep functions small and readable
- Follow existing project structure

## 10. Pull Requests

Before opening a PR:

- Ensure all tests pass
- Run required generators:
  - `make codegen` (for API changes)
  - `make sqlc` (for DB changes)
- Ensure your branch is up to date with `dev`

All PRs should:

- Target the `dev` branch
- Include a clear description of changes
- Reference related issues if applicable

## 11. Getting Help

If you run into issues:

- Ask the lead developer
- Reach out to teammates
- Or contact a mentor

Collaboration is encouraged don't hesitate to ask questions early.
