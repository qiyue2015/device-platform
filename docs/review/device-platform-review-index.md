# Device Platform Review Index

This review index is the entrypoint for the responsibility branch set. `master` stays frozen as the reference implementation; these branches are review candidates only.

## Review Order

| Order | Lane | Branch | Learning commits | Verification | Naming Check | Audit Result |
| --- | --- | --- | --- | --- | --- | --- |
| 1 | Orchestration rules | `review/device-orchestration-rules` | `5f14b08`, `af6b918` | `git diff --check` | Passed; stage words appear only as forbidden examples | Ready for review |
| 2 | Backend contract | `review/device-backend-contract` | `1a2092f`, `bcee08b` | `cd backend && make test`; `cd backend && make lint` | Passed | Review fixed; suggested merge |
| 3 | Command domain | `review/device-command-domain` | `42c87cf`, `5487385` | `cd backend && make test`; `cd backend && make lint` | Passed | Review fixed; suggested merge |
| 4 | Simulator gateway | `review/device-simulator-gateway` | `0b348c5`, `694163f` | `cd backend && make test`; `cd backend && make lint` | Passed | Review fixed; suggested merge |
| 5 | Webhook audit | `review/device-webhook-audit` | `306f8f8`, `9dd35c2` | `cd backend && make test`; `cd backend && make lint` | Passed | Review fixed; suggested merge |
| 6 | Admin console | `review/device-admin-console` | `a17d5d9`, `e87aa5e` | `pnpm --dir frontend type:check`; `pnpm --dir frontend i18n:check`; `pnpm --dir frontend build`; `cd backend && make test` | Passed for device-platform additions | Review fixed; suggested merge |
| 7 | Cloud API adapter | `review/device-cloud-api-adapter` | `51a5392`, `9c03633` | `cd backend && make test`; `cd backend && make lint` | Passed | Review fixed; suggested merge; live vendor test intentionally not run |
| 8 | Verification index | `review/device-verification` | `df1202f` | `git diff --check`; final scans; whole-chain backend and frontend checks | Allows documented false positives | Current branch; whole-chain audit candidate |

## Learning Commit Lines

- Orchestration rules: first add formal agent role files, then add delivery gates and naming policy.
- Backend contract: first establish backend app foundation, then add durable API/domain/storage contracts.
- Command domain: first add `devicecore` lifecycle behavior, then expose HTTP APIs and command-created hooks.
- Simulator gateway: first add deterministic simulator behavior, then expose simulator controls over HTTP.
- Webhook audit: first add event/outbox/audit service and migration, then wire command-created observation.
- Admin console: first name the route/menu/default entrypoint, then add the operations console UI.
- Cloud API adapter: first add dry-run WWTIOT adapter behavior, then expose preview and callback routes.

## Branch Responsibilities

- `device-orchestration-rules`: project-local agent roles, lane order, handoff rules, and formal naming rules.
- `device-backend-contract`: backend config, JSON responses, auth gates, health checks, DTOs, domain types, repository contracts, and initial schema.
- `device-command-domain`: project, device, command lifecycle, idempotency, delivery policy, command status transitions, and API router hooks.
- `device-simulator-gateway`: local simulator modes, heartbeat, online/offline recovery, command dispatch simulation, and simulator HTTP controls.
- `device-webhook-audit`: event creation, webhook delivery, retry/dead/resend behavior, audit logs, and command-created observation hook.
- `device-admin-console`: Vue admin route, menu, API client, device console page, simulator controls, webhook/audit tables, and formal UI wording.
- `device-cloud-api-adapter`: WWTIOT dry-run adapter, signing, command preview, callback normalization, and raw-message redaction.

## Naming Scan Notes

Formal code artifacts were scanned for stage or temporary terms. Remaining expected matches are not product naming leaks:

- `docs/development/multi-agent-delivery.md` lists forbidden words as policy examples.
- `frontend/src/components/qq-map-select/index.vue` contains a third-party documentation URL with `demo-center`.
- `frontend/src/utils/is.ts` contains JavaScript `Object.prototype`.

## Review Policy

Review each branch in order. If a branch fails review, fix it on that branch or a follow-up branch derived from it. Do not patch `master`, and do not merge any `review/*` branch into `master` until the final code audit is explicitly approved.
