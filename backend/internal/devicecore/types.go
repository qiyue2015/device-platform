package devicecore

import "time"

type Project struct {
	ID          string    `json:"id"`
	Name        string    `json:"name"`
	APIKey      string    `json:"api_key"`
	WebhookURL  string    `json:"webhook_url,omitempty"`
	IPWhitelist []string  `json:"ip_whitelist,omitempty"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

type Device struct {
	ID                string         `json:"id"`
	ProjectID         string         `json:"project_id"`
	ExternalID        string         `json:"external_id,omitempty"`
	DeviceTypeID      string         `json:"device_type_id,omitempty"`
	Name              string         `json:"name"`
	DeviceType        string         `json:"device_type"`
	ProviderCode      string         `json:"provider_code"`
	ProviderDeviceID  string         `json:"provider_device_id"`
	AccessType        string         `json:"access_type"`
	TransportProtocol string         `json:"transport_protocol"`
	Adapter           string         `json:"adapter"`
	ConnectionStatus  string         `json:"connection_status"`
	LifecycleStatus   string         `json:"lifecycle_status"`
	Metadata          map[string]any `json:"metadata,omitempty"`
	Online            bool           `json:"online"`
	CurrentState      map[string]any `json:"current_state,omitempty"`
	LastSeenAt        *time.Time     `json:"last_seen_at,omitempty"`
	CreatedAt         time.Time      `json:"created_at"`
	UpdatedAt         time.Time      `json:"updated_at"`
}

type CommandStatus string

const (
	CommandStatusCreated   CommandStatus = "created"
	CommandStatusQueued    CommandStatus = "queued"
	CommandStatusSent      CommandStatus = "sent"
	CommandStatusAcked     CommandStatus = "acked"
	CommandStatusSuccess   CommandStatus = "success"
	CommandStatusFailed    CommandStatus = "failed"
	CommandStatusTimeout   CommandStatus = "timeout"
	CommandStatusCancelled CommandStatus = "cancelled"
	CommandStatusOffline   CommandStatus = "offline"
)

type DeliveryPolicy string

const (
	DeliveryPolicyOnlineOnly       DeliveryPolicy = "online_only"
	DeliveryPolicyQueueUntilExpire DeliveryPolicy = "queue_until_expire"
	DeliveryPolicyReplaceLatest    DeliveryPolicy = "replace_latest"
)

type Command struct {
	ID                string           `json:"id"`
	ProjectID         string           `json:"project_id"`
	DeviceID          string           `json:"device_id"`
	CommandType       string           `json:"command_type"`
	Payload           map[string]any   `json:"payload,omitempty"`
	IdempotencyKey    string           `json:"idempotency_key,omitempty"`
	RequestHash       string           `json:"request_hash,omitempty"`
	DeliveryPolicy    DeliveryPolicy   `json:"delivery_policy"`
	Status            CommandStatus    `json:"status"`
	Reason            string           `json:"reason,omitempty"`
	Attempts          []CommandAttempt `json:"attempts,omitempty"`
	Events            []CommandEvent   `json:"events,omitempty"`
	ExpiresAt         time.Time        `json:"expires_at"`
	SentAt            *time.Time       `json:"sent_at,omitempty"`
	FinishedAt        *time.Time       `json:"finished_at,omitempty"`
	CompensationUntil *time.Time       `json:"compensation_until,omitempty"`
	Corrected         bool             `json:"corrected"`
	CreatedAt         time.Time        `json:"created_at"`
	UpdatedAt         time.Time        `json:"updated_at"`
}

type CommandAttempt struct {
	AttemptNo int       `json:"attempt_no"`
	Status    string    `json:"status"`
	At        time.Time `json:"at"`
	Error     string    `json:"error,omitempty"`
}

type CommandEvent struct {
	Type      string         `json:"type"`
	Payload   map[string]any `json:"payload,omitempty"`
	At        time.Time      `json:"at"`
	Corrected bool           `json:"corrected,omitempty"`
}

type CreateProjectRequest struct {
	Name        string   `json:"name"`
	WebhookURL  string   `json:"webhook_url"`
	IPWhitelist []string `json:"ip_whitelist"`
}

type UpdateProjectRequest struct {
	Name        *string  `json:"name"`
	WebhookURL  *string  `json:"webhook_url"`
	IPWhitelist []string `json:"ip_whitelist"`
}

type CreateDeviceRequest struct {
	ProjectID         string         `json:"project_id"`
	ExternalID        string         `json:"external_id"`
	DeviceTypeID      string         `json:"device_type_id"`
	Name              string         `json:"name"`
	DeviceType        string         `json:"device_type"`
	ProviderCode      string         `json:"provider_code"`
	ProviderDeviceID  string         `json:"provider_device_id"`
	AccessType        string         `json:"access_type"`
	TransportProtocol string         `json:"transport_protocol"`
	Adapter           string         `json:"adapter"`
	ConnectionStatus  string         `json:"connection_status"`
	LifecycleStatus   string         `json:"lifecycle_status"`
	Metadata          map[string]any `json:"metadata"`
	Online            bool           `json:"online"`
	CurrentState      map[string]any `json:"current_state"`
}

type CreateCommandRequest struct {
	ProjectID      string         `json:"project_id"`
	DeviceID       string         `json:"device_id"`
	CommandType    string         `json:"command_type"`
	Payload        map[string]any `json:"payload"`
	IdempotencyKey string         `json:"idempotency_key"`
	DeliveryPolicy DeliveryPolicy `json:"delivery_policy"`
	ExpiresAt      *time.Time     `json:"expires_at"`
}
