# Repository Guidelines

## Project Structure & Module Organization

This repository is split into a Go backend and a Vue 3 admin frontend. `backend/` contains the IoT device platform service; its entrypoint is `backend/cmd/server/main.go`, and Make targets live in `backend/Makefile`. `frontend/` contains the Admin9 Pro web app. UI code is under `frontend/src/`, grouped into `api/`, `components/`, `layout/`, `router/`, `store/`, `utils/`, and `views/`. Vite config is in `frontend/config/`, tooling scripts in `frontend/scripts/`, static assets in `frontend/src/assets/`, and API/MVP contracts in `docs/`.

## Build, Test, and Development Commands

Run backend commands from `backend/`: `make run` starts the API server, `make build` writes `bin/device-platform`, `make test` runs short Go tests, `make test-int` runs integration tests with the `integration` tag, `make lint` runs `go vet` plus `staticcheck`, and `make migrate-up` / `make migrate-down` apply database migrations using `DATABASE_URL`.

Run frontend commands from `frontend/`: `pnpm install` installs dependencies, `pnpm dev` starts Vite, `pnpm build` runs `vue-tsc` and a production build, and `pnpm type:check`, `pnpm format`, `pnpm lint:fix`, and `pnpm i18n:check` validate types, formatting, lint, and locale keys. Before committing frontend work, install dependencies with `pnpm --dir frontend install --frozen-lockfile`; hooks fail fast when dependencies are missing and do not install packages during commit.

## Coding Style & Naming Conventions

Backend Go code should follow `gofmt`, idiomatic package names, and focused `_test.go` files near the code they verify. Frontend code uses TypeScript, Vue SFCs, Arco Design Vue, Pinia, and Vue Router. Use PascalCase for Vue component files when they represent components. Keep route, API, hook, and utility modules inside the existing `src/` folders. Frontend formatting is enforced by Prettier, ESLint, and Stylelint with Airbnb/TypeScript/Vue rules.

## Testing Guidelines

Backend tests should use Go’s standard testing package and be named `TestXxx` in `*_test.go`. Use `make test` for normal verification and reserve `make test-int` for external-service scenarios. No frontend unit test runner is currently configured; for frontend changes, run `pnpm type:check`, `pnpm build`, and relevant manual UI checks. Update both `zh-CN` and `en-US` locale files, then run `pnpm i18n:check`.

## Commit & Pull Request Guidelines

Follow the configured frontend commitlint convention: Conventional Commits such as `feat: add device list` or `fix: handle expired token`. Local Codex/OMX commits must also use inline `git commit -m ...` paragraphs with a narrative Lore body, Lore trailers, and `Co-authored-by: OmX <omx@oh-my-codex.dev>`.

PRs should include the purpose, affected backend/frontend areas, verification commands, linked issues when available, and screenshots or screen recordings for visible UI changes. Keep changes scoped and update `docs/` when API contracts or behavior change.

## Security & Configuration Tips

Never commit real secrets. Start from `backend/.env.example` and `frontend/.env.example`; use local `.env` files for `DATABASE_URL`, `VITE_API_BASE_URL`, auth strategy, and optional map keys.
