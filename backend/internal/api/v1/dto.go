package v1

import (
	"encoding/json"
	"errors"
	"time"

	"github.com/qiyue2015/device-platform/internal/domain"
)

type ErrorResponse struct {
	Code    string         `json:"code"`
	Message string         `json:"message"`
	Details map[string]any `json:"details,omitempty"`
}

type ProjectResponse struct {
	ID                string                 `json:"id"`
	Name              string                 `json:"name"`
	ManagerUserID     string                 `json:"manager_user_id"`
	Manager           ProjectManagerResponse `json:"manager"`
	WebhookURL        **string               `json:"webhook_url,omitempty"`
	WebhookConfigured *bool                  `json:"webhook_configured,omitempty"`
	IPWhitelist       *[]string              `json:"ip_whitelist,omitempty"`
	CreatedAt         time.Time              `json:"created_at"`
	UpdatedAt         time.Time              `json:"updated_at"`
}

type ProjectManagerResponse struct {
	ID          string            `json:"id"`
	Email       string            `json:"email"`
	DisplayName string            `json:"display_name"`
	Status      domain.UserStatus `json:"status"`
}

type ProjectCredentialResponse struct {
	ProjectResponse
	APIKey        string `json:"api_key,omitempty"`
	WebhookSecret string `json:"webhook_secret,omitempty"`
}

type OpenProjectResponse struct {
	ID        string    `json:"id"`
	Name      string    `json:"name"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

type CreateProjectRequest struct {
	Name          string   `json:"name"`
	ManagerUserID string   `json:"manager_user_id"`
	WebhookURL    *string  `json:"webhook_url,omitempty"`
	IPWhitelist   []string `json:"ip_whitelist,omitempty"`
}

type TransferProjectRequest struct {
	ManagerUserID string `json:"manager_user_id"`
}

type UpdateProjectRequest struct {
	Name        *string   `json:"name,omitempty"`
	WebhookURL  **string  `json:"webhook_url,omitempty"`
	IPWhitelist *[]string `json:"ip_whitelist,omitempty"`
}

type CapabilityActionResponse struct {
	Identifier                    domain.ActionIdentifier `json:"identifier"`
	PayloadSchema                 map[string]any          `json:"payload_schema"`
	RiskLevel                     string                  `json:"risk_level"`
	DeliveryPolicy                domain.DeliveryPolicy   `json:"delivery_policy"`
	DispatchDeadlineMS            int64                   `json:"dispatch_deadline_ms"`
	ProviderRequestTimeoutMS      int64                   `json:"provider_request_timeout_ms"`
	ResultObservationTimeoutMS    int64                   `json:"result_observation_timeout_ms"`
	RetryAllowed                  bool                    `json:"retry_allowed"`
	DeliveryPolicyOverrideAllowed bool                    `json:"delivery_policy_override_allowed"`
}

type DeviceTypeResponse struct {
	Code     string                     `json:"code"`
	Revision int                        `json:"revision"`
	Name     string                     `json:"name"`
	Actions  []CapabilityActionResponse `json:"actions"`
}

type ProviderResponse struct {
	Code              string                           `json:"code"`
	Name              string                           `json:"name"`
	AccessType        domain.AccessType                `json:"access_type"`
	TransportProtocol domain.TransportProtocol         `json:"transport_protocol"`
	Adapter           domain.Adapter                   `json:"adapter"`
	Profiles          []string                         `json:"profiles"`
	IntegrationStatus domain.ProviderIntegrationStatus `json:"integration_status"`
}

type DeviceStateResponse struct {
	State          map[string]any        `json:"state"`
	EvidenceStatus domain.EvidenceStatus `json:"evidence_status"`
	ReportedAt     *time.Time            `json:"reported_at"`
	ObservedAt     time.Time             `json:"observed_at"`
}

type DeviceResponse struct {
	ID                string                   `json:"id"`
	ProjectID         string                   `json:"project_id"`
	DeviceTypeCode    string                   `json:"device_type_code"`
	Name              string                   `json:"name"`
	ProviderCode      string                   `json:"provider_code"`
	ProviderProfile   string                   `json:"provider_profile"`
	ProviderDeviceID  string                   `json:"provider_device_id"`
	AccessType        domain.AccessType        `json:"access_type"`
	TransportProtocol domain.TransportProtocol `json:"transport_protocol"`
	Adapter           domain.Adapter           `json:"adapter"`
	ConnectionStatus  domain.ConnectionStatus  `json:"connection_status"`
	LifecycleStatus   domain.LifecycleStatus   `json:"lifecycle_status"`
	CurrentState      *DeviceStateResponse     `json:"current_state"`
	LastSeenAt        *time.Time               `json:"last_seen_at"`
	CreatedAt         time.Time                `json:"created_at"`
	UpdatedAt         time.Time                `json:"updated_at"`
}

type CreateDeviceRequest struct {
	ProjectID        string  `json:"project_id"`
	Name             string  `json:"name"`
	DeviceTypeCode   string  `json:"device_type_code"`
	ProviderCode     string  `json:"provider_code"`
	ProviderProfile  string  `json:"provider_profile"`
	ProviderDeviceID *string `json:"provider_device_id,omitempty"`
}

type UpdateDeviceRequest struct {
	Name            *string                 `json:"name,omitempty"`
	LifecycleStatus *domain.LifecycleStatus `json:"lifecycle_status,omitempty"`
}

type CreateCommandRequest struct {
	ProjectID      string                  `json:"project_id,omitempty"`
	DeviceID       string                  `json:"device_id"`
	CommandType    domain.ActionIdentifier `json:"command_type"`
	Payload        OptionalObject          `json:"payload,omitempty"`
	IdempotencyKey string                  `json:"idempotency_key"`
}

type OptionalObject map[string]any

func (value *OptionalObject) UnmarshalJSON(data []byte) error {
	if string(data) == "null" {
		return errors.New("object must not be null")
	}
	var decoded map[string]any
	if err := json.Unmarshal(data, &decoded); err != nil {
		return err
	}
	*value = decoded
	return nil
}

type CommandAttemptResponse struct {
	AttemptNo          int                      `json:"attempt_no"`
	Phase              domain.AttemptPhase      `json:"phase"`
	ProviderCode       string                   `json:"provider_code"`
	Adapter            domain.Adapter           `json:"adapter"`
	ProviderRequestKey string                   `json:"provider_request_key"`
	Outcome            *domain.AttemptOutcome   `json:"outcome"`
	ConfirmationLevel  domain.ConfirmationLevel `json:"confirmation_level"`
	EvidenceStatus     domain.EvidenceStatus    `json:"evidence_status"`
	RequestSummary     map[string]any           `json:"request_summary,omitempty"`
	ResponseSummary    map[string]any           `json:"response_summary,omitempty"`
	ReasonCode         *string                  `json:"reason_code"`
	ErrorDetail        *string                  `json:"error_detail"`
	ClaimedAt          time.Time                `json:"claimed_at"`
	DispatchingAt      *time.Time               `json:"dispatching_at"`
	CompletedAt        *time.Time               `json:"completed_at"`
}

type CommandResultResponse struct {
	ResultID          string                   `json:"result_id"`
	AttemptID         *string                  `json:"attempt_id"`
	Source            domain.EventSource       `json:"source"`
	Outcome           domain.ResultOutcome     `json:"outcome"`
	ConfirmationLevel domain.ConfirmationLevel `json:"confirmation_level"`
	EvidenceStatus    domain.EvidenceStatus    `json:"evidence_status"`
	ReportedAt        *time.Time               `json:"reported_at"`
	ObservedAt        time.Time                `json:"observed_at"`
	Late              bool                     `json:"late"`
}

type EventResponse struct {
	EventID       string             `json:"event_id"`
	SchemaVersion int                `json:"schema_version"`
	EventType     domain.EventType   `json:"event_type"`
	ProjectID     string             `json:"project_id"`
	DeviceID      *string            `json:"device_id"`
	CommandID     *string            `json:"command_id"`
	OccurredAt    time.Time          `json:"occurred_at"`
	Source        domain.EventSource `json:"source"`
	Payload       map[string]any     `json:"payload"`
}

type CommandResponse struct {
	ID                 string                   `json:"id"`
	ProjectID          string                   `json:"project_id"`
	DeviceID           string                   `json:"device_id"`
	ProviderCode       string                   `json:"provider_code"`
	ProviderProfile    string                   `json:"provider_profile"`
	CommandType        domain.ActionIdentifier  `json:"command_type"`
	Payload            map[string]any           `json:"payload"`
	DeviceTypeRevision int                      `json:"device_type_revision"`
	DeliveryPolicy     domain.DeliveryPolicy    `json:"delivery_policy"`
	Status             domain.CommandStatus     `json:"status"`
	ReasonCode         *string                  `json:"reason_code"`
	ReasonDetail       *string                  `json:"reason_detail"`
	ConfirmationLevel  domain.ConfirmationLevel `json:"confirmation_level"`
	EvidenceStatus     domain.EvidenceStatus    `json:"evidence_status"`
	IdempotencyKey     string                   `json:"idempotency_key"`
	QueuedAt           time.Time                `json:"queued_at"`
	DispatchDeadlineAt time.Time                `json:"dispatch_deadline_at"`
	SentAt             *time.Time               `json:"sent_at"`
	ResultDeadlineAt   *time.Time               `json:"result_deadline_at"`
	FinishedAt         *time.Time               `json:"finished_at"`
	CreatedAt          time.Time                `json:"created_at"`
	UpdatedAt          time.Time                `json:"updated_at"`
}

type CommandDetailResponse struct {
	CommandResponse
	Attempts []CommandAttemptResponse `json:"attempts"`
	Results  []CommandResultResponse  `json:"results"`
	Events   []EventResponse          `json:"events"`
}

type WebhookDeliveryAttemptResponse struct {
	AttemptNo       int        `json:"attempt_no"`
	StartedAt       time.Time  `json:"started_at"`
	CompletedAt     *time.Time `json:"completed_at"`
	HTTPStatus      *int       `json:"http_status"`
	ResponseSummary *string    `json:"response_summary"`
	ErrorCode       *string    `json:"error_code"`
	ErrorDetail     *string    `json:"error_detail"`
}

type WebhookDeliveryResponse struct {
	ID                   string                           `json:"id"`
	EventID              string                           `json:"event_id"`
	ProjectID            string                           `json:"project_id"`
	TargetURL            string                           `json:"target_url"`
	WebhookConfigVersion int64                            `json:"webhook_config_version"`
	Status               domain.WebhookDeliveryStatus     `json:"status"`
	AttemptCount         int                              `json:"attempt_count"`
	NextAttemptAt        *time.Time                       `json:"next_attempt_at"`
	ReplayOfDeliveryID   *string                          `json:"replay_of_delivery_id"`
	DeliveredAt          *time.Time                       `json:"delivered_at"`
	CreatedAt            time.Time                        `json:"created_at"`
	UpdatedAt            time.Time                        `json:"updated_at"`
	Attempts             []WebhookDeliveryAttemptResponse `json:"attempts,omitempty"`
}

type AuditLogResponse struct {
	ID           string             `json:"id"`
	ActorType    domain.ActorType   `json:"actor_type"`
	ActorUserID  *string            `json:"actor_user_id"`
	ActorID      *string            `json:"actor_id"`
	ProjectID    *string            `json:"project_id"`
	Action       string             `json:"action"`
	Result       domain.AuditResult `json:"result"`
	ResourceType string             `json:"resource_type"`
	ResourceID   *string            `json:"resource_id"`
	IPAddress    *string            `json:"ip_address"`
	RequestID    *string            `json:"request_id"`
	Metadata     map[string]any     `json:"metadata"`
	OccurredAt   time.Time          `json:"occurred_at"`
}

type UpdateSimulatorRequest struct {
	Outcome domain.SimulatorOutcome `json:"outcome"`
	DelayMS int                     `json:"delay_ms"`
}

type SimulatorResponse struct {
	Outcome   domain.SimulatorOutcome `json:"outcome"`
	DelayMS   int                     `json:"delay_ms"`
	Version   int64                   `json:"version"`
	UpdatedAt time.Time               `json:"updated_at"`
}
