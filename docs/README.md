---
title: Device Platform Engineering Docs
created: 2026-05-16
updated: 2026-05-16
status: current
---

# Device Platform Engineering Docs

This directory is the engineering documentation entry point for the device platform repository.

It is intentionally not a full copy of the Obsidian project knowledge base. The split is:

- Obsidian owns project background, decisions, vendor communication, source attachments, and long-form rationale.
- This repository owns implementation-facing contracts that must stay aligned with code.

Full project knowledge base:

```text
/Users/fengqiyue/Documents/Obsidian Vault/Projects/设备平台/
```

## Engineering Documents

- [MVP-1 Contract](./mvp-1-contract.md): current coding scope, acceptance criteria, simulator behavior, and stage boundaries.
- [API Contract](./api-contract.md): API namespace rules, command lifecycle, delivery policy, and webhook/outbox expectations.
- [Local Development](./local-development.md): local MVP-1 run commands, env files, health check, and simulator acceptance path.

## Maintenance Rule

Do not duplicate long design explanations from Obsidian here.

When something changes:

- Change Obsidian if the change is about why the platform is built this way, vendor facts, scope decisions, or pending questions.
- Change repository docs if the change affects current code behavior, API contracts, database semantics, runtime commands, deployment, tests, or acceptance criteria.
- Change both only when a design decision changes and the implementation contract also changes.

## Current Stage

MVP-1 can enter implementation.

MVP-1 is a simulator-backed closed loop with no external vendor dependency. It should prove the platform skeleton before real vendor-cloud or direct-device integration:

```text
Project -> Device -> Command -> Gateway/Adapter -> State/Event -> Webhook
```

MVP-1.5 and MVP-2 are documented as follow-up stages, but their real integration and acceptance still depend on vendor credentials, callback configuration, device ownership confirmation, and real-device protocol verification.
