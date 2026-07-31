package commandservice

import (
	"errors"
	"io"
	"time"

	"github.com/qiyue2015/device-platform/internal/domain"
)

var (
	ErrInvalidRequest         = errors.New("invalid Command request")
	ErrProjectNotFound        = errors.New("Project not found")
	ErrDeviceNotFound         = errors.New("Device not found")
	ErrDeviceDisabled         = errors.New("Device cannot accept Commands")
	ErrDeviceNotOnline        = errors.New("Device is not online")
	ErrCapabilityUnsupported  = errors.New("Device capability is unsupported")
	ErrPayloadInvalid         = errors.New("Command payload is invalid")
	ErrProviderNotConfigured  = errors.New("Provider is not configured")
	ErrCommandNotFound        = errors.New("Command not found")
	ErrIdempotencyKeyConflict = errors.New("idempotency key conflicts with another request")
	ErrCommandNotCancellable  = errors.New("Command is not cancellable")
	ErrIdentifierGeneration   = errors.New("Command identifier generation failed")
)

type Clock interface {
	Now() time.Time
}

type Config struct {
	Providers []domain.Provider
	Random    io.Reader
	Clock     Clock
}

type ScopeKind string

const (
	ScopeAdmin   ScopeKind = "admin"
	ScopeProject ScopeKind = "project"
)

type Scope struct {
	Kind      ScopeKind
	ProjectID string
}

func AdminScope() Scope { return Scope{Kind: ScopeAdmin} }

func ProjectScope(projectID string) Scope {
	return Scope{Kind: ScopeProject, ProjectID: projectID}
}

type RequestMetadata struct {
	ActorType domain.ActorType
	ActorID   string
	IPAddress string
	RequestID string
}

type CreateRequest struct {
	ProjectID      string
	DeviceID       string
	CommandType    domain.ActionIdentifier
	Payload        map[string]any
	IdempotencyKey string
}

type CreateResult struct {
	Command          domain.Command
	IdempotentReplay bool
}

type ListRequest struct {
	ProjectID   *string
	DeviceID    *string
	CommandType *domain.ActionIdentifier
	Status      *domain.CommandStatus
	Page        int
	PageSize    int
}

type ListResult struct {
	Items    []domain.Command
	Page     int
	PageSize int
	Total    int64
}

type Detail struct {
	Command  domain.Command
	Attempts []domain.CommandAttempt
	Results  []domain.CommandResult
	Events   []domain.Event
}
