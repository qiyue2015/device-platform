CREATE TABLE users (
    id UUID PRIMARY KEY,
    email TEXT NOT NULL UNIQUE,
    password_hash TEXT NOT NULL,
    display_name TEXT NOT NULL,
    is_admin BOOLEAN NOT NULL DEFAULT FALSE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE projects (
    id UUID PRIMARY KEY,
    name TEXT NOT NULL,
    api_key_hash TEXT NOT NULL UNIQUE,
    webhook_url TEXT,
    webhook_secret TEXT,
    ip_whitelist TEXT[] NOT NULL DEFAULT '{}',
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE device_types (
    id UUID PRIMARY KEY,
    code TEXT NOT NULL UNIQUE,
    name TEXT NOT NULL,
    capabilities JSONB NOT NULL DEFAULT '[]'::jsonb,
    default_command_policy JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE devices (
    id UUID PRIMARY KEY,
    project_id UUID NOT NULL REFERENCES projects(id),
    device_type_id UUID NOT NULL REFERENCES device_types(id),
    name TEXT NOT NULL,
    provider_code TEXT NOT NULL,
    provider_device_id TEXT NOT NULL,
    access_type TEXT NOT NULL,
    transport_protocol TEXT NOT NULL,
    adapter TEXT NOT NULL,
    connection_status TEXT NOT NULL DEFAULT 'unknown',
    lifecycle_status TEXT NOT NULL DEFAULT 'active',
    metadata JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (project_id, provider_code, provider_device_id),
    CHECK (access_type IN ('mock_gateway', 'cloud_api')),
    CHECK (transport_protocol IN ('simulator', 'http', 'mqtt', 'tcp', 'ble')),
    CHECK (adapter IN ('mock_gateway', 'wwtiot_cloud_api')),
    CHECK (
        (access_type = 'mock_gateway' AND adapter = 'mock_gateway')
        OR (access_type = 'cloud_api' AND adapter = 'wwtiot_cloud_api')
    ),
    CHECK (connection_status IN ('unknown', 'online', 'offline')),
    CHECK (lifecycle_status IN ('active', 'disabled', 'deleted'))
);

CREATE INDEX idx_devices_project_id ON devices(project_id);
CREATE INDEX idx_devices_provider_identity ON devices(provider_code, provider_device_id);

CREATE TABLE device_states (
    id UUID PRIMARY KEY,
    device_id UUID NOT NULL REFERENCES devices(id),
    state JSONB NOT NULL DEFAULT '{}'::jsonb,
    reported_at TIMESTAMPTZ NOT NULL,
    observed_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    raw_message_id UUID,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX idx_device_states_device_observed ON device_states(device_id, observed_at DESC);

CREATE TABLE device_commands (
    id UUID PRIMARY KEY,
    project_id UUID NOT NULL REFERENCES projects(id),
    device_id UUID NOT NULL REFERENCES devices(id),
    command_type TEXT NOT NULL,
    payload JSONB NOT NULL DEFAULT '{}'::jsonb,
    status TEXT NOT NULL DEFAULT 'created',
    delivery_policy TEXT NOT NULL,
    idempotency_key TEXT,
    request_hash TEXT,
    reason TEXT,
    expires_at TIMESTAMPTZ,
    sent_at TIMESTAMPTZ,
    finished_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (project_id, idempotency_key)
);

CREATE INDEX idx_device_commands_project_created ON device_commands(project_id, created_at DESC);
CREATE INDEX idx_device_commands_device_status ON device_commands(device_id, status);
CREATE INDEX idx_device_commands_expires_at ON device_commands(expires_at) WHERE expires_at IS NOT NULL;

CREATE TABLE device_command_attempts (
    id UUID PRIMARY KEY,
    command_id UUID NOT NULL REFERENCES device_commands(id),
    attempt_no INTEGER NOT NULL,
    adapter TEXT NOT NULL,
    status TEXT NOT NULL DEFAULT 'created',
    request_body JSONB NOT NULL DEFAULT '{}'::jsonb,
    response_body JSONB NOT NULL DEFAULT '{}'::jsonb,
    error_message TEXT,
    started_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    finished_at TIMESTAMPTZ,
    UNIQUE (command_id, attempt_no),
    CHECK (adapter IN ('mock_gateway', 'wwtiot_cloud_api')),
    CHECK (status IN ('created', 'sent', 'acked', 'success', 'failed', 'timeout'))
);

CREATE INDEX idx_device_command_attempts_command_id ON device_command_attempts(command_id);

CREATE TABLE device_raw_messages (
    id UUID PRIMARY KEY,
    device_id UUID REFERENCES devices(id),
    provider_code TEXT NOT NULL,
    provider_device_id TEXT NOT NULL,
    access_type TEXT NOT NULL,
    transport_protocol TEXT NOT NULL,
    adapter TEXT NOT NULL,
    direction TEXT NOT NULL,
    headers JSONB NOT NULL DEFAULT '{}'::jsonb,
    body BYTEA NOT NULL,
    received_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    CHECK (access_type IN ('mock_gateway', 'cloud_api')),
    CHECK (transport_protocol IN ('simulator', 'http', 'mqtt', 'tcp', 'ble')),
    CHECK (adapter IN ('mock_gateway', 'wwtiot_cloud_api')),
    CHECK (
        (access_type = 'mock_gateway' AND adapter = 'mock_gateway')
        OR (access_type = 'cloud_api' AND adapter = 'wwtiot_cloud_api')
    ),
    CHECK (direction IN ('inbound', 'outbound'))
);

CREATE INDEX idx_device_raw_messages_device_received ON device_raw_messages(device_id, received_at DESC);
CREATE INDEX idx_device_raw_messages_provider_identity ON device_raw_messages(provider_code, provider_device_id);

CREATE TABLE device_events (
    id UUID PRIMARY KEY,
    project_id UUID NOT NULL REFERENCES projects(id),
    device_id UUID REFERENCES devices(id),
    command_id UUID REFERENCES device_commands(id),
    event_type TEXT NOT NULL,
    source TEXT NOT NULL,
    payload JSONB NOT NULL DEFAULT '{}'::jsonb,
    raw_message_id UUID REFERENCES device_raw_messages(id),
    occurred_at TIMESTAMPTZ NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX idx_device_events_project_created ON device_events(project_id, created_at DESC);
CREATE INDEX idx_device_events_device_occurred ON device_events(device_id, occurred_at DESC);
CREATE INDEX idx_device_events_command_id ON device_events(command_id) WHERE command_id IS NOT NULL;

ALTER TABLE device_states
    ADD CONSTRAINT fk_device_states_raw_message
    FOREIGN KEY (raw_message_id) REFERENCES device_raw_messages(id);

CREATE TABLE webhook_deliveries (
    id UUID PRIMARY KEY,
    project_id UUID NOT NULL REFERENCES projects(id),
    event_id UUID NOT NULL REFERENCES device_events(id),
    target_url TEXT NOT NULL,
    payload JSONB NOT NULL DEFAULT '{}'::jsonb,
    signature TEXT,
    attempt_count INTEGER NOT NULL DEFAULT 0,
    status TEXT NOT NULL DEFAULT 'pending',
    last_error TEXT,
    next_attempt_at TIMESTAMPTZ,
    delivered_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX idx_webhook_deliveries_project_created ON webhook_deliveries(project_id, created_at DESC);
CREATE INDEX idx_webhook_deliveries_status_next_attempt ON webhook_deliveries(status, next_attempt_at);

CREATE TABLE audit_logs (
    id UUID PRIMARY KEY,
    project_id UUID REFERENCES projects(id),
    user_id UUID REFERENCES users(id),
    actor_type TEXT NOT NULL,
    action TEXT NOT NULL,
    resource_type TEXT NOT NULL,
    resource_id TEXT,
    request_id TEXT,
    ip_address INET,
    user_agent TEXT,
    metadata JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX idx_audit_logs_project_created ON audit_logs(project_id, created_at DESC);
CREATE INDEX idx_audit_logs_resource ON audit_logs(resource_type, resource_id);
