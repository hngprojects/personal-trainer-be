# Personal Trainer Backend

A modern, scalable backend service for personal training management built with Go. This service provides a RESTful API for managing trainers, clients, training sessions, and fitness programs.

## Features

- **Clean Architecture**: Layered architecture with clear separation of concerns (handlers, services, repositories)
- **RESTful API**: Standard REST conventions with JSON responses
- **PostgreSQL Database**: Robust relational database with migration management
- **Session-Based Authentication**: Secure session management for user authentication
- **Structured Logging**: Comprehensive logging using Go's `slog` package
- **Error Recovery**: Middleware to gracefully handle panics
- **Database Migrations**: Version-controlled schema changes using `golang-migrate`
- **Health Checks**: Built-in health check endpoints for monitoring

## Tech Stack

- **Language**: Go 1.25.3
- **Database**: PostgreSQL
- **HTTP Framework**: `Gin`
- **Logging**: `log/slog` (stdlib)
- **Migrations**: `golang-migrate`
- **Logging Enhancement**: `tint` for colored console output

## Project Structure

```
.
├── cmd/
│   └── server/
│       └── main.go             # Application entry point
├── internal/
│   ├── config/                 # Configuration loading from environment
│   ├── handlers/               # HTTP request handlers
│   ├── middleware/             # HTTP middleware (logging, recovery, etc.)
│   ├── models/                 # Domain models
│   ├── repository/             # Data access layer
│   ├── server/                 # HTTP server setup
│   └── service/                # Business logic layer
├── pkg/
│   └── logger/                 # Reusable logger utilities
├── migrations/                 # Database migration files (SQL)
├── Makefile                    # Development commands
├── ARCHITECTURE.md             # Detailed architecture documentation
└── go.mod                      # Go module definition
```

### Architecture Layers

- **Handler Layer**: Decodes HTTP requests, validates input, encodes responses
- **Service Layer**: Contains business logic and orchestrates repository calls
- **Repository Layer**: Abstracts database interactions and returns domain models
- **Domain Layer**: Plain Go structs representing core entities

## Prerequisites

- Go 1.22 or higher
- PostgreSQL 12 or higher
- Docker & Docker Compose (for local development)
- `golang-migrate` CLI

## Getting Started

### 1. Clone the Repository

```bash
git clone <repository-url>
cd personal-trainer-be
```

### 2. Set Up Environment Variables

```bash
cp .env.example .env
```

Edit `.env` with your configuration:

```env
APP_ENV=development
PORT=8080
LOG_LEVEL=debug
LOG_FORMAT=json
DATABASE_URL=postgres://user:password@localhost:5432/trainer_db?sslmode=disable
```

### 3. Start Dependencies

```bash
docker compose up -d
```

### 4. Install Tools

```bash
make install-tools
```

### 5. Run Database Migrations

```bash
make migrate-up
```

### 6. Start the Server

```bash
make run
```

The server will start on `http://localhost:8080`

## API Endpoints

### Health Check

- `GET /` — Service status message
- `GET /health` — Health check endpoint

**Example:**

```bash
curl http://localhost:8080/health
```

Response:

```json
{
  "status": "ok",
  "time": "2024-05-03T12:00:00Z"
}
```

### Authentication — Password Recovery (admin-only)

- `POST /api/v1/auth/forgot-password` — request a 6-digit reset code by email
- `POST /api/v1/auth/reset-password` — set a new password using the emailed code

Both endpoints are gated server-side to users that hold the `admin` role.
Failures (unknown email, non-admin, deactivated, wrong code) return a generic
response so the existence of an account or its role cannot be probed.

**Request a reset code**

```bash
curl -X POST http://localhost:8080/api/v1/auth/forgot-password \
  -H "Content-Type: application/json" \
  -d '{"email":"admin@example.com"}'
```

Response (always the same, regardless of whether the email exists):

```json
{
  "status": "success",
  "message": "if the email is registered, a reset code has been sent",
  "code": "OK",
  "data": null
}
```

In development the code is printed to the server log instead of being emailed
(see [Email Delivery](#email-delivery) below).

**Reset the password**

```bash
curl -X POST http://localhost:8080/api/v1/auth/reset-password \
  -H "Content-Type: application/json" \
  -d '{"email":"admin@example.com","code":"123456","new_password":"Str0ngPassw0rd"}'
```

On success the code is consumed (single-use) and all existing refresh sessions
for the user are revoked. The new password must be 8–128 characters and contain
upper case, lower case, and a digit.

### Email Delivery

Outbound email is sent through one of three mailers, picked at startup in this
order:

1. **Resend** — if both `RESEND_API_KEY` and `RESEND_FROM` are set.
2. **SMTP** — if `SMTP_HOST` is set.
3. **LogMailer** — silent default in development; in any other environment a
   warning is logged and emails are not delivered.

Resend takes precedence over SMTP whenever it is configured, including in
development — useful for end-to-end testing of the live email pipeline. To
fall back to console-only delivery in development, leave the Resend env vars
empty.

## Development

### Available Make Commands

```bash
make run           # Start the development server
make build         # Build binary to bin/server
make test          # Run tests with race detector and coverage
make test-cover    # Generate HTML coverage report
make lint          # Run go vet
make fmt           # Format code with gofmt
make tidy          # Tidy go.mod and go.sum
make clean         # Remove build and coverage artifacts
```

### Database Migrations

```bash
make migrate-up           # Apply all pending migrations
make migrate-down         # Rollback the most recent migration
make migrate-create NAME=migration_name  # Create new migration
make migrate-version      # Show current migration version
make migrate-drop         # Drop all tables (destructive)
```

### Running Tests

```bash
# Run all tests
make test

# Generate coverage report
make test-cover
```

The coverage report is generated as `coverage.html`.

## Code Standards

- Follow [Effective Go](https://go.dev/doc/effective_go) guidelines
- Code formatting enforced with `goimports`
- Linting via `golangci-lint`
- Structured logging using `log/slog`
- Error handling with context wrapping
- Unit tests alongside source code

### Error Handling

Errors are wrapped with context to maintain error chains:

```go
if err != nil {
    return fmt.Errorf("doing X: %w", err)
}
```

### Logging

Structured logging with request IDs:

```go
log.Info("user created", "user_id", userID, "email", email)
log.Error("database error", "err", err)
```

## Security

- Passwords hashed with bcrypt (cost factor ≥ 12)
- All secrets stored in environment variables
- SQL injection prevention via parameterized queries
- Session-based authentication with 7-day expiration
- HTTPS enforced in production
- Request payload size limits
- Rate limiting middleware support
- CORS explicitly configured

## API Response Format

### Success Response

```json
{
  "status": "success",
  "message": "Human-readable message",
  "code": "MACHINE_READABLE_CODE",
  "data": {},
  "meta": {}
}
```

### Error Response

```json
{
  "status": "error",
  "message": "Human-readable error message",
  "code": "MACHINE_READABLE_ERROR_CODE",
  "errors": []
}
```

### HTTP Status Codes

| Code | Meaning               |
| ---- | --------------------- |
| 200  | OK                    |
| 201  | Created               |
| 204  | No Content            |
| 400  | Bad Request           |
| 401  | Unauthenticated       |
| 403  | Forbidden             |
| 404  | Not Found             |
| 409  | Conflict              |
| 422  | Unprocessable Entity  |
| 500  | Internal Server Error |

## Configuration

All configuration is loaded from environment variables at startup:

- `APP_ENV`: Application environment (`development`, `production`)
- `PORT`: Server port (default: `8080`)
- `LOG_LEVEL`: Logging level (`debug`, `info`, `warn`, `error`)
- `LOG_FORMAT`: Log format (`json`, `text`)
- `DATABASE_URL`: PostgreSQL connection string

### Email (Resend)

Set both to enable Resend — leave empty to fall back to SMTP / LogMailer:

- `RESEND_API_KEY`: API key from the Resend dashboard
- `RESEND_FROM`: verified sender address (e.g. `fitcal@hng14.com`)

### Email (SMTP — fallback)

Used only when Resend is not configured:

- `SMTP_HOST`, `SMTP_PORT` (default `587`), `SMTP_USER`, `SMTP_PASSWORD`, `SMTP_FROM`

Missing required variables will cause the server to exit with a descriptive error.

## Database

### Schema Management

- All schema changes must be made via migration files
- Never edit the database manually
- Migrations run automatically in development
- Use transactions for multi-table operations

### Conventions

- Timestamps stored as `TIMESTAMPTZ` (UTC)
- UUIDs used as primary keys (`uuid_generate_v4()`)
- Connection pooling configured via pgx
- Parameterized queries to prevent SQL injection

## Deployment

### Building for Production

```bash
make build
```

Output binary: `bin/server`

### Pre-Deployment Steps

1. Run migrations: `make migrate-up`
2. Set environment variables for production
3. Start the application

### Environment Variables

Set these in your production environment:

```env
APP_ENV=production
PORT=8080
LOG_LEVEL=info
LOG_FORMAT=json
DATABASE_URL=postgres://user:password@prod-db:5432/trainer_db?sslmode=require
```

## Branch Protection

This repository enforces the following branch protection rules on `prod` and `staging`:

- **Pull Requests are required** before any code can be merged
- **At least 2 approvals** is required on every PR
- **Direct pushes are disabled** on both branches
- All changes to `staging` and `prod` must go through a reviewed PR

### Branch Strategy

| Branch | Environment | Purpose |
|--------|-------------|---------|
| `dev` | Development | Active development work |
| `staging` | Pre-production | Testing before release |
| `prod` | Production | Live production code |

## Contributing

1. Follow the code standards defined in `ARCHITECTURE.md`
2. Write tests for new features
3. Ensure tests pass: `make test`
4. Run linting: `make lint`
5. Format code: `make fmt`

## Troubleshooting

### Database Connection Issues

```bash
# Check if the database is running
docker ps

# View logs
docker logs <container_id>

# Verify connection string in .env
```

### Migration Errors

```bash
# Check current migration version
make migrate-version

# Force a version if migrations are dirty
make migrate-force VERSION=1
```

### Port Already in Use

Change the `PORT` in `.env`:

```env
PORT=3000
```

## Additional Resources

- [Architecture Documentation](ARCHITECTURE.md)
- [Go Effective Documentation](https://go.dev/doc/effective_go)
- [golang-migrate Documentation](https://github.com/golang-migrate/migrate)
- [PostgreSQL Documentation](https://www.postgresql.org/docs/)

## License

TBD

## Contact

For issues or questions, please reach out to the development team.
