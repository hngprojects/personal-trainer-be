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

- setup redis server with

```bash
make up
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
