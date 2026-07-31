ALTER TABLE device_commands
    DROP CONSTRAINT chk_device_commands_status_timing,
    ADD CONSTRAINT chk_device_commands_status_timing CHECK (
        (status = 'queued' AND sent_at IS NULL AND result_deadline_at IS NULL) OR
        (status IN ('sent', 'acked', 'success') AND sent_at IS NOT NULL AND result_deadline_at IS NOT NULL) OR
        (status = 'failed' AND (
            (reason_code = 'provider_not_configured' AND sent_at IS NULL AND result_deadline_at IS NULL) OR
            (reason_code IN ('provider_transport_error', 'provider_rejected', 'device_reported_failure') AND sent_at IS NOT NULL AND result_deadline_at IS NOT NULL)
        )) OR
        (status = 'unknown' AND sent_at IS NOT NULL AND result_deadline_at IS NOT NULL) OR
        (status = 'timeout' AND (
            (reason_code = 'dispatch_deadline_exceeded' AND sent_at IS NULL AND result_deadline_at IS NULL) OR
            (reason_code = 'result_observation_timeout' AND sent_at IS NOT NULL AND result_deadline_at IS NOT NULL)
        )) OR
        (status = 'cancelled' AND sent_at IS NULL AND result_deadline_at IS NULL)
    );
