DO $$
BEGIN
    IF EXISTS (SELECT 1 FROM device_command_results) THEN
        RAISE EXCEPTION 'cannot rollback Platform Core v4 while CommandResult records exist';
    END IF;
    IF EXISTS (SELECT 1 FROM device_commands WHERE device_type_revision = 2) THEN
        RAISE EXCEPTION 'cannot rollback Platform Core v4 while revision 2 Commands exist';
    END IF;
END
$$;

DROP TRIGGER trg_validate_command_result_event ON device_events;
DROP FUNCTION validate_command_result_event();

ALTER TABLE device_events
    DROP CONSTRAINT chk_device_events_associations,
    DROP CONSTRAINT chk_device_events_payload_contract,
    DROP CONSTRAINT chk_device_events_type,
    ADD CONSTRAINT chk_device_events_type CHECK (event_type IN (
        'device.created', 'device.lifecycle_changed', 'device.connection_changed',
        'device.state_updated', 'command.created', 'command.status_changed',
        'command.evidence_updated'
    )),
    ADD CONSTRAINT chk_device_events_payload_contract CHECK (
        (event_type = 'device.created' AND payload ?& ARRAY['device_type_code', 'provider_code', 'lifecycle_status']) OR
        (event_type = 'device.lifecycle_changed' AND payload ?& ARRAY['from', 'to', 'reason_code']) OR
        (event_type = 'device.connection_changed' AND payload ?& ARRAY['from', 'to', 'evidence_status']) OR
        (event_type = 'device.state_updated' AND payload ?& ARRAY['state', 'observed_at', 'evidence_status']) OR
        (event_type = 'command.created' AND payload ?& ARRAY['command_type', 'delivery_policy', 'status']) OR
        (
            event_type = 'command.status_changed'
            AND payload ?& ARRAY['from', 'to', 'reason_code', 'confirmation_level', 'evidence_status']
            AND payload->>'from' IN ('queued', 'sent', 'acked', 'success', 'failed', 'timeout', 'cancelled', 'unknown')
            AND payload->>'to' IN ('queued', 'sent', 'acked', 'success', 'failed', 'timeout', 'cancelled', 'unknown')
            AND payload->>'from' <> payload->>'to'
        ) OR
        (
            event_type = 'command.evidence_updated'
            AND payload ?& ARRAY['status', 'attempt_id', 'outcome', 'confirmation_level', 'evidence_status']
            AND payload->>'status' IN ('queued', 'sent', 'acked', 'success', 'failed', 'timeout', 'cancelled', 'unknown')
            AND payload->>'attempt_id' ~* '^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$'
            AND payload->>'outcome' IN (
                'not_dispatched', 'invalid_request', 'provider_accepted', 'provider_rejected',
                'transport_error_before_send', 'transport_error_after_send', 'invalid_response',
                'device_acked', 'device_succeeded', 'device_failed'
            )
            AND payload->>'confirmation_level' IN ('none', 'transport_sent', 'provider_accepted', 'device_acked', 'device_final')
            AND payload->>'evidence_status' IN ('none', 'verified', 'unverified')
            AND deduplication_key = concat(
                'command.evidence_updated:', command_id::text,
                ':attempt:', payload->>'attempt_id',
                ':', payload->>'outcome',
                ':', payload->>'confirmation_level',
                ':', payload->>'evidence_status'
            )
        )
    ),
    ADD CONSTRAINT chk_device_events_associations CHECK (
        (event_type IN ('command.created', 'command.status_changed', 'command.evidence_updated') AND device_id IS NOT NULL AND command_id IS NOT NULL) OR
        (event_type IN ('device.created', 'device.lifecycle_changed', 'device.connection_changed', 'device.state_updated') AND device_id IS NOT NULL AND command_id IS NULL)
    );

DROP TRIGGER trg_command_results_immutable ON device_command_results;
DROP FUNCTION reject_command_result_mutation();
DROP TABLE device_command_results;

ALTER TABLE device_command_attempts
    DROP CONSTRAINT uq_command_attempts_id_command,
    DROP CONSTRAINT chk_command_attempts_outcome,
    DROP CONSTRAINT chk_command_attempts_dispatching_at,
    DROP CONSTRAINT chk_command_attempts_outcome_evidence,
    DROP CONSTRAINT chk_command_attempts_reason;

ALTER TABLE device_command_attempts RENAME COLUMN reason_code TO error_code;

ALTER TABLE device_command_attempts
    ADD CONSTRAINT chk_command_attempts_outcome CHECK (outcome IS NULL OR outcome IN (
        'not_dispatched', 'invalid_request', 'provider_accepted', 'provider_rejected',
        'transport_error_before_send', 'transport_error_after_send', 'invalid_response',
        'device_acked', 'device_succeeded', 'device_failed'
    )),
    ADD CONSTRAINT chk_command_attempts_dispatching_at CHECK (
        (phase = 'claimed' AND dispatching_at IS NULL) OR
        (phase = 'dispatching' AND dispatching_at IS NOT NULL) OR
        (phase = 'completed' AND (
            (outcome IN ('not_dispatched', 'invalid_request') AND dispatching_at IS NULL) OR
            (outcome NOT IN ('not_dispatched', 'invalid_request') AND dispatching_at IS NOT NULL)
        ))
    ),
    ADD CONSTRAINT chk_command_attempts_outcome_evidence CHECK (
        outcome IS NULL OR
        (outcome IN ('not_dispatched', 'invalid_request', 'transport_error_before_send') AND confirmation_level = 'none' AND evidence_status = 'none') OR
        (outcome IN ('transport_error_after_send', 'invalid_response', 'provider_rejected') AND confirmation_level = 'transport_sent') OR
        (outcome = 'provider_accepted' AND confirmation_level = 'provider_accepted') OR
        (outcome = 'device_acked' AND confirmation_level IN ('device_acked', 'device_final') AND evidence_status = 'verified') OR
        (outcome IN ('device_succeeded', 'device_failed') AND confirmation_level = 'device_final' AND evidence_status = 'verified')
    );

ALTER TABLE device_commands
    DROP CONSTRAINT chk_device_commands_status,
    DROP CONSTRAINT chk_device_commands_delivery_policy,
    DROP CONSTRAINT chk_device_commands_snapshot_identity,
    DROP CONSTRAINT chk_device_commands_snapshot_policy,
    DROP CONSTRAINT chk_device_commands_status_confirmation,
    DROP CONSTRAINT chk_device_commands_reason,
    DROP CONSTRAINT chk_device_commands_reason_evidence,
    DROP CONSTRAINT chk_device_commands_finished,
    DROP CONSTRAINT chk_device_commands_status_timing,
    DROP COLUMN device_type_code,
    DROP COLUMN provider_code,
    DROP COLUMN provider_device_id,
    DROP COLUMN adapter,
    DROP COLUMN dispatch_deadline_ms,
    DROP COLUMN provider_request_timeout_ms,
    DROP COLUMN result_observation_timeout_ms,
    DROP COLUMN retry_allowed,
    ADD CONSTRAINT chk_device_commands_status CHECK (status IN ('queued', 'sent', 'acked', 'success', 'failed', 'timeout', 'cancelled', 'unknown')),
    ADD CONSTRAINT chk_device_commands_delivery_policy CHECK (delivery_policy = 'dispatch_once'),
    ADD CONSTRAINT chk_device_commands_status_confirmation CHECK (
        (status = 'queued' AND confirmation_level = 'none' AND evidence_status = 'none') OR
        (status = 'sent' AND confirmation_level IN ('none', 'transport_sent', 'provider_accepted')) OR
        (status = 'acked' AND confirmation_level IN ('device_acked', 'device_final') AND evidence_status = 'verified') OR
        (status = 'success' AND confirmation_level = 'device_final' AND evidence_status = 'verified') OR
        status IN ('failed', 'timeout', 'cancelled', 'unknown')
    ),
    ADD CONSTRAINT chk_device_commands_reason CHECK (
        (status IN ('queued', 'sent', 'acked', 'success') AND reason_code IS NULL) OR
        (status = 'failed' AND reason_code IN ('provider_not_configured', 'provider_transport_error', 'provider_rejected', 'device_reported_failure')) OR
        (status = 'unknown' AND reason_code IN ('provider_response_invalid', 'provider_delivery_unknown')) OR
        (status = 'timeout' AND reason_code IN ('dispatch_deadline_exceeded', 'result_observation_timeout')) OR
        (status = 'cancelled' AND reason_code = 'cancelled_by_request')
    ),
    ADD CONSTRAINT chk_device_commands_reason_evidence CHECK (
        reason_code IS NULL OR
        (reason_code IN ('provider_not_configured', 'provider_transport_error', 'dispatch_deadline_exceeded', 'cancelled_by_request') AND confirmation_level = 'none' AND evidence_status = 'none') OR
        (reason_code IN ('provider_rejected', 'provider_response_invalid', 'provider_delivery_unknown') AND confirmation_level = 'transport_sent') OR
        (reason_code = 'device_reported_failure' AND confirmation_level = 'device_final' AND evidence_status = 'verified') OR
        (reason_code = 'result_observation_timeout' AND confirmation_level IN ('transport_sent', 'provider_accepted', 'device_acked'))
    ),
    ADD CONSTRAINT chk_device_commands_finished CHECK (
        (status IN ('queued', 'sent', 'acked') AND finished_at IS NULL) OR
        (status IN ('success', 'failed', 'timeout', 'cancelled', 'unknown') AND finished_at IS NOT NULL AND
            finished_at >= queued_at AND (sent_at IS NULL OR finished_at >= sent_at))
    ),
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

ALTER TABLE device_types DROP CONSTRAINT chk_device_types_release_profile;
UPDATE device_types SET current_revision = 1 WHERE code = 'smart-lock';
DROP TRIGGER trg_device_type_profiles_immutable ON device_type_profiles;
DELETE FROM device_type_profiles WHERE revision = 2;
CREATE TRIGGER trg_device_type_profiles_immutable
BEFORE UPDATE OR DELETE ON device_type_profiles
FOR EACH ROW EXECUTE FUNCTION reject_device_type_profile_mutation();
ALTER TABLE device_types
    ADD CONSTRAINT chk_device_types_release_profile CHECK (code = 'smart-lock' AND current_revision = 1);
