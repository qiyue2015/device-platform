package commandservice

import (
	"errors"
	"io"
	"time"

	"github.com/qiyue2015/device-platform/internal/domain"
)

var (
	ErrInvalidRequest            = errors.New("invalid command request")
	ErrProjectNotFound           = errors.New("project not found")
	ErrDeviceNotFound            = errors.New("device not found")
	ErrDeviceDisabled            = errors.New("device cannot accept commands")
	ErrDeviceNotOnline           = errors.New("device is not online")
	ErrCapabilityUnsupported     = errors.New("device capability is unsupported")
	ErrPayloadInvalid            = errors.New("command payload is invalid")
	ErrProviderNotConfigured     = errors.New("provider is not configured")
	ErrProviderActionUnsupported = errors.New("provider action is unsupported")
	ErrProviderMappingUnknown    = errors.New("provider action mapping is unknown")
	ErrCommandNotFound           = errors.New("command not found")
	ErrIdempotencyKeyConflict    = errors.New("idempotency key conflicts with another request")
	ErrCommandNotCancellable     = errors.New("command is not cancellable")
	ErrIdentifierGeneration      = errors.New("command identifier generation failed")
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
