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

| Method   | Usage                        |
| -------- | ---------------------------- |
| `GET`    | Read a resource or list      |
| `POST`   | Create a new resource        |
| `PUT`    | Replace a resource           |
| `PATCH`  | Partial update of a resource |
| `DELETE` | Remove a resource            |

---

### Response Format

**Content-Type Header and Body Rules:**

- Successful responses (`2xx`) with content MUST return `Content-Type: application/json` with a JSON envelope.
- `204 No Content` responses MUST NOT include a response body or a Content-Type header (this is the HTTP standard).
- All other responses (errors, success with data) use the JSON envelope format below.

#### Success (200, 201, and responses with body)

```json
{
  "status": "success",
  "message": "Human-readable message",
  "code": "MACHINE_READABLE_CODE",
  "data": {},
  "meta": {}
}
```

#### Error (4xx, 5xx)

```json
{
  "status": "error",
  "message": "Human-readable error message",
  "code": "MACHINE_READABLE_ERROR_CODE",
  "errors": []
}
```

---

### Conventions

- Always include: `status`, `message`, `code`
- Use:

  - `data` → successful responses only
  - `errors` → validation or detailed errors only
  - `meta` → pagination or extra metadata

- Do not mix `data` and `errors`
- `code` values must be:

  - UPPERCASE
  - underscore-separated
  - stable (do not change frequently)

- Messages are human-readable, not for program logic

---

### HTTP Status Codes

| Code  | Meaning               |
| ----- | --------------------- |
| `200` | OK                    |
| `201` | Created               |
| `204` | No Content            |
| `400` | Bad Request           |
| `401` | Unauthorized          |
| `403` | Forbidden             |
| `404` | Not Found             |
| `409` | Conflict              |
| `422` | Validation Error      |
| `500` | Internal Server Error |

---

### Rules

- Never return `200` with an error body
- Use `201` for resource creation
- Use `422` for validation errors
- `500` responses must not expose internal details (log internally)

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

- `id`
- `user_id`
- `created_at`
- `expires_at` (7 days from creation)
- Optional metadata (device, IP address, user agent)

Sessions are stored in the database.

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

### Running Locally

```bash
go mod tidy
make run
```

### Common Makefile Commands

```bash
make run          # Run the server
```
