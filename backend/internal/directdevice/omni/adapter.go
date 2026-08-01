package omni

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/qiyue2015/device-platform/internal/domain"
	"github.com/qiyue2015/device-platform/internal/provideradapter"
)

type Clock interface {
	Now() time.Time
}

func NormalizeDeviceIdentity(requested *string, _ string) (string, error) {
	if requested == nil {
		return "", fmt.Errorf("provider_device_id is required")
	}
	value := strings.TrimSpace(*requested)
	if !validIMEI(value) {
		return "", fmt.Errorf("provider_device_id must be a 15-digit IMEI")
	}
	return value, nil
}

type realClock struct{}

func (realClock) Now() time.Time { return time.Now().UTC() }

type AdapterConfig struct {
	Configured bool
	Clock      Clock
	Available  func() bool
}

type Adapter struct {
	registry   *Registry
	configured bool
	available  func() bool
	clock      Clock
}

func NewAdapter(registry *Registry, config AdapterConfig) *Adapter {
	clock := config.Clock
	if clock == nil {
		clock = realClock{}
	}
	return &Adapter{registry: registry, configured: config.Configured, available: config.Available, clock: clock}
}

func (a *Adapter) Configured() bool {
	return a != nil && a.configured && a.registry != nil && (a.available == nil || a.available())
}

func (a *Adapter) Prepare(request provideradapter.DispatchRequest) (provideradapter.PreparedDispatch, error) {
	if a == nil || !a.Configured() {
		return nil, provideradapter.NewPrepareError(provideradapter.PrepareSessionUnavailable, "Omni session registry is unavailable")
	}
	requestedDeviceID := request.ProviderDeviceID
	if _, err := NormalizeDeviceIdentity(&requestedDeviceID, ""); !validProfile(request.ProviderProfile) || err != nil || strings.TrimSpace(request.ProviderRequestKey) == "" {
		return nil, provideradapter.NewPrepareError(provideradapter.PrepareRequestInvalid, "Omni dispatch request is invalid")
	}
	switch request.ProviderProfile {
	case domain.ProviderProfileOmniBikeV207:
		switch request.Action {
		case domain.ActionIdentifier("unlock"):
			return nil, provideradapter.NewPrepareError(provideradapter.PrepareMappingUnknown, "Omni bike unlock mapping is not frozen")
		case domain.ActionIdentifier("lock"):
			return nil, provideradapter.NewPrepareError(provideradapter.PrepareActionUnsupported, "Omni bike does not define active lock dispatch")
		case domain.ActionIdentifier("query_status"):
		default:
			return nil, provideradapter.NewPrepareError(provideradapter.PrepareActionUnsupported, "Omni action is unsupported")
		}
	case domain.ProviderProfileOmniIoTV135:
		switch request.Action {
		case domain.ActionIdentifier("unlock"), domain.ActionIdentifier("lock"):
			return nil, provideradapter.NewPrepareError(provideradapter.PrepareMappingUnknown, "Omni IoT physical action mapping is not frozen")
		case domain.ActionIdentifier("query_status"):
		default:
			return nil, provideradapter.NewPrepareError(provideradapter.PrepareActionUnsupported, "Omni action is unsupported")
		}
	}
	if len(request.Payload) != 0 {
		return nil, provideradapter.NewPrepareError(provideradapter.PrepareRequestInvalid, "Omni query_status payload must be empty")
	}
	session, err := a.registry.LookupUnique(request.ProviderProfile, request.ProviderDeviceID)
	if err != nil {
		return nil, provideradapter.NewPrepareError(provideradapter.PrepareSessionUnavailable, "Omni requires one unique current session")
	}
	if session.DeviceID() != request.DeviceID || session.ProjectID() != request.ProjectID {
		return nil, provideradapter.NewPrepareError(provideradapter.PrepareSessionUnavailable, "Omni session ownership does not match the frozen Command")
	}
	frame, err := EncodeQueryStatus(request.ProviderProfile, request.ProviderDeviceID, a.clock.Now())
	if err != nil {
		return nil, provideradapter.NewPrepareError(provideradapter.PrepareRequestInvalid, "Omni query_status frame cannot be encoded")
	}
	command := "S6"
	if request.ProviderProfile == domain.ProviderProfileOmniBikeV207 {
		command = "S5"
	}
	return &preparedDispatch{
		registry: a.registry, session: session, frame: frame,
		summary: map[string]any{
			"provider_profile":   request.ProviderProfile,
			"command":            command,
			"frame_bytes":        len(frame),
			"session_generation": session.Generation(),
		},
	}, nil
}

type preparedDispatch struct {
	registry *Registry
	session  *Session
	frame    []byte
	summary  map[string]any
}

func (p *preparedDispatch) RequestSummary() map[string]any {
	return cloneSummary(p.summary)
}

func (p *preparedDispatch) Dispatch(ctx context.Context) provideradapter.DispatchResult {
	write := p.registry.WriteUnique(ctx, p.session, p.frame)
	response := map[string]any{
		"bytes_written":      write.BytesWritten,
		"frame_bytes":        len(p.frame),
		"session_generation": p.session.Generation(),
	}
	if write.Complete {
		return provideradapter.DispatchResult{
			Outcome: domain.AttemptOutcomeIndeterminate, ConfirmationLevel: domain.ConfirmationTransportSent,
			EvidenceStatus: domain.EvidenceVerified, RequestSummary: p.RequestSummary(), ResponseSummary: response,
			ReasonCode:  "provider_delivery_unknown",
			ErrorDetail: "Omni frame was written completely; Device receipt and execution remain unknown",
		}
	}
	if write.BytesWritten == 0 {
		return provideradapter.DispatchResult{
			Outcome: domain.AttemptOutcomeTransportErrorBeforeSend, ConfirmationLevel: domain.ConfirmationNone,
			EvidenceStatus: domain.EvidenceNone, RequestSummary: p.RequestSummary(), ResponseSummary: response,
			ReasonCode: "provider_transport_error", ErrorDetail: "Omni write failed before sending any frame bytes",
		}
	}
	return provideradapter.DispatchResult{
		Outcome: domain.AttemptOutcomeIndeterminate, ConfirmationLevel: domain.ConfirmationTransportSent,
		EvidenceStatus: domain.EvidenceVerified, RequestSummary: p.RequestSummary(), ResponseSummary: response,
		ReasonCode:  "provider_delivery_unknown",
		ErrorDetail: fmt.Sprintf("Omni delivery is unknown after writing %d of %d frame bytes", write.BytesWritten, len(p.frame)),
	}
}

func cloneSummary(source map[string]any) map[string]any {
	result := make(map[string]any, len(source))
	for key, value := range source {
		result[key] = value
	}
	return result
}

var _ provideradapter.Adapter = (*Adapter)(nil)
