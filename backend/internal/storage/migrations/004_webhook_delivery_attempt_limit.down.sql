DO $$
BEGIN
    IF EXISTS (
        SELECT 1
        FROM webhook_deliveries
        WHERE status = 'dead' AND attempt_count <> 5
    ) THEN
        RAISE EXCEPTION 'cannot rollback configurable Webhook attempt limit while early-dead Deliveries exist';
    END IF;
END $$;

ALTER TABLE webhook_deliveries
    DROP CONSTRAINT chk_webhook_deliveries_state,
    ADD CONSTRAINT chk_webhook_deliveries_state CHECK (
        (status = 'pending' AND attempt_count = 0 AND next_attempt_at IS NOT NULL AND lease_token IS NULL) OR
        (status = 'sending' AND attempt_count BETWEEN 1 AND 5 AND lease_token IS NOT NULL) OR
        (status = 'failed' AND attempt_count BETWEEN 1 AND 4 AND next_attempt_at IS NOT NULL AND lease_token IS NULL) OR
        (status = 'delivered' AND attempt_count BETWEEN 1 AND 5 AND next_attempt_at IS NULL AND lease_token IS NULL) OR
        (status = 'dead' AND attempt_count = 5 AND next_attempt_at IS NULL AND lease_token IS NULL)
    );
