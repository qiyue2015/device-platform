// Package repository defines the persistence boundary for the canonical domain.
// Mutating services use Store.WithinTransaction so state, evidence, Event,
// Delivery, and Audit records share one database commit.
package repository

import (
	"context"
	"errors"
	"time"

	"github.com/qiyue2015/device-platform/internal/domain"
)

var (
	ErrInvalidRepositoryRequest   = errors.New("invalid repository request")
	ErrProviderRequestKeyConflict = errors.New("provider request key conflict")
	ErrWebhookEventNotFound       = errors.New("webhook event not found")
	ErrWebhookDeliveryNotFound    = errors.New("webhook delivery not found")
	ErrWebhookDeliveryNotDead     = errors.New("webhook delivery not dead")
	ErrWebhookNotConfigured       = errors.New("webhook not configured")
)

type Store interface {
	Queries
	WithinTransaction(ctx context.Context, fn func(Tx) error) error
}

type ProjectStore interface {
	Projects() ProjectQueries
	TransactProject(ctx context.Context, fn func(ProjectTx) error) error
}

type ProjectTx interface {
	Projects() ProjectRepository
	Audits() AuditRepository
}

type DeviceStore interface {
	Projects() ProjectQueries
	DeviceTypes() DeviceTypeQueries
	Devices() DeviceQueries
	TransactDevice(ctx context.Context, fn func(DeviceTx) error) error
}

type CommandStore interface {
	Commands() CommandQueries
	Events() EventQueries
	TransactCommand(ctx context.Context, fn func(CommandTx) error) error
}

type DeviceTx interface {
	Projects() ProjectRepository
	DeviceTypes() DeviceTypeQueries
	Devices() DeviceRepository
	Events() EventRepository
	Webhooks() WebhookRepository
	Audits() AuditRepository
}

type CommandTx interface {
	Projects() ProjectRepository
	DeviceTypes() DeviceTypeQueries
	Devices() DeviceRepository
	Commands() CommandRepository
	Events() EventRepository
	Webhooks() WebhookRepository
	Audits() AuditRepository
}

type Tx interface {
	Users() UserRepository
	AuthRateLimits() AuthRateLimitRepository
	Projects() ProjectRepository
	DeviceTypes() DeviceTypeQueries
	Devices() DeviceRepository
	Commands() CommandRepository
	Messages() RawMessageRepository
	Events() EventRepository
	Webhooks() WebhookRepository
	Audits() AuditRepository
	Simulator() SimulatorRepository
}

type Queries interface {
	Users() UserQueries
	AuthRateLimits() AuthRateLimitQueries
	Projects() ProjectQueries
	DeviceTypes() DeviceTypeQueries
	Devices() DeviceQueries
	Commands() CommandQueries
	Events() EventQueries
	Webhooks() WebhookQueries
	Audits() AuditQueries
	Simulator() SimulatorQueries
}

type UserQueries interface {
	Get(ctx context.Context, id string) (domain.User, error)
	GetByEmail(ctx context.Context, normalizedEmail string) (domain.User, error)
}

type UserRepository interface {
	UserQueries
	Create(ctx context.Context, user domain.User) error
	IncrementSessionGeneration(ctx context.Context, id string, expected int64) (next int64, updated bool, err error)
}

type AuthRateLimitQueries interface {
	CountActiveEmailIP(ctx context.Context, emailIPDigest []byte, now time.Time) (int, error)
	CountActiveIP(ctx context.Context, ipDigest []byte, now time.Time) (int, error)
}

type AuthRateLimitRepository interface {
	AuthRateLimitQueries
	RecordFailure(ctx context.Context, emailIPDigest, ipDigest []byte, occurredAt, expiresAt time.Time) error
	ClearEmailIP(ctx context.Context, emailIPDigest []byte) error
	DeleteExpired(ctx context.Context, now time.Time, limit int) (int64, error)
}

type ProjectQueries interface {
	Get(ctx context.Context, id string) (domain.Project, error)
	GetByAPIKeyDigest(ctx context.Context, digest []byte) (domain.Project, error)
	GetWebhookSecretVersion(ctx context.Context, projectID string, version int) (domain.WebhookSecretVersion, error)
	List(ctx context.Context, request ListProjectsRequest) ([]domain.Project, int64, error)
}

type ListProjectsRequest struct {
	Name   *string
	Limit  int
	Offset int
}

type ProjectRepository interface {
	ProjectQueries
	Create(ctx context.Context, project domain.Project) error
	GetForUpdate(ctx context.Context, id string) (domain.Project, error)
	Rename(ctx context.Context, id, name string) error
	ReplaceIPWhitelist(ctx context.Context, id string, whitelist []string) error
	SetWebhookConfiguration(ctx context.Context, id string, webhookURL *string, configVersion int64, secretVersion *int) error
	ReplaceAPIKeyDigest(ctx context.Context, id string, digest []byte) error
	CreateWebhookSecretVersion(ctx context.Context, secret domain.WebhookSecretVersion) error
	RetireWebhookSecretVersion(ctx context.Context, projectID string, version int) error
}

type DeviceTypeQueries interface {
	GetByCode(ctx context.Context, code string) (domain.DeviceType, error)
	GetProfile(ctx context.Context, deviceTypeID string, revision int) (domain.DeviceTypeProfile, error)
	List(ctx context.Context) ([]domain.DeviceType, error)
}

type DeviceQueries interface {
	Get(ctx context.Context, id string) (domain.Device, error)
	GetByProject(ctx context.Context, projectID, id string) (domain.Device, error)
	GetByProviderIdentity(ctx context.Context, providerCode, providerDeviceID string) (domain.Device, error)
	ListByProject(ctx context.Context, projectID string) ([]domain.Device, error)
	List(ctx context.Context, request ListDevicesRequest) ([]domain.Device, int64, error)
	GetCurrentState(ctx context.Context, deviceID string) (domain.DeviceState, error)
}

type ListDevicesRequest struct {
	ProjectID        *string
	DeviceTypeCode   *string
	ProviderCode     *string
	ConnectionStatus *domain.ConnectionStatus
	LifecycleStatus  *domain.LifecycleStatus
	Limit            int
	Offset           int
}

type DeviceRepository interface {
	DeviceQueries
	Create(ctx context.Context, device domain.Device) error
	GetForUpdate(ctx context.Context, id string) (domain.Device, error)
	Rename(ctx context.Context, id, name string) error
	SetLifecycleStatus(ctx context.Context, id string, from, to domain.LifecycleStatus) (bool, error)
	SetConnectionStatus(ctx context.Context, id string, from, to domain.ConnectionStatus) (bool, error)
	SaveState(ctx context.Context, state domain.DeviceState) error
}

type ClaimCommandRequest struct {
	WorkerID       string
	LeaseToken     string
	LeaseDuration  time.Duration
	ProviderCode   string
	Adapter        domain.Adapter
	RequestKey     string
	RequestSummary map[string]any
}

type ReclaimAttemptRequest struct {
	WorkerID      string
	LeaseToken    string
	LeaseDuration time.Duration
}

type CompleteCommandAttemptRequest struct {
	Outcome           domain.AttemptOutcome
	ConfirmationLevel domain.ConfirmationLevel
	EvidenceStatus    domain.EvidenceStatus
	ResponseSummary   map[string]any
	ErrorCode         *string
	ErrorDetail       *string
}

type MarkDispatchingRequest struct {
	ResultObservationTimeout time.Duration
	DispatchLeaseDuration    time.Duration
	RequestSummary           map[string]any
}

type CommandStatusTransition struct {
	From              domain.CommandStatus
	To                domain.CommandStatus
	ReasonCode        *string
	ReasonDetail      *string
	ConfirmationLevel domain.ConfirmationLevel
	EvidenceStatus    domain.EvidenceStatus
}

type VerifiedEvidenceUpdateRequest struct {
	AttemptID                  string
	RawMessageID               string
	RawMessageDeduplicationKey string
	AttemptOutcome             domain.AttemptOutcome
	ResponseSummary            map[string]any
	ExpectedStatus             domain.CommandStatus
}

type CommandQueries interface {
	Get(ctx context.Context, id string) (domain.Command, error)
	GetByIdempotencyKey(ctx context.Context, projectID, idempotencyKey string) (domain.Command, error)
	List(ctx context.Context, request ListCommandsRequest) ([]domain.Command, int64, error)
	ListAttempts(ctx context.Context, commandID string) ([]domain.CommandAttempt, error)
}

type ListCommandsRequest struct {
	ProjectID   *string
	DeviceID    *string
	CommandType *domain.ActionIdentifier
	Status      *domain.CommandStatus
	Limit       int
	Offset      int
}

type CommandRepository interface {
	CommandQueries
	Create(ctx context.Context, command domain.Command) error
	GetForUpdate(ctx context.Context, id string) (domain.Command, error)

	// ClaimNext atomically locks one eligible queued Command and creates its
	// claimed Attempt. The token fences every later write by that worker.
	ClaimNext(ctx context.Context, request ClaimCommandRequest) (domain.Command, domain.CommandAttempt, bool, error)
	ReclaimAttempt(ctx context.Context, attemptID, expiredToken string, request ReclaimAttemptRequest) (domain.CommandAttempt, bool, error)
	MarkDispatching(ctx context.Context, commandID, attemptID, leaseToken string, request MarkDispatchingRequest) (bool, error)
	CompleteAttempt(ctx context.Context, commandID, attemptID, expectedLeaseToken string, request CompleteCommandAttemptRequest) (bool, error)
	RecoverExpiredDispatching(ctx context.Context, commandID, attemptID, expiredLeaseToken string) (bool, error)
	RecoverNextExpiredDispatching(ctx context.Context) (domain.Command, domain.CommandAttempt, bool, error)

	// CancelQueued and ExpireQueued atomically refuse a queued Command while an
	// unexpired claimed Attempt owns it. Both operations complete an expired
	// claimed Attempt as not_dispatched in the same transaction before changing
	// the Command status.
	CancelQueued(ctx context.Context, commandID string, reasonDetail *string) (bool, error)
	ExpireQueued(ctx context.Context, commandID string) (bool, error)
	ExpireResultObservation(ctx context.Context, commandID string) (bool, error)
	ExpireNextQueued(ctx context.Context) (domain.Command, bool, error)
	ExpireNextResultObservation(ctx context.Context) (domain.Command, domain.CommandStatus, bool, error)
	UpdateEvidenceFromAttempt(ctx context.Context, commandID, attemptID, expectedLeaseToken string, expectedStatus domain.CommandStatus) (bool, error)
	TransitionFromAttempt(ctx context.Context, commandID, attemptID, expectedLeaseToken string, transition CommandStatusTransition) (bool, error)
	UpdateProviderAcceptanceFromVerifiedMessage(ctx context.Context, commandID string, request VerifiedEvidenceUpdateRequest) (bool, error)
}

type RawMessageRepository interface {
	GetByDeduplicationKey(ctx context.Context, providerCode, deduplicationKey string) (domain.RawMessage, error)
	Create(ctx context.Context, message domain.RawMessage) error
}

type EventQueries interface {
	Get(ctx context.Context, id string) (domain.Event, error)
	GetByDeduplicationKey(ctx context.Context, projectID, deduplicationKey string) (domain.Event, error)
	ListByCommand(ctx context.Context, commandID string) ([]domain.Event, error)
}

type EventRepository interface {
	EventQueries
	Create(ctx context.Context, event domain.Event) error
}

type ClaimWebhookRequest struct {
	WorkerID      string
	LeaseToken    string
	LeaseDuration time.Duration
	MaxAttempts   int
}

type RecoverExpiredWebhookRequest struct {
	ErrorCode     string
	ErrorDetail   string
	RetrySchedule []time.Duration
	MaxAttempts   int
}

type CompleteWebhookAttemptRequest struct {
	AttemptID       string
	HTTPStatus      *int
	ResponseSummary *string
	ErrorCode       *string
	ErrorDetail     *string
	RetryDelay      time.Duration
	MaxAttempts     int
}

type CreateWebhookDeliveryRequest struct {
	ID      string
	EventID string
	RawBody []byte
}

type CreateWebhookReplayRequest struct {
	ID                 string
	ReplayOfDeliveryID string
}

type WebhookQueries interface {
	GetDelivery(ctx context.Context, id string) (domain.WebhookDelivery, error)
	ListAttempts(ctx context.Context, deliveryID string) ([]domain.WebhookDeliveryAttempt, error)
}

type WebhookRepository interface {
	WebhookQueries
	CreateDelivery(ctx context.Context, request CreateWebhookDeliveryRequest) (domain.WebhookDelivery, bool, error)
	CreateReplay(ctx context.Context, request CreateWebhookReplayRequest) (domain.WebhookDelivery, error)
	ClaimDue(ctx context.Context, request ClaimWebhookRequest) (domain.WebhookDelivery, domain.WebhookDeliveryAttempt, bool, error)
	ExhaustRetryBudget(ctx context.Context, maxAttempts int) (domain.WebhookDelivery, bool, error)
	CompleteAttempt(ctx context.Context, deliveryID, leaseToken string, request CompleteWebhookAttemptRequest) (bool, error)
	RecoverNextExpiredSending(ctx context.Context, request RecoverExpiredWebhookRequest) (domain.WebhookDelivery, bool, error)
}

type AuditQueries interface {
	Get(ctx context.Context, id string) (domain.AuditLog, error)
}

type AuditRepository interface {
	AuditQueries
	Create(ctx context.Context, log domain.AuditLog) error
}

type SimulatorQueries interface {
	Get(ctx context.Context) (domain.SimulatorConfig, error)
}

type UpdateSimulatorRequest struct {
	Outcome domain.SimulatorOutcome
	Delay   time.Duration
}

type SimulatorRepository interface {
	SimulatorQueries
	Update(ctx context.Context, expectedVersion int64, request UpdateSimulatorRequest) (bool, error)
}
