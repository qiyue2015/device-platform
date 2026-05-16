package gateway

import (
	"context"
	"errors"
	"sync"
	"time"
)

const (
	StatusCreated   = "created"
	StatusQueued    = "queued"
	StatusSent      = "sent"
	StatusSuccess   = "success"
	StatusFailed    = "failed"
	StatusTimeout   = "timeout"
	StatusOffline   = "offline"
	StatusCancelled = "cancelled"
	PolicyOnline    = "online_only"
	PolicyQueue     = "queue_until_expire"
	PolicyReplace   = "replace_latest"
	defaultDeviceID = "simulator"
)

var ErrCommandNotFound = errors.New("command not found")

type CommandRecord struct {
	ID              string         `json:"id"`
	DeviceID        string         `json:"device_id"`
	Type            string         `json:"command_type"`
	Payload         map[string]any `json:"payload,omitempty"`
	DeliveryPolicy  string         `json:"delivery_policy"`
	Status          string         `json:"status"`
	Reason          string         `json:"reason,omitempty"`
	Corrected       bool           `json:"corrected"`
	Attempts        int            `json:"attempts"`
	FinalEventCount int            `json:"final_event_count"`
	CreatedAt       time.Time      `json:"created_at"`
	UpdatedAt       time.Time      `json:"updated_at"`
	SentAt          time.Time      `json:"sent_at,omitempty"`
	TimeoutAt       time.Time      `json:"timeout_at,omitempty"`
}

type ServiceConfig struct {
	HeartbeatTimeout time.Duration
	OfflineScan      time.Duration
	CommandTimeouts  map[string]time.Duration
}

type Service struct {
	mu               sync.Mutex
	gateway          Gateway
	commands         map[string]*CommandRecord
	deviceOnline     bool
	lastHeartbeat    time.Time
	heartbeatTimeout time.Duration
	offlineScan      time.Duration
	commandTimeouts  map[string]time.Duration
	nextID           int
	ctx              context.Context
	cancel           context.CancelFunc
}

func NewService(gateway Gateway, config ServiceConfig) *Service {
	ctx, cancel := context.WithCancel(context.Background())
	if config.HeartbeatTimeout <= 0 {
		config.HeartbeatTimeout = 300 * time.Millisecond
	}
	if config.OfflineScan <= 0 {
		config.OfflineScan = 25 * time.Millisecond
	}

	s := &Service{
		gateway:          gateway,
		commands:         make(map[string]*CommandRecord),
		deviceOnline:     gateway.Snapshot().Online,
		lastHeartbeat:    gateway.Snapshot().LastHeartbeat,
		heartbeatTimeout: config.HeartbeatTimeout,
		offlineScan:      config.OfflineScan,
		commandTimeouts:  defaultCommandTimeouts(config.CommandTimeouts),
		ctx:              ctx,
		cancel:           cancel,
	}
	go s.consumeHeartbeats()
	go s.consumeResults()
	go s.offlineLoop()
	return s
}

func (s *Service) Stop() {
	s.cancel()
	s.gateway.Stop()
}

func (s *Service) SetMode(config ModeConfig) error {
	return s.gateway.SetMode(config)
}

func (s *Service) Snapshot() Snapshot {
	s.mu.Lock()
	defer s.mu.Unlock()
	snapshot := s.gateway.Snapshot()
	snapshot.Online = s.deviceOnline
	snapshot.LastHeartbeat = s.lastHeartbeat
	return snapshot
}

func (s *Service) CreateCommand(ctx context.Context, commandType string, payload map[string]any) (CommandRecord, error) {
	now := time.Now()
	record := &CommandRecord{
		ID:             s.newCommandID(),
		DeviceID:       defaultDeviceID,
		Type:           commandType,
		Payload:        payload,
		DeliveryPolicy: deliveryPolicyFor(commandType),
		Status:         StatusCreated,
		CreatedAt:      now,
		UpdatedAt:      now,
	}

	s.mu.Lock()
	if record.DeliveryPolicy == PolicyReplace {
		s.replaceLatestPredecessorsLocked(record, now)
	}
	if !s.deviceOnline {
		switch record.DeliveryPolicy {
		case PolicyOnline:
			record.Status = StatusFailed
			record.Reason = "device_offline"
			record.FinalEventCount = 1
		case PolicyQueue, PolicyReplace:
			record.Status = StatusOffline
			record.Reason = "device_offline"
		}
		s.commands[record.ID] = record
		out := *record
		s.mu.Unlock()
		return out, nil
	}

	record.Status = StatusQueued
	s.commands[record.ID] = record
	out := *record
	s.mu.Unlock()

	s.dispatch(record.ID)
	return out, nil
}

func (s *Service) replaceLatestPredecessorsLocked(command *CommandRecord, now time.Time) {
	for _, existing := range s.commands {
		if existing.DeviceID != command.DeviceID ||
			existing.Type != command.Type ||
			existing.DeliveryPolicy != PolicyReplace {
			continue
		}
		if existing.Status != StatusOffline && existing.Status != StatusQueued {
			continue
		}
		existing.Status = StatusCancelled
		existing.Reason = "replaced_by_latest"
		existing.FinalEventCount = 1
		existing.UpdatedAt = now
	}
}

func (s *Service) GetCommand(id string) (CommandRecord, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	record, ok := s.commands[id]
	if !ok {
		return CommandRecord{}, ErrCommandNotFound
	}
	return *record, nil
}

func (s *Service) dispatch(id string) {
	s.mu.Lock()
	record, ok := s.commands[id]
	if !ok || record.Status != StatusQueued {
		s.mu.Unlock()
		return
	}
	if !s.deviceOnline {
		record.Status = StatusOffline
		record.Reason = "device_offline"
		record.UpdatedAt = time.Now()
		s.mu.Unlock()
		return
	}

	now := time.Now()
	record.Status = StatusSent
	record.Reason = ""
	record.Attempts++
	record.SentAt = now
	record.UpdatedAt = now
	timeout := s.timeoutFor(record.Type)
	command := Command{
		ID:             record.ID,
		DeviceID:       record.DeviceID,
		Type:           record.Type,
		Payload:        record.Payload,
		DeliveryPolicy: record.DeliveryPolicy,
		Timeout:        timeout,
	}
	s.mu.Unlock()

	_ = s.gateway.Dispatch(s.ctx, command)
	go s.timeoutCommand(id, timeout)
}

func (s *Service) timeoutCommand(id string, timeout time.Duration) {
	timer := time.NewTimer(timeout)
	defer timer.Stop()
	select {
	case <-s.ctx.Done():
		return
	case <-timer.C:
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	record, ok := s.commands[id]
	if !ok || record.Status != StatusSent {
		return
	}
	record.Status = StatusTimeout
	record.Reason = "gateway_timeout"
	record.TimeoutAt = time.Now()
	record.UpdatedAt = record.TimeoutAt
	record.FinalEventCount = 1
}

func (s *Service) consumeHeartbeats() {
	for {
		select {
		case <-s.ctx.Done():
			return
		case heartbeat := <-s.gateway.SubscribeHeartbeats():
			s.handleHeartbeat(heartbeat)
		}
	}
}

func (s *Service) handleHeartbeat(heartbeat Heartbeat) {
	var requeue []string

	s.mu.Lock()
	wasOnline := s.deviceOnline
	s.deviceOnline = heartbeat.Online
	s.lastHeartbeat = heartbeat.At
	if heartbeat.Online && !wasOnline {
		for id, record := range s.commands {
			if record.Status == StatusOffline {
				record.Status = StatusQueued
				record.Reason = ""
				record.UpdatedAt = time.Now()
				requeue = append(requeue, id)
			}
		}
	}
	s.mu.Unlock()

	for _, id := range requeue {
		s.dispatch(id)
	}
}

func (s *Service) consumeResults() {
	for {
		select {
		case <-s.ctx.Done():
			return
		case result := <-s.gateway.SubscribeResults():
			s.applyResult(result)
		}
	}
}

func (s *Service) applyResult(result CommandResult) {
	s.mu.Lock()
	defer s.mu.Unlock()
	record, ok := s.commands[result.CommandID]
	if !ok {
		return
	}

	switch record.Status {
	case StatusSent:
		s.finishRecord(record, result)
	case StatusTimeout:
		if result.Corrected && record.DeliveryPolicy != PolicyOnline {
			s.finishRecord(record, result)
		}
	}
}

func (s *Service) finishRecord(record *CommandRecord, result CommandResult) {
	if record.Status == StatusSuccess || record.Status == StatusFailed {
		return
	}
	record.Status = result.Status
	record.Reason = result.Reason
	record.Corrected = result.Corrected
	record.UpdatedAt = result.At
	record.FinalEventCount++
}

func (s *Service) offlineLoop() {
	ticker := time.NewTicker(s.offlineScan)
	defer ticker.Stop()
	for {
		select {
		case <-s.ctx.Done():
			return
		case <-ticker.C:
			s.markOfflineIfHeartbeatExpired()
		}
	}
}

func (s *Service) markOfflineIfHeartbeatExpired() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.deviceOnline || s.lastHeartbeat.IsZero() {
		return
	}
	if time.Since(s.lastHeartbeat) <= s.heartbeatTimeout {
		return
	}
	s.deviceOnline = false
}

func (s *Service) newCommandID() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.nextID++
	return "cmd-" + itoa(s.nextID)
}

func (s *Service) timeoutFor(commandType string) time.Duration {
	if timeout, ok := s.commandTimeouts[commandType]; ok {
		return timeout
	}
	return 10 * time.Second
}

func deliveryPolicyFor(commandType string) string {
	switch commandType {
	case "unlock", "lock":
		return PolicyOnline
	case "query_status", "reboot":
		return PolicyQueue
	case "set_config":
		return PolicyReplace
	default:
		return PolicyQueue
	}
}

func defaultCommandTimeouts(overrides map[string]time.Duration) map[string]time.Duration {
	timeouts := map[string]time.Duration{
		"unlock":       10 * time.Second,
		"lock":         10 * time.Second,
		"query_status": 15 * time.Second,
		"set_config":   60 * time.Second,
		"reboot":       30 * time.Second,
	}
	for key, value := range overrides {
		if value > 0 {
			timeouts[key] = value
		}
	}
	return timeouts
}

func itoa(value int) string {
	if value == 0 {
		return "0"
	}
	var buf [20]byte
	i := len(buf)
	for value > 0 {
		i--
		buf[i] = byte('0' + value%10)
		value /= 10
	}
	return string(buf[i:])
}
