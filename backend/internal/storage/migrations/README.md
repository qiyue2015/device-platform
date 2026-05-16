# Database Migrations

This directory stores SQL migrations for the device platform development schema.

## Naming

Use standard `golang-migrate` up/down file names:

```text
001_device_platform_core.up.sql
001_device_platform_core.down.sql
```

## Rules

- Keep each migration focused on one logical schema change.
- Do not modify a migration after it has been applied to any shared environment.
- If an applied migration needs correction, create a new migration that fixes it.
- Prefer forward-only migrations; document manual rollback steps when needed.
- Run migrations from `backend/` with `make migrate-up`.
- The setup wizard uses the same embedded `.up.sql` files during first installation.

`make migrate-up` and `make migrate-down` read `backend/.env` when it exists, so local `DATABASE_URL` can stay there.
