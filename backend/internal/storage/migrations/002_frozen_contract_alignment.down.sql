DO $$
BEGIN
    IF EXISTS (SELECT 1 FROM device_commands)
        OR EXISTS (SELECT 1 FROM device_command_attempts)
        OR EXISTS (SELECT 1 FROM device_states)
        OR EXISTS (SELECT 1 FROM device_raw_messages)
        OR EXISTS (SELECT 1 FROM device_events)
        OR EXISTS (SELECT 1 FROM webhook_deliveries)
        OR EXISTS (SELECT 1 FROM webhook_delivery_attempts)
        OR EXISTS (SELECT 1 FROM audit_logs)
        OR EXISTS (SELECT 1 FROM auth_login_failure_events)
        OR EXISTS (SELECT 1 FROM project_webhook_secrets)
        OR EXISTS (SELECT 1 FROM projects WHERE webhook_config_version <> 0 OR current_webhook_secret_version IS NOT NULL)
        OR EXISTS (SELECT 1 FROM users WHERE session_generation <> 0)
        OR EXISTS (SELECT 1 FROM simulator_config WHERE outcome <> 'provider_accepted' OR delay_ms <> 0 OR version <> 1) THEN
        RAISE EXCEPTION '002 down cannot discard frozen lifecycle data';
    END IF;
END
$$;

DROP TABLE simulator_config;
DROP TABLE auth_login_failure_events;

ALTER TABLE audit_logs
    DROP CONSTRAINT chk_audit_logs_metadata,
    DROP CONSTRAINT chk_audit_logs_action,
    DROP CONSTRAINT chk_audit_logs_result,
    DROP CONSTRAINT chk_audit_logs_actor_type,
    DROP COLUMN result,
    DROP COLUMN actor_id;
ALTER TABLE audit_logs RENAME COLUMN occurred_at TO created_at;
ALTER TABLE audit_logs
    ADD COLUMN user_id UUID REFERENCES users(id),
    ADD COLUMN user_agent TEXT;

DROP INDEX uq_webhook_delivery_attempts_one_incomplete;
DROP TABLE webhook_delivery_attempts;
DROP INDEX uq_webhook_deliveries_initial_event;
DROP INDEX idx_webhook_deliveries_lease;
ALTER TABLE webhook_deliveries
    DROP CONSTRAINT fk_webhook_deliveries_replay_project,
    DROP CONSTRAINT fk_webhook_deliveries_event_project,
    DROP CONSTRAINT uq_webhook_deliveries_id_project,
    DROP CONSTRAINT chk_webhook_deliveries_replay,
    DROP CONSTRAINT chk_webhook_deliveries_terminal,
    DROP CONSTRAINT chk_webhook_deliveries_state,
    DROP CONSTRAINT chk_webhook_deliveries_lease,
    DROP CONSTRAINT chk_webhook_deliveries_attempt_count,
    DROP CONSTRAINT chk_webhook_deliveries_status,
    DROP CONSTRAINT chk_webhook_deliveries_config_version,
    DROP CONSTRAINT chk_webhook_deliveries_raw_body,
    DROP CONSTRAINT fk_webhook_deliveries_secret_version,
    DROP COLUMN replay_of_delivery_id,
    DROP COLUMN lease_expires_at,
    DROP COLUMN lease_owner,
    DROP COLUMN lease_token,
    DROP COLUMN last_error_detail,
    DROP COLUMN last_error_code,
    DROP COLUMN raw_body,
    DROP COLUMN webhook_secret_version,
    DROP COLUMN webhook_config_version,
    ADD COLUMN signature TEXT,
    ADD COLUMN payload JSONB NOT NULL DEFAULT '{}'::jsonb,
    ADD COLUMN last_error TEXT,
    ADD CONSTRAINT webhook_deliveries_event_id_fkey FOREIGN KEY (event_id) REFERENCES device_events(id);

DROP INDEX uq_device_events_project_deduplication;
ALTER TABLE device_events
    DROP CONSTRAINT uq_device_events_id_project,
    DROP CONSTRAINT fk_device_events_raw_message_device,
    DROP CONSTRAINT fk_device_events_command_project_device,
    DROP CONSTRAINT fk_device_events_device_project,
    DROP CONSTRAINT chk_device_events_associations,
    DROP CONSTRAINT chk_device_events_deduplication_key,
    DROP CONSTRAINT chk_device_events_payload_contract,
    DROP CONSTRAINT chk_device_events_payload,
    DROP CONSTRAINT chk_device_events_source,
    DROP CONSTRAINT chk_device_events_type,
    DROP CONSTRAINT chk_device_events_schema_version,
    DROP COLUMN deduplication_key,
    DROP COLUMN schema_version,
    ADD CONSTRAINT device_events_raw_message_id_fkey FOREIGN KEY (raw_message_id) REFERENCES device_raw_messages(id);

DROP INDEX uq_raw_messages_provider_deduplication;
ALTER TABLE device_raw_messages
    DROP CONSTRAINT chk_raw_messages_provider,
    DROP CONSTRAINT chk_raw_messages_deduplication_key,
    DROP COLUMN deduplication_key;
UPDATE device_raw_messages
SET provider_code = 'simulator', access_type = 'mock_gateway', transport_protocol = 'simulator', adapter = 'mock_gateway'
WHERE provider_code = 'simulator';
ALTER TABLE device_raw_messages
    ADD CONSTRAINT device_raw_messages_access_type_check CHECK (access_type IN ('mock_gateway', 'cloud_api')),
    ADD CONSTRAINT device_raw_messages_transport_protocol_check CHECK (transport_protocol IN ('simulator', 'http')),
    ADD CONSTRAINT device_raw_messages_adapter_check CHECK (adapter IN ('mock_gateway', 'wwtiot_cloud_api')),
    ADD CONSTRAINT device_raw_messages_check CHECK (
        (access_type = 'mock_gateway' AND transport_protocol = 'simulator' AND adapter = 'mock_gateway') OR
        (access_type = 'cloud_api' AND transport_protocol = 'http' AND adapter = 'wwtiot_cloud_api')
    );

DROP INDEX idx_command_attempts_lease;
DROP INDEX uq_command_attempts_one_incomplete;
DROP INDEX uq_command_attempts_provider_request_key;
ALTER TABLE device_command_attempts
    DROP CONSTRAINT chk_command_attempts_outcome_evidence,
    DROP CONSTRAINT chk_command_attempts_confirmation_evidence,
    DROP CONSTRAINT chk_command_attempts_lease,
    DROP CONSTRAINT chk_command_attempts_dispatching_at,
    DROP CONSTRAINT chk_command_attempts_completion,
    DROP CONSTRAINT chk_command_attempts_pending_evidence,
    DROP CONSTRAINT chk_command_attempts_timestamps,
    DROP CONSTRAINT chk_command_attempts_evidence,
    DROP CONSTRAINT chk_command_attempts_confirmation,
    DROP CONSTRAINT chk_command_attempts_outcome,
    DROP CONSTRAINT chk_command_attempts_request_key,
    DROP CONSTRAINT chk_command_attempts_provider,
    DROP CONSTRAINT chk_command_attempts_number,
    DROP CONSTRAINT chk_command_attempts_phase,
    DROP COLUMN dispatching_at,
    DROP COLUMN error_code,
    DROP COLUMN lease_expires_at,
    DROP COLUMN lease_owner,
    DROP COLUMN lease_token,
    DROP COLUMN evidence_status,
    DROP COLUMN confirmation_level,
    DROP COLUMN outcome,
    DROP COLUMN provider_request_key,
    DROP COLUMN provider_code,
    ALTER COLUMN phase SET DEFAULT 'created';
ALTER TABLE device_command_attempts RENAME COLUMN completed_at TO finished_at;
ALTER TABLE device_command_attempts RENAME COLUMN claimed_at TO started_at;
ALTER TABLE device_command_attempts RENAME COLUMN error_detail TO error_message;
ALTER TABLE device_command_attempts RENAME COLUMN response_summary TO response_body;
ALTER TABLE device_command_attempts RENAME COLUMN request_summary TO request_body;
ALTER TABLE device_command_attempts RENAME COLUMN phase TO status;
ALTER TABLE device_command_attempts
    ADD CONSTRAINT device_command_attempts_adapter_check CHECK (adapter IN ('mock_gateway', 'wwtiot_cloud_api')),
    ADD CONSTRAINT device_command_attempts_status_check CHECK (status IN ('created', 'sent', 'acked', 'success', 'failed', 'timeout'));

DROP INDEX idx_device_commands_result_due;
DROP INDEX idx_device_commands_dispatch_due;
ALTER TABLE device_commands
    DROP CONSTRAINT chk_device_commands_deadlines,
    DROP CONSTRAINT chk_device_commands_status_timing,
    DROP CONSTRAINT chk_device_commands_finished,
    DROP CONSTRAINT chk_device_commands_reason_evidence,
    DROP CONSTRAINT chk_device_commands_reason,
    DROP CONSTRAINT chk_device_commands_acked_evidence,
    DROP CONSTRAINT chk_device_commands_status_confirmation,
    DROP CONSTRAINT chk_device_commands_success_evidence,
    DROP CONSTRAINT chk_device_commands_confirmation_evidence,
    DROP CONSTRAINT chk_device_commands_high_confirmation_evidence,
    DROP CONSTRAINT chk_device_commands_evidence,
    DROP CONSTRAINT chk_device_commands_confirmation,
    DROP CONSTRAINT chk_device_commands_revision,
    DROP CONSTRAINT chk_device_commands_request_hash,
    DROP CONSTRAINT chk_device_commands_idempotency_key,
    DROP CONSTRAINT chk_device_commands_delivery_policy,
    DROP CONSTRAINT chk_device_commands_status,
    DROP CONSTRAINT chk_device_commands_payload,
    DROP CONSTRAINT chk_device_commands_action,
    DROP CONSTRAINT uq_device_commands_project_idempotency,
    DROP CONSTRAINT uq_device_commands_id_project_device,
    DROP CONSTRAINT fk_device_commands_type_profile,
    DROP CONSTRAINT fk_device_commands_device_project,
    DROP COLUMN result_deadline_at,
    DROP COLUMN dispatch_deadline_at,
    DROP COLUMN queued_at,
    DROP COLUMN evidence_status,
    DROP COLUMN confirmation_level,
    DROP COLUMN reason_detail,
    DROP COLUMN reason_code,
    DROP COLUMN device_type_revision,
    DROP COLUMN device_type_id,
    ADD COLUMN reason TEXT,
    ADD COLUMN expires_at TIMESTAMPTZ,
    ALTER COLUMN request_hash TYPE TEXT USING encode(request_hash, 'hex'),
    ALTER COLUMN request_hash DROP NOT NULL,
    ALTER COLUMN idempotency_key DROP NOT NULL,
    ALTER COLUMN delivery_policy DROP DEFAULT,
    ALTER COLUMN status SET DEFAULT 'created',
    ADD CONSTRAINT device_commands_device_id_fkey FOREIGN KEY (device_id) REFERENCES devices(id),
    ADD CONSTRAINT device_commands_project_id_idempotency_key_key UNIQUE (project_id, idempotency_key);
CREATE INDEX idx_device_commands_expires_at ON device_commands(expires_at) WHERE expires_at IS NOT NULL;

ALTER TABLE device_states
    DROP CONSTRAINT fk_device_states_raw_message_device,
    DROP CONSTRAINT chk_device_states_evidence,
    DROP COLUMN evidence_status,
    ALTER COLUMN raw_message_id DROP NOT NULL,
    ALTER COLUMN reported_at SET NOT NULL,
    ADD CONSTRAINT fk_device_states_raw_message FOREIGN KEY (raw_message_id) REFERENCES device_raw_messages(id);

ALTER TABLE device_raw_messages DROP CONSTRAINT uq_raw_messages_id_device;

DROP INDEX uq_devices_active_provider_identity;
ALTER TABLE devices
    DROP CONSTRAINT chk_devices_provider_binding,
    DROP CONSTRAINT chk_devices_adapter,
    DROP CONSTRAINT chk_devices_transport_protocol,
    DROP CONSTRAINT chk_devices_access_type,
    DROP CONSTRAINT chk_devices_provider_code,
    DROP CONSTRAINT chk_devices_name,
    DROP CONSTRAINT uq_devices_id_project_type,
    DROP CONSTRAINT uq_devices_id_project,
    ADD COLUMN metadata JSONB NOT NULL DEFAULT '{}'::jsonb;
UPDATE devices
SET access_type = 'mock_gateway', transport_protocol = 'simulator', adapter = 'mock_gateway'
WHERE provider_code = 'simulator';
ALTER TABLE devices
    ADD CONSTRAINT devices_access_type_check CHECK (access_type IN ('mock_gateway', 'cloud_api')),
    ADD CONSTRAINT devices_transport_protocol_check CHECK (transport_protocol IN ('simulator', 'http')),
    ADD CONSTRAINT devices_adapter_check CHECK (adapter IN ('mock_gateway', 'wwtiot_cloud_api')),
    ADD CONSTRAINT devices_check CHECK (
        (access_type = 'mock_gateway' AND transport_protocol = 'simulator' AND adapter = 'mock_gateway') OR
        (access_type = 'cloud_api' AND transport_protocol = 'http' AND adapter = 'wwtiot_cloud_api')
    );
CREATE UNIQUE INDEX idx_devices_project_provider_identity_active
    ON devices(project_id, lower(provider_code), provider_device_id)
    WHERE lifecycle_status <> 'deleted';

DROP TRIGGER trg_device_type_profiles_immutable ON device_type_profiles;
DROP FUNCTION reject_device_type_profile_mutation();
ALTER TABLE device_types DROP CONSTRAINT fk_device_types_current_profile;
DROP TABLE device_type_profiles;
ALTER TABLE device_types
    DROP CONSTRAINT chk_device_types_release_profile,
    DROP COLUMN current_revision,
    ADD COLUMN capabilities JSONB NOT NULL DEFAULT '[]'::jsonb,
    ADD COLUMN default_command_policy JSONB NOT NULL DEFAULT '{}'::jsonb;
UPDATE device_types dt
SET code = legacy.original_code
FROM migration_002_device_type_legacy legacy
WHERE dt.id = legacy.device_type_id;
DELETE FROM device_types dt
WHERE dt.code = 'smart-lock'
  AND NOT EXISTS (SELECT 1 FROM migration_002_device_type_legacy legacy WHERE legacy.device_type_id = dt.id)
  AND NOT EXISTS (SELECT 1 FROM devices WHERE device_type_id = dt.id);
UPDATE device_types dt
SET code = 'smart_lock'
WHERE dt.code = 'smart-lock'
  AND NOT EXISTS (SELECT 1 FROM migration_002_device_type_legacy legacy WHERE legacy.device_type_id = dt.id);
DROP TABLE migration_002_device_type_legacy;

ALTER TABLE projects DROP CONSTRAINT fk_projects_current_webhook_secret;
DROP TABLE project_webhook_secrets;
ALTER TABLE projects
    DROP CONSTRAINT chk_projects_webhook_configuration,
    DROP CONSTRAINT chk_projects_webhook_secret_version,
    DROP CONSTRAINT chk_projects_webhook_config_version,
    DROP CONSTRAINT chk_projects_api_key_digest,
    DROP CONSTRAINT chk_projects_name,
    DROP COLUMN current_webhook_secret_version,
    DROP COLUMN webhook_config_version,
    ADD COLUMN webhook_secret TEXT,
    ALTER COLUMN api_key_hash TYPE TEXT USING encode(api_key_hash, 'hex');

ALTER TABLE users
    DROP CONSTRAINT chk_users_session_generation,
    DROP COLUMN session_generation;
