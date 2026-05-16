CREATE TABLE device_events (
    id TEXT PRIMARY KEY,
    project_id TEXT NOT NULL,
    device_id TEXT,
    command_id TEXT,
    event_type TEXT NOT NULL,
    source TEXT NOT NULL,
    payload JSONB NOT NULL DEFAULT '{}'::jsonb,
    occurred_at TIMESTAMPTZ NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX idx_device_events_project_created ON device_events(project_id, created_at DESC);
CREATE INDEX idx_device_events_command_id ON device_events(command_id) WHERE command_id IS NOT NULL;

CREATE TABLE webhook_deliveries (
    id TEXT PRIMARY KEY,
    event_id TEXT NOT NULL REFERENCES device_events(id),
    project_id TEXT NOT NULL,
    device_id TEXT,
    webhook_url TEXT NOT NULL,
    status TEXT NOT NULL DEFAULT 'pending',
    attempt_count INTEGER NOT NULL DEFAULT 0,
    max_attempts INTEGER NOT NULL DEFAULT 3,
    last_response_code INTEGER,
    last_response_body TEXT,
    last_error TEXT,
    next_retry_at TIMESTAMPTZ,
    request_body JSONB NOT NULL DEFAULT '{}'::jsonb,
    signature TEXT NOT NULL,
    delivered_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    CHECK (attempt_count >= 0),
    CHECK (max_attempts = 3),
    CHECK (status IN ('pending', 'sending', 'delivered', 'failed', 'dead'))
);

CREATE INDEX idx_webhook_deliveries_project_created ON webhook_deliveries(project_id, created_at DESC);
CREATE INDEX idx_webhook_deliveries_status_next_retry ON webhook_deliveries(status, next_retry_at);

CREATE TABLE audit_logs (
    id TEXT PRIMARY KEY,
    action TEXT NOT NULL,
    actor_type TEXT NOT NULL,
    project_id TEXT,
    resource_type TEXT NOT NULL,
    resource_id TEXT,
    request_id TEXT,
    ip_address TEXT,
    user_agent TEXT,
    metadata JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX idx_audit_logs_project_created ON audit_logs(project_id, created_at DESC);
CREATE INDEX idx_audit_logs_resource ON audit_logs(resource_type, resource_id);
