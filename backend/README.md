# device-platform

IoT Device Platform — general-purpose device management, command dispatch, and event delivery.

## Quick Start

```bash
cp .env.example .env
make run
```

On a fresh environment, start the frontend and complete `/setup` before using `/auth/login`. The setup wizard applies the same migrations used by `make migrate-up` and creates the first administrator in the `users` table.

## Development

Use `make migrate-up` only when you need to apply migrations manually against an existing configured `DATABASE_URL`.
