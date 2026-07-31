package domain

import "testing"

func TestCommandTransitionsMatchFrozenContract(t *testing.T) {
	allowed := [][2]CommandStatus{
		{CommandStatusQueued, CommandStatusSent},
		{CommandStatusQueued, CommandStatusCancelled},
		{CommandStatusQueued, CommandStatusFailed},
		{CommandStatusQueued, CommandStatusTimeout},
		{CommandStatusSent, CommandStatusAcked},
		{CommandStatusSent, CommandStatusSuccess},
		{CommandStatusSent, CommandStatusFailed},
		{CommandStatusSent, CommandStatusTimeout},
		{CommandStatusAcked, CommandStatusSuccess},
		{CommandStatusAcked, CommandStatusFailed},
		{CommandStatusAcked, CommandStatusTimeout},
	}
	for _, transition := range allowed {
		if !CanTransitionCommand(transition[0], transition[1]) {
			t.Fatalf("expected %s -> %s to be allowed", transition[0], transition[1])
		}
	}
	for _, terminal := range []CommandStatus{CommandStatusSuccess, CommandStatusFailed, CommandStatusTimeout, CommandStatusCancelled} {
		if !terminal.IsTerminal() {
			t.Fatalf("expected %s to be terminal", terminal)
		}
		if CanTransitionCommand(terminal, CommandStatusSent) {
			t.Fatalf("terminal status %s must not transition", terminal)
		}
	}
	if CanTransitionCommand(CommandStatusQueued, CommandStatusSuccess) {
		t.Fatal("queued -> success must not be allowed")
	}
}

func TestAttemptTransitionsAndConfirmationAreMonotonic(t *testing.T) {
	if !CanTransitionAttempt(AttemptPhaseClaimed, AttemptPhaseDispatching) ||
		!CanTransitionAttempt(AttemptPhaseClaimed, AttemptPhaseCompleted) ||
		!CanTransitionAttempt(AttemptPhaseDispatching, AttemptPhaseCompleted) {
		t.Fatal("frozen attempt paths must be allowed")
	}
	if CanTransitionAttempt(AttemptPhaseDispatching, AttemptPhaseClaimed) || CanTransitionAttempt(AttemptPhaseCompleted, AttemptPhaseDispatching) {
		t.Fatal("attempt phase must not move backwards")
	}
	if !CanAdvanceConfirmation(ConfirmationTransportSent, ConfirmationProviderAccepted) {
		t.Fatal("provider acceptance must advance transport confirmation")
	}
	if CanAdvanceConfirmation(ConfirmationDeviceFinal, ConfirmationDeviceAcked) {
		t.Fatal("confirmation must not move backwards")
	}
	if CanAdvanceConfirmation(ConfirmationLevel("invalid"), ConfirmationLevel("invalid")) {
		t.Fatal("unknown confirmation values must be rejected")
	}
}
