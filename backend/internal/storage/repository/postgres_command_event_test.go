package repository

import (
	"testing"

	"github.com/qiyue2015/device-platform/internal/domain"
)

func TestAttemptTransitionMatches(t *testing.T) {
	providerNotConfigured := "provider_not_configured"
	providerTransportError := "provider_transport_error"
	providerRejected := "provider_rejected"
	providerDeliveryUnknown := "provider_delivery_unknown"
	providerResponseInvalid := "provider_response_invalid"
	tests := []struct {
		name       string
		outcome    domain.AttemptOutcome
		transition CommandStatusTransition
		want       bool
	}{
		{
			name:    "invalid request before dispatch",
			outcome: domain.AttemptOutcomeInvalidRequest,
			transition: CommandStatusTransition{
				From: domain.CommandStatusQueued, To: domain.CommandStatusFailed, ReasonCode: &providerNotConfigured,
			},
			want: true,
		},
		{
			name:    "before-send transport failure",
			outcome: domain.AttemptOutcomeTransportErrorBeforeSend,
			transition: CommandStatusTransition{
				From: domain.CommandStatusSent, To: domain.CommandStatusFailed, ReasonCode: &providerTransportError,
			},
			want: true,
		},
		{
			name:    "after-send transport failure is unknown",
			outcome: domain.AttemptOutcomeTransportErrorAfterSend,
			transition: CommandStatusTransition{
				From: domain.CommandStatusSent, To: domain.CommandStatusUnknown, ReasonCode: &providerDeliveryUnknown,
			},
			want: true,
		},
		{
			name:    "Provider rejection",
			outcome: domain.AttemptOutcomeProviderRejected,
			transition: CommandStatusTransition{
				From: domain.CommandStatusSent, To: domain.CommandStatusFailed, ReasonCode: &providerRejected,
			},
			want: true,
		},
		{
			name:    "invalid response is unknown",
			outcome: domain.AttemptOutcomeInvalidResponse,
			transition: CommandStatusTransition{
				From: domain.CommandStatusSent, To: domain.CommandStatusUnknown, ReasonCode: &providerResponseInvalid,
			},
			want: true,
		},
		{
			name:    "invalid response cannot become rejection",
			outcome: domain.AttemptOutcomeInvalidResponse,
			transition: CommandStatusTransition{
				From: domain.CommandStatusSent, To: domain.CommandStatusFailed, ReasonCode: &providerRejected,
			},
			want: false,
		},
		{
			name:    "Provider rejection cannot become invalid response",
			outcome: domain.AttemptOutcomeProviderRejected,
			transition: CommandStatusTransition{
				From: domain.CommandStatusSent, To: domain.CommandStatusUnknown, ReasonCode: &providerResponseInvalid,
			},
			want: false,
		},
		{
			name:    "Provider acceptance is not a state transition",
			outcome: domain.AttemptOutcomeProviderAccepted,
			transition: CommandStatusTransition{
				From: domain.CommandStatusSent, To: domain.CommandStatusSent,
			},
			want: false,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := attemptTransitionMatches(test.outcome, test.transition); got != test.want {
				t.Fatalf("attemptTransitionMatches(%q, %+v) = %v, want %v", test.outcome, test.transition, got, test.want)
			}
		})
	}
}

func TestEvidenceProgresses(t *testing.T) {
	tests := []struct {
		name            string
		currentLevel    domain.ConfirmationLevel
		currentEvidence domain.EvidenceStatus
		nextLevel       domain.ConfirmationLevel
		nextEvidence    domain.EvidenceStatus
		want            bool
	}{
		{
			name:            "confirmation advances",
			currentLevel:    domain.ConfirmationTransportSent,
			currentEvidence: domain.EvidenceUnverified,
			nextLevel:       domain.ConfirmationProviderAccepted,
			nextEvidence:    domain.EvidenceUnverified,
			want:            true,
		},
		{
			name:            "evidence improves at same confirmation",
			currentLevel:    domain.ConfirmationProviderAccepted,
			currentEvidence: domain.EvidenceUnverified,
			nextLevel:       domain.ConfirmationProviderAccepted,
			nextEvidence:    domain.EvidenceVerified,
			want:            true,
		},
		{
			name:            "same unverified evidence is idempotent",
			currentLevel:    domain.ConfirmationProviderAccepted,
			currentEvidence: domain.EvidenceUnverified,
			nextLevel:       domain.ConfirmationProviderAccepted,
			nextEvidence:    domain.EvidenceUnverified,
			want:            false,
		},
		{
			name:            "same verified evidence is idempotent",
			currentLevel:    domain.ConfirmationProviderAccepted,
			currentEvidence: domain.EvidenceVerified,
			nextLevel:       domain.ConfirmationProviderAccepted,
			nextEvidence:    domain.EvidenceVerified,
			want:            false,
		},
		{
			name:            "confirmation cannot regress",
			currentLevel:    domain.ConfirmationProviderAccepted,
			currentEvidence: domain.EvidenceUnverified,
			nextLevel:       domain.ConfirmationTransportSent,
			nextEvidence:    domain.EvidenceUnverified,
			want:            false,
		},
		{
			name:            "verified evidence cannot regress at same confirmation",
			currentLevel:    domain.ConfirmationDeviceAcked,
			currentEvidence: domain.EvidenceVerified,
			nextLevel:       domain.ConfirmationDeviceAcked,
			nextEvidence:    domain.EvidenceUnverified,
			want:            false,
		},
		{
			name:            "unknown level is rejected",
			currentLevel:    domain.ConfirmationNone,
			currentEvidence: domain.EvidenceNone,
			nextLevel:       domain.ConfirmationLevel("future"),
			nextEvidence:    domain.EvidenceVerified,
			want:            false,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := evidenceProgresses(test.currentLevel, test.currentEvidence, test.nextLevel, test.nextEvidence); got != test.want {
				t.Fatalf("evidenceProgresses(%q, %q, %q, %q) = %v, want %v", test.currentLevel, test.currentEvidence, test.nextLevel, test.nextEvidence, got, test.want)
			}
		})
	}
}

func TestAttemptCompletionAllowed(t *testing.T) {
	tests := []struct {
		name     string
		provider string
		phase    domain.AttemptPhase
		request  CompleteCommandAttemptRequest
		want     bool
	}{
		{
			name: "WWTIOT acceptance remains unverified", provider: domain.ProviderCodeWWTIOT, phase: domain.AttemptPhaseDispatching,
			request: CompleteCommandAttemptRequest{Outcome: domain.AttemptOutcomeProviderAccepted, ConfirmationLevel: domain.ConfirmationProviderAccepted, EvidenceStatus: domain.EvidenceUnverified}, want: true,
		},
		{
			name: "WWTIOT acceptance cannot be verified", provider: domain.ProviderCodeWWTIOT, phase: domain.AttemptPhaseDispatching,
			request: CompleteCommandAttemptRequest{Outcome: domain.AttemptOutcomeProviderAccepted, ConfirmationLevel: domain.ConfirmationProviderAccepted, EvidenceStatus: domain.EvidenceVerified}, want: false,
		},
		{
			name: "WWTIOT cannot produce Device final", provider: domain.ProviderCodeWWTIOT, phase: domain.AttemptPhaseDispatching,
			request: CompleteCommandAttemptRequest{Outcome: domain.AttemptOutcomeDeviceSucceeded, ConfirmationLevel: domain.ConfirmationDeviceFinal, EvidenceStatus: domain.EvidenceVerified}, want: false,
		},
		{
			name: "simulator rejection is verified transport evidence", provider: domain.ProviderCodeSimulator, phase: domain.AttemptPhaseDispatching,
			request: CompleteCommandAttemptRequest{Outcome: domain.AttemptOutcomeProviderRejected, ConfirmationLevel: domain.ConfirmationTransportSent, EvidenceStatus: domain.EvidenceVerified}, want: true,
		},
		{
			name: "simulator acceptance must be verified", provider: domain.ProviderCodeSimulator, phase: domain.AttemptPhaseDispatching,
			request: CompleteCommandAttemptRequest{Outcome: domain.AttemptOutcomeProviderAccepted, ConfirmationLevel: domain.ConfirmationProviderAccepted, EvidenceStatus: domain.EvidenceUnverified}, want: false,
		},
		{
			name: "simulator verified acceptance is allowed", provider: domain.ProviderCodeSimulator, phase: domain.AttemptPhaseDispatching,
			request: CompleteCommandAttemptRequest{Outcome: domain.AttemptOutcomeProviderAccepted, ConfirmationLevel: domain.ConfirmationProviderAccepted, EvidenceStatus: domain.EvidenceVerified}, want: true,
		},
		{
			name: "simulator cannot produce Device ACK", provider: domain.ProviderCodeSimulator, phase: domain.AttemptPhaseDispatching,
			request: CompleteCommandAttemptRequest{Outcome: domain.AttemptOutcomeDeviceAcked, ConfirmationLevel: domain.ConfirmationDeviceAcked, EvidenceStatus: domain.EvidenceVerified}, want: false,
		},
		{
			name: "invalid request is pre-dispatch WWTIOT only", provider: domain.ProviderCodeWWTIOT, phase: domain.AttemptPhaseClaimed,
			request: CompleteCommandAttemptRequest{Outcome: domain.AttemptOutcomeInvalidRequest, ConfirmationLevel: domain.ConfirmationNone, EvidenceStatus: domain.EvidenceNone}, want: true,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := attemptCompletionAllowed(test.provider, test.phase, test.request); got != test.want {
				t.Fatalf("attemptCompletionAllowed(%q, %q, %+v) = %v, want %v", test.provider, test.phase, test.request, got, test.want)
			}
		})
	}
}
