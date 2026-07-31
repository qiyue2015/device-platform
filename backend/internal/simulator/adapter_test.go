package simulator

import (
	"context"
	"testing"
	"time"

	"github.com/qiyue2015/device-platform/internal/domain"
	"github.com/qiyue2015/device-platform/internal/provideradapter"
)

func TestAdapterResultMatrix(t *testing.T) {
	tests := []struct {
		outcome      domain.SimulatorOutcome
		wantOutcome  domain.AttemptOutcome
		confirmation domain.ConfirmationLevel
		evidence     domain.EvidenceStatus
		wrote        bool
	}{
		{domain.SimulatorOutcomeProviderAccepted, domain.AttemptOutcomeProviderAccepted, domain.ConfirmationProviderAccepted, domain.EvidenceVerified, true},
		{domain.SimulatorOutcomeProviderRejected, domain.AttemptOutcomeProviderRejected, domain.ConfirmationTransportSent, domain.EvidenceVerified, true},
		{domain.SimulatorOutcomeTransportErrorBeforeSend, domain.AttemptOutcomeTransportErrorBeforeSend, domain.ConfirmationNone, domain.EvidenceNone, false},
		{domain.SimulatorOutcomeTransportErrorAfterSend, domain.AttemptOutcomeIndeterminate, domain.ConfirmationTransportSent, domain.EvidenceVerified, true},
		{domain.SimulatorOutcomeInvalidResponse, domain.AttemptOutcomeIndeterminate, domain.ConfirmationTransportSent, domain.EvidenceVerified, true},
	}
	for _, test := range tests {
		t.Run(string(test.outcome), func(t *testing.T) {
			prepared, err := NewAdapter().Prepare(dispatchRequest(test.outcome, 0, 7))
			if err != nil {
				t.Fatal(err)
			}
			summary := prepared.RequestSummary()
			summary[summaryOutcomeKey] = "mutated"
			if prepared.RequestSummary()[summaryOutcomeKey] != test.outcome {
				t.Fatal("Prepared request summary is mutable through a returned map")
			}
			result := prepared.Dispatch(context.Background())
			if result.Outcome != test.wantOutcome || result.ConfirmationLevel != test.confirmation || result.EvidenceStatus != test.evidence {
				t.Fatalf("result=%+v", result)
			}
			if result.ResponseSummary["simulated_write"] != test.wrote || result.ResponseSummary["provider_timeout"] != false {
				t.Fatalf("response summary=%+v", result.ResponseSummary)
			}
		})
	}
}

func TestAdapterProviderTimeoutDuringDelayIsAfterSend(t *testing.T) {
	for _, configured := range []domain.SimulatorOutcome{
		domain.SimulatorOutcomeProviderAccepted,
		domain.SimulatorOutcomeTransportErrorBeforeSend,
	} {
		t.Run(string(configured), func(t *testing.T) {
			prepared, err := NewAdapter().Prepare(dispatchRequest(configured, 100, 3))
			if err != nil {
				t.Fatal(err)
			}
			ctx, cancel := context.WithTimeout(context.Background(), time.Millisecond)
			defer cancel()
			result := prepared.Dispatch(ctx)
			if result.Outcome != domain.AttemptOutcomeIndeterminate ||
				result.ConfirmationLevel != domain.ConfirmationTransportSent || result.EvidenceStatus != domain.EvidenceVerified ||
				result.ResponseSummary["simulated_write"] != true || result.ResponseSummary["provider_timeout"] != true {
				t.Fatalf("timeout result=%+v", result)
			}
		})
	}
}

func TestAdapterRejectsMissingOrInvalidClaimSnapshot(t *testing.T) {
	requests := []provideradapter.DispatchRequest{
		dispatchRequest(domain.SimulatorOutcome("future"), 0, 1),
		dispatchRequest(domain.SimulatorOutcomeProviderAccepted, 60001, 1),
		dispatchRequest(domain.SimulatorOutcomeProviderAccepted, 0, 0),
		{ProviderDeviceID: "device", Action: "unlock", Payload: map[string]any{}, ProviderRequestKey: "1"},
	}
	for _, request := range requests {
		if _, err := NewAdapter().Prepare(request); err == nil {
			t.Fatalf("invalid request accepted: %+v", request)
		}
	}
}

func dispatchRequest(outcome domain.SimulatorOutcome, delayMS, version int64) provideradapter.DispatchRequest {
	return provideradapter.DispatchRequest{
		ProviderDeviceID: "92000000-0000-4000-8000-000000000001", Action: "unlock",
		Payload: map[string]any{}, ProviderRequestKey: "123",
		AttemptRequestSummary: map[string]any{
			summaryOutcomeKey: outcome, summaryDelayKey: delayMS, summaryVersionKey: version,
		},
	}
}
