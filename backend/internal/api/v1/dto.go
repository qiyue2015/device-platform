package v1

import (
	"time"

	"github.com/qiyue2015/device-platform/internal/domain"
)

type ErrorResponse struct {
	Code    string         `json:"code"`
	Message string         `json:"message"`
	Details map[string]any `json:"details,omitempty"`
}

type ProjectResponse struct {
	ID          string    `json:"id"`
	Name        string    `json:"name"`
	WebhookURL  string    `json:"webhook_url,omitempty"`
	IPWhitelist []string  `json:"ip_whitelist,omitempty"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

type CreateProjectRequest struct {
	Name        string   `json:"name"`
	WebhookURL  string   `json:"webhook_url,omitempty"`
	IPWhitelist []string `json:"ip_whitelist,omitempty"`
}

type UpdateProjectRequest struct {
	Name        *string   `json:"name,omitempty"`
	WebhookURL  *string   `json:"webhook_url,omitempty"`
	IPWhitelist *[]string `json:"ip_whitelist,omitempty"`
}

type DeviceTypeResponse struct {
	ID           string   `json:"id"`
	Code         string   `json:"code"`
	Name         string   `json:"name"`
	Capabilities []string `json:"capabilities"`
}

type DeviceResponse struct {
	ID                string                   `json:"id"`
	ProjectID         string                   `json:"project_id"`
	DeviceTypeID      string                   `json:"device_type_id"`
	Name              string                   `json:"name"`
	ProviderCode      string                   `json:"provider_code"`
	ProviderDeviceID  string                   `json:"provider_device_id"`
	AccessType        domain.AccessType        `json:"access_type"`
	TransportProtocol domain.TransportProtocol `json:"transport_protocol"`
	Adapter           domain.Adapter           `json:"adapter"`
	ConnectionStatus  domain.ConnectionStatus  `json:"connection_status"`
	LifecycleStatus   domain.LifecycleStatus   `json:"lifecycle_status"`
	Metadata          map[string]any           `json:"metadata,omitempty"`
	CurrentState      *DeviceStateResponse     `json:"current_state,omitempty"`
	CreatedAt         time.Time                `json:"created_at"`
	UpdatedAt         time.Time                `json:"updated_at"`
}

type CreateDeviceRequest struct {
	ProjectID         string                   `json:"project_id,omitempty"`
	DeviceTypeID      string                   `json:"device_type_id"`
	Name              string                   `json:"name"`
	ProviderCode      string                   `json:"provider_code"`
	ProviderDeviceID  string                   `json:"provider_device_id"`
	AccessType        domain.AccessType        `json:"access_type"`
	TransportProtocol domain.TransportProtocol `json:"transport_protocol"`
	Adapter           domain.Adapter           `json:"adapter"`
	Metadata          map[string]any           `json:"metadata,omitempty"`
	ConnectionStatus  domain.ConnectionStatus  `json:"connection_status,omitempty"`
	LifecycleStatus   domain.LifecycleStatus   `json:"lifecycle_status,omitempty"`
}

type DeviceStateResponse struct {
	State      map[string]any `json:"state"`
	ReportedAt time.Time      `json:"reported_at"`
	ObservedAt time.Time      `json:"observed_at"`
}

type CreateDeviceCommandRequest struct {
	DeviceID       string                 `json:"device_id"`
	CommandType    domain.CommandType     `json:"command_type"`
	Payload        map[string]any         `json:"payload,omitempty"`
	IdempotencyKey string                 `json:"idempotency_key,omitempty"`
	DeliveryPolicy *domain.DeliveryPolicy `json:"delivery_policy,omitempty"`
	ExpiresAt      *time.Time             `json:"expires_at,omitempty"`
}

type DeviceCommandResponse struct {
	ID             string                `json:"id"`
	ProjectID      string                `json:"project_id"`
	DeviceID       string                `json:"device_id"`
	CommandType    domain.CommandType    `json:"command_type"`
	Payload        map[string]any        `json:"payload,omitempty"`
	Status         domain.CommandStatus  `json:"status"`
	DeliveryPolicy domain.DeliveryPolicy `json:"delivery_policy"`
	Reason         string                `json:"reason,omitempty"`
	ExpiresAt      *time.Time            `json:"expires_at,omitempty"`
	SentAt         *time.Time            `json:"sent_at,omitempty"`
	FinishedAt     *time.Time            `json:"finished_at,omitempty"`
	CreatedAt      time.Time             `json:"created_at"`
	UpdatedAt      time.Time             `json:"updated_at"`
}

type DeviceCommandAttemptResponse struct {
	ID           string               `json:"id"`
	CommandID    string               `json:"command_id"`
	AttemptNo    int                  `json:"attempt_no"`
	Adapter      domain.Adapter       `json:"adapter"`
	Status       domain.AttemptStatus `json:"status"`
	ErrorMessage string               `json:"error_message,omitempty"`
	StartedAt    time.Time            `json:"started_at"`
	FinishedAt   *time.Time           `json:"finished_at,omitempty"`
}

type DeviceEventResponse struct {
	ID         string             `json:"id"`
	ProjectID  string             `json:"project_id"`
	DeviceID   string             `json:"device_id"`
	CommandID  string             `json:"command_id,omitempty"`
	EventType  string             `json:"event_type"`
	Source     domain.EventSource `json:"source"`
	Payload    map[string]any     `json:"payload,omitempty"`
	OccurredAt time.Time          `json:"occurred_at"`
	CreatedAt  time.Time          `json:"created_at"`
}

type WebhookDeliveryResponse struct {
	ID            string                       `json:"id"`
	ProjectID     string                       `json:"project_id"`
	EventID       string                       `json:"event_id"`
	TargetURL     string                       `json:"target_url"`
	AttemptCount  int                          `json:"attempt_count"`
	Status        domain.WebhookDeliveryStatus `json:"status"`
	LastError     string                       `json:"last_error,omitempty"`
	NextAttemptAt *time.Time                   `json:"next_attempt_at,omitempty"`
	DeliveredAt   *time.Time                   `json:"delivered_at,omitempty"`
	CreatedAt     time.Time                    `json:"created_at"`
	UpdatedAt     time.Time                    `json:"updated_at"`
}

type DeviceRawMessageResponse struct {
	ID                string                     `json:"id"`
	DeviceID          string                     `json:"device_id,omitempty"`
	ProviderCode      string                     `json:"provider_code"`
	ProviderDeviceID  string                     `json:"provider_device_id"`
	AccessType        domain.AccessType          `json:"access_type"`
	TransportProtocol domain.TransportProtocol   `json:"transport_protocol"`
	Adapter           domain.Adapter             `json:"adapter"`
	Direction         domain.RawMessageDirection `json:"direction"`
	Headers           map[string]any             `json:"headers,omitempty"`
	Body              string                     `json:"body"`
	ReceivedAt        time.Time                  `json:"received_at"`
	CreatedAt         time.Time                  `json:"created_at"`
}

type AuditLogResponse struct {
	ID           string         `json:"id"`
	ProjectID    string         `json:"project_id,omitempty"`
	UserID       string         `json:"user_id,omitempty"`
	ActorType    string         `json:"actor_type"`
	Action       string         `json:"action"`
	ResourceType string         `json:"resource_type"`
	ResourceID   string         `json:"resource_id,omitempty"`
	RequestID    string         `json:"request_id,omitempty"`
	IPAddress    string         `json:"ip_address,omitempty"`
	UserAgent    string         `json:"user_agent,omitempty"`
	Metadata     map[string]any `json:"metadata,omitempty"`
	CreatedAt    time.Time      `json:"created_at"`
}

type SetSimulatorModeRequest struct {
	Mode        domain.SimulatorMode `json:"mode"`
	DelayMillis int                  `json:"delay_millis,omitempty"`
}

type SimulatorModeResponse struct {
	Mode        domain.SimulatorMode `json:"mode"`
	DelayMillis int                  `json:"delay_millis,omitempty"`
	UpdatedAt   time.Time            `json:"updated_at"`
}
