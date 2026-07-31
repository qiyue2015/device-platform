package webhookaudit

import (
	"errors"
	"io"
	"time"

	"github.com/qiyue2015/device-platform/internal/domain"
)

var (
	ErrInvalidRequest         = errors.New("invalid request")
	ErrResourceNotFound       = errors.New("resource not found")
	ErrWebhookDeliveryNotDead = errors.New("webhook delivery not dead")
	ErrWebhookNotConfigured   = errors.New("webhook not configured")
	ErrIdentifierGeneration   = errors.New("identifier generation failed")
)

type PersistentConfig struct {
	Random io.Reader
	Clock  Clock
}

type Clock interface {
	Now() time.Time
}

type EventListRequest struct {
	ProjectID *string
	DeviceID  *string
	CommandID *string
	EventType *string
	Page      int
	PageSize  int
}

type DeliveryListRequest struct {
	ProjectID *string
	EventID   *string
	Status    *string
	Page      int
	PageSize  int
}

type AuditListRequest struct {
	ProjectID    *string
	ActorType    *string
	Action       *string
	Result       *string
	ResourceType *string
	ResourceID   *string
	Page         int
	PageSize     int
}

type ReplayRequest struct {
	ActorID   string
	IPAddress string
	RequestID string
}

type ListResult[T any] struct {
	Items    []T
	Page     int
	PageSize int
	Total    int64
}

type PersistentEvent struct {
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

type PersistentDelivery struct {
	ID                   string                       `json:"id"`
	EventID              string                       `json:"event_id"`
	ProjectID            string                       `json:"project_id"`
	TargetURL            string                       `json:"target_url"`
	WebhookConfigVersion int64                        `json:"webhook_config_version"`
	Status               domain.WebhookDeliveryStatus `json:"status"`
	AttemptCount         int                          `json:"attempt_count"`
	NextAttemptAt        *time.Time                   `json:"next_attempt_at"`
	ReplayOfDeliveryID   *string                      `json:"replay_of_delivery_id"`
	DeliveredAt          *time.Time                   `json:"delivered_at"`
	CreatedAt            time.Time                    `json:"created_at"`
	UpdatedAt            time.Time                    `json:"updated_at"`
	Attempts             *[]PersistentDeliveryAttempt `json:"attempts,omitempty"`
}

type PersistentDeliveryAttempt struct {
	AttemptNo       int        `json:"attempt_no"`
	StartedAt       time.Time  `json:"started_at"`
	CompletedAt     *time.Time `json:"completed_at"`
	HTTPStatus      *int       `json:"http_status"`
	ResponseSummary *string    `json:"response_summary"`
	ErrorCode       *string    `json:"error_code"`
	ErrorDetail     *string    `json:"error_detail"`
}

type PersistentAudit struct {
	ID           string             `json:"id"`
	ActorType    domain.ActorType   `json:"actor_type"`
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
