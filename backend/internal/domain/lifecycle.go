package domain

var commandTransitions = map[CommandStatus]map[CommandStatus]struct{}{
	CommandStatusQueued: {
		CommandStatusSent: {}, CommandStatusCancelled: {}, CommandStatusFailed: {}, CommandStatusTimeout: {},
	},
	CommandStatusSent: {
		CommandStatusAcked: {}, CommandStatusSuccess: {}, CommandStatusFailed: {}, CommandStatusTimeout: {}, CommandStatusUnknown: {},
	},
	CommandStatusAcked: {
		CommandStatusSuccess: {}, CommandStatusFailed: {}, CommandStatusTimeout: {},
	},
}

func CanTransitionCommand(from, to CommandStatus) bool {
	_, ok := commandTransitions[from][to]
	return ok
}

func (status CommandStatus) IsTerminal() bool {
	switch status {
	case CommandStatusSuccess, CommandStatusFailed, CommandStatusTimeout, CommandStatusCancelled, CommandStatusUnknown:
		return true
	default:
		return false
	}
}

func CanTransitionAttempt(from, to AttemptPhase) bool {
	return from == AttemptPhaseClaimed && (to == AttemptPhaseDispatching || to == AttemptPhaseCompleted) ||
		from == AttemptPhaseDispatching && to == AttemptPhaseCompleted
}

func CanAdvanceConfirmation(from, to ConfirmationLevel) bool {
	fromRank, fromOK := confirmationRank(from)
	toRank, toOK := confirmationRank(to)
	return fromOK && toOK && toRank >= fromRank
}

func confirmationRank(level ConfirmationLevel) (int, bool) {
	switch level {
	case ConfirmationNone:
		return 0, true
	case ConfirmationTransportSent:
		return 1, true
	case ConfirmationProviderAccepted:
		return 2, true
	case ConfirmationDeviceAcked:
		return 3, true
	case ConfirmationDeviceFinal:
		return 4, true
	default:
		return 0, false
	}
}
