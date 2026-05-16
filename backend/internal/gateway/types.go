package gateway

import (
	"context"
	"errors"
	"time"
)

type Mode string

const (
	ModeNormal         Mode = "normal"
	ModeDelay          Mode = "delay"
	ModeOffline        Mode = "offline"
	ModeTimeoutThenAck Mode = "timeout_then_ack"
	ModeDuplicateAck   Mode = "duplicate_ack"
	ModeFail           Mode = "fail"
)

var validModes = map[Mode]struct{}{
	ModeNormal:         {},
	ModeDelay:          {},
	ModeOffline:        {},
	ModeTimeoutThenAck: {},
	ModeDuplicateAck:   {},
	ModeFail:           {},
}

var ErrInvalidMode = errors.New("invalid simulator mode")

func ParseMode(value string) (Mode, error) {
	mode := Mode(value)
	if _, ok := validModes[mode]; !ok {
		return "", ErrInvalidMode
	}
	return mode, nil
}

type Command struct {
	ID             string
	DeviceID       string
	Type           string
	Payload        map[string]any
	DeliveryPolicy string
	Timeout        time.Duration
}

type CommandResult struct {
	CommandID string
	Status    string
	Reason    string
	Corrected bool
	At        time.Time
}

type Heartbeat struct {
	DeviceID string
	At       time.Time
	Online   bool
}

type ModeConfig struct {
	Mode          Mode          `json:"mode"`
	Delay         time.Duration `json:"delay"`
	TimeoutOffset time.Duration `json:"timeout_offset"`
	Heartbeat     time.Duration `json:"heartbeat"`
}

type Snapshot struct {
	Mode          Mode          `json:"mode"`
	Delay         time.Duration `json:"delay"`
	TimeoutOffset time.Duration `json:"timeout_offset"`
	Heartbeat     time.Duration `json:"heartbeat"`
	Online        bool          `json:"online"`
	LastHeartbeat time.Time     `json:"last_heartbeat,omitempty"`
}

type Gateway interface {
	Dispatch(ctx context.Context, command Command) error
	SetMode(config ModeConfig) error
	Snapshot() Snapshot
	SubscribeResults() <-chan CommandResult
	SubscribeHeartbeats() <-chan Heartbeat
	Stop()
}
