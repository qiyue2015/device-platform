DO $$
BEGIN
    IF EXISTS (
        SELECT 1
        FROM audit_logs
        WHERE action IN ('project.webhook_secret_decryption_failed', 'provider.callback_rejected')
    ) THEN
        RAISE EXCEPTION 'cannot rollback audit log actions while extended Audit actions exist';
    END IF;
END $$;

ALTER TABLE audit_logs
    DROP CONSTRAINT chk_audit_logs_action,
    ADD CONSTRAINT chk_audit_logs_action CHECK (action IN (
        'setup.completed', 'auth.login', 'auth.refresh', 'auth.logout',
        'project.created', 'project.updated', 'project.api_key_rotated', 'project.webhook_secret_rotated',
        'device.created', 'device.updated', 'device.lifecycle_changed',
        'command.created', 'command.cancelled', 'webhook.delivery_replayed', 'simulator.updated'
    ));
