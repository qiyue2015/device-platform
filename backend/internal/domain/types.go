package domain

import "time"

const (
	DeviceTypeSmartLock         = "smart-lock"
	DeviceTypeSmartLockRevision = 2
	ProviderCodeWWTIOT          = "wwtiot"
	ProviderCodeOmni            = "omni"
	ProviderCodeSimulator       = "simulator"
	ProviderProfileWWTIOTV2     = "wwtiot-cloud-api-v2"
	ProviderProfileOmniBikeV207 = "omni-bike-tcp-v2.0.7"
	ProviderProfileOmniIoTV135  = "omni-iot-tcp-v1.3.5"
	ProviderProfileSimulatorV1  = "simulator-v1"
	ProviderProfileUnresolved   = "unresolved"
	EventSchemaVersion          = 1
)

type AccessType string

const (
	AccessTypeCloudAPI     AccessType = "cloud_api"
	AccessTypeDirectDevice AccessType = "direct_device"
	AccessTypeSimulator    AccessType = "simulator"
)

type TransportProtocol string

const (
	TransportProtocolHTTP     TransportProtocol = "http"
	TransportProtocolTCP      TransportProtocol = "tcp"
	TransportProtocolInternal TransportProtocol = "internal"
)

type Adapter string

const (
	AdapterWWTIOTCloudAPI Adapter = "wwtiot_cloud_api"
	AdapterOmniDirectTCP  Adapter = "omni_direct_tcp"
	AdapterSimulator      Adapter = "simulator"
)

type ProviderActionAvailability string

const (
	ProviderActionSupported      ProviderActionAvailability = "supported"
	ProviderActionUnsupported    ProviderActionAvailability = "unsupported"
	ProviderActionMappingUnknown ProviderActionAvailability = "mapping_unknown"
)

type ProviderIntegrationStatus string

const (
	ProviderIntegrationUnconfigured         ProviderIntegrationStatus = "unconfigured"
	ProviderIntegrationConfiguredUnverified ProviderIntegrationStatus = "configured_unverified"
	ProviderIntegrationVerified             ProviderIntegrationStatus = "verified"
)

type ConnectionStatus string

const (
	ConnectionStatusUnknown ConnectionStatus = "unknown"
	ConnectionStatusOnline  ConnectionStatus = "online"
	ConnectionStatusOffline ConnectionStatus = "offline"
)

type LifecycleStatus string

const (
	LifecycleStatusActive   LifecycleStatus = "active"
	LifecycleStatusDisabled LifecycleStatus = "disabled"
	LifecycleStatusDeleted  LifecycleStatus = "deleted"
)

// ActionIdentifier is carried by the command_type wire field. Its allowed
// values come from the Device Type profile, not from Platform Core.
type ActionIdentifier string
type CommandType = ActionIdentifier

type CommandStatus string

const (
	CommandStatusQueued    CommandStatus = "queued"
	CommandStatusSent      CommandStatus = "sent"
	CommandStatusAcked     CommandStatus = "acked"
	CommandStatusSuccess   CommandStatus = "success"
	CommandStatusFailed    CommandStatus = "failed"
	CommandStatusTimeout   CommandStatus = "timeout"
	CommandStatusCancelled CommandStatus = "cancelled"
)

type DeliveryPolicy string

const (
	DeliveryPolicyDispatchOnce DeliveryPolicy = "dispatch_once"
	DeliveryPolicyOnlineOnly   DeliveryPolicy = "online_only"
)

type AttemptPhase string

const (
	AttemptPhaseClaimed     AttemptPhase = "claimed"
	AttemptPhaseDispatching AttemptPhase = "dispatching"
	AttemptPhaseCompleted   AttemptPhase = "completed"
)

type AttemptOutcome string

const (
	AttemptOutcomeNotDispatched            AttemptOutcome = "not_dispatched"
	AttemptOutcomeInvalidRequest           AttemptOutcome = "invalid_request"
	AttemptOutcomeProviderAccepted         AttemptOutcome = "provider_accepted"
	AttemptOutcomeProviderRejected         AttemptOutcome = "provider_rejected"
	AttemptOutcomeTransportErrorBeforeSend AttemptOutcome = "transport_error_before_send"
	AttemptOutcomeIndeterminate            AttemptOutcome = "indeterminate"
)

type ResultOutcome string

const (
	ResultOutcomeDeviceAcked     ResultOutcome = "device_acked"
	ResultOutcomeDeviceSucceeded ResultOutcome = "device_succeeded"
	ResultOutcomeDeviceFailed    ResultOutcome = "device_failed"
)

type ConfirmationLevel string

const (
	ConfirmationNone             ConfirmationLevel = "none"
	ConfirmationTransportSent    ConfirmationLevel = "transport_sent"
	ConfirmationProviderAccepted ConfirmationLevel = "provider_accepted"
	ConfirmationDeviceAcked      ConfirmationLevel = "device_acked"
	ConfirmationDeviceFinal      ConfirmationLevel = "device_final"
)

type EvidenceStatus string

const (
	EvidenceNone       EvidenceStatus = "none"
	EvidenceVerified   EvidenceStatus = "verified"
	EvidenceUnverified EvidenceStatus = "unverified"
)

type EventSource string

const (
	EventSourceAdmin            EventSource = "admin"
	EventSourceOpenAPI          EventSource = "open_api"
	EventSourceProviderCallback EventSource = "provider_callback"
	EventSourceSimulator        EventSource = "simulator"
	EventSourceSystem           EventSource = "system"
)

type EventType string

const (
	EventTypeDeviceCreated           EventType = "device.created"
	EventTypeDeviceLifecycleChanged  EventType = "device.lifecycle_changed"
	EventTypeDeviceConnectionChanged EventType = "device.connection_changed"
	EventTypeDeviceStateUpdated      EventType = "device.state_updated"
	EventTypeCommandCreated          EventType = "command.created"
	EventTypeCommandStatusChanged    EventType = "command.status_changed"
	EventTypeCommandEvidenceUpdated  EventType = "command.evidence_updated"
	EventTypeCommandResultRecorded   EventType = "command.result_recorded"
)

type RawMessageDirection string

const (
	RawMessageInbound  RawMessageDirection = "inbound"
	RawMessageOutbound RawMessageDirection = "outbound"
)

type WebhookDeliveryStatus string

const (
	WebhookDeliveryStatusPending   WebhookDeliveryStatus = "pending"
	WebhookDeliveryStatusSending   WebhookDeliveryStatus = "sending"
	WebhookDeliveryStatusDelivered WebhookDeliveryStatus = "delivered"
	WebhookDeliveryStatusFailed    WebhookDeliveryStatus = "failed"
	WebhookDeliveryStatusDead      WebhookDeliveryStatus = "dead"
)

type SimulatorOutcome string

const (
	SimulatorOutcomeProviderAccepted         SimulatorOutcome = "provider_accepted"
	SimulatorOutcomeProviderRejected         SimulatorOutcome = "provider_rejected"
	SimulatorOutcomeTransportErrorBeforeSend SimulatorOutcome = "transport_error_before_send"
	SimulatorOutcomeTransportErrorAfterSend  SimulatorOutcome = "transport_error_after_send"
	SimulatorOutcomeInvalidResponse          SimulatorOutcome = "invalid_response"
)

type ActorType string

const (
	ActorTypeAdmin    ActorType = "admin"
	ActorTypeProject  ActorType = "project"
	ActorTypeProvider ActorType = "provider"
	ActorTypeSystem   ActorType = "system"
)

type AuditResult string

const (
	AuditResultSuccess AuditResult = "success"
	AuditResultFailure AuditResult = "failure"
)

type User struct {
	ID                string
	Email             string
	PasswordHash      string
	DisplayName       string
	IsAdmin           bool
	SessionGeneration int64
	CreatedAt         time.Time
	UpdatedAt         time.Time
}

type Project struct {
	ID                          string
	Name                        string
	APIKeyDigest                []byte
	WebhookURL                  *string
	WebhookConfigVersion        int64
	CurrentWebhookSecretVersion *int
	IPWhitelist                 []string
	CreatedAt                   time.Time
	UpdatedAt                   time.Time
}

type WebhookSecretVersion struct {
	ProjectID            string
	Version              int
	Ciphertext           []byte
	Nonce                []byte
	EncryptionKeyVersion int
	CreatedAt            time.Time
	RetiredAt            *time.Time
}

type CapabilityAction struct {
	Identifier                    ActionIdentifier
	PayloadSchema                 map[string]any
	RiskLevel                     string
	DeliveryPolicy                DeliveryPolicy
	DispatchDeadline              time.Duration
	ProviderRequestTimeout        time.Duration
	ResultObservationTimeout      time.Duration
	RetryAllowed                  bool
	DeliveryPolicyOverrideAllowed bool
}

type DeviceType struct {
	ID              string
	Code            string
	CurrentRevision int
	Name            string
	CreatedAt       time.Time
	UpdatedAt       time.Time
}

type DeviceTypeProfile struct {
	DeviceTypeID string
	Revision     int
	Actions      []CapabilityAction
	ProfileHash  []byte
	CreatedAt    time.Time
}

type Provider struct {
	Code              string
	Name              string
	AccessType        AccessType
	TransportProtocol TransportProtocol
	Adapter           Adapter
	Profiles          []string
	ProfileActions    map[string]map[ActionIdentifier]ProviderActionAvailability
	IntegrationStatus ProviderIntegrationStatus
}

type Device struct {
	ID                string
	ProjectID         string
	DeviceTypeID      string
	DeviceTypeCode    string
	Name              string
	ProviderCode      string
	ProviderProfile   string
	ProviderDeviceID  string
	AccessType        AccessType
	TransportProtocol TransportProtocol
	Adapter           Adapter
	ConnectionStatus  ConnectionStatus
	LifecycleStatus   LifecycleStatus
	CreatedAt         time.Time
	UpdatedAt         time.Time
}

type DeviceState struct {
	ID             string
	DeviceID       string
	State          map[string]any
	EvidenceStatus EvidenceStatus
	ReportedAt     *time.Time
	ObservedAt     time.Time
	RawMessageID   string
	CreatedAt      time.Time
}

type Command struct {
	ID                 string
	ProjectID          string
	DeviceID           string
	DeviceTypeID       string
	DeviceTypeCode     string
	ProviderCode       string
	ProviderProfile    string
	ProviderDeviceID   string
	Adapter            Adapter
	CommandType        ActionIdentifier
	Payload            map[string]any
	DeviceTypeRevision int
	DeliveryPolicy     DeliveryPolicy
	DispatchDeadline   time.Duration
	ProviderTimeout    time.Duration
	ResultTimeout      time.Duration
	RetryAllowed       bool
	Status             CommandStatus
	ReasonCode         *string
	ReasonDetail       *string
	ConfirmationLevel  ConfirmationLevel
	EvidenceStatus     EvidenceStatus
	IdempotencyKey     string
	RequestHash        []byte
	QueuedAt           time.Time
	DispatchDeadlineAt time.Time
	SentAt             *time.Time
	ResultDeadlineAt   *time.Time
	FinishedAt         *time.Time
	CreatedAt          time.Time
	UpdatedAt          time.Time
}

type DeviceCommand = Command

type CommandAttempt struct {
	ID                 string
	CommandID          string
	AttemptNo          int
	Phase              AttemptPhase
	ProviderCode       string
	Adapter            Adapter
	ProviderRequestKey string
	Outcome            *AttemptOutcome
	ConfirmationLevel  ConfirmationLevel
	EvidenceStatus     EvidenceStatus
	RequestSummary     map[string]any
	ResponseSummary    map[string]any
	ReasonCode         *string
	ErrorDetail        *string
	LeaseToken         string
	LeaseOwner         string
	LeaseExpiresAt     time.Time
	ClaimedAt          time.Time
	DispatchingAt      *time.Time
	CompletedAt        *time.Time
}

type DeviceCommandAttempt = CommandAttempt

type CommandResult struct {
	ID                string
	CommandID         string
	AttemptID         *string
	Source            EventSource
	Outcome           ResultOutcome
	ConfirmationLevel ConfirmationLevel
	EvidenceStatus    EvidenceStatus
	DeduplicationKey  string
	ReportedAt        *time.Time
	ObservedAt        time.Time
	Late              bool
	Payload           map[string]any
	CreatedAt         time.Time
}

type RawMessage struct {
	ID                string
	DeviceID          *string
	ProviderCode      string
	ProviderProfile   string
	ProviderDeviceID  string
	AccessType        AccessType
	TransportProtocol TransportProtocol
	Adapter           Adapter
	Direction         RawMessageDirection
	EvidenceStatus    EvidenceStatus
	DeduplicationKey  string
	Headers           map[string]any
	Body              []byte
	ReceivedAt        time.Time
	CreatedAt         time.Time
}

type DeviceRawMessage = RawMessage

type Event struct {
	ID               string
	SchemaVersion    int
	EventType        EventType
	ProjectID        string
	DeviceID         *string
	CommandID        *string
	Source           EventSource
	Payload          map[string]any
	RawMessageID     *string
	DeduplicationKey string
	OccurredAt       time.Time
	CreatedAt        time.Time
}

type DeviceEvent = Event

type WebhookDelivery struct {
	ID                   string
	ProjectID            string
	EventID              string
	TargetURL            string
	WebhookConfigVersion int64
	WebhookSecretVersion int
	RawBody              []byte
	AttemptCount         int
	Status               WebhookDeliveryStatus
	LastErrorCode        *string
	LastErrorDetail      *string
	NextAttemptAt        *time.Time
	LeaseToken           *string
	LeaseOwner           *string
	LeaseExpiresAt       *time.Time
	ReplayOfDeliveryID   *string
	DeliveredAt          *time.Time
	CreatedAt            time.Time
	UpdatedAt            time.Time
}

type WebhookDeliveryAttempt struct {
	ID               string
	DeliveryID       string
	AttemptNo        int
	RequestTimestamp int64
	HTTPStatus       *int
	ResponseSummary  *string
	ErrorCode        *string
	ErrorDetail      *string
	StartedAt        time.Time
	CompletedAt      *time.Time
}

type AuditLog struct {
	ID           string
	ActorType    ActorType
	ActorID      *string
	ProjectID    *string
	Action       string
	Result       AuditResult
	ResourceType string
	ResourceID   *string
	IPAddress    *string
	RequestID    *string
	Metadata     map[string]any
	OccurredAt   time.Time
}

type AuthRateLimitScope string

const (
	AuthRateLimitEmailIP AuthRateLimitScope = "email_ip"
	AuthRateLimitIP      AuthRateLimitScope = "ip"
)

type AuthLoginFailureEvent struct {
	ID         string
	Scope      AuthRateLimitScope
	KeyDigest  []byte
	OccurredAt time.Time
	ExpiresAt  time.Time
}

type SimulatorConfig struct {
	Outcome   SimulatorOutcome
	Delay     time.Duration
	Version   int64
	UpdatedAt time.Time
}
