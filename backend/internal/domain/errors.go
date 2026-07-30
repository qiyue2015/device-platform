package domain

const (
	ErrCodeInvalidRequest             = "invalid_request"
	ErrCodeUnauthorized               = "unauthorized"
	ErrCodeForbidden                  = "forbidden"
	ErrCodeNotFound                   = "not_found"
	ErrCodeIdempotencyKeyConflict     = "idempotency_key_conflict"
	ErrCodeInvalidStateTransition     = "invalid_state_transition"
	ErrCodeProviderDeviceConflict     = "provider_device_conflict"
	ErrCodeDeviceDisabled             = "device_disabled"
	ErrCodeDeviceDeleted              = "device_deleted"
	ErrCodeProviderNotConfigured      = "provider_not_configured"
	ErrCodeCommandNotCancellable      = "command_not_cancellable"
	ErrCodeWebhookDeliveryNotDead     = "webhook_delivery_not_dead"
	ErrCodeWebhookNotConfigured       = "webhook_not_configured"
	ErrCodeUnsupportedCapability      = "unsupported_capability"
	ErrCodeInvalidCapabilityPayload   = "invalid_capability_payload"
	ErrCodeRateLimited                = "rate_limited"
	ErrCodeAuthDependencyUnavailable  = "auth_dependency_unavailable"
	ErrCodeProviderCallbackUnverified = "provider_callback_unverified"
)
