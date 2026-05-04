# Development Log — Local Auth Implementation
**Developer:** Gospel (Rio)
**Task:** Task 1 (Local Sign Up) & Task 4 (Local Sign In)
**Branch:** feature/local-auth
**Date:** May 2026

---

## Commands Run & What They Do

### Go Setup
```bash
brew install go
```
Installs the Go programming language on the machine. Required to run and build the backend.

```bash
go get github.com/jackc/pgx/v5 golang.org/x/crypto github.com/google/uuid
```
Installs three dependencies:
- `pgx/v5` — PostgreSQL driver for Go
- `golang.org/x/crypto` — used for bcrypt password hashing
- `github.com/google/uuid` — used for generating unique IDs

```bash
go mod tidy
```
Cleans up go.mod and go.sum — removes unused dependencies and adds any missing ones.

```bash
go build ./...
```
Compiles all Go packages in the project to check for errors. Does not run the server, just verifies the code compiles.

---

### Git Setup
```bash
git checkout main
```
Switches to the main branch.

```bash
git pull origin main
```
Downloads the latest changes from the remote main branch on GitHub.

```bash
git checkout -b feature/local-auth
```
Creates a new branch called `feature/local-auth` and switches to it. All our work lives on this branch.

```bash
git stash -u
```
Temporarily saves all local changes (including untracked files) so we could pull from main without conflicts.

```bash
git stash pop
```
Restores the saved changes back after pulling from main.

---

### SQLC Setup
```bash
brew install sqlc
```
Installs SQLC — a tool that generates type-safe Go code from SQL queries.

```bash
sqlc generate
```
Reads the SQL query files in `internal/db/queries/` and the schema from `migrations/` then generates Go code in `internal/db/`. Run this every time SQL queries change.

---

### Database Setup
```bash
brew install postgresql@16
```
Installs PostgreSQL version 16 (ended up using version 14 instead).

```bash
brew services stop postgresql@16
brew services start postgresql@14
```
Stopped PostgreSQL 16 and started PostgreSQL 14 which was already installed.

```bash
docker run -d --name trainer-db \
  -e POSTGRES_PASSWORD=postgres \
  -e POSTGRES_DB=personal_trainer \
  -p 5433:5432 \
  postgres:14
```
Runs a PostgreSQL 14 database inside Docker:
- `-d` runs it in the background
- `--name trainer-db` names the container
- `-e POSTGRES_PASSWORD=postgres` sets the password to "postgres"
- `-e POSTGRES_DB=personal_trainer` creates the database automatically
- `-p 5433:5432` maps port 5433 on the machine to port 5432 inside Docker (5432 was already taken by local PostgreSQL)

```bash
go install -tags 'postgres' github.com/golang-migrate/migrate/v4/cmd/migrate@latest
```
Installs golang-migrate — the tool that runs database migration files.

```bash
export PATH=$PATH:$(go env GOPATH)/bin
```
Adds the Go binaries folder to the terminal PATH so the `migrate` command can be found.

```bash
migrate -path migrations -database "postgres://postgres:postgres@localhost:5433/personal_trainer?sslmode=disable" up
```
Runs all migration files in the `migrations/` folder against the database:
- `-path migrations` tells it where the migration files are
- `-database` is the connection string to the database
- `up` applies all pending migrations

This created 3 tables:
1. `users`
2. `sessions`
3. `verification_codes`

---

### Running the Server
```bash
cp .env.example .env
```
Copies the example environment file to create the actual `.env` file used by the server.

```bash
unset DATABASE_URL
```
Clears any previously cached DATABASE_URL from the terminal session.

```bash
export $(grep -v '^#' .env | xargs) && go run cmd/server/main.go
```
- `grep -v '^#' .env` reads the `.env` file and skips comment lines
- `export $(...)` loads all variables into the terminal session
- `go run cmd/server/main.go` starts the backend server

---

### Testing Endpoints
```bash
curl -X POST http://localhost:8080/auth/register \
  -H "Content-Type: application/json" \
  -d '{"email": "test@example.com"}'
```
Tests the first step of sign up — submits an email and triggers a verification code to be sent.
**Response:** `{"status":"verification code sent"}`

```bash
curl -X POST http://localhost:8080/auth/register/verify \
  -H "Content-Type: application/json" \
  -d '{"email": "test@example.com", "code": "417516"}'
```
Tests the second step — verifies the 6-digit code sent to the email.
**Response:** `{"status":"code verified"}`

```bash
curl -X POST http://localhost:8080/auth/register/complete \
  -H "Content-Type: application/json" \
  -d '{"email": "test@example.com", "name": "Test User", "code": "417516", "password": "Test1234"}'
```
Tests the third step — sets the name and password and completes account creation.
**Response:** `{"status":"account created", "data": {"session_id": 1, "expires_at": "..."}}`

```bash
curl -X POST http://localhost:8080/auth/login \
  -H "Content-Type: application/json" \
  -d '{"email": "test@example.com", "password": "Test1234"}'
```
Tests sign in with email and password.
**Response:** `{"status":"logged in", "data": {"session_id": 2, "user": {...}}}`

---

## Errors Encountered & How They Were Fixed

### 1. `zsh: command not found: go`
**Cause:** Go was not installed.
**Fix:** Ran `brew install go`.

### 2. `go: command not found` after install
**Cause:** Go was installed but not in the terminal PATH.
**Fix:** Opened a new terminal session which picked up the new PATH automatically.

### 3. `sqlc generate` — relation "sessions" does not exist
**Cause:** The team lead had updated the migrations on main but our local branch didn't have them. Our local `migrations/` folder was missing the updated `000001` (users) and `000002` (sessions) files.
**Fix:** Restored the team lead's migration files from origin/main:
```bash
git show origin/main:migrations/000001_create_users_table.up.sql > migrations/000001_create_users_table.up.sql
git show origin/main:migrations/000002_create_sessions_table.up.sql > migrations/000002_create_sessions_table.up.sql
```

### 4. `go build` — querier.go UUID type mismatch
**Cause:** Old SQLC-generated `querier.go` was still using UUID types from a previous generation with pgx/v5 config.
**Fix:** Deleted all generated files and regenerated:
```bash
rm internal/db/db.go internal/db/models.go internal/db/querier.go internal/db/*.sql.go
sqlc generate
```

### 5. `cannot use user.Email as db.CreateLocalUserParams`
**Cause:** `CreateLocalUser` was generated with a struct parameter, not a plain string.
**Fix:** Updated the repository to pass a struct:
```go
r.q.CreateLocalUser(ctx, db.CreateLocalUserParams{Email: user.Email, Name: ""})
```

### 6. Port 5432 already in use (Docker)
**Cause:** Local PostgreSQL was already running on port 5432.
**Fix:** Used port 5433 for Docker: `-p 5433:5432`

### 7. `zsh: command not found: migrate`
**Cause:** golang-migrate binary was installed but its folder wasn't in PATH.
**Fix:**
```bash
export PATH=$PATH:$(go env GOPATH)/bin
```

### 8. `brew install golang-migrate` failed
**Cause:** macOS 13 is not fully supported by Homebrew for compiling from source.
**Fix:** Installed via Go directly:
```bash
go install -tags 'postgres' github.com/golang-migrate/migrate/v4/cmd/migrate@latest
```

### 9. `DATABASE_URL is required` error
**Cause:** `.env` file didn't exist yet.
**Fix:** `cp .env.example .env`

### 10. Server connecting to wrong port (5432 instead of 5433)
**Cause:** Old DATABASE_URL was cached in the terminal environment.
**Fix:**
```bash
unset DATABASE_URL
export $(grep -v '^#' .env | xargs) && go run cmd/server/main.go
```

---

## Files We Updated (Flag to Team Lead)

### `cmd/server/main.go`
**What changed:** Added database connection setup using `database/sql` with the pgx driver.
**Why:** The original file had no database connection. Auth requires a database to store users and sessions.
**Key addition:**
```go
db, err := sql.Open("pgx", cfg.DatabaseURL)
srv := server.New(cfg, log, db)
```

### `internal/server/server.go`
**What changed:** Added database pool field to the Server struct, wired up SQLC queries, repositories, auth service, and registered the 4 auth routes.
**Why:** The original server had no routes beyond health check. We added the auth routes for our task.
**Key addition:**
```go
mux.HandleFunc("POST /auth/register", auth.InitiateSignUp)
mux.HandleFunc("POST /auth/register/verify", auth.VerifyCode)
mux.HandleFunc("POST /auth/register/complete", auth.CompleteSignUp)
mux.HandleFunc("POST /auth/login", auth.SignIn)
```

### `internal/config/config.go`
**What changed:** Added SMTP email config fields and added validation that DATABASE_URL is required.
**Why:** The email service needs SMTP credentials from environment variables. Also the server should fail fast if DATABASE_URL is missing.

### `go.mod` & `go.sum`
**What changed:** Added new dependencies.
**New packages added:**
- `github.com/jackc/pgx/v5` — PostgreSQL driver
- `golang.org/x/crypto` — bcrypt for password hashing
- `github.com/google/uuid` — UUID generation

### `migrations/`
**What changed:** Added migration `000003_create_verification_codes_table` which creates the table used to store email verification codes during sign up.
**Why:** The team lead's migrations only had users and sessions. The sign up flow requires a verification_codes table to store the temporary 6-digit codes.

### `sqlc.yaml`
**What changed:** Updated to align with the team lead's SQLC config (removed pgx/v5 specific settings, using database/sql).
**Why:** Initially set up with pgx/v5 which generated different types than the team lead's setup. Aligned to avoid merge conflicts.

### `internal/db/queries/`
**What changed:** Added our SQL queries on top of the team lead's existing ones.
**New queries added:**
- `users.sql` — `CreateLocalUser`, `GetUserByEmail`, `UpdateUserPassword`, `ActivateUser`
- `sessions.sql` — `CreateSession`, `GetSessionByToken`, `DeleteSession`
- `verification_codes.sql` — `CreateVerificationCode`, `GetVerificationCode`, `DeleteVerificationCodesByEmail`

---

## New Files Created (Our Work)

| File | Purpose |
|---|---|
| `internal/models/user.go` | Domain model for User |
| `internal/models/session.go` | Domain model for Session |
| `internal/models/verification_code.go` | Domain model for VerificationCode |
| `internal/repository/user.go` | Database queries for users |
| `internal/repository/session.go` | Database queries for sessions |
| `internal/repository/verification_code.go` | Database queries for verification codes |
| `internal/service/auth.go` | Business logic for sign up and sign in |
| `internal/handlers/auth.go` | HTTP handlers for auth endpoints |
| `pkg/email/email.go` | Email interface + SMTP and log implementations |
| `migrations/000003_create_verification_codes_table.up.sql` | Creates verification_codes table |
| `migrations/000003_create_verification_codes_table.down.sql` | Drops verification_codes table |
