DO $$
BEGIN
    IF EXISTS (
        SELECT 1
        FROM devices
        GROUP BY lower(provider_code), provider_device_id
        HAVING COUNT(*) > 1
    ) THEN
        RAISE EXCEPTION 'cannot preserve Provider identity tombstones while historical identities are reused';
    END IF;
END
$$;

DROP INDEX uq_devices_active_provider_identity;
CREATE UNIQUE INDEX uq_devices_provider_identity
    ON devices(lower(provider_code), provider_device_id);

ALTER TABLE devices
    ADD COLUMN provider_profile TEXT;

UPDATE devices
SET provider_profile = CASE provider_code
    WHEN 'wwtiot' THEN 'wwtiot-cloud-api-v2'
    WHEN 'simulator' THEN 'simulator-v1'
END;

ALTER TABLE devices
    ALTER COLUMN provider_profile SET NOT NULL,
    DROP CONSTRAINT chk_devices_provider_code,
    DROP CONSTRAINT chk_devices_access_type,
    DROP CONSTRAINT chk_devices_transport_protocol,
    DROP CONSTRAINT chk_devices_adapter,
    DROP CONSTRAINT chk_devices_provider_binding,
    ADD CONSTRAINT chk_devices_provider_code CHECK (provider_code IN ('wwtiot', 'omni', 'simulator')),
    ADD CONSTRAINT chk_devices_access_type CHECK (access_type IN ('cloud_api', 'direct_device', 'simulator')),
    ADD CONSTRAINT chk_devices_transport_protocol CHECK (transport_protocol IN ('http', 'tcp', 'internal')),
    ADD CONSTRAINT chk_devices_adapter CHECK (adapter IN ('wwtiot_cloud_api', 'omni_direct_tcp', 'simulator')),
    ADD CONSTRAINT chk_devices_provider_binding CHECK (
        (provider_code = 'wwtiot' AND provider_profile = 'wwtiot-cloud-api-v2'
            AND access_type = 'cloud_api' AND transport_protocol = 'http' AND adapter = 'wwtiot_cloud_api'
            AND provider_device_id ~ '^[A-Za-z0-9._:-]{1,128}$')
        OR
        (provider_code = 'omni' AND provider_profile IN ('omni-bike-tcp-v2.0.7', 'omni-iot-tcp-v1.3.5')
            AND access_type = 'direct_device' AND transport_protocol = 'tcp' AND adapter = 'omni_direct_tcp'
            AND provider_device_id ~ '^[0-9]{15}$')
        OR
        (provider_code = 'simulator' AND provider_profile = 'simulator-v1'
            AND access_type = 'simulator' AND transport_protocol = 'internal' AND adapter = 'simulator'
            AND provider_device_id = id::text)
    ),
    ADD CONSTRAINT uq_devices_command_binding
        UNIQUE (id, provider_code, provider_profile, provider_device_id, adapter);

ALTER TABLE device_commands
    ADD COLUMN provider_profile TEXT;

UPDATE device_commands
SET provider_profile = CASE provider_code
    WHEN 'wwtiot' THEN 'wwtiot-cloud-api-v2'
    WHEN 'simulator' THEN 'simulator-v1'
END;

ALTER TABLE device_commands
    ALTER COLUMN provider_profile SET NOT NULL,
    DROP CONSTRAINT chk_device_commands_snapshot_identity,
    DROP CONSTRAINT chk_device_commands_reason,
    DROP CONSTRAINT chk_device_commands_reason_evidence,
    DROP CONSTRAINT chk_device_commands_status_timing,
    ADD CONSTRAINT chk_device_commands_snapshot_identity CHECK (
        device_type_code = 'smart-lock'
        AND (
            (provider_code = 'wwtiot' AND provider_profile = 'wwtiot-cloud-api-v2'
                AND adapter = 'wwtiot_cloud_api' AND provider_device_id ~ '^[A-Za-z0-9._:-]{1,128}$')
            OR
            (provider_code = 'omni' AND provider_profile IN ('omni-bike-tcp-v2.0.7', 'omni-iot-tcp-v1.3.5')
                AND adapter = 'omni_direct_tcp' AND provider_device_id ~ '^[0-9]{15}$')
            OR
            (provider_code = 'simulator' AND provider_profile = 'simulator-v1'
                AND adapter = 'simulator' AND provider_device_id = device_id::text)
        )
    ),
    ADD CONSTRAINT chk_device_commands_reason CHECK (
        (status IN ('queued', 'sent', 'acked', 'success') AND reason_code IS NULL) OR
        (status = 'failed' AND reason_code IN (
            'provider_not_configured', 'provider_action_unsupported', 'provider_mapping_unknown',
            'provider_session_unavailable', 'provider_request_invalid', 'device_not_online',
            'provider_transport_error', 'provider_rejected', 'device_reported_failure'
        )) OR
        (status = 'timeout' AND reason_code IN ('dispatch_deadline_exceeded', 'result_observation_timeout')) OR
        (status = 'cancelled' AND reason_code = 'cancelled_by_request')
    ),
    ADD CONSTRAINT chk_device_commands_reason_evidence CHECK (
        reason_code IS NULL OR
        (reason_code IN (
            'provider_not_configured', 'provider_action_unsupported', 'provider_mapping_unknown',
            'provider_session_unavailable', 'provider_request_invalid', 'device_not_online',
            'provider_transport_error', 'dispatch_deadline_exceeded', 'cancelled_by_request'
        ) AND confirmation_level = 'none' AND evidence_status = 'none') OR
        (reason_code = 'provider_rejected' AND confirmation_level = 'transport_sent') OR
        (reason_code = 'device_reported_failure' AND confirmation_level = 'device_final' AND evidence_status = 'verified') OR
        (reason_code = 'result_observation_timeout' AND confirmation_level IN ('transport_sent', 'provider_accepted', 'device_acked'))
    ),
    ADD CONSTRAINT chk_device_commands_status_timing CHECK (
        (status = 'queued' AND sent_at IS NULL AND result_deadline_at IS NULL) OR
        (status IN ('sent', 'acked', 'success') AND sent_at IS NOT NULL AND result_deadline_at IS NOT NULL) OR
        (status = 'failed' AND (
            (reason_code IN (
                'provider_not_configured', 'provider_action_unsupported', 'provider_mapping_unknown',
                'provider_session_unavailable', 'provider_request_invalid', 'device_not_online'
            ) AND sent_at IS NULL AND result_deadline_at IS NULL) OR
            (reason_code IN ('provider_transport_error', 'provider_rejected', 'device_reported_failure')
                AND sent_at IS NOT NULL AND result_deadline_at IS NOT NULL)
        )) OR
        (status = 'timeout' AND (
            (reason_code = 'dispatch_deadline_exceeded' AND sent_at IS NULL AND result_deadline_at IS NULL) OR
            (reason_code = 'result_observation_timeout' AND sent_at IS NOT NULL AND result_deadline_at IS NOT NULL)
        )) OR
        (status = 'cancelled' AND sent_at IS NULL AND result_deadline_at IS NULL)
    ),
    ADD CONSTRAINT uq_device_commands_attempt_binding UNIQUE (id, provider_code, adapter),
    ADD CONSTRAINT fk_device_commands_provider_binding
        FOREIGN KEY (device_id, provider_code, provider_profile, provider_device_id, adapter)
        REFERENCES devices(id, provider_code, provider_profile, provider_device_id, adapter);

ALTER TABLE device_command_attempts
    DROP CONSTRAINT chk_command_attempts_provider,
    DROP CONSTRAINT chk_command_attempts_request_key,
    DROP CONSTRAINT chk_command_attempts_reason,
    ADD CONSTRAINT chk_command_attempts_provider CHECK (
        (provider_code = 'wwtiot' AND adapter = 'wwtiot_cloud_api') OR
        (provider_code = 'omni' AND adapter = 'omni_direct_tcp') OR
        (provider_code = 'simulator' AND adapter = 'simulator')
    ),
    ADD CONSTRAINT chk_command_attempts_request_key CHECK (
        (provider_code = 'wwtiot' AND provider_request_key ~ '^[1-9][0-9]{0,8}$') OR
        (provider_code IN ('omni', 'simulator') AND char_length(provider_request_key) BETWEEN 1 AND 128)
    ),
    ADD CONSTRAINT chk_command_attempts_reason CHECK (
        (outcome IS NULL AND reason_code IS NULL) OR
        (outcome = 'provider_accepted' AND reason_code IS NULL) OR
        (outcome = 'not_dispatched' AND reason_code IN ('cancelled_by_request', 'dispatch_deadline_exceeded', 'device_not_online')) OR
        (outcome = 'invalid_request' AND reason_code IN (
            'provider_not_configured', 'provider_action_unsupported', 'provider_mapping_unknown',
            'provider_session_unavailable', 'provider_request_invalid'
        )) OR
        (outcome = 'transport_error_before_send' AND reason_code = 'provider_transport_error') OR
        (outcome = 'provider_rejected' AND reason_code = 'provider_rejected') OR
        (outcome = 'indeterminate' AND reason_code IN ('provider_delivery_unknown', 'provider_response_invalid'))
    ),
    ADD CONSTRAINT fk_command_attempts_provider_binding
        FOREIGN KEY (command_id, provider_code, adapter)
        REFERENCES device_commands(id, provider_code, adapter);

ALTER TABLE device_raw_messages
    ADD COLUMN provider_profile TEXT,
    ADD COLUMN evidence_status TEXT;

UPDATE device_raw_messages
SET provider_profile = CASE provider_code
        WHEN 'wwtiot' THEN 'wwtiot-cloud-api-v2'
        WHEN 'simulator' THEN 'simulator-v1'
    END,
    evidence_status = CASE provider_code
        WHEN 'simulator' THEN 'verified'
        ELSE 'unverified'
    END;

ALTER TABLE device_raw_messages
    ALTER COLUMN provider_profile SET NOT NULL,
    ALTER COLUMN evidence_status SET NOT NULL,
    DROP CONSTRAINT chk_raw_messages_provider,
    ADD CONSTRAINT chk_raw_messages_evidence CHECK (evidence_status IN ('verified', 'unverified')),
    ADD CONSTRAINT chk_raw_messages_provider CHECK (
        (provider_code = 'wwtiot' AND provider_profile = 'wwtiot-cloud-api-v2'
            AND access_type = 'cloud_api' AND transport_protocol = 'http' AND adapter = 'wwtiot_cloud_api'
            AND evidence_status IN ('verified', 'unverified')) OR
        (provider_code = 'omni' AND provider_profile IN ('omni-bike-tcp-v2.0.7', 'omni-iot-tcp-v1.3.5', 'unresolved')
            AND access_type = 'direct_device' AND transport_protocol = 'tcp' AND adapter = 'omni_direct_tcp'
            AND evidence_status = 'unverified') OR
        (provider_code = 'simulator' AND provider_profile = 'simulator-v1'
            AND access_type = 'simulator' AND transport_protocol = 'internal' AND adapter = 'simulator'
            AND evidence_status = 'verified')
    ),
    ADD CONSTRAINT fk_raw_messages_provider_binding
        FOREIGN KEY (device_id, provider_code, provider_profile, provider_device_id, adapter)
        REFERENCES devices(id, provider_code, provider_profile, provider_device_id, adapter);

ALTER TABLE audit_logs
    DROP CONSTRAINT chk_audit_logs_action,
    ADD CONSTRAINT chk_audit_logs_action CHECK (action IN (
        'auth.login', 'auth.refresh', 'auth.logout',
        'project.created', 'project.updated', 'project.api_key_rotated', 'project.webhook_secret_rotated',
        'project.webhook_secret_decryption_failed',
        'device.created', 'device.updated', 'device.lifecycle_changed',
        'command.created', 'command.cancelled', 'command.dispatch_recorded',
        'webhook.delivery_replayed', 'simulator.updated',
        'provider.callback_rejected', 'provider.message_received', 'provider.message_rejected'
    ));
