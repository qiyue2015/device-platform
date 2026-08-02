DROP INDEX uq_users_singleton;

ALTER TABLE users
    DROP CONSTRAINT chk_users_single_admin;

ALTER TABLE users RENAME COLUMN is_admin TO is_super_admin;

ALTER TABLE users
    ADD COLUMN status TEXT NOT NULL DEFAULT 'active',
    ADD CONSTRAINT chk_users_status CHECK (status IN ('active', 'disabled')),
    ADD CONSTRAINT chk_users_super_admin_active CHECK (NOT is_super_admin OR status = 'active');

CREATE UNIQUE INDEX uq_users_single_super_admin
    ON users ((is_super_admin))
    WHERE is_super_admin = true;

CREATE UNIQUE INDEX uq_users_email_normalized ON users (lower(email));

ALTER TABLE projects ADD COLUMN manager_user_id UUID;

UPDATE projects
SET manager_user_id = (
    SELECT id
    FROM users
    WHERE is_super_admin = true
);

DO $$
BEGIN
    IF EXISTS (SELECT 1 FROM projects WHERE manager_user_id IS NULL) THEN
        RAISE EXCEPTION '010 cannot backfill Project manager without the installation super administrator';
    END IF;
END $$;

ALTER TABLE projects
    ALTER COLUMN manager_user_id SET NOT NULL,
    ADD CONSTRAINT fk_projects_manager_user
        FOREIGN KEY (manager_user_id) REFERENCES users(id) ON DELETE RESTRICT;

CREATE INDEX idx_projects_manager_created
    ON projects(manager_user_id, created_at DESC, id DESC);

ALTER TABLE audit_logs
    DROP CONSTRAINT chk_audit_logs_actor_type,
    DROP CONSTRAINT chk_audit_logs_action,
    ADD COLUMN actor_user_id UUID REFERENCES users(id) ON DELETE RESTRICT;

UPDATE audit_logs AS audit
SET actor_type = 'user',
    actor_user_id = target.id,
    actor_id = NULL
FROM users AS target
WHERE audit.actor_type = 'admin'
  AND audit.actor_id = target.id::text;

UPDATE audit_logs
SET actor_type = 'system',
    actor_id = COALESCE(actor_id, 'anonymous')
WHERE actor_type = 'admin';

ALTER TABLE audit_logs
    ADD CONSTRAINT chk_audit_logs_actor_type CHECK (actor_type IN ('user', 'project', 'provider', 'system')),
    ADD CONSTRAINT chk_audit_logs_actor_identity CHECK (
        (actor_type = 'user' AND actor_user_id IS NOT NULL AND actor_id IS NULL)
        OR (actor_type <> 'user' AND actor_user_id IS NULL)
    ),
    ADD CONSTRAINT chk_audit_logs_action CHECK (action IN (
        'auth.login', 'auth.refresh', 'auth.logout',
        'user.created', 'user.status_changed',
        'project.created', 'project.updated', 'project.transferred',
        'project.api_key_rotated', 'project.webhook_secret_rotated',
        'project.webhook_secret_decryption_failed',
        'device.created', 'device.updated', 'device.lifecycle_changed',
        'command.created', 'command.cancelled',
        'webhook.delivery_replayed', 'simulator.updated',
        'provider.message_received', 'provider.message_rejected',
        'provider.callback_rejected', 'command.dispatch_recorded'
    ));

CREATE INDEX idx_audit_logs_actor_user_occurred
    ON audit_logs(actor_user_id, occurred_at DESC, id DESC)
    WHERE actor_user_id IS NOT NULL;
