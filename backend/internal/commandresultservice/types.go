package commandresultservice

import (
	"errors"
	"io"
	"time"

	"github.com/qiyue2015/device-platform/internal/domain"
)

var (
	ErrInvalidResult  = errors.New("invalid CommandResult")
	ErrCommandMissing = errors.New("Command not found")
	ErrResultConflict = errors.New("CommandResult deduplication conflict")
)

type Clock interface {
	Now() time.Time
}

type Config struct {
	Random io.Reader
	Clock  Clock
}

type RecordRequest struct {
	CommandID        string
	AttemptID        *string
	Source           domain.EventSource
	Outcome          domain.ResultOutcome
	DeduplicationKey string
	ReportedAt       *time.Time
	ObservedAt       time.Time
	Payload          map[string]any
}

type RecordResult struct {
	Result           domain.CommandResult
	Command          domain.Command
	IdempotentReplay bool
}
