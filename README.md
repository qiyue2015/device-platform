# Device Platform

Generic IoT device platform for device management, command dispatch, state tracking, offline queueing, simulator-based verification, and webhook delivery.

The first hardware sample is a smart lock, but the platform should stay device-type agnostic. Device-specific behavior belongs in device types, capabilities, adapters, payloads, and raw message records.

## Repository Structure

```text
.
├── backend/    Go API service
├── frontend/   Vue 3 admin frontend
└── docs/       Engineering contracts for implementation
```

## Documentation

- [Engineering docs](./docs/README.md): implementation-facing contracts and current stage notes.
- [MVP-1 contract](./docs/mvp-1-contract.md): coding scope, simulator behavior, and acceptance criteria.
- [API contract](./docs/api-contract.md): API namespaces, command lifecycle, delivery policy, offline queue, and webhook expectations.
- [Local development](./docs/local-development.md): local MVP-1 run commands, env files, health check, and simulator acceptance path.
- [Backend README](./backend/README.md): backend local commands.
- [Frontend README](./frontend/README.md): frontend local commands and UI project conventions.

The full project knowledge base, including background, decisions, vendor materials, and long-form rationale, stays in Obsidian:

```text
/Users/fengqiyue/Documents/Obsidian Vault/Projects/设备平台/
```

## Current Stage

MVP-1 is ready for implementation.

MVP-1 is a simulator-backed closed loop with no vendor dependency:

```text
Project -> Device -> Command -> Gateway/Adapter -> State/Event -> Webhook
```

MVP-1.5 covers vendor-cloud adapter integration. MVP-2 covers direct-device protocol integration. Both are follow-up stages and still depend on vendor credentials, callback configuration, device ownership confirmation, and real-device protocol verification.

## Quick Start

Use this path to get the local MVP-1 development skeleton running.

### 1. Prerequisites

Install or prepare:

- Go `1.23+`
- Node.js and pnpm
- PostgreSQL on `localhost:5432`
- Redis on `localhost:6379`

Create the local database if it does not exist:

```bash
createdb device_platform
```

### 2. Prepare Local Env Files

From the repository root:

```bash
make setup-local
```

This creates the ignored local env files when they do not already exist:

```text
backend/.env
frontend/.env.development
```

Backend defaults:

```text
DATABASE_URL=postgres://postgres:postgres@localhost:5432/device_platform?sslmode=disable
REDIS_URL=redis://localhost:6379/0
SERVER_ADDR=:8080
```

Frontend local development keeps `VITE_API_BASE_URL` empty so browser requests use relative `/v1/...` paths through the Vite proxy.

### 3. Verify PostgreSQL and Redis

First verify that the local services are reachable:

```bash
make check-services
```

Expected result:

```text
localhost:5432 - accepting connections
PONG
```

Then verify that `backend/.env` can connect to the target application database:

```bash
make check-db
```

Expected result:

```text
device_platform
```

If `make check-db` fails with a password or database error, update `DATABASE_URL` in `backend/.env` to match the local PostgreSQL user, password, host, port, and database name.

Create the database when needed:

```bash
createdb device_platform
```

The current skeleton has the migration directory and rules, but no schema SQL yet. `make migrate-up` becomes meaningful once the first migration file is added under `backend/internal/storage/migrations/`.

Current local verification note: `make check-services` is the required skeleton check for PostgreSQL/Redis service availability. `make check-db` is the stricter credential/database check and depends on the developer's local PostgreSQL password and database setup.

### 4. Install Frontend Dependencies

```bash
pnpm --dir frontend install
```

### 5. Run Checks

```bash
make check
```

This runs:

- Backend tests and lint.
- Frontend type check, production build, and i18n key check.
- PostgreSQL and Redis service reachability.

`staticcheck` is optional locally. If it is not installed, backend lint still runs `go vet` and prints a skip message for `staticcheck`.

### 6. Run Backend

```bash
make dev-backend
```

In another terminal, verify the health check:

```bash
curl http://localhost:8080/healthz
```

Expected response:

```json
{"status":"ok"}
```

`/healthz` only proves the backend process is alive. PostgreSQL/Redis readiness will be covered later by `/readyz` after those clients are wired in.

### 7. Run Frontend

```bash
make dev-frontend
```

The frontend dev server proxies `/v1` to `http://localhost:8080`, so MVP-1 frontend API modules should use relative `/v1/...` paths.

### Current Local Skeleton Boundary

The local skeleton is ready when `make check` passes and `/healthz` returns `{"status":"ok"}`.

MVP-1 is still simulator-backed and local-only at this stage. It does not require real `cloud_api`, real hardware, public callback URLs, Docker Compose, Nginx, or CI/CD.

## Backend

Run backend commands from `backend/`.

```bash
cp .env.example .env
make migrate-up
make run
```

Useful commands:

```bash
make build
make test
make test-int
make lint
make migrate-down
```

Health check:

```bash
curl http://localhost:8080/healthz
```

Backend entrypoint:

```text
backend/cmd/server/main.go
```

## Frontend

Run frontend commands from `frontend/`.

```bash
pnpm install
cp .env.example .env.development
pnpm dev
```

Local development uses relative `/v1/...` API requests through the Vite proxy to `http://localhost:8080`. Keep `VITE_API_BASE_URL` empty in `.env.development` unless a special debugging scenario needs absolute API URLs.

Useful commands:

```bash
pnpm build
pnpm type:check
pnpm lint:fix
pnpm format
pnpm i18n:check
```

## API Namespaces

```text
/v1/auth/...      Backend user authentication
/v1/...           Logged-in backend APIs
/v1/admin/...     Platform administrator APIs only
/v1/open/...      Business-system Open API with Project API Key auth
```

`/v1/admin/...` is only for platform-level administration. Normal logged-in backend APIs should stay under `/v1/...`.

## Maintenance Rule

Keep repository docs focused on implementation contracts.

- Update `docs/` when code behavior, API contracts, database semantics, runtime commands, tests, deployment, or acceptance criteria change.
- Update Obsidian when project background, vendor facts, scope decisions, rationale, or pending questions change.
- Update both only when a design decision changes and the implementation contract also changes.
