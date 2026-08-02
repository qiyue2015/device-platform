DO $$
DECLARE
    super_admin_id UUID;
BEGIN
    IF (SELECT count(*) FROM users) > 1 THEN
        RAISE EXCEPTION 'cannot rollback multi-user authorization while multiple Users exist';
    END IF;
    SELECT id INTO super_admin_id FROM users WHERE is_super_admin = true;
    IF EXISTS (
        SELECT 1 FROM projects
        WHERE super_admin_id IS NULL OR manager_user_id <> super_admin_id
    ) THEN
        RAISE EXCEPTION 'cannot rollback multi-user authorization while Project managers cannot be represented';
    END IF;
    IF EXISTS (
        SELECT 1 FROM audit_logs
        WHERE action IN ('user.created', 'user.status_changed', 'project.transferred')
    ) THEN
        RAISE EXCEPTION 'cannot rollback multi-user authorization while new Audit actions exist';
    END IF;
END $$;

DROP INDEX idx_audit_logs_actor_user_occurred;

ALTER TABLE audit_logs
    DROP CONSTRAINT chk_audit_logs_actor_identity,
    DROP CONSTRAINT chk_audit_logs_actor_type,
    DROP CONSTRAINT chk_audit_logs_action;

UPDATE audit_logs
SET actor_type = 'admin',
    actor_id = actor_user_id::text
WHERE actor_type = 'user';

ALTER TABLE audit_logs
    DROP COLUMN actor_user_id,
    ADD CONSTRAINT chk_audit_logs_actor_type CHECK (actor_type IN ('admin', 'project', 'provider', 'system')),
    ADD CONSTRAINT chk_audit_logs_action CHECK (action IN (
        'auth.login', 'auth.refresh', 'auth.logout',
        'project.created', 'project.updated', 'project.api_key_rotated', 'project.webhook_secret_rotated',
        'project.webhook_secret_decryption_failed',
        'device.created', 'device.updated', 'device.lifecycle_changed',
        'command.created', 'command.cancelled', 'command.dispatch_recorded',
        'webhook.delivery_replayed', 'simulator.updated',
        'provider.callback_rejected', 'provider.message_received', 'provider.message_rejected'
    ));

DROP INDEX idx_projects_manager_created;

ALTER TABLE projects
    DROP CONSTRAINT fk_projects_manager_user,
    DROP COLUMN manager_user_id;

DROP INDEX uq_users_single_super_admin;
DROP INDEX uq_users_email_normalized;

ALTER TABLE users
    DROP CONSTRAINT chk_users_super_admin_active,
    DROP CONSTRAINT chk_users_status,
    DROP COLUMN status;

ALTER TABLE users RENAME COLUMN is_super_admin TO is_admin;

ALTER TABLE users
    ADD CONSTRAINT chk_users_single_admin CHECK (is_admin = true);

CREATE UNIQUE INDEX uq_users_singleton ON users ((true));
