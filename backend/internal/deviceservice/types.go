package deviceservice

import (
	"errors"
	"io"
	"time"

	"github.com/qiyue2015/device-platform/internal/access"
	"github.com/qiyue2015/device-platform/internal/domain"
)

var (
	ErrInvalidRequest          = errors.New("invalid Device request")
	ErrProjectNotFound         = errors.New("project not found")
	ErrDeviceTypeNotFound      = errors.New("device type not found")
	ErrProviderNotFound        = errors.New("provider not found")
	ErrProviderProfileNotFound = errors.New("provider profile not found")
	ErrDeviceNotFound          = errors.New("device not found")
	ErrProviderDeviceConflict  = errors.New("provider identity already exists")
	ErrDeviceImmutable         = errors.New("deleted device is immutable")
	ErrLifecycleTransition     = errors.New("device lifecycle transition is not allowed")
	ErrIdentifierGeneration    = errors.New("device identifier generation failed")
)

type Clock interface {
	Now() time.Time
}

type Config struct {
	Providers []ProviderRegistration
	Random    io.Reader
	Clock     Clock
}

type DeviceIdentityPolicy interface {
	NormalizeDeviceIdentity(requested *string, platformDeviceID string) (string, error)
}

type DeviceIdentityPolicyFunc func(requested *string, platformDeviceID string) (string, error)

func (policy DeviceIdentityPolicyFunc) NormalizeDeviceIdentity(requested *string, platformDeviceID string) (string, error) {
	return policy(requested, platformDeviceID)
}

type Scope = access.Scope

const (
	ScopeUser    = access.ScopeUser
	ScopeProject = access.ScopeProject
)

func HumanScope(userID string, isSuperAdmin bool) Scope {
	if isSuperAdmin {
		return access.SuperAdmin(userID)
	}
	return access.User(userID)
}

func ProjectScope(projectID string) Scope { return access.Project(projectID) }

type RequestMetadata struct {
	ActorType   domain.ActorType
	ActorUserID string
	ActorID     string
	IPAddress   string
	RequestID   string
}

type CapabilityAction struct {
	Identifier                    domain.ActionIdentifier
	PayloadSchema                 map[string]any
	RiskLevel                     string
	DeliveryPolicy                domain.DeliveryPolicy
	DispatchDeadlineMS            int64
	ProviderRequestTimeoutMS      int64
	ResultObservationTimeoutMS    int64
	RetryAllowed                  bool
	DeliveryPolicyOverrideAllowed bool
}

type DeviceType struct {
	Code     string
	Revision int
	Name     string
	Actions  []CapabilityAction
}

type Provider struct {
	Code              string
	Name              string
	AccessType        domain.AccessType
	TransportProtocol domain.TransportProtocol
	Adapter           domain.Adapter
	Profiles          []string
	ProfileActions    map[string]map[domain.ActionIdentifier]domain.ProviderActionAvailability
	IntegrationStatus domain.ProviderIntegrationStatus
}

type ProviderRegistration struct {
	Provider       Provider
	IdentityPolicy DeviceIdentityPolicy
}

type DeviceState struct {
	State          map[string]any
	EvidenceStatus domain.EvidenceStatus
	ReportedAt     *time.Time
	ObservedAt     time.Time
}

type Device struct {
	ID                string
	ProjectID         string
	DeviceTypeCode    string
	Name              string
	ProviderCode      string
	ProviderProfile   string
	ProviderDeviceID  string
	AccessType        domain.AccessType
	TransportProtocol domain.TransportProtocol
	Adapter           domain.Adapter
	ConnectionStatus  domain.ConnectionStatus
	LifecycleStatus   domain.LifecycleStatus
	CurrentState      *DeviceState
	LastSeenAt        *time.Time
	CreatedAt         time.Time
	UpdatedAt         time.Time
}

type CreateRequest struct {
	ProjectID        string
	Name             string
	DeviceTypeCode   string
	ProviderCode     string
	ProviderProfile  string
	ProviderDeviceID *string
}

type UpdateRequest struct {
	Name            *string
	LifecycleStatus *domain.LifecycleStatus
}

type ListRequest struct {
	ProjectID        *string
	DeviceTypeCode   *string
	ProviderCode     *string
	ConnectionStatus *domain.ConnectionStatus
	LifecycleStatus  *domain.LifecycleStatus
	Page             int
	PageSize         int
}

type ListResult struct {
	Items    []Device
	Page     int
	PageSize int
	Total    int64
}
