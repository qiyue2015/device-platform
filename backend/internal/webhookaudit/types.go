package webhookaudit

import "time"

const (
	StatusPending   = "pending"
	StatusSending   = "sending"
	StatusDelivered = "delivered"
	StatusFailed    = "failed"
	StatusDead      = "dead"

	MaxDeliveryAttempts = 3
)

type ProjectEndpoint struct {
	ProjectID     string `json:"project_id"`
	WebhookURL    string `json:"webhook_url"`
	WebhookSecret string `json:"webhook_secret"`
}

type Event struct {
	ID         string         `json:"id"`
	ProjectID  string         `json:"project_id"`
	DeviceID   string         `json:"device_id,omitempty"`
	CommandID  string         `json:"command_id,omitempty"`
	EventType  string         `json:"event_type"`
	Source     string         `json:"source"`
	Payload    map[string]any `json:"payload"`
	OccurredAt time.Time      `json:"occurred_at"`
	CreatedAt  time.Time      `json:"created_at"`
}

type WebhookDelivery struct {
	ID               string         `json:"id"`
	EventID          string         `json:"event_id"`
	ProjectID        string         `json:"project_id"`
	DeviceID         string         `json:"device_id,omitempty"`
	WebhookURL       string         `json:"webhook_url"`
	Status           string         `json:"status"`
	AttemptCount     int            `json:"attempt_count"`
	MaxAttempts      int            `json:"max_attempts"`
	LastResponseCode int            `json:"last_response_code,omitempty"`
	LastResponseBody string         `json:"last_response_body,omitempty"`
	LastError        string         `json:"last_error,omitempty"`
	NextRetryAt      *time.Time     `json:"next_retry_at,omitempty"`
	RequestBody      map[string]any `json:"request_body"`
	Signature        string         `json:"signature"`
	CreatedAt        time.Time      `json:"created_at"`
	UpdatedAt        time.Time      `json:"updated_at"`
	DeliveredAt      *time.Time     `json:"delivered_at,omitempty"`
}

type AuditLog struct {
	ID           string         `json:"id"`
	Action       string         `json:"action"`
	ActorType    string         `json:"actor_type"`
	ProjectID    string         `json:"project_id,omitempty"`
	ResourceType string         `json:"resource_type"`
	ResourceID   string         `json:"resource_id,omitempty"`
	RequestID    string         `json:"request_id,omitempty"`
	IPAddress    string         `json:"ip_address,omitempty"`
	UserAgent    string         `json:"user_agent,omitempty"`
	Metadata     map[string]any `json:"metadata,omitempty"`
	CreatedAt    time.Time      `json:"created_at"`
}

type CreateEventRequest struct {
	ProjectID string         `json:"project_id"`
	DeviceID  string         `json:"device_id"`
	CommandID string         `json:"command_id"`
	EventType string         `json:"event_type"`
	Source    string         `json:"source"`
	Payload   map[string]any `json:"payload"`
}

type AuditRequest struct {
	Action       string         `json:"action"`
	ActorType    string         `json:"actor_type"`
	ProjectID    string         `json:"project_id"`
	ResourceType string         `json:"resource_type"`
	ResourceID   string         `json:"resource_id"`
	RequestID    string         `json:"request_id"`
	IPAddress    string         `json:"ip_address"`
	UserAgent    string         `json:"user_agent"`
	Metadata     map[string]any `json:"metadata"`
}
