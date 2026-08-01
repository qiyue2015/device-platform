package webhookworker

import (
	"context"
	"errors"
	"io"
	"net/http"
	"time"

	"github.com/qiyue2015/device-platform/internal/storage/repository"
)

var (
	ErrInvalidConfig = errors.New("invalid Webhook worker configuration")
	ErrLeaseLost     = errors.New("webhook worker lease lost")
)

type Store interface {
	repository.WebhookAuditStore
	Projects() repository.ProjectQueries
}

type SecretResolver interface {
	ResolveWebhookSecret(ctx context.Context, projectID string, version int) (string, error)
}

type HTTPClient interface {
	Do(*http.Request) (*http.Response, error)
}

type ErrorReporter func(error)

type Config struct {
	WorkerID      string
	PollInterval  time.Duration
	LeaseDuration time.Duration
	MaxAttempts   int
	RetrySchedule []time.Duration
	Client        HTTPClient
	Random        io.Reader
}
