# Database Migrations

This directory stores SQL migrations for the device platform development schema.

## Naming

Use sequential, descriptive file names:

```text
001_create_projects.sql
002_create_devices.sql
003_create_device_commands.sql
```

## Rules

- Keep each migration focused on one logical schema change.
- Do not modify a migration after it has been applied to any shared environment.
- If an applied migration needs correction, create a new migration that fixes it.
- Prefer forward-only migrations; document manual rollback steps when needed.
- Run migrations from `backend/` with `make migrate-up`.

`make migrate-up` and `make migrate-down` read `backend/.env` when it exists, so local `DATABASE_URL` can stay there.
