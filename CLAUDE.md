# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Project

IoT Device Platform — generic device management with command dispatch, status tracking, offline queuing, webhook delivery, and a simulator gateway. First hardware target is smart locks but the design is device-type agnostic.

Monorepo with two independent codebases:
- `backend/` — Go 1.23 API server (PostgreSQL, Redis)
- `frontend/` — Vue 3 + TypeScript admin UI (Arco Design Vue, Vite 3, Pinia)

## Commands

### Backend (run from `backend/`)

```bash
make run                # Start server (go run ./cmd/server)
make build              # Compile to bin/device-platform
make test               # Unit tests (-short)
make test-int           # Integration tests (-tags=integration -timeout=120s)
make lint               # go vet + staticcheck
make migrate-up         # Apply migrations (requires DATABASE_URL env)
make migrate-down       # Roll back one migration
```

### Frontend (run from `frontend/`)

```bash
pnpm dev                # Vite dev server
pnpm build              # vue-tsc + production build
pnpm type:check         # TypeScript only
pnpm lint:fix           # ESLint auto-fix
pnpm format             # Prettier + ESLint
pnpm i18n:check         # Verify zh-CN/en-US locale key parity
pnpm new                # Scaffold view/component via plop
```

No frontend unit test runner is configured. Verify frontend changes with `pnpm type:check` and `pnpm build`.

## Architecture

### Backend

Entrypoint: `cmd/server/main.go`. Migrations: `internal/storage/migrations/`. Config via `.env` (see `.env.example`).

### Frontend

- **Routing** — Auto-imported from `src/router/routes/modules/*.ts`. Guards in `src/router/guard/` handle auth and permissions.
- **State** — Pinia stores in `src/store/modules/` (app, user, tab-bar) with persistence.
- **Auth** — Dual strategy (`local` | `oidc`) set by `VITE_AUTH_STRATEGY`. OIDC flow: redirect → exchange → token storage.
- **API layer** — Axios interceptors (`src/api/interceptor.ts`) auto-attach Bearer token and transform pagination params (`current`→`page`, `pageSize`→`page_size`).
- **i18n** — `zh-CN` and `en-US` required together. View-level locales at `src/views/{module}/locale/`.
- **Path aliases** — `@/` → `src/`, `assets/` → `src/assets/`.
- **Styling** — Less with Arco theme vars; Tailwind CSS available; Stylelint enforces property order.

### API Namespaces

```
/v1/auth/...    Authentication
/v1/...         Logged-in backend APIs (not admin-only)
/v1/admin/...   Platform administrator APIs only
/v1/open/...    Machine-to-machine Open API (X-API-Key + IP whitelist)
```

Do not put normal logged-in APIs under `/v1/admin/` — that namespace is strictly for platform-level operations.

## Conventions

- Conventional Commits enforced by commitlint + husky (`feat:`, `fix:`, `chore:`, etc.)
- Pre-commit hooks run lint-staged (Prettier + ESLint + Stylelint)
- Both locale files (`zh-CN`, `en-US`) must be updated together
- Backend Go: `gofmt`, idiomatic package names, `_test.go` next to source
- Frontend: PascalCase for component files, TypeScript strict
- API contracts and MVP scope documented in `docs/` — update when behavior changes

## Environment Setup

```bash
cp backend/.env.example backend/.env          # DATABASE_URL, REDIS_URL, SERVER_ADDR
cp frontend/.env.example frontend/.env.development  # VITE_API_BASE_URL, VITE_AUTH_STRATEGY
```
