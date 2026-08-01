package provideradapter

import (
	"context"
	"fmt"

	"github.com/qiyue2015/device-platform/internal/domain"
)

// DispatchRequest is the provider-neutral input produced from a frozen
// Command, Device binding, and persisted Attempt request key.
type DispatchRequest struct {
	ProjectID          string
	DeviceID           string
	ProviderDeviceID   string
	ProviderProfile    string
	Action             domain.ActionIdentifier
	Payload            map[string]any
	ProviderRequestKey string
	// AttemptRequestSummary is the allowlisted snapshot persisted when the
	// Attempt was claimed. Adapters may use it to preserve claim-time config.
	AttemptRequestSummary map[string]any
}

type PrepareFailure string

const (
	PrepareActionUnsupported  PrepareFailure = "provider_action_unsupported"
	PrepareMappingUnknown     PrepareFailure = "provider_mapping_unknown"
	PrepareSessionUnavailable PrepareFailure = "provider_session_unavailable"
	PrepareRequestInvalid     PrepareFailure = "provider_request_invalid"
)

// PrepareError classifies failures that are proven to happen before any
// Provider I/O. The Command worker persists this stable reason without
// promoting it to transport or Device evidence.
type PrepareError struct {
	Failure PrepareFailure
	Detail  string
}

func (e *PrepareError) Error() string {
	if e == nil || e.Detail == "" {
		return "Provider dispatch preparation failed"
	}
	return e.Detail
}

func NewPrepareError(failure PrepareFailure, detail string) error {
	switch failure {
	case PrepareActionUnsupported, PrepareMappingUnknown, PrepareSessionUnavailable, PrepareRequestInvalid:
		return &PrepareError{Failure: failure, Detail: detail}
	default:
		panic(fmt.Sprintf("invalid Provider prepare failure %q", failure))
	}
}

// DispatchResult records only evidence that the adapter can support. It never
// implies Device ACK or final execution unless a provider contract explicitly
// supplies that evidence.
type DispatchResult struct {
	Outcome           domain.AttemptOutcome
	ConfirmationLevel domain.ConfirmationLevel
	EvidenceStatus    domain.EvidenceStatus
	HTTPStatus        int
	RequestSummary    map[string]any
	ResponseSummary   map[string]any
	ReasonCode        string
	ErrorDetail       string
}

type PreparedDispatch interface {
	RequestSummary() map[string]any
	Dispatch(context.Context) DispatchResult
}

type Adapter interface {
	Configured() bool
	Prepare(DispatchRequest) (PreparedDispatch, error)
}
