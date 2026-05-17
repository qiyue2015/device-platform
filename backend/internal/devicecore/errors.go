package devicecore

import "errors"

var (
	ErrNotFound               = errors.New("not_found")
	ErrInvalidArgument        = errors.New("invalid_argument")
	ErrDuplicateDevice        = errors.New("duplicate_device")
	ErrIdempotencyConflict    = errors.New("idempotency_key_conflict")
	ErrUnsafeDeliveryOverride = errors.New("unsafe_delivery_policy_override")
	ErrInvalidTransition      = errors.New("invalid_command_transition")
)
