DO $$
BEGIN
    IF EXISTS (SELECT 1 FROM device_commands WHERE status = 'unknown') THEN
        RAISE EXCEPTION '008 cannot remove Command unknown while historical unknown Commands exist';
    END IF;
    IF EXISTS (
        SELECT 1 FROM device_command_attempts
        WHERE outcome IN ('transport_error_after_send', 'invalid_response', 'device_acked', 'device_succeeded', 'device_failed')
    ) THEN
        RAISE EXCEPTION '008 cannot reinterpret historical CommandAttempt outcomes';
    END IF;
    IF EXISTS (SELECT 1 FROM device_commands) THEN
        RAISE EXCEPTION '008 cannot infer the complete effect fingerprint for historical Commands';
    END IF;
    IF NOT EXISTS (
        SELECT 1
        FROM device_types dt
        JOIN device_type_profiles p ON p.device_type_id = dt.id AND p.revision = 1
        WHERE dt.code = 'smart-lock'
          AND dt.current_revision = 1
          AND p.profile_hash = decode('81f6d5efb5f627a56fc19a2e2fb7fadcccc9b6a6b53fa411d7265a15eda5b596', 'hex')
    ) OR EXISTS (SELECT 1 FROM device_type_profiles WHERE revision = 2) THEN
        RAISE EXCEPTION '008 smart-lock revision 1 profile hash or revision state conflicts with the frozen release';
    END IF;
END
$$;

ALTER TABLE device_types DROP CONSTRAINT chk_device_types_release_profile;

INSERT INTO device_type_profiles (device_type_id, revision, profile, profile_hash)
SELECT id, 2, '{
    "actions": [
        {"identifier":"unlock","risk_level":"high","payload_schema":{"type":"object","maxProperties":0,"additionalProperties":false},"delivery_policy":"online_only","dispatch_deadline_ms":30000,"provider_request_timeout_ms":10000,"result_observation_timeout_ms":60000,"retry_allowed":false,"delivery_policy_override_allowed":false},
        {"identifier":"lock","risk_level":"high","payload_schema":{"type":"object","maxProperties":0,"additionalProperties":false},"delivery_policy":"online_only","dispatch_deadline_ms":30000,"provider_request_timeout_ms":10000,"result_observation_timeout_ms":60000,"retry_allowed":false,"delivery_policy_override_allowed":false},
        {"identifier":"query_status","risk_level":"low","payload_schema":{"type":"object","maxProperties":0,"additionalProperties":false},"delivery_policy":"dispatch_once","dispatch_deadline_ms":30000,"provider_request_timeout_ms":10000,"result_observation_timeout_ms":60000,"retry_allowed":false,"delivery_policy_override_allowed":false}
    ]
}'::jsonb, decode('853c4d6f3ad2bc73931de0bb64998f6d94bd977fce5f03ed0a190eef342de0e2', 'hex')
FROM device_types WHERE code = 'smart-lock';

UPDATE device_types SET current_revision = 2 WHERE code = 'smart-lock';

ALTER TABLE device_types
    ADD CONSTRAINT chk_device_types_release_profile CHECK (code = 'smart-lock' AND current_revision = 2);

ALTER TABLE device_commands
    DROP CONSTRAINT chk_device_commands_status,
    DROP CONSTRAINT chk_device_commands_delivery_policy,
    DROP CONSTRAINT chk_device_commands_status_confirmation,
    DROP CONSTRAINT chk_device_commands_reason,
    DROP CONSTRAINT chk_device_commands_reason_evidence,
    DROP CONSTRAINT chk_device_commands_finished,
    DROP CONSTRAINT chk_device_commands_status_timing,
    ADD COLUMN device_type_code TEXT NOT NULL,
    ADD COLUMN provider_code TEXT NOT NULL,
    ADD COLUMN provider_device_id TEXT NOT NULL,
    ADD COLUMN adapter TEXT NOT NULL,
    ADD COLUMN dispatch_deadline_ms BIGINT NOT NULL,
    ADD COLUMN provider_request_timeout_ms BIGINT NOT NULL,
    ADD COLUMN result_observation_timeout_ms BIGINT NOT NULL,
    ADD COLUMN retry_allowed BOOLEAN NOT NULL,
    ADD CONSTRAINT chk_device_commands_status CHECK (status IN ('queued', 'sent', 'acked', 'success', 'failed', 'timeout', 'cancelled')),
    ADD CONSTRAINT chk_device_commands_delivery_policy CHECK (delivery_policy IN ('dispatch_once', 'online_only')),
    ADD CONSTRAINT chk_device_commands_snapshot_identity CHECK (
        device_type_code = 'smart-lock'
        AND provider_code IN ('wwtiot', 'simulator')
        AND (
            (provider_code = 'wwtiot' AND adapter = 'wwtiot_cloud_api' AND provider_device_id ~ '^[A-Za-z0-9._:-]{1,128}$')
            OR (provider_code = 'simulator' AND adapter = 'simulator' AND provider_device_id = device_id::text)
        )
    ),
    ADD CONSTRAINT chk_device_commands_snapshot_policy CHECK (
        dispatch_deadline_ms > 0
        AND provider_request_timeout_ms > 0
        AND result_observation_timeout_ms > 0
        AND retry_allowed = false
        AND dispatch_deadline_at = queued_at + dispatch_deadline_ms * interval '1 millisecond'
    ),
    ADD CONSTRAINT chk_device_commands_status_confirmation CHECK (
        (status = 'queued' AND confirmation_level = 'none' AND evidence_status = 'none') OR
        (status = 'sent' AND confirmation_level IN ('none', 'transport_sent', 'provider_accepted')) OR
        (status = 'acked' AND confirmation_level IN ('device_acked', 'device_final') AND evidence_status = 'verified') OR
        (status = 'success' AND confirmation_level = 'device_final' AND evidence_status = 'verified') OR
        status IN ('failed', 'timeout', 'cancelled')
    ),
    ADD CONSTRAINT chk_device_commands_reason CHECK (
        (status IN ('queued', 'sent', 'acked', 'success') AND reason_code IS NULL) OR
        (status = 'failed' AND reason_code IN ('provider_not_configured', 'device_not_online', 'provider_transport_error', 'provider_rejected', 'device_reported_failure')) OR
        (status = 'timeout' AND reason_code IN ('dispatch_deadline_exceeded', 'result_observation_timeout')) OR
        (status = 'cancelled' AND reason_code = 'cancelled_by_request')
    ),
    ADD CONSTRAINT chk_device_commands_reason_evidence CHECK (
        reason_code IS NULL OR
        (reason_code IN ('provider_not_configured', 'device_not_online', 'provider_transport_error', 'dispatch_deadline_exceeded', 'cancelled_by_request') AND confirmation_level = 'none' AND evidence_status = 'none') OR
        (reason_code = 'provider_rejected' AND confirmation_level = 'transport_sent') OR
        (reason_code = 'device_reported_failure' AND confirmation_level = 'device_final' AND evidence_status = 'verified') OR
        (reason_code = 'result_observation_timeout' AND confirmation_level IN ('transport_sent', 'provider_accepted', 'device_acked'))
    ),
    ADD CONSTRAINT chk_device_commands_finished CHECK (
        (status IN ('queued', 'sent', 'acked') AND finished_at IS NULL) OR
        (status IN ('success', 'failed', 'timeout', 'cancelled') AND finished_at IS NOT NULL AND
            finished_at >= queued_at AND (sent_at IS NULL OR finished_at >= sent_at))
    ),
    ADD CONSTRAINT chk_device_commands_status_timing CHECK (
        (status = 'queued' AND sent_at IS NULL AND result_deadline_at IS NULL) OR
        (status IN ('sent', 'acked', 'success') AND sent_at IS NOT NULL AND result_deadline_at IS NOT NULL) OR
        (status = 'failed' AND (
            (reason_code IN ('provider_not_configured', 'device_not_online') AND sent_at IS NULL AND result_deadline_at IS NULL) OR
            (reason_code IN ('provider_transport_error', 'provider_rejected', 'device_reported_failure') AND sent_at IS NOT NULL AND result_deadline_at IS NOT NULL)
        )) OR
        (status = 'timeout' AND (
            (reason_code = 'dispatch_deadline_exceeded' AND sent_at IS NULL AND result_deadline_at IS NULL) OR
            (reason_code = 'result_observation_timeout' AND sent_at IS NOT NULL AND result_deadline_at IS NOT NULL)
        )) OR
        (status = 'cancelled' AND sent_at IS NULL AND result_deadline_at IS NULL)
    );

ALTER TABLE device_command_attempts
    DROP CONSTRAINT chk_command_attempts_outcome,
    DROP CONSTRAINT chk_command_attempts_dispatching_at,
    DROP CONSTRAINT chk_command_attempts_outcome_evidence;

ALTER TABLE device_command_attempts RENAME COLUMN error_code TO reason_code;

ALTER TABLE device_command_attempts
    ADD CONSTRAINT uq_command_attempts_id_command UNIQUE (id, command_id),
    ADD CONSTRAINT chk_command_attempts_outcome CHECK (outcome IS NULL OR outcome IN (
        'not_dispatched', 'invalid_request', 'provider_accepted', 'provider_rejected',
        'transport_error_before_send', 'indeterminate'
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
        (outcome IN ('provider_rejected', 'indeterminate') AND confirmation_level = 'transport_sent') OR
        (outcome = 'provider_accepted' AND confirmation_level = 'provider_accepted')
    ),
    ADD CONSTRAINT chk_command_attempts_reason CHECK (
        (outcome IS NULL AND reason_code IS NULL) OR
        (outcome = 'provider_accepted' AND reason_code IS NULL) OR
        (outcome = 'not_dispatched' AND reason_code IN ('cancelled_by_request', 'dispatch_deadline_exceeded', 'device_not_online')) OR
        (outcome = 'invalid_request' AND reason_code = 'provider_not_configured') OR
        (outcome = 'transport_error_before_send' AND reason_code = 'provider_transport_error') OR
        (outcome = 'provider_rejected' AND reason_code = 'provider_rejected') OR
        (outcome = 'indeterminate' AND reason_code IN ('provider_delivery_unknown', 'provider_response_invalid'))
    );

CREATE TABLE device_command_results (
    id UUID PRIMARY KEY,
    command_id UUID NOT NULL REFERENCES device_commands(id),
    attempt_id UUID,
    source TEXT NOT NULL,
    outcome TEXT NOT NULL,
    confirmation_level TEXT NOT NULL,
    evidence_status TEXT NOT NULL,
    deduplication_key TEXT NOT NULL,
    reported_at TIMESTAMPTZ,
    observed_at TIMESTAMPTZ NOT NULL,
    late BOOLEAN NOT NULL,
    payload JSONB NOT NULL,
    created_at TIMESTAMPTZ NOT NULL,
    CONSTRAINT fk_command_results_attempt
        FOREIGN KEY (attempt_id, command_id) REFERENCES device_command_attempts(id, command_id),
    CONSTRAINT chk_command_results_source CHECK (source IN ('provider_callback', 'simulator', 'system')),
    CONSTRAINT chk_command_results_outcome CHECK (outcome IN ('device_acked', 'device_succeeded', 'device_failed')),
    CONSTRAINT chk_command_results_confirmation CHECK (
        (outcome = 'device_acked' AND confirmation_level = 'device_acked') OR
        (outcome IN ('device_succeeded', 'device_failed') AND confirmation_level = 'device_final')
    ),
    CONSTRAINT chk_command_results_evidence CHECK (evidence_status = 'verified'),
    CONSTRAINT chk_command_results_deduplication CHECK (
        deduplication_key = btrim(deduplication_key) AND char_length(deduplication_key) BETWEEN 1 AND 256
    ),
    CONSTRAINT chk_command_results_payload CHECK (jsonb_typeof(payload) = 'object'),
    CONSTRAINT chk_command_results_timestamps CHECK (
        (reported_at IS NULL OR reported_at <= observed_at) AND created_at = observed_at
    ),
    CONSTRAINT uq_command_results_source_deduplication UNIQUE (source, deduplication_key)
);

CREATE INDEX idx_command_results_command_order
    ON device_command_results(command_id, observed_at ASC, id ASC);

CREATE FUNCTION reject_command_result_mutation() RETURNS trigger
LANGUAGE plpgsql AS $$
BEGIN
    RAISE EXCEPTION 'CommandResult records are immutable';
END
$$;

CREATE TRIGGER trg_command_results_immutable
BEFORE UPDATE OR DELETE ON device_command_results
FOR EACH ROW EXECUTE FUNCTION reject_command_result_mutation();

DROP TRIGGER trg_validate_command_evidence_event ON device_events;
DROP FUNCTION validate_command_evidence_event();

ALTER TABLE device_events
    DROP CONSTRAINT chk_device_events_associations,
    DROP CONSTRAINT chk_device_events_payload_contract,
    DROP CONSTRAINT chk_device_events_type,
    ADD CONSTRAINT chk_device_events_type CHECK (event_type IN (
        'device.created', 'device.lifecycle_changed', 'device.connection_changed',
        'device.state_updated', 'command.created', 'command.status_changed',
        'command.evidence_updated', 'command.result_recorded'
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
            AND payload->>'from' IN ('queued', 'sent', 'acked', 'success', 'failed', 'timeout', 'cancelled')
            AND payload->>'to' IN ('queued', 'sent', 'acked', 'success', 'failed', 'timeout', 'cancelled')
            AND payload->>'from' <> payload->>'to'
        ) OR
        (
            event_type = 'command.evidence_updated'
            AND payload ?& ARRAY['status', 'attempt_id', 'outcome', 'confirmation_level', 'evidence_status']
            AND payload->>'status' IN ('queued', 'sent', 'acked', 'success', 'failed', 'timeout', 'cancelled')
            AND payload->>'attempt_id' ~* '^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$'
            AND payload->>'outcome' IN ('not_dispatched', 'invalid_request', 'provider_accepted', 'provider_rejected', 'transport_error_before_send', 'indeterminate')
            AND payload->>'confirmation_level' IN ('none', 'transport_sent', 'provider_accepted', 'device_acked', 'device_final')
            AND payload->>'evidence_status' IN ('none', 'verified', 'unverified')
            AND deduplication_key = concat(
                'command.evidence_updated:', command_id::text,
                ':attempt:', payload->>'attempt_id',
                ':', payload->>'outcome',
                ':', payload->>'confirmation_level',
                ':', payload->>'evidence_status'
            )
        ) OR
        (
            event_type = 'command.result_recorded'
            AND payload ?& ARRAY['status', 'result_id', 'outcome', 'confirmation_level', 'evidence_status', 'late']
            AND payload->>'status' IN ('queued', 'sent', 'acked', 'success', 'failed', 'timeout', 'cancelled')
            AND payload->>'result_id' ~* '^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$'
            AND payload->>'outcome' IN ('device_acked', 'device_succeeded', 'device_failed')
            AND payload->>'confirmation_level' IN ('device_acked', 'device_final')
            AND payload->>'evidence_status' = 'verified'
            AND payload->>'late' IN ('true', 'false')
            AND deduplication_key = concat('command.result_recorded:', command_id::text, ':result:', payload->>'result_id')
        )
    ),
    ADD CONSTRAINT chk_device_events_associations CHECK (
        (event_type IN ('command.created', 'command.status_changed', 'command.evidence_updated', 'command.result_recorded') AND device_id IS NOT NULL AND command_id IS NOT NULL) OR
        (event_type IN ('device.created', 'device.lifecycle_changed', 'device.connection_changed', 'device.state_updated') AND device_id IS NOT NULL AND command_id IS NULL)
    );

CREATE FUNCTION validate_command_evidence_event() RETURNS trigger
LANGUAGE plpgsql
AS $$
BEGIN
    IF NEW.event_type <> 'command.evidence_updated' THEN
        RETURN NEW;
    END IF;
    IF NOT EXISTS (
        SELECT 1
        FROM device_command_attempts a
        JOIN device_commands c ON c.id = a.command_id
        WHERE a.id = (NEW.payload->>'attempt_id')::uuid
          AND a.command_id = NEW.command_id
          AND a.phase = 'completed'
          AND a.outcome = NEW.payload->>'outcome'
          AND a.confirmation_level = NEW.payload->>'confirmation_level'
          AND a.evidence_status = NEW.payload->>'evidence_status'
          AND c.status = NEW.payload->>'status'
          AND c.confirmation_level = NEW.payload->>'confirmation_level'
          AND c.evidence_status = NEW.payload->>'evidence_status'
    ) THEN
        RAISE EXCEPTION 'command.evidence_updated must match its completed Attempt and current Command evidence';
    END IF;
    RETURN NEW;
END
$$;

CREATE TRIGGER trg_validate_command_evidence_event
BEFORE INSERT OR UPDATE ON device_events
FOR EACH ROW EXECUTE FUNCTION validate_command_evidence_event();

CREATE FUNCTION validate_command_result_event() RETURNS trigger
LANGUAGE plpgsql
AS $$
BEGIN
    IF NEW.event_type <> 'command.result_recorded' THEN
        RETURN NEW;
    END IF;
    IF NOT EXISTS (
        SELECT 1
        FROM device_command_results r
        JOIN device_commands c ON c.id = r.command_id
        WHERE r.id = (NEW.payload->>'result_id')::uuid
          AND r.command_id = NEW.command_id
          AND r.outcome = NEW.payload->>'outcome'
          AND r.confirmation_level = NEW.payload->>'confirmation_level'
          AND r.evidence_status = NEW.payload->>'evidence_status'
          AND r.late = (NEW.payload->>'late')::boolean
          AND c.status = NEW.payload->>'status'
    ) THEN
        RAISE EXCEPTION 'command.result_recorded must match its immutable Result and current Command status';
    END IF;
    RETURN NEW;
END
$$;

CREATE TRIGGER trg_validate_command_result_event
BEFORE INSERT OR UPDATE ON device_events
FOR EACH ROW EXECUTE FUNCTION validate_command_result_event();
