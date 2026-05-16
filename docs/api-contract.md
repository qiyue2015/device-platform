---
title: API and Command Contract
created: 2026-05-16
updated: 2026-05-16
status: current
---

# API and Command Contract

This document records implementation-facing contracts for the current repository.

Long-form rationale, vendor background, and source attachments remain in the Obsidian project knowledge base.

## API Namespaces

Version comes first in every API path.

```text
/v1/auth/...      Backend user authentication.
/v1/...           Logged-in backend APIs for normal project work.
/v1/admin/...     Platform administrator and operations APIs only.
/v1/open/...      Business-system Open API, authenticated by Project API Key.
```

Do not put all backend UI APIs under `/v1/admin/`. `admin` means platform-level administration, not simply "has login state".

## Open API Authentication

`/v1/open/...` is for machine-to-machine calls from business systems.

It must use project-level controls:

- `X-API-Key: {project_api_key}`
- IP whitelist when configured.
- HTTPS in non-local environments.

Open API must not use backend login session as its primary authentication.

## Core Open API

Minimum MVP-1 Open API:

```http
GET    /v1/open/projects/{project_id}
GET    /v1/open/devices
GET    /v1/open/devices/{device_id}
POST   /v1/open/device-commands
GET    /v1/open/device-commands/{command_id}
POST   /v1/open/device-commands/{command_id}/cancel
```

`GET /v1/open/devices/{device_id}` should include the current device state snapshot as `current_state`.

## Command Creation

`POST /v1/open/device-commands` accepts:

```text
device_id:        required
command_type:     required, for example unlock / lock / query_status / set_config / reboot
payload:          optional, required for command types that need parameters
idempotency_key:  optional but recommended
delivery_policy:  optional override when allowed
expires_at:       optional future absolute time
```

Idempotency scope:

```text
project_id + idempotency_key
```

Request hash:

```text
sha256(canonical_json(device_id, command_type, payload))
```

`expires_at` must not participate in the request hash.

Idempotency behavior:

| Case | Behavior |
| --- | --- |
| Key does not exist | Create command. |
| Key exists and request hash matches | Return existing command status. |
| Key exists and request hash differs | Return 409 `idempotency_key_conflict`. |

## Command Status

Supported command statuses:

```text
created
queued
sent
acked
success
failed
timeout
cancelled
offline
```

Normal creation should treat `created` as transient. A valid request should return after the command has moved to `queued` or `failed`.

Allowed state transitions:

```text
created -> queued -> sent -> acked -> success
queued -> sent
created -> queued -> offline
created -> cancelled
created -> failed
queued -> cancelled
queued -> failed
queued -> offline
sent -> timeout
sent -> failed
acked -> success
acked -> failed
acked -> timeout
timeout -> success
timeout -> failed
offline -> queued
offline -> failed
offline -> cancelled
```

Use conditional updates around status transitions so duplicate workers, late callbacks, and repeated simulator events do not produce duplicate final effects.

## Delivery Policy

Default delivery policies:

| Command type | Policy | Default timeout | Offline behavior |
| --- | --- | --- | --- |
| `unlock` | `online_only` | 10s | Fail directly. |
| `lock` | `online_only` | 10s | Fail directly. |
| `query_status` | `queue_until_expire` | 15s | Queue temporarily. |
| `set_config` | `replace_latest` | 60s | Keep only latest same-type command. |
| `reboot` | `queue_until_expire` | 30s | Queue temporarily. |

Physical action commands must default to `online_only` and must not be replayed after offline recovery.

`delivery_policy` overrides are allowed only for low-risk commands. Reject unsafe overrides for physical action commands with 400.

## Timeout and Compensation

Timeout starts when a command enters `sent`.

MVP-1 does not perform automatic retry. `timeout` and `failed` are terminal except for low-risk timeout compensation.

Low-risk commands using `queue_until_expire` or `replace_latest` may be corrected within the compensation window, default 5 minutes:

```text
timeout -> success
timeout -> failed
```

High-risk `online_only` commands such as `unlock` and `lock` must not be corrected after timeout.

When compensation happens, emit a new `command_finished` webhook with `corrected: true`.

## Offline Queue

When a device is offline:

- `online_only` commands fail directly.
- `queue_until_expire` commands enter `offline`.
- `replace_latest` commands cancel older same-device same-type commands in `offline` or `queued` state.

When a device returns online:

- Unexpired `offline` commands move back to `queued`.
- Expired offline commands become `failed` with reason `expired_while_offline`.

Implement both event-driven requeue on connection restore and a periodic fallback scan.

## Webhook Delivery

Webhook delivery should use an outbox-style record.

Minimum expectations:

- Record event type, payload, target URL, attempt count, status, and last error.
- Sign outbound webhook payloads.
- Retry failed deliveries 3 times in MVP-1.
- Mark exhausted deliveries as dead.
- Allow manual resend for dead deliveries through backend/admin UI.

Webhook state updates must not be lost when command state changes. Prefer writing command state, event record, and outbox record in one transaction where practical.
