DO $$
BEGIN
    IF EXISTS (SELECT 1 FROM device_events WHERE event_type = 'command.evidence_updated') THEN
        RAISE EXCEPTION 'cannot rollback command evidence Event contract while command.evidence_updated Events exist';
    END IF;
END
$$;

DROP TRIGGER trg_validate_command_evidence_event ON device_events;
DROP FUNCTION validate_command_evidence_event();

ALTER TABLE device_events
    DROP CONSTRAINT chk_device_events_associations,
    DROP CONSTRAINT chk_device_events_payload_contract,
    DROP CONSTRAINT chk_device_events_type,
    ADD CONSTRAINT chk_device_events_type CHECK (event_type IN (
        'device.created', 'device.lifecycle_changed', 'device.connection_changed',
        'device.state_updated', 'command.created', 'command.status_changed'
    )),
    ADD CONSTRAINT chk_device_events_payload_contract CHECK (
        (event_type = 'device.created' AND payload ?& ARRAY['device_type_code', 'provider_code', 'lifecycle_status']) OR
        (event_type = 'device.lifecycle_changed' AND payload ?& ARRAY['from', 'to', 'reason_code']) OR
        (event_type = 'device.connection_changed' AND payload ?& ARRAY['from', 'to', 'evidence_status']) OR
        (event_type = 'device.state_updated' AND payload ?& ARRAY['state', 'observed_at', 'evidence_status']) OR
        (event_type = 'command.created' AND payload ?& ARRAY['command_type', 'delivery_policy', 'status']) OR
        (event_type = 'command.status_changed' AND payload ?& ARRAY['from', 'to', 'reason_code', 'confirmation_level', 'evidence_status'])
    ),
    ADD CONSTRAINT chk_device_events_associations CHECK (
        (event_type IN ('command.created', 'command.status_changed') AND device_id IS NOT NULL AND command_id IS NOT NULL) OR
        (event_type IN ('device.created', 'device.lifecycle_changed', 'device.connection_changed', 'device.state_updated') AND device_id IS NOT NULL AND command_id IS NULL)
    );
