# Contributing to Personal Trainer Backend

Thank you for contributing to **Personal Trainer Backend**, a Go REST API for managing trainers, clients, sessions, and fitness programs.

This guide explains how to contribute effectively. Read it before opening a pull request.

---

## How to Contribute

### 1. Fork and Clone

```bash
git clone https://github.com/<your-username>/personal-trainer-be.git
cd personal-trainer-be
```

### 2. Add Upstream Remote

```bash
git remote add upstream https://github.com/hngprojects/personal-trainer-be.git
git fetch upstream
```

### 3. Create a Branch

Branch from `dev`.

| Prefix    | Use case          |
| --------- | ----------------- |
| feat/     | New feature       |
| fix/      | Bug fix           |
| refactor/ | Code changes only |
| test/     | Tests             |
| chore/    | Maintenance       |
| docs/     | Documentation     |

```bash
git checkout dev
git pull upstream dev
git checkout -b feat/your-feature
```

### 4. Make Changes

Follow code style and API rules. Add tests for new logic.

### 5. Push and Open PR

```bash
git push origin feat/your-feature
```

Open a PR to `dev`.

---

## Development Setup

### Requirements

- Go 1.22+
- PostgreSQL 12+
- Docker
- Make

### Setup

```bash
make install-tools
cp .env.example .env
docker compose up -d
make migrate-up
make run
```

Server runs on:

```
http://localhost:8080
```

### Code Generation

After changes:

```bash
make codegen
make sqlc
```

Do not edit generated files manually.

### Tests

```bash
make test
make test-cover
```

---

## Code Style

### General Rules

- Format code with `gofmt -s` (`make fmt`)
- Follow Effective Go guidelines
- Keep functions small and focused

### Architecture

Structure is domain-based:

```
cmd/server          app entry
internal/
  routes            routing only
  auth              auth domain
  root              root endpoint
  health            health checks
  common            shared utilities
  middleware        middleware
  repository        database access
  models            domain models and queries
  config            environment config
  handlers          non-domain handlers
  api               generated OpenAPI code
```

### Rules

- Business logic stays in domain packages
- Only repositories access the database
- Handlers use dependency injection
- Routes only wire components

### Errors

```go
return fmt.Errorf("create user: %w", err)
```

### Logging

```go
log.Info("user created", "user_id", id)
log.Error("db error", "err", err)
```

---

## API Guidelines

### Response Format

Use standard helpers only.

**Success**

```go
api.NewSuccess("Created", api.CodeCreated, data)
```

```json
{
  "status": "success",
  "message": "Created",
  "code": "CREATED",
  "data": {}
}
```

**Error**

```go
api.NewError("Invalid request", api.CodeBadRequest)
```

```json
{
  "status": "error",
  "message": "Invalid request",
  "code": "BAD_REQUEST"
}
```

### Response Codes

- OK: 200
- CREATED: 201
- BAD_REQUEST: 400
- UNAUTHORIZED: 401
- FORBIDDEN: 403
- NOT_FOUND: 404
- SERVER_ERROR: 500

### Pagination

```go
meta := api.NewPaginationMeta(page, perPage, total)
api.NewSuccessWithMeta("List", api.CodeOK, data, meta)
```

---

## Commit Standards

Use Conventional Commits.

### Format

```
type(scope): description
```

### Types

- feat
- fix
- refactor
- test
- docs
- chore
- style
- perf
- ci

### Examples

```
feat(auth): add login endpoint
fix(auth): correct token expiry
refactor(repo): simplify query layer
test(auth): add login tests
docs: update setup guide
```

Rules:

- Use present tense
- Keep under 72 characters
- Reference issues when needed

---

## Pull Requests

### Title

Follow commit format.

### Checklist

- Tests pass
- Lint passes
- Code formatted
- Codegen run if needed
- SQLC run if needed
- Up to date with `dev`

```bash
make test
make lint
make fmt
make codegen
make sqlc
```

### PR Content

Include:

- What changed
- Why it changed
- How it was tested
- Related issue

### Review Rules

- One approval required
- Address all comments
- Keep PRs small

### Merge

- Squash merge into `dev`
- `main` is for releases only

---

## Security

- Never commit secrets
- Use `.env` only locally
- Validate all OAuth state values
- Use strong JWT secrets
- Never log credentials
- Use parameterized queries only

Report vulnerabilities privately.

---

## Good First Issues

Look for:

- good first issue
- help wanted
- docs

Examples:

- Add tests
- Improve validation
- Fix docs
- Extend OpenAPI spec

---

## General Rules

- Follow domain structure
- Write tests
- Keep PRs small
- Do not modify generated code
- Propose new dependencies first
- Document exported functions
- Keep changes focused

---

## Getting Help

- Open a discussion for questions
- Open an issue for bugs
- Ask before large changes

---

Contributions are welcome. Keep changes focused and intentional.
