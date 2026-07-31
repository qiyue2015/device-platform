package provideradapter

import (
	"context"

	"github.com/qiyue2015/device-platform/internal/domain"
)

// DispatchRequest is the provider-neutral input produced from a frozen
// Command, Device binding, and persisted Attempt request key.
type DispatchRequest struct {
	ProviderDeviceID   string
	Action             domain.ActionIdentifier
	Payload            map[string]any
	ProviderRequestKey string
	// AttemptRequestSummary is the allowlisted snapshot persisted when the
	// Attempt was claimed. Adapters may use it to preserve claim-time config.
	AttemptRequestSummary map[string]any
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
