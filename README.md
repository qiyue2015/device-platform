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

Private background notes, decisions, vendor materials, and long-form rationale should stay outside this repository.

## Current Stage

MVP-1 is the active implementation stage.

MVP-1 is a simulator-backed closed loop with no vendor dependency:

```text
Project -> Device -> Command -> Gateway/Adapter -> State/Event -> Webhook
```

MVP-1.5 covers vendor-cloud adapter integration. MVP-2 covers direct-device protocol integration. Both are follow-up stages and still depend on vendor credentials, callback configuration, device ownership confirmation, and real-device protocol verification.

## Start Development

Start by reading the implementation contracts:

- [MVP-1 contract](./docs/mvp-1-contract.md): what must be built and accepted in the current stage.
- [API contract](./docs/api-contract.md): API namespaces, command lifecycle, delivery policy, offline queue, and webhook rules.
- [Local development](./docs/local-development.md): detailed local setup and acceptance flow.

Prepare local services and env files from the repository root:

```bash
createdb device_platform
make setup-local
make check-services
make check-db
pnpm --dir frontend install
```

`make setup-local` creates ignored local env files:

```text
backend/.env
frontend/.env.development
```

Run the backend in one terminal:

```bash
make dev-backend
curl http://localhost:8080/healthz
```

Run the frontend in another terminal:

```bash
make dev-frontend
```

The frontend dev server proxies relative `/v1/...` requests to `http://localhost:8080`.

Open the first-run setup wizard in the browser:

```text
http://localhost:5173/setup
```

Complete the database, Redis, runtime, and administrator-account checks. After installation, open:

```text
http://localhost:5173/auth/login
```

Use the administrator account created in the setup wizard. If Vite starts on another port because `5173` is occupied, use the URL printed by `make dev-frontend` with the same path.

Before handing off changes, run:

```bash
make check
```

This verifies backend tests and lint, frontend type checking and build, i18n keys, and PostgreSQL/Redis service reachability.

## Where To Make Changes

- Backend HTTP entrypoint and handlers: `backend/cmd/server/`.
- Backend domain logic: `backend/internal/devicecore/`, `backend/internal/gateway/`, and `backend/internal/webhookaudit/`.
- Backend migrations and storage contracts: `backend/internal/storage/`.
- Frontend API modules: `frontend/src/api/`.
- Frontend pages and reusable UI: `frontend/src/views/` and `frontend/src/components/`.
- Frontend routes, state, utilities, and locale text: `frontend/src/router/`, `frontend/src/store/`, `frontend/src/utils/`, and `frontend/src/locale/`.
- Implementation contracts: `docs/`.

Update `docs/` whenever behavior, API shape, database semantics, local commands, or acceptance criteria change.

## Common Commands

Run backend commands from `backend/` when using the backend Makefile directly:

```bash
make build
make test
make test-int
make lint
make migrate-up
make migrate-down
```

Run frontend commands from `frontend/` when using package scripts directly:

```bash
pnpm dev
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
- Update the private knowledge base when project background, vendor facts, scope decisions, rationale, or pending questions change.
- Update both only when a design decision changes and the implementation contract also changes.
