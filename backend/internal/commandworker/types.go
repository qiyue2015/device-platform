package commandworker

import (
	"context"
	"errors"
	"io"
	"time"

	"github.com/qiyue2015/device-platform/internal/domain"
	"github.com/qiyue2015/device-platform/internal/provideradapter"
	"github.com/qiyue2015/device-platform/internal/storage/repository"
)

var (
	ErrInvalidConfig = errors.New("invalid Command worker configuration")
	ErrLeaseLost     = errors.New("Command worker lease lost")
	ErrRuntimeState  = errors.New("invalid persistent Command runtime state")
)

type Store interface {
	Commands() repository.CommandQueries
	Devices() repository.DeviceQueries
	DeviceTypes() repository.DeviceTypeQueries
	TransactCommand(context.Context, func(repository.CommandTx) error) error
}

type AdapterRegistration struct {
	ProviderCode string
	AdapterCode  domain.Adapter
	Adapter      provideradapter.Adapter
	ResultSource domain.EventSource
}

type Config struct {
	WorkerID      string
	LeaseDuration time.Duration
	PollInterval  time.Duration
	Random        io.Reader
	Adapters      []AdapterRegistration
}

type ErrorReporter func(error)
