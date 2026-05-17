package domain

import "time"

type AccessType string

const (
	AccessTypeMockGateway AccessType = "mock_gateway"
	AccessTypeCloudAPI    AccessType = "cloud_api"
)

type TransportProtocol string

const (
	TransportProtocolSimulator TransportProtocol = "simulator"
	TransportProtocolHTTP      TransportProtocol = "http"
)

type Adapter string

const (
	AdapterMockGateway    Adapter = "mock_gateway"
	AdapterWWTIOTCloudAPI Adapter = "wwtiot_cloud_api"
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

type CommandType string

const (
	CommandTypeUnlock      CommandType = "unlock"
	CommandTypeLock        CommandType = "lock"
	CommandTypeQueryStatus CommandType = "query_status"
	CommandTypeSetConfig   CommandType = "set_config"
	CommandTypeReboot      CommandType = "reboot"
)

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

type AttemptStatus string

const (
	AttemptStatusCreated AttemptStatus = "created"
	AttemptStatusSent    AttemptStatus = "sent"
	AttemptStatusAcked   AttemptStatus = "acked"
	AttemptStatusSuccess AttemptStatus = "success"
	AttemptStatusFailed  AttemptStatus = "failed"
	AttemptStatusTimeout AttemptStatus = "timeout"
)

type EventSource string

const (
	EventSourceMockGateway EventSource = "mock_gateway"
	EventSourceSystem      EventSource = "system"
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

type SimulatorMode string

const (
	SimulatorModeNormal         SimulatorMode = "normal"
	SimulatorModeDelay          SimulatorMode = "delay"
	SimulatorModeOffline        SimulatorMode = "offline"
	SimulatorModeTimeoutThenAck SimulatorMode = "timeout_then_ack"
	SimulatorModeDuplicateAck   SimulatorMode = "duplicate_ack"
	SimulatorModeFail           SimulatorMode = "fail"
)

type User struct {
	ID           string
	Email        string
	PasswordHash string
	DisplayName  string
	IsAdmin      bool
	CreatedAt    time.Time
	UpdatedAt    time.Time
}

type Project struct {
	ID            string
	Name          string
	APIKeyHash    string
	WebhookURL    string
	WebhookSecret string
	IPWhitelist   []string
	CreatedAt     time.Time
	UpdatedAt     time.Time
}

type DeviceType struct {
	ID                   string
	Code                 string
	Name                 string
	Capabilities         []string
	DefaultCommandPolicy map[CommandType]DeliveryPolicy
	CreatedAt            time.Time
	UpdatedAt            time.Time
}

type Device struct {
	ID                string
	ProjectID         string
	DeviceTypeID      string
	Name              string
	ProviderCode      string
	ProviderDeviceID  string
	AccessType        AccessType
	TransportProtocol TransportProtocol
	Adapter           Adapter
	ConnectionStatus  ConnectionStatus
	LifecycleStatus   LifecycleStatus
	Metadata          map[string]any
	CreatedAt         time.Time
	UpdatedAt         time.Time
}

type DeviceState struct {
	ID           string
	DeviceID     string
	State        map[string]any
	ReportedAt   time.Time
	ObservedAt   time.Time
	RawMessageID string
}

type DeviceCommand struct {
	ID             string
	ProjectID      string
	DeviceID       string
	CommandType    CommandType
	Payload        map[string]any
	Status         CommandStatus
	DeliveryPolicy DeliveryPolicy
	IdempotencyKey string
	RequestHash    string
	Reason         string
	ExpiresAt      *time.Time
	SentAt         *time.Time
	FinishedAt     *time.Time
	CreatedAt      time.Time
	UpdatedAt      time.Time
}

type DeviceCommandAttempt struct {
	ID           string
	CommandID    string
	AttemptNo    int
	Adapter      Adapter
	Status       AttemptStatus
	RequestBody  map[string]any
	ResponseBody map[string]any
	ErrorMessage string
	StartedAt    time.Time
	FinishedAt   *time.Time
}

type DeviceEvent struct {
	ID           string
	ProjectID    string
	DeviceID     string
	CommandID    string
	EventType    string
	Source       EventSource
	Payload      map[string]any
	RawMessageID string
	OccurredAt   time.Time
	CreatedAt    time.Time
}

type DeviceRawMessage struct {
	ID                string
	DeviceID          string
	ProviderCode      string
	ProviderDeviceID  string
	AccessType        AccessType
	TransportProtocol TransportProtocol
	Adapter           Adapter
	Direction         RawMessageDirection
	Headers           map[string]any
	Body              []byte
	ReceivedAt        time.Time
	CreatedAt         time.Time
}

type WebhookDelivery struct {
	ID            string
	ProjectID     string
	EventID       string
	TargetURL     string
	Payload       map[string]any
	Signature     string
	AttemptCount  int
	Status        WebhookDeliveryStatus
	LastError     string
	NextAttemptAt *time.Time
	DeliveredAt   *time.Time
	CreatedAt     time.Time
	UpdatedAt     time.Time
}

type AuditLog struct {
	ID           string
	ProjectID    string
	UserID       string
	ActorType    string
	Action       string
	ResourceType string
	ResourceID   string
	RequestID    string
	IPAddress    string
	UserAgent    string
	Metadata     map[string]any
	CreatedAt    time.Time
}
