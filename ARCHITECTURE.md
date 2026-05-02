# Project Name

> A brief one-line description of what this service does.

---

## Table of Contents

- [Architecture](#architecture)
- [Codebase Structure](#codebase-structure)
- [Database Decisions](#database-decisions)
- [API Standards](#api-standards)
- [Authentication & Security](#authentication--security)
- [Code Practices](#code-practices)
- [Getting Started](#getting-started)

---

## Architecture

This service follows a **layered architecture** pattern, separating concerns across distinct layers:

```
HTTP Request → Router → Handler → Service → Repository → Database
```

- **Handler layer** — Decodes requests, validates input, encodes responses. No business logic.
- **Service layer** — Contains all business logic. Orchestrates calls to one or more repositories.
- **Repository layer** — Abstracts all database interactions. Returns domain models, never raw DB types.
- **Domain/Model layer** — Plain Go structs representing core entities. No framework dependencies.

The application is structured as a single deployable binary. Inter-service communication (if any) is done over HTTP/gRPC with clearly defined contracts.

---

## Codebase Structure

```
.
├── cmd/
│   └── server/
│       └── main.go             # Entry point — wires dependencies and starts the server
├── internal/
│   ├── config/                 # Environment config loading (e.g. via envconfig or viper)
│   ├── handlers/                # HTTP handlers (one file per resource group)
│   ├── service/                # Business logic layer
│   ├── repository/             # Database access layer
│   ├── middleware/             # HTTP middleware (auth, logging, recovery, etc.)
│   └── server/                 # HTTP server setup and route registration
├── pkg/                        # Shared, reusable packages (safe to import externally)
├── migrations/                 # SQL migration files (up/down)
├── docs/                       # OpenAPI/Swagger specs and supporting docs
├── .env.example                # Example environment variable file
├── Makefile                    # Common dev commands
├── go.mod
└── go.sum
```

- All application code lives under `internal/` to prevent unintended external imports.
- `pkg/` is reserved for utilities genuinely reusable outside this service (e.g. custom error types, response helpers).
- `cmd/` contains only the wiring and startup logic — keep it thin.

---

## Database Decisions

**Database:** PostgreSQL  
**Driver/Query builder:** [`pgx`](https://github.com/jackc/pgx) (preferred) or [`database/sql`](https://pkg.go.dev/database/sql) with `lib/pq`  
**Migrations:** [`golang-migrate`](https://github.com/golang-migrate/migrate) — migrations are versioned SQL files under `migrations/`

### Conventions

- All schema changes are made via migration files — never edit the database manually in any environment.
- Migrations run automatically on server startup in development; in production they are run explicitly as a pre-deploy step.
- Use transactions for any operation that touches more than one table.
- Avoid ORM frameworks. Raw SQL (or a thin query builder like [`sqlc`](https://sqlc.dev/)) keeps queries explicit, testable, and performant.
- Connection pooling is configured via `pgx`'s pool settings (`MaxConns`, `MinConns`, `MaxConnLifetime`).
- All timestamps are stored as `TIMESTAMPTZ` (UTC). The application never stores local time.
- UUIDs (`uuid_generate_v4()`) are used as primary keys across all tables.

---

## API Standards

This service exposes a **RESTful JSON API**.

### URL Structure

```
/api/{resource}
/api/{resource}/{id}
/api/{resource}/{id}/{sub-resource}

```

- Resources are always **plural nouns** (`/users`, `/orders`).
- Versioning is in the URL path (`/v1/`).
- All paths are **lowercase and hyphen-separated** (`/api/v1/user-profiles`).

### HTTP Methods

| Method   | Usage                     |
| -------- | ------------------------- |
| `GET`    | Read a resource or list   |
| `POST`   | Create a new resource     |
| `PUT`    | Replacement of a resource |
| `DELETE` | Remove a resource         |

### Response Format

Responses return `Content-Type: application/json`.

**Success:**

```json
{
  "status":"",
  "data": { ... },
  "meta": { "page": 1, "per_page": 20, "total": 100 }
}
```

**Error:**

```json
{
  "error": {
    "code": "VALIDATION_FAILED",
    "message": "Email is required.",
    "details": [ ... ]
  }
}
```

### HTTP Status Codes

| Code  | Meaning                        |
| ----- | ------------------------------ |
| `200` | OK                             |
| `201` | Created                        |
| `204` | No Content (successful delete) |
| `400` | Bad Request / Validation error |
| `401` | Unauthenticated                |
| `403` | Forbidden                      |
| `404` | Not Found                      |
| `409` | Conflict                       |
| `422` | Unprocessable Entity           |
| `500` | Internal Server Error          |

- Never return `200` with an error body.
- `500` responses never expose internal error details to the client — log them server-side only.

---

## Authentication & Security

### Authentication Method — Sessions

**Flow:**

1. Client sends credentials to `POST /api/v1/auth/login`.
2. Server validates credentials and creates a **session** with a lifespan of **7 days**.
3. Server stores the session server-side (Database) and returns a secure session ID to the client.
4. Client automatically includes the session ID on all subsequent requests.
5. On each request, the server validates the session ID, retrieves the session, and loads the associated user.
6. Logout deletes the session server-side and clears it on the client.

---

### Session Details

Each session record typically contains:

- `id` (UUID)
- `user_id`
- `created_at`
- `expires_at` (7 days from creation)
- `last_seen_at` (optional, for activity tracking)
- Optional metadata (device, IP address, user agent)

Sessions are stored in a fast key-value store (commonly Redis) or a database.

Expired sessions are cleaned up automatically via background jobs or TTL expiration.

---

## Security Practices

- Passwords are hashed using **bcrypt** (cost factor ≥ 12). Plaintext passwords are never stored or logged.
- All secrets (DB credentials, session secret, API keys) are stored in environment variables — never committed to version control.
- HTTPS is enforced in production. TLS termination happens at the load balancer or reverse proxy.
- All `/admin` routes should store session IDs as `httpOnlyCookies`
- CORS is explicitly configured — wildcard origins are never used in production.
- Rate limiting is applied at the middleware level to prevent abuse.
- Request payload size is limited using `http.MaxBytesReader`.
- SQL injection is prevented using parameterized queries — never raw string concatenation.
- Sensitive data (passwords, session IDs, auth cookies) are never logged or returned in API responses.
- Sessions are rotated on login and privilege changes to prevent session fixation.
- Logout immediately invalidates the session server-side.

---

## Code Practices

### General

- Follow the [Effective Go](https://go.dev/doc/effective_go) guidelines.
- `goimports` is enforced; code that doesn't pass is not merged.
- Linting is handled by [`golangci-lint`](https://golangci-lint.run/) with a project-level config (`.golangci.yml`).

### Error Handling

- Errors are always handled explicitly — never silently ignored with `_`.
- Errors are wrapped with context using `fmt.Errorf("doing X: %w", err)` to preserve the chain.
- Sentinel errors (e.g. `ErrNotFound`, `ErrUnauthorised`) are defined in the domain layer and checked with `errors.Is()`.
- Panics are only used for truly unrecoverable startup failures. A middleware recovers from unexpected panics in handlers.

### Dependency Injection

- Dependencies (DB pool, logger, config) are explicitly injected via constructors — no global state or `init()` functions.
- Interfaces are defined at the **consumer** side (the layer that uses them), not the implementation side.

### Testing

- Unit tests live alongside the code they test (`foo_test.go` in the same package).
- Integration tests (those touching a real DB) are in a separate `_integration_test.go` file and are gated behind a build tag: `//go:build integration`.
- The repository layer is tested against a real database using a test container ([`testcontainers-go`](https://golang.testcontainers.org/)) or a local Docker Compose setup.
- Handler tests use `net/http/httptest` — no live server required.
- Target: ≥ 80% coverage on the service layer.

### Logging

- Structured logging via [`log/slog`](https://pkg.go.dev/log/slog) (stdlib, Go 1.21+).
- Log levels: `DEBUG` in development, `INFO` in production.
- Every request is logged with: method, path, status code, latency, and request ID.
- A request ID (`X-Request-ID`) is attached to every incoming request and propagated through the context.

### Configuration

- All configuration is read from environment variables at startup using a dedicated `config` package.
- The app fails fast with a descriptive error if any required variable is missing.
- An `.env.example` file documents every variable — it is always kept up to date.

---

## Getting Started

### Prerequisites

- Go 1.22+
- Docker & Docker Compose
- `golang-migrate` CLI (`go install github.com/golang-migrate/migrate/v4/cmd/migrate@latest`)

### Running Locally

```bash
# 1. Copy and fill in environment variables
cp .env.example .env

# 2. Start dependencies (Postgres, etc.)
docker compose up -d

# 3. Run database migrations
make migrate-up

# 4. Start the server
make run
```

### Common Makefile Commands

```bash
make run          # Run the server
make test         # Run unit tests
make test-all     # Run unit + integration tests
make lint         # Run golangci-lint
make migrate-up   # Apply all pending migrations
make migrate-down # Roll back the last migration
make build        # Compile the binary
```
