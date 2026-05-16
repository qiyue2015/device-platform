package repository

import (
	"context"

	"github.com/qiyue2015/device-platform/internal/domain"
)

type UserRepository interface {
	Create(ctx context.Context, user domain.User) error
	Get(ctx context.Context, id string) (domain.User, error)
	GetByEmail(ctx context.Context, email string) (domain.User, error)
}

type ProjectRepository interface {
	Create(ctx context.Context, project domain.Project) error
	Get(ctx context.Context, id string) (domain.Project, error)
	Update(ctx context.Context, project domain.Project) error
}

type DeviceRepository interface {
	CreateType(ctx context.Context, deviceType domain.DeviceType) error
	Create(ctx context.Context, device domain.Device) error
	Get(ctx context.Context, id string) (domain.Device, error)
	ListByProject(ctx context.Context, projectID string) ([]domain.Device, error)
	SaveState(ctx context.Context, state domain.DeviceState) error
	GetCurrentState(ctx context.Context, deviceID string) (domain.DeviceState, error)
}

type CommandRepository interface {
	Create(ctx context.Context, command domain.DeviceCommand) error
	Get(ctx context.Context, id string) (domain.DeviceCommand, error)
	GetByIdempotencyKey(ctx context.Context, projectID string, idempotencyKey string) (domain.DeviceCommand, error)
	TransitionStatus(ctx context.Context, commandID string, from domain.CommandStatus, to domain.CommandStatus, reason string) (bool, error)
	CreateAttempt(ctx context.Context, attempt domain.DeviceCommandAttempt) error
}

type EventRepository interface {
	CreateRawMessage(ctx context.Context, message domain.DeviceRawMessage) error
	CreateDeviceEvent(ctx context.Context, event domain.DeviceEvent) error
}

type WebhookRepository interface {
	CreateDelivery(ctx context.Context, delivery domain.WebhookDelivery) error
	ListDeadDeliveries(ctx context.Context, projectID string) ([]domain.WebhookDelivery, error)
	MarkForManualResend(ctx context.Context, deliveryID string) error
}

type AuditRepository interface {
	Create(ctx context.Context, log domain.AuditLog) error
}
