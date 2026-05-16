---
title: MVP-1 Local Development
created: 2026-05-16
updated: 2026-05-16
status: current
---

# MVP-1 Local Development

This document is the local development contract for MVP-1. It is intentionally limited to the local simulator-backed loop and does not define production deployment.

MVP-1 does not depend on real `cloud_api`, public callback URLs, vendor credentials, or real hardware. The first closed loop is:

```text
Project -> Device -> Command -> Simulator Gateway -> State/Event -> Webhook
```

## Local Dependencies

Use local services already installed on the machine:

| Service | Default |
| --- | --- |
| PostgreSQL | `localhost:5432` |
| Redis | `localhost:6379` |
| Backend API | `localhost:8080` |
| Frontend dev server | Vite default |

Create the local database if it does not exist:

```bash
createdb device_platform
```

Verify service reachability from the repository root:

```bash
make check-services
```

Verify the database connection string in `backend/.env`:

```bash
make check-db
```

If `make check-db` fails, update `DATABASE_URL` in `backend/.env` to match the local PostgreSQL credentials and database name.

`make check-services` is the required local skeleton check. `make check-db` is stricter and verifies the actual `DATABASE_URL`, so it depends on each developer's local PostgreSQL credentials.

## Backend

Run backend commands from `backend/`.

```bash
cp .env.example .env
make migrate-up
make run
```

The backend reads `backend/.env` by default. The default local values are:

```text
DATABASE_URL=postgres://postgres:postgres@localhost:5432/device_platform?sslmode=disable
REDIS_URL=redis://localhost:6379/0
JWT_SECRET=replace-with-a-random-32-character-minimum-secret
DEVICE_PLATFORM_INSTALLED=false
SERVER_ADDR=:8080
```

Health check:

```bash
curl http://localhost:8080/healthz
```

`/healthz` only proves that the process is alive. `/readyz` reports `setup_required` until the first-run setup is complete.

## Frontend

Run frontend commands from `frontend/`.

```bash
pnpm install
cp .env.example .env.development
pnpm dev
```

For local integration, frontend API calls should use relative `/v1/...` and `/setup/...` paths. Vite proxies both namespaces to `http://localhost:8080`.

Local `.env.development` should keep `VITE_API_BASE_URL` empty unless a special debugging scenario needs direct absolute API URLs:

```text
VITE_API_BASE_URL=''
VITE_AUTH_STRATEGY='local'
```

New MVP-1 API modules should use `/v1/...`. Setup APIs use `/setup/...`. Existing template APIs using `/api/...` are not the contract for new device-platform work and can be cleaned up when those template surfaces are replaced.

## First-Run Setup

After the backend and frontend are running, open:

```text
http://localhost:5173/setup
```

The setup wizard checks PostgreSQL, Redis, writable runtime files, WWTIOT mode, and the administrator account. Failed checks stay on the current step and block installation.

After installation, open:

```text
http://localhost:5173/auth/login
```

Use the administrator account created in the setup wizard. If the Vite dev server prints a different local URL, use that URL with the same path.

## Root Commands

From the repository root:

```bash
make setup-local
make check-services
make check-db
make dev-backend
make dev-frontend
make check-backend
make check-frontend
make check
```

`check-backend` runs backend tests and lint. `go vet` always runs; `staticcheck` runs when it is installed locally. `check-frontend` runs type checking, production build, and i18n key checks.

## MVP-1 Manual Acceptance Path

After the local skeleton and MVP-1 features are implemented, verify:

- Create a Project.
- Create a Device.
- Create a command through `/v1/open/device-commands`.
- Switch simulator modes: `normal`, `delay`, `offline`, `timeout_then_ack`, `duplicate_ack`, `fail`.
- Observe command status, device state, attempts, events, and webhook delivery records.
- Confirm offline, timeout, late ACK, duplicate ACK, and failure behavior matches `docs/mvp-1-contract.md`.

## Deferred

These are intentionally out of scope for the local development skeleton:

- Docker Compose deployment.
- Production Nginx hosting.
- Go-embedded frontend assets.
- One-click install scripts.
- Full CI/CD.
