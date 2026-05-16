---
title: MVP-1 Engineering Contract
created: 2026-05-16
updated: 2026-05-16
status: current
---

# MVP-1 Engineering Contract

MVP-1 is the first implementable stage of the device platform.

The goal is to verify the generic device-platform loop without depending on vendor credentials, vendor callback configuration, or real hardware protocol behavior.

Smart locks are only the first hardware sample. The implementation must stay generic: device type, capabilities, adapter behavior, command payload, and raw message records should carry device differences.

## Scope

MVP-1 must include:

- Project creation and update.
- Device creation and detail view.
- Open API for business systems under `/v1/open/...`.
- Auth API under `/v1/auth/...`.
- Logged-in backend API under `/v1/...`.
- Platform-admin-only API under `/v1/admin/...`.
- Command creation, dispatch, status tracking, timeout handling, and cancellation where allowed.
- Simulator gateway with configurable behavior.
- Webhook delivery with retry and delivery record visibility.
- Audit records for important project, device, command, and security actions.
- Minimal admin UI for acceptance testing.

MVP-1 must not depend on:

- Vendor UserID / UserKey.
- Vendor callback URL configuration.
- Real device TCP/BLE protocol.
- Real hardware connectivity.

## Minimal Admin UI

The MVP-1 admin UI only needs the surfaces required for acceptance:

- Project list, create, and edit, including webhook URL and IP whitelist.
- Device list, create, and detail, including `connection_status` and `lifecycle_status`.
- Command history list and detail, including status and dispatch attempts.
- Webhook delivery list and manual resend for dead deliveries.
- Simulator gateway mode switch panel.

The MVP-1 admin UI does not need:

- Full dashboard charts.
- Full user/role management UI.
- Geofence management.
- Device map display.

## Simulator Modes

The simulator must support these modes:

| Mode | Meaning |
| --- | --- |
| `normal` | Immediate ACK and successful result. |
| `delay` | Delayed response with configurable delay. |
| `offline` | Stop heartbeat and let platform mark the device offline. |
| `timeout_then_ack` | Send ACK/result after timeout to verify compensation behavior. |
| `duplicate_ack` | Send repeated ACKs to verify idempotent event handling. |
| `fail` | Return execution failure. |

`offline` behavior:

- Switching to `offline` stops simulator heartbeat.
- The platform marks the device offline after heartbeat timeout.
- `online_only` commands such as `unlock` and `lock` fail directly after the device is offline.
- `queue_until_expire` commands enter offline queue.
- Switching back to `normal` restores heartbeat and allows unexpired offline commands to re-enter the queue.

Simulator mode should be switchable by admin API or configuration without restarting the service.

## Acceptance Criteria

MVP-1 is accepted when all of these are true:

- A project can be created and configured with webhook URL and IP whitelist.
- A device can be registered and queried.
- A command can be created through `/v1/open/device-commands`.
- Command status can be tracked from creation to final state.
- Command attempts are recorded.
- Device online/offline state can be observed.
- `unlock` fails directly when the device is offline.
- `query_status` can queue while offline and execute after the device comes back online.
- Timeout behavior can be verified.
- Late ACK/result behavior can be verified for allowed low-risk commands.
- Duplicate ACKs do not produce duplicate final effects.
- A failed simulator response marks the command failed.
- Webhook deliveries are recorded and retried.
- Dead webhook deliveries can be manually resent.
- Audit logs show important security and command operations.

## Later Stages

MVP-1.5 is the vendor-cloud adapter stage. Coding can start for adapter skeleton, signing, callback endpoint, raw message records, and event normalization, but real integration requires vendor credentials, callback URL configuration, source IP confirmation, and test-device ownership confirmation.

MVP-2 is the direct-device protocol stage. Coding can start for parser/encoder skeleton and offline protocol samples, but real gateway integration requires real protocol verification, IMEI/device mapping, device model confirmation, and proof that the test device can point to this platform.
