DO $$
BEGIN
    IF EXISTS (
        SELECT 1
        FROM device_events
        WHERE event_type = 'command.status_changed'
          AND (
              payload->>'from' IS NULL
              OR payload->>'to' IS NULL
              OR payload->>'from' = payload->>'to'
          )
    ) THEN
        RAISE EXCEPTION '007 cannot enforce status Event transitions while invalid command.status_changed Events exist';
    END IF;
END
$$;

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
