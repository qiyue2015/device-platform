package domain

const (
	ErrCodeInvalidRequest           = "invalid_request"
	ErrCodeUnauthorized             = "unauthorized"
	ErrCodeForbidden                = "forbidden"
	ErrCodeNotFound                 = "not_found"
	ErrCodeIdempotencyKeyConflict   = "idempotency_key_conflict"
	ErrCodeUnsafeDeliveryPolicy     = "unsafe_delivery_policy"
	ErrCodeDeviceOffline            = "device_offline"
	ErrCodeCommandExpired           = "command_expired"
	ErrCodeInvalidCommandTransition = "invalid_command_transition"
	ErrCodeWebhookDeliveryNotDead   = "webhook_delivery_not_dead"
)
