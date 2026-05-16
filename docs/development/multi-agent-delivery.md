# Multi-Agent Delivery Rules

This project uses independent agent lanes for device-platform delivery. The delivery line is part of the acceptance evidence, so lane boundaries, handoffs, verification commands, and proof paths must stay visible.

## Required Roles

- `device-platform-integration-lead`: split work, enforce boundaries, audit, merge, and run final smoke tests.
- `device-platform-backend-contract`: backend infrastructure, shared contracts, schemas, DTOs, error codes, auth base, routing, health checks, and cross-adapter fields.
- `device-platform-command-domain`: project/device/command lifecycle and delivery policy.
- `device-platform-mock-gateway`: simulator gateway modes, heartbeat, online/offline recovery.
- `device-platform-webhook-audit`: events, webhook outbox, signatures, retries, audit records.
- `device-platform-admin-ui`: admin pages, API wiring, route and locale updates.
- `device-platform-cloud-api-adapter`: `cloud_api` adapter, vendor signing, callbacks, raw messages.
- `device-platform-verifier`: final evidence, tests, builds, smoke tests, and sensitive scan.

## Hard Rules

1. Every feature lane must use an independent worktree and branch.
2. Every feature lane must have its own commit before integration.
3. Every feature lane must verify in its own worktree before merge.
4. `master` is an integration branch; it must not receive direct feature commits.
5. Integration uses `git merge --no-ff` to preserve visible branch lines.
6. Merge conflict fixes must only reconcile the merged branches and restore buildability.
7. If a business gap is found during integration, send it back to the owning lane.
8. Final delivery requires page-level unattended smoke testing; API-only checks are insufficient.

## Lane Acceptance Gate

Before a lane can merge, record:

- lane name
- agent or role
- worktree path
- branch
- commit hash
- clean `git status --short`
- verification commands and results
- bounded audit result

## Integration Gate

Review and candidate integration order:

1. `review/device-orchestration-rules`
2. `review/device-backend-contract`
3. `review/device-command-domain`
4. `review/device-simulator-gateway`
5. `review/device-webhook-audit`
6. `review/device-admin-console`
7. `review/device-cloud-api-adapter`
8. `review/device-verification`

If a lane fails audit or verification, do not patch it on `master`; return it to the lane branch.

## Naming Rules

Formal code artifacts must use long-lived business or technical responsibility names. Do not use stage, experiment, or temporary delivery words such as MVP, POC, demo, temp, tmp, or prototype in package names, directories, migration files, route names, menu keys, API types, tests, or branch names.

Stage language may appear only in internal planning notes, review notes, or learning records. `mock` is allowed only when it names an explicit simulator capability or third-party library convention.

## Bounded Ralph Audit Checklist

Use the Ralph verification discipline as a bounded checklist, not as an uncontrolled runtime loop:

- original requirement fully covered
- lane boundary respected
- fresh verification evidence exists
- no hidden direct feature commit on `master`
- no credential leak
- architectural or security risks noted
- final smoke test evidence collected

## Final Evidence

The final report must include:

- lane table with commit and verification
- merge commit table
- test/build results
- smoke test flow and screenshots/logs
- known external conditions for real hardware cloud API closure
- remaining risks or explicit none
