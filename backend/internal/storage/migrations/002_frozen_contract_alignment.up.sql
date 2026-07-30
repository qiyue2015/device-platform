-- Freeze revision 2026-07-31.1 cannot safely reinterpret legacy command,
-- event, or secret evidence. Refuse to guess when such rows exist.
DO $$
BEGIN
    IF EXISTS (SELECT 1 FROM projects WHERE webhook_secret IS NOT NULL OR webhook_url IS NOT NULL) THEN
        RAISE EXCEPTION '002 requires explicit encryption of existing webhook configuration';
    END IF;
    IF EXISTS (SELECT 1 FROM projects WHERE api_key_hash !~ '^[0-9A-Fa-f]{64}$') THEN
        RAISE EXCEPTION '002 requires projects.api_key_hash to contain a SHA-256 hex digest';
    END IF;
    IF EXISTS (SELECT 1 FROM devices WHERE metadata <> '{}'::jsonb) THEN
        RAISE EXCEPTION '002 cannot discard non-empty legacy device metadata';
    END IF;
    IF EXISTS (SELECT 1 FROM device_types WHERE code NOT IN ('smart_lock', 'smart-lock')) THEN
        RAISE EXCEPTION '002 cannot infer capability profiles for legacy device types';
    END IF;
    IF (SELECT count(*) FROM device_types) > 1 THEN
        RAISE EXCEPTION '002 cannot merge multiple legacy smart-lock Device Types';
    END IF;
    IF EXISTS (
        SELECT 1 FROM device_types
        WHERE capabilities <> '[]'::jsonb OR default_command_policy <> '{}'::jsonb OR name <> 'Smart Lock'
    ) THEN
        RAISE EXCEPTION '002 cannot discard or reinterpret legacy Device Type configuration';
    END IF;
    IF EXISTS (
        SELECT 1 FROM devices
        WHERE NOT (
            (provider_code = 'wwtiot' AND access_type = 'cloud_api' AND transport_protocol = 'http' AND adapter = 'wwtiot_cloud_api') OR
            (access_type = 'mock_gateway' AND transport_protocol = 'simulator' AND adapter = 'mock_gateway')
        )
    ) THEN
        RAISE EXCEPTION '002 cannot infer a canonical Provider tuple from legacy Device rows';
    END IF;
    IF EXISTS (
        SELECT 1 FROM devices
        WHERE (access_type = 'mock_gateway' OR adapter = 'mock_gateway')
          AND (provider_code <> 'simulator' OR provider_device_id <> id::text)
    ) THEN
        RAISE EXCEPTION '002 cannot reversibly normalize a legacy simulator Provider identity';
    END IF;
    IF EXISTS (
        SELECT 1 FROM devices
        WHERE lifecycle_status <> 'deleted' AND provider_code = 'wwtiot'
        GROUP BY lower(provider_code), provider_device_id HAVING count(*) > 1
    ) THEN
        RAISE EXCEPTION '002 requires globally unique active Provider identities';
    END IF;
    IF EXISTS (SELECT 1 FROM device_commands)
        OR EXISTS (SELECT 1 FROM device_command_attempts)
        OR EXISTS (SELECT 1 FROM device_states)
        OR EXISTS (SELECT 1 FROM device_raw_messages)
        OR EXISTS (SELECT 1 FROM device_events)
        OR EXISTS (SELECT 1 FROM webhook_deliveries)
        OR EXISTS (SELECT 1 FROM audit_logs) THEN
        RAISE EXCEPTION '002 cannot infer frozen lifecycle evidence from legacy runtime rows';
    END IF;
END
$$;

ALTER TABLE users
    ADD COLUMN session_generation BIGINT NOT NULL DEFAULT 0,
    ADD CONSTRAINT chk_users_session_generation CHECK (session_generation >= 0);

ALTER TABLE projects
    ALTER COLUMN api_key_hash TYPE BYTEA USING decode(api_key_hash, 'hex'),
    DROP COLUMN webhook_secret,
    ADD COLUMN webhook_config_version BIGINT NOT NULL DEFAULT 0,
    ADD COLUMN current_webhook_secret_version INTEGER,
    ADD CONSTRAINT chk_projects_name CHECK (char_length(btrim(name)) BETWEEN 1 AND 120),
    ADD CONSTRAINT chk_projects_api_key_digest CHECK (octet_length(api_key_hash) = 32),
    ADD CONSTRAINT chk_projects_webhook_config_version CHECK (webhook_config_version >= 0),
    ADD CONSTRAINT chk_projects_webhook_secret_version CHECK (current_webhook_secret_version IS NULL OR current_webhook_secret_version > 0),
    ADD CONSTRAINT chk_projects_webhook_configuration CHECK (
        webhook_url IS NULL OR (webhook_config_version > 0 AND current_webhook_secret_version IS NOT NULL)
    );

CREATE TABLE project_webhook_secrets (
    project_id UUID NOT NULL REFERENCES projects(id),
    version INTEGER NOT NULL,
    ciphertext BYTEA NOT NULL,
    nonce BYTEA NOT NULL,
    encryption_key_version INTEGER NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    retired_at TIMESTAMPTZ,
    PRIMARY KEY (project_id, version),
    CHECK (version > 0),
    CHECK (octet_length(ciphertext) > 16),
    CHECK (octet_length(nonce) = 12),
    CHECK (encryption_key_version > 0),
    CHECK (retired_at IS NULL OR retired_at >= created_at)
);

ALTER TABLE projects
    ADD CONSTRAINT fk_projects_current_webhook_secret
    FOREIGN KEY (id, current_webhook_secret_version)
    REFERENCES project_webhook_secrets(project_id, version);

CREATE TABLE migration_002_device_type_legacy (
    device_type_id UUID PRIMARY KEY,
    original_code TEXT NOT NULL CHECK (original_code IN ('smart_lock', 'smart-lock'))
);

INSERT INTO migration_002_device_type_legacy (device_type_id, original_code)
SELECT id, code FROM device_types;

ALTER TABLE device_types
    ADD COLUMN current_revision INTEGER NOT NULL DEFAULT 1;

UPDATE device_types SET code = 'smart-lock', name = 'Smart Lock', current_revision = 1;

INSERT INTO device_types (id, code, name, current_revision)
VALUES (
    '00000000-0000-0000-0000-000000000001',
    'smart-lock',
    'Smart Lock',
    1
)
ON CONFLICT (code) DO NOTHING;

ALTER TABLE device_types
    DROP COLUMN capabilities,
    DROP COLUMN default_command_policy,
    ALTER COLUMN current_revision DROP DEFAULT,
    ADD CONSTRAINT chk_device_types_release_profile CHECK (code = 'smart-lock' AND current_revision = 1);

CREATE TABLE device_type_profiles (
    device_type_id UUID NOT NULL REFERENCES device_types(id),
    revision INTEGER NOT NULL,
    profile JSONB NOT NULL,
    profile_hash BYTEA NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (device_type_id, revision),
    CHECK (revision > 0),
    CHECK (jsonb_typeof(profile) = 'object'),
    CHECK (octet_length(profile_hash) = 32)
);

INSERT INTO device_type_profiles (device_type_id, revision, profile, profile_hash)
SELECT id, 1, '{
    "actions": [
        {"identifier":"unlock","risk_level":"high","payload_schema":{"type":"object","maxProperties":0,"additionalProperties":false},"delivery_policy":"dispatch_once","dispatch_deadline_ms":30000,"provider_request_timeout_ms":10000,"result_observation_timeout_ms":60000,"retry_allowed":false,"delivery_policy_override_allowed":false},
        {"identifier":"lock","risk_level":"high","payload_schema":{"type":"object","maxProperties":0,"additionalProperties":false},"delivery_policy":"dispatch_once","dispatch_deadline_ms":30000,"provider_request_timeout_ms":10000,"result_observation_timeout_ms":60000,"retry_allowed":false,"delivery_policy_override_allowed":false},
        {"identifier":"query_status","risk_level":"low","payload_schema":{"type":"object","maxProperties":0,"additionalProperties":false},"delivery_policy":"dispatch_once","dispatch_deadline_ms":30000,"provider_request_timeout_ms":10000,"result_observation_timeout_ms":60000,"retry_allowed":false,"delivery_policy_override_allowed":false}
    ]
}'::jsonb, decode('81f6d5efb5f627a56fc19a2e2fb7fadcccc9b6a6b53fa411d7265a15eda5b596', 'hex')
FROM device_types WHERE code = 'smart-lock';

ALTER TABLE device_types
    ADD CONSTRAINT fk_device_types_current_profile
    FOREIGN KEY (id, current_revision) REFERENCES device_type_profiles(device_type_id, revision);

CREATE FUNCTION reject_device_type_profile_mutation() RETURNS trigger
LANGUAGE plpgsql AS $$
BEGIN
    RAISE EXCEPTION 'device type profile revisions are immutable';
END
$$;

CREATE TRIGGER trg_device_type_profiles_immutable
BEFORE UPDATE OR DELETE ON device_type_profiles
FOR EACH ROW EXECUTE FUNCTION reject_device_type_profile_mutation();

ALTER TABLE devices
    DROP CONSTRAINT devices_access_type_check,
    DROP CONSTRAINT devices_transport_protocol_check,
    DROP CONSTRAINT devices_adapter_check,
    DROP CONSTRAINT devices_check;

UPDATE devices
SET provider_code = 'simulator',
    provider_device_id = id::text,
    access_type = 'simulator',
    transport_protocol = 'internal',
    adapter = 'simulator'
WHERE access_type = 'mock_gateway' OR adapter = 'mock_gateway';

ALTER TABLE devices
    DROP COLUMN metadata,
    ADD CONSTRAINT uq_devices_id_project UNIQUE (id, project_id),
    ADD CONSTRAINT uq_devices_id_project_type UNIQUE (id, project_id, device_type_id),
    ADD CONSTRAINT chk_devices_name CHECK (char_length(btrim(name)) BETWEEN 1 AND 120),
    ADD CONSTRAINT chk_devices_provider_code CHECK (provider_code IN ('wwtiot', 'simulator')),
    ADD CONSTRAINT chk_devices_access_type CHECK (access_type IN ('cloud_api', 'simulator')),
    ADD CONSTRAINT chk_devices_transport_protocol CHECK (transport_protocol IN ('http', 'internal')),
    ADD CONSTRAINT chk_devices_adapter CHECK (adapter IN ('wwtiot_cloud_api', 'simulator')),
    ADD CONSTRAINT chk_devices_provider_binding CHECK (
        (provider_code = 'wwtiot' AND access_type = 'cloud_api' AND transport_protocol = 'http' AND adapter = 'wwtiot_cloud_api'
            AND provider_device_id ~ '^[A-Za-z0-9._:-]{1,128}$')
        OR
        (provider_code = 'simulator' AND access_type = 'simulator' AND transport_protocol = 'internal' AND adapter = 'simulator'
            AND provider_device_id = id::text)
    );

DROP INDEX idx_devices_project_provider_identity_active;
CREATE UNIQUE INDEX uq_devices_active_provider_identity
    ON devices(lower(provider_code), provider_device_id)
    WHERE lifecycle_status <> 'deleted';

ALTER TABLE device_states
    DROP CONSTRAINT fk_device_states_raw_message,
    ALTER COLUMN reported_at DROP NOT NULL,
    ALTER COLUMN raw_message_id SET NOT NULL,
    ADD COLUMN evidence_status TEXT NOT NULL DEFAULT 'verified',
    ADD CONSTRAINT chk_device_states_evidence CHECK (evidence_status = 'verified');

ALTER TABLE device_commands
    DROP CONSTRAINT device_commands_project_id_idempotency_key_key,
    DROP CONSTRAINT device_commands_device_id_fkey,
    ALTER COLUMN status SET DEFAULT 'queued',
    ALTER COLUMN delivery_policy SET DEFAULT 'dispatch_once',
    ALTER COLUMN idempotency_key SET NOT NULL,
    ALTER COLUMN request_hash TYPE BYTEA USING decode(request_hash, 'hex'),
    ALTER COLUMN request_hash SET NOT NULL,
    DROP COLUMN reason,
    DROP COLUMN expires_at,
    ADD COLUMN device_type_id UUID NOT NULL,
    ADD COLUMN device_type_revision INTEGER NOT NULL DEFAULT 1,
    ADD COLUMN reason_code TEXT,
    ADD COLUMN reason_detail TEXT,
    ADD COLUMN confirmation_level TEXT NOT NULL DEFAULT 'none',
    ADD COLUMN evidence_status TEXT NOT NULL DEFAULT 'none',
    ADD COLUMN queued_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    ADD COLUMN dispatch_deadline_at TIMESTAMPTZ NOT NULL DEFAULT (now() + interval '30 seconds'),
    ADD COLUMN result_deadline_at TIMESTAMPTZ,
    ADD CONSTRAINT fk_device_commands_device_project
        FOREIGN KEY (device_id, project_id, device_type_id) REFERENCES devices(id, project_id, device_type_id),
    ADD CONSTRAINT fk_device_commands_type_profile
        FOREIGN KEY (device_type_id, device_type_revision) REFERENCES device_type_profiles(device_type_id, revision),
    ADD CONSTRAINT uq_device_commands_id_project_device UNIQUE (id, project_id, device_id),
    ADD CONSTRAINT uq_device_commands_project_idempotency UNIQUE (project_id, idempotency_key),
    ADD CONSTRAINT chk_device_commands_action CHECK (char_length(btrim(command_type)) BETWEEN 1 AND 128),
    ADD CONSTRAINT chk_device_commands_payload CHECK (jsonb_typeof(payload) = 'object'),
    ADD CONSTRAINT chk_device_commands_status CHECK (status IN ('queued', 'sent', 'acked', 'success', 'failed', 'timeout', 'cancelled', 'unknown')),
    ADD CONSTRAINT chk_device_commands_delivery_policy CHECK (delivery_policy = 'dispatch_once'),
    ADD CONSTRAINT chk_device_commands_idempotency_key CHECK (idempotency_key = btrim(idempotency_key) AND char_length(idempotency_key) BETWEEN 1 AND 128),
    ADD CONSTRAINT chk_device_commands_request_hash CHECK (octet_length(request_hash) = 32),
    ADD CONSTRAINT chk_device_commands_revision CHECK (device_type_revision > 0),
    ADD CONSTRAINT chk_device_commands_confirmation CHECK (confirmation_level IN ('none', 'transport_sent', 'provider_accepted', 'device_acked', 'device_final')),
    ADD CONSTRAINT chk_device_commands_evidence CHECK (evidence_status IN ('none', 'verified', 'unverified')),
    ADD CONSTRAINT chk_device_commands_confirmation_evidence CHECK (
        (confirmation_level = 'none' AND evidence_status = 'none') OR
        (confirmation_level <> 'none' AND evidence_status <> 'none')
    ),
    ADD CONSTRAINT chk_device_commands_high_confirmation_evidence CHECK (
        confirmation_level NOT IN ('device_acked', 'device_final') OR evidence_status = 'verified'
    ),
    ADD CONSTRAINT chk_device_commands_success_evidence CHECK (
        status <> 'success' OR (confirmation_level = 'device_final' AND evidence_status = 'verified')
    ),
    ADD CONSTRAINT chk_device_commands_acked_evidence CHECK (
        status <> 'acked' OR (confirmation_level IN ('device_acked', 'device_final') AND evidence_status = 'verified')
    ),
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
            (reason_code IN ('provider_not_configured', 'provider_transport_error') AND sent_at IS NULL AND result_deadline_at IS NULL) OR
            (reason_code IN ('provider_rejected', 'device_reported_failure') AND sent_at IS NOT NULL AND result_deadline_at IS NOT NULL)
        )) OR
        (status = 'unknown' AND sent_at IS NOT NULL AND result_deadline_at IS NOT NULL) OR
        (status = 'timeout' AND (
            (reason_code = 'dispatch_deadline_exceeded' AND sent_at IS NULL AND result_deadline_at IS NULL) OR
            (reason_code = 'result_observation_timeout' AND sent_at IS NOT NULL AND result_deadline_at IS NOT NULL)
        )) OR
        (status = 'cancelled' AND sent_at IS NULL AND result_deadline_at IS NULL)
    ),
    ADD CONSTRAINT chk_device_commands_deadlines CHECK (
        dispatch_deadline_at > queued_at AND
        (sent_at IS NULL OR sent_at >= queued_at) AND
        (result_deadline_at IS NULL OR (sent_at IS NOT NULL AND result_deadline_at > sent_at))
    );

DROP INDEX IF EXISTS idx_device_commands_expires_at;
CREATE INDEX idx_device_commands_dispatch_due
    ON device_commands(status, dispatch_deadline_at)
    WHERE status = 'queued';
CREATE INDEX idx_device_commands_result_due
    ON device_commands(status, result_deadline_at)
    WHERE status IN ('sent', 'acked');

ALTER TABLE device_command_attempts
    DROP CONSTRAINT device_command_attempts_adapter_check,
    DROP CONSTRAINT device_command_attempts_status_check;
ALTER TABLE device_command_attempts RENAME COLUMN status TO phase;
ALTER TABLE device_command_attempts RENAME COLUMN request_body TO request_summary;
ALTER TABLE device_command_attempts RENAME COLUMN response_body TO response_summary;
ALTER TABLE device_command_attempts RENAME COLUMN error_message TO error_detail;
ALTER TABLE device_command_attempts RENAME COLUMN started_at TO claimed_at;
ALTER TABLE device_command_attempts RENAME COLUMN finished_at TO completed_at;

ALTER TABLE device_command_attempts
    ALTER COLUMN phase SET DEFAULT 'claimed',
    ADD COLUMN provider_code TEXT NOT NULL,
    ADD COLUMN provider_request_key TEXT NOT NULL,
    ADD COLUMN outcome TEXT,
    ADD COLUMN confirmation_level TEXT NOT NULL DEFAULT 'none',
    ADD COLUMN evidence_status TEXT NOT NULL DEFAULT 'none',
    ADD COLUMN error_code TEXT,
    ADD COLUMN lease_token UUID NOT NULL,
    ADD COLUMN lease_owner TEXT NOT NULL,
    ADD COLUMN lease_expires_at TIMESTAMPTZ NOT NULL,
    ADD COLUMN dispatching_at TIMESTAMPTZ,
    ADD CONSTRAINT chk_command_attempts_phase CHECK (phase IN ('claimed', 'dispatching', 'completed')),
    ADD CONSTRAINT chk_command_attempts_number CHECK (attempt_no > 0),
    ADD CONSTRAINT chk_command_attempts_provider CHECK (
        (provider_code = 'wwtiot' AND adapter = 'wwtiot_cloud_api') OR
        (provider_code = 'simulator' AND adapter = 'simulator')
    ),
    ADD CONSTRAINT chk_command_attempts_request_key CHECK (
        (provider_code = 'wwtiot' AND provider_request_key ~ '^[1-9][0-9]{0,8}$') OR
        (provider_code = 'simulator' AND char_length(provider_request_key) BETWEEN 1 AND 128)
    ),
    ADD CONSTRAINT chk_command_attempts_outcome CHECK (outcome IS NULL OR outcome IN (
        'not_dispatched', 'invalid_request', 'provider_accepted', 'provider_rejected',
        'transport_error_before_send', 'transport_error_after_send', 'invalid_response',
        'device_acked', 'device_succeeded', 'device_failed'
    )),
    ADD CONSTRAINT chk_command_attempts_confirmation CHECK (confirmation_level IN ('none', 'transport_sent', 'provider_accepted', 'device_acked', 'device_final')),
    ADD CONSTRAINT chk_command_attempts_evidence CHECK (evidence_status IN ('none', 'verified', 'unverified')),
    ADD CONSTRAINT chk_command_attempts_completion CHECK (
        (phase = 'completed' AND outcome IS NOT NULL AND completed_at IS NOT NULL) OR
        (phase <> 'completed' AND outcome IS NULL AND completed_at IS NULL)
    ),
    ADD CONSTRAINT chk_command_attempts_pending_evidence CHECK (
        phase = 'completed' OR (confirmation_level = 'none' AND evidence_status = 'none')
    ),
    ADD CONSTRAINT chk_command_attempts_dispatching_at CHECK (
        (phase = 'claimed' AND dispatching_at IS NULL) OR
        (phase = 'dispatching' AND dispatching_at IS NOT NULL) OR
        (phase = 'completed' AND (
            (outcome IN ('not_dispatched', 'invalid_request') AND dispatching_at IS NULL) OR
            (outcome NOT IN ('not_dispatched', 'invalid_request') AND dispatching_at IS NOT NULL)
        ))
    ),
    ADD CONSTRAINT chk_command_attempts_confirmation_evidence CHECK (
        (confirmation_level = 'none' AND evidence_status = 'none') OR
        (confirmation_level <> 'none' AND evidence_status <> 'none')
    ),
    ADD CONSTRAINT chk_command_attempts_lease CHECK (
        char_length(btrim(lease_owner)) > 0 AND lease_expires_at > claimed_at
    ),
    ADD CONSTRAINT chk_command_attempts_timestamps CHECK (
        (dispatching_at IS NULL OR dispatching_at >= claimed_at) AND
        (completed_at IS NULL OR completed_at >= claimed_at) AND
        (completed_at IS NULL OR dispatching_at IS NULL OR completed_at >= dispatching_at)
    ),
    ADD CONSTRAINT chk_command_attempts_outcome_evidence CHECK (
        (outcome IS NULL) OR
        (outcome IN ('not_dispatched', 'invalid_request', 'transport_error_before_send') AND confirmation_level = 'none' AND evidence_status = 'none') OR
        (outcome IN ('transport_error_after_send', 'invalid_response', 'provider_rejected') AND confirmation_level = 'transport_sent') OR
        (outcome = 'provider_accepted' AND confirmation_level = 'provider_accepted') OR
        (outcome = 'device_acked' AND confirmation_level IN ('device_acked', 'device_final') AND evidence_status = 'verified') OR
        (outcome IN ('device_succeeded', 'device_failed') AND confirmation_level = 'device_final' AND evidence_status = 'verified')
    );

CREATE UNIQUE INDEX uq_command_attempts_provider_request_key
    ON device_command_attempts(provider_code, provider_request_key);
CREATE UNIQUE INDEX uq_command_attempts_one_incomplete
    ON device_command_attempts(command_id)
    WHERE phase <> 'completed';
CREATE INDEX idx_command_attempts_lease
    ON device_command_attempts(phase, lease_expires_at)
    WHERE phase <> 'completed';

UPDATE device_raw_messages
SET provider_code = 'simulator', access_type = 'simulator', transport_protocol = 'internal', adapter = 'simulator'
WHERE access_type = 'mock_gateway' OR adapter = 'mock_gateway';

ALTER TABLE device_raw_messages
    DROP CONSTRAINT device_raw_messages_access_type_check,
    DROP CONSTRAINT device_raw_messages_transport_protocol_check,
    DROP CONSTRAINT device_raw_messages_adapter_check,
    DROP CONSTRAINT device_raw_messages_check,
    ADD COLUMN deduplication_key TEXT NOT NULL,
    ADD CONSTRAINT uq_raw_messages_id_device UNIQUE (id, device_id),
    ADD CONSTRAINT chk_raw_messages_deduplication_key CHECK (
        deduplication_key = btrim(deduplication_key) AND char_length(deduplication_key) > 0
    ),
    ADD CONSTRAINT chk_raw_messages_provider CHECK (
        (provider_code = 'wwtiot' AND access_type = 'cloud_api' AND transport_protocol = 'http' AND adapter = 'wwtiot_cloud_api') OR
        (provider_code = 'simulator' AND access_type = 'simulator' AND transport_protocol = 'internal' AND adapter = 'simulator')
    );

CREATE UNIQUE INDEX uq_raw_messages_provider_deduplication
    ON device_raw_messages(provider_code, deduplication_key);

ALTER TABLE device_states
    ADD CONSTRAINT fk_device_states_raw_message_device
    FOREIGN KEY (raw_message_id, device_id) REFERENCES device_raw_messages(id, device_id);

ALTER TABLE device_events
    DROP CONSTRAINT device_events_raw_message_id_fkey,
    ADD COLUMN schema_version INTEGER NOT NULL DEFAULT 1,
    ADD COLUMN deduplication_key TEXT NOT NULL,
    ADD CONSTRAINT chk_device_events_deduplication_key CHECK (
        deduplication_key = btrim(deduplication_key) AND char_length(deduplication_key) > 0
    ),
    ADD CONSTRAINT chk_device_events_schema_version CHECK (schema_version = 1),
    ADD CONSTRAINT chk_device_events_type CHECK (event_type IN (
        'device.created', 'device.lifecycle_changed', 'device.connection_changed',
        'device.state_updated', 'command.created', 'command.status_changed'
    )),
    ADD CONSTRAINT chk_device_events_source CHECK (source IN ('admin', 'open_api', 'provider_callback', 'simulator', 'system')),
    ADD CONSTRAINT chk_device_events_payload CHECK (jsonb_typeof(payload) = 'object'),
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
    ),
    ADD CONSTRAINT fk_device_events_device_project
        FOREIGN KEY (device_id, project_id) REFERENCES devices(id, project_id),
    ADD CONSTRAINT fk_device_events_command_project_device
        FOREIGN KEY (command_id, project_id, device_id) REFERENCES device_commands(id, project_id, device_id),
    ADD CONSTRAINT fk_device_events_raw_message_device
        FOREIGN KEY (raw_message_id, device_id) REFERENCES device_raw_messages(id, device_id),
    ADD CONSTRAINT uq_device_events_id_project UNIQUE (id, project_id);

CREATE UNIQUE INDEX uq_device_events_project_deduplication
    ON device_events(project_id, deduplication_key);

ALTER TABLE webhook_deliveries
    DROP CONSTRAINT webhook_deliveries_event_id_fkey,
    DROP COLUMN signature,
    DROP COLUMN payload,
    DROP COLUMN last_error,
    ADD COLUMN webhook_config_version BIGINT NOT NULL,
    ADD COLUMN webhook_secret_version INTEGER NOT NULL,
    ADD COLUMN raw_body BYTEA NOT NULL,
    ADD COLUMN last_error_code TEXT,
    ADD COLUMN last_error_detail TEXT,
    ADD COLUMN lease_token UUID,
    ADD COLUMN lease_owner TEXT,
    ADD COLUMN lease_expires_at TIMESTAMPTZ,
    ADD COLUMN replay_of_delivery_id UUID,
    ADD CONSTRAINT uq_webhook_deliveries_id_project UNIQUE (id, project_id),
    ADD CONSTRAINT fk_webhook_deliveries_event_project
        FOREIGN KEY (event_id, project_id) REFERENCES device_events(id, project_id),
    ADD CONSTRAINT fk_webhook_deliveries_replay_project
        FOREIGN KEY (replay_of_delivery_id, project_id) REFERENCES webhook_deliveries(id, project_id),
    ADD CONSTRAINT fk_webhook_deliveries_secret_version
        FOREIGN KEY (project_id, webhook_secret_version) REFERENCES project_webhook_secrets(project_id, version),
    ADD CONSTRAINT chk_webhook_deliveries_config_version CHECK (webhook_config_version > 0),
    ADD CONSTRAINT chk_webhook_deliveries_raw_body CHECK (octet_length(raw_body) > 0),
    ADD CONSTRAINT chk_webhook_deliveries_status CHECK (status IN ('pending', 'sending', 'delivered', 'failed', 'dead')),
    ADD CONSTRAINT chk_webhook_deliveries_attempt_count CHECK (attempt_count BETWEEN 0 AND 5),
    ADD CONSTRAINT chk_webhook_deliveries_lease CHECK (
        (lease_token IS NULL AND lease_owner IS NULL AND lease_expires_at IS NULL) OR
        (lease_token IS NOT NULL AND lease_owner IS NOT NULL AND lease_expires_at IS NOT NULL)
    ),
    ADD CONSTRAINT chk_webhook_deliveries_state CHECK (
        (status = 'pending' AND attempt_count = 0 AND next_attempt_at IS NOT NULL AND lease_token IS NULL) OR
        (status = 'sending' AND attempt_count BETWEEN 1 AND 5 AND lease_token IS NOT NULL) OR
        (status = 'failed' AND attempt_count BETWEEN 1 AND 4 AND next_attempt_at IS NOT NULL AND lease_token IS NULL) OR
        (status = 'delivered' AND attempt_count BETWEEN 1 AND 5 AND next_attempt_at IS NULL AND lease_token IS NULL) OR
        (status = 'dead' AND attempt_count = 5 AND next_attempt_at IS NULL AND lease_token IS NULL)
    ),
    ADD CONSTRAINT chk_webhook_deliveries_terminal CHECK (
        (status = 'delivered' AND delivered_at IS NOT NULL) OR
        (status <> 'delivered' AND delivered_at IS NULL)
    ),
    ADD CONSTRAINT chk_webhook_deliveries_replay CHECK (replay_of_delivery_id IS NULL OR replay_of_delivery_id <> id);

CREATE UNIQUE INDEX uq_webhook_deliveries_initial_event
    ON webhook_deliveries(event_id)
    WHERE replay_of_delivery_id IS NULL;
CREATE INDEX idx_webhook_deliveries_lease
    ON webhook_deliveries(lease_expires_at)
    WHERE lease_expires_at IS NOT NULL;

CREATE TABLE webhook_delivery_attempts (
    id UUID PRIMARY KEY,
    delivery_id UUID NOT NULL REFERENCES webhook_deliveries(id),
    attempt_no INTEGER NOT NULL,
    request_timestamp BIGINT NOT NULL,
    http_status INTEGER,
    response_summary TEXT,
    error_code TEXT,
    error_detail TEXT,
    started_at TIMESTAMPTZ NOT NULL,
    completed_at TIMESTAMPTZ,
    UNIQUE (delivery_id, attempt_no),
    CHECK (attempt_no BETWEEN 1 AND 5),
    CHECK (http_status IS NULL OR http_status BETWEEN 100 AND 599),
    CHECK (response_summary IS NULL OR octet_length(response_summary) <= 4096),
    CHECK (completed_at IS NULL OR completed_at >= started_at)
);

CREATE INDEX idx_webhook_delivery_attempts_delivery
    ON webhook_delivery_attempts(delivery_id, attempt_no);
CREATE UNIQUE INDEX uq_webhook_delivery_attempts_one_incomplete
    ON webhook_delivery_attempts(delivery_id)
    WHERE completed_at IS NULL;

ALTER TABLE audit_logs
    DROP COLUMN user_id,
    DROP COLUMN user_agent;
ALTER TABLE audit_logs RENAME COLUMN created_at TO occurred_at;

ALTER TABLE audit_logs
    ADD COLUMN actor_id TEXT,
    ADD COLUMN result TEXT NOT NULL,
    ADD CONSTRAINT chk_audit_logs_actor_type CHECK (actor_type IN ('admin', 'project', 'provider', 'system')),
    ADD CONSTRAINT chk_audit_logs_result CHECK (result IN ('success', 'failure')),
    ADD CONSTRAINT chk_audit_logs_action CHECK (action IN (
        'setup.completed', 'auth.login', 'auth.refresh', 'auth.logout',
        'project.created', 'project.updated', 'project.api_key_rotated', 'project.webhook_secret_rotated',
        'device.created', 'device.updated', 'device.lifecycle_changed',
        'command.created', 'command.cancelled', 'webhook.delivery_replayed', 'simulator.updated'
    )),
    ADD CONSTRAINT chk_audit_logs_metadata CHECK (jsonb_typeof(metadata) = 'object');

CREATE TABLE auth_login_failure_events (
    id UUID PRIMARY KEY,
    scope TEXT NOT NULL,
    key_digest BYTEA NOT NULL,
    occurred_at TIMESTAMPTZ NOT NULL,
    expires_at TIMESTAMPTZ NOT NULL,
    CHECK (scope IN ('email_ip', 'ip')),
    CHECK (octet_length(key_digest) = 32),
    CHECK (expires_at > occurred_at)
);

CREATE INDEX idx_auth_login_failure_events_lookup
    ON auth_login_failure_events(scope, key_digest, occurred_at DESC);
CREATE INDEX idx_auth_login_failure_events_expiry
    ON auth_login_failure_events(expires_at);

CREATE TABLE simulator_config (
    singleton BOOLEAN PRIMARY KEY DEFAULT TRUE CHECK (singleton),
    outcome TEXT NOT NULL,
    delay_ms INTEGER NOT NULL,
    version BIGINT NOT NULL DEFAULT 1,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    CHECK (outcome IN (
        'provider_accepted', 'provider_rejected', 'transport_error_before_send',
        'transport_error_after_send', 'invalid_response'
    )),
    CHECK (delay_ms BETWEEN 0 AND 60000),
    CHECK (version > 0)
);

INSERT INTO simulator_config (outcome, delay_ms) VALUES ('provider_accepted', 0);
