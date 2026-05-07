# Contributing Guide

This document outlines how to get started with the codebase and the required development workflow.

---

## 1. Project Setup

Install all required development tools:

```bash
make install-tools
```

---

## 2. Environment Variables

Copy the example environment file and fill in your local values:

```bash
cp .env.example .env
```

---

## 3. Running the Project

```bash
make run
```

---

## 4. Code Generation

**API routes / boilerplate** — run after any changes to `api.yaml`:

```bash
make codegen
```

**Database models** — run after any changes to SQL queries:

```bash
make sqlc
```

Do not manually edit generated files.

---

## 5. Database Migrations

```bash
make migrate-create NAME=your_migration_name
make migrate-up
```

---

## 6. Testing

All new features must include tests.

```bash
make test
make test-cover   # generates coverage.html
```

---

## 7. Branching

- Branch off `dev`
- PRs must target `dev`, not `main`

---

## 8. Commit Style

Follow conventional commits:

```text
feat: add trainer profile endpoint
fix: resolve token expiry bug
refactor: simplify auth middleware
test: add unit tests for booking service
chore: update dependencies
```

---

## 9. Before Opening a PR

- [ ] `make test` passes
- [ ] `make codegen` run if `api.yaml` changed
- [ ] `make sqlc` run if SQL queries changed
- [ ] Branch is up to date with `dev`

---

## 10. Getting Help

Ask the lead developer, a teammate, or a mentor. Raise questions early.
