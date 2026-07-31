DO $$
BEGIN
    IF (SELECT count(*) FROM users) > 1 THEN
        RAISE EXCEPTION '006 requires users to contain at most one row';
    END IF;
    IF EXISTS (SELECT 1 FROM users WHERE is_admin IS DISTINCT FROM true) THEN
        RAISE EXCEPTION '006 requires the only user to be the setup administrator';
    END IF;
END
$$;

ALTER TABLE users
    ADD CONSTRAINT chk_users_single_admin CHECK (is_admin = true);

CREATE UNIQUE INDEX uq_users_singleton ON users ((true));

ALTER TABLE audit_logs
    DROP CONSTRAINT chk_audit_logs_action,
    ADD CONSTRAINT chk_audit_logs_action CHECK (action IN (
        'auth.login', 'auth.refresh', 'auth.logout',
        'project.created', 'project.updated', 'project.api_key_rotated', 'project.webhook_secret_rotated',
        'project.webhook_secret_decryption_failed',
        'device.created', 'device.updated', 'device.lifecycle_changed',
        'command.created', 'command.cancelled', 'webhook.delivery_replayed', 'simulator.updated',
        'provider.callback_rejected'
    ));
