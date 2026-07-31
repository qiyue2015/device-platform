package simulator

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/qiyue2015/device-platform/internal/domain"
	"github.com/qiyue2015/device-platform/internal/provideradapter"
	"github.com/qiyue2015/device-platform/internal/storage/repository"
)

const (
	summaryOutcomeKey = "simulator_outcome"
	summaryDelayKey   = "simulator_delay_ms"
	summaryVersionKey = "simulator_config_version"
)

type Adapter struct{}

var _ provideradapter.Adapter = Adapter{}

type preparedDispatch struct {
	outcome domain.SimulatorOutcome
	delay   time.Duration
	summary map[string]any
}

func NewAdapter() Adapter {
	return Adapter{}
}

func (Adapter) Configured() bool {
	return true
}

// ClaimSnapshot runs inside the same transaction that creates a claimed
// Attempt. Reclaimed Attempts retain their already-persisted request summary.
func ClaimSnapshot(ctx context.Context, tx repository.CommandTx) (map[string]any, error) {
	config, err := tx.Simulator().GetForUpdate(ctx)
	if err != nil {
		return nil, fmt.Errorf("load Simulator claim snapshot: %w", err)
	}
	return snapshotSummary(config), nil
}

func (Adapter) Prepare(request provideradapter.DispatchRequest) (provideradapter.PreparedDispatch, error) {
	if strings.TrimSpace(request.ProviderDeviceID) == "" || strings.TrimSpace(request.ProviderRequestKey) == "" ||
		len(request.ProviderRequestKey) > 128 || len(request.Payload) != 0 {
		return nil, fmt.Errorf("invalid Simulator dispatch request")
	}
	switch request.Action {
	case domain.ActionIdentifier("unlock"), domain.ActionIdentifier("lock"), domain.ActionIdentifier("query_status"):
	default:
		return nil, fmt.Errorf("unsupported Simulator action")
	}
	outcome, delay, version, err := parseSnapshot(request.AttemptRequestSummary)
	if err != nil {
		return nil, err
	}
	summary := map[string]any{
		"action": request.Action, "provider_device_id": request.ProviderDeviceID,
		"provider_request_key": request.ProviderRequestKey,
		summaryOutcomeKey:      outcome,
		summaryDelayKey:        delay.Milliseconds(),
		summaryVersionKey:      version,
	}
	return &preparedDispatch{outcome: outcome, delay: delay, summary: summary}, nil
}

func (p *preparedDispatch) RequestSummary() map[string]any {
	return cloneMap(p.summary)
}

func (p *preparedDispatch) Dispatch(ctx context.Context) provideradapter.DispatchResult {
	if p.delay > 0 {
		timer := time.NewTimer(p.delay)
		defer timer.Stop()
		select {
		case <-ctx.Done():
			return simulatorResult(
				domain.AttemptOutcomeIndeterminate,
				domain.ConfirmationTransportSent,
				domain.EvidenceVerified,
				p.outcome,
				true,
				true,
				"Simulator Provider request timeout",
			)
		case <-timer.C:
		}
	}

	switch p.outcome {
	case domain.SimulatorOutcomeProviderAccepted:
		return simulatorResult(domain.AttemptOutcomeProviderAccepted, domain.ConfirmationProviderAccepted, domain.EvidenceVerified, p.outcome, true, false, "")
	case domain.SimulatorOutcomeProviderRejected:
		return simulatorResult(domain.AttemptOutcomeProviderRejected, domain.ConfirmationTransportSent, domain.EvidenceVerified, p.outcome, true, false, "Simulator configured Provider rejection")
	case domain.SimulatorOutcomeTransportErrorBeforeSend:
		return simulatorResult(domain.AttemptOutcomeTransportErrorBeforeSend, domain.ConfirmationNone, domain.EvidenceNone, p.outcome, false, false, "Simulator configured transport error before send")
	case domain.SimulatorOutcomeTransportErrorAfterSend:
		return simulatorResult(domain.AttemptOutcomeIndeterminate, domain.ConfirmationTransportSent, domain.EvidenceVerified, p.outcome, true, false, "Simulator configured transport error after send")
	case domain.SimulatorOutcomeInvalidResponse:
		return simulatorResult(domain.AttemptOutcomeIndeterminate, domain.ConfirmationTransportSent, domain.EvidenceVerified, p.outcome, true, false, "Simulator configured invalid response")
	default:
		return simulatorResult(domain.AttemptOutcomeInvalidRequest, domain.ConfirmationNone, domain.EvidenceNone, p.outcome, false, false, "Simulator snapshot outcome is invalid")
	}
}

func snapshotSummary(config domain.SimulatorConfig) map[string]any {
	return map[string]any{
		summaryOutcomeKey: config.Outcome,
		summaryDelayKey:   config.Delay.Milliseconds(),
		summaryVersionKey: config.Version,
	}
}

func parseSnapshot(summary map[string]any) (domain.SimulatorOutcome, time.Duration, int64, error) {
	outcomeText, ok := summary[summaryOutcomeKey].(string)
	if !ok {
		if typed, typedOK := summary[summaryOutcomeKey].(domain.SimulatorOutcome); typedOK {
			outcomeText, ok = string(typed), true
		}
	}
	delayMilliseconds, delayOK := integer(summary[summaryDelayKey])
	version, versionOK := integer(summary[summaryVersionKey])
	outcome := domain.SimulatorOutcome(outcomeText)
	if !ok || !validOutcome(outcome) || !delayOK || delayMilliseconds < 0 || delayMilliseconds > 60000 || !versionOK || version < 1 {
		return "", 0, 0, fmt.Errorf("invalid Simulator claim snapshot")
	}
	return outcome, time.Duration(delayMilliseconds) * time.Millisecond, version, nil
}

func integer(value any) (int64, bool) {
	switch typed := value.(type) {
	case int:
		return int64(typed), true
	case int64:
		return typed, true
	case float64:
		if typed != float64(int64(typed)) {
			return 0, false
		}
		return int64(typed), true
	case string:
		parsed, err := strconv.ParseInt(typed, 10, 64)
		return parsed, err == nil
	default:
		return 0, false
	}
}

func validOutcome(outcome domain.SimulatorOutcome) bool {
	switch outcome {
	case domain.SimulatorOutcomeProviderAccepted,
		domain.SimulatorOutcomeProviderRejected,
		domain.SimulatorOutcomeTransportErrorBeforeSend,
		domain.SimulatorOutcomeTransportErrorAfterSend,
		domain.SimulatorOutcomeInvalidResponse:
		return true
	default:
		return false
	}
}

func simulatorResult(
	outcome domain.AttemptOutcome,
	confirmation domain.ConfirmationLevel,
	evidence domain.EvidenceStatus,
	configuredOutcome domain.SimulatorOutcome,
	simulatedWrite bool,
	timedOut bool,
	detail string,
) provideradapter.DispatchResult {
	reasonCode := ""
	if outcome == domain.AttemptOutcomeIndeterminate {
		reasonCode = "provider_delivery_unknown"
		if !timedOut && configuredOutcome == domain.SimulatorOutcomeInvalidResponse {
			reasonCode = "provider_response_invalid"
		}
	}
	return provideradapter.DispatchResult{
		Outcome: outcome, ConfirmationLevel: confirmation, EvidenceStatus: evidence,
		ReasonCode: reasonCode,
		ResponseSummary: map[string]any{
			"configured_outcome": configuredOutcome,
			"simulated_write":    simulatedWrite,
			"provider_timeout":   timedOut,
		},
		ErrorDetail: detail,
	}
}

func cloneMap(source map[string]any) map[string]any {
	result := make(map[string]any, len(source))
	for key, value := range source {
		result[key] = value
	}
	return result
}
