package devicecore

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"
)

const (
	AccessTypeMockGateway        = "mock_gateway"
	TransportProtocolSimulator   = "simulator"
	AdapterMockGateway           = "mock_gateway"
	ConnectionStatusUnknown      = "unknown"
	ConnectionStatusOnline       = "online"
	ConnectionStatusOffline      = "offline"
	LifecycleStatusActive        = "active"
	LifecycleStatusDisabled      = "disabled"
	LifecycleStatusDeleted       = "deleted"
	defaultSimulatorProviderCode = "simulator"
)

type Clock interface {
	Now() time.Time
}

type realClock struct{}

func (realClock) Now() time.Time { return time.Now().UTC() }

type Service struct {
	mu    sync.RWMutex
	clock Clock

	projects       map[string]Project
	projectsByKey  map[string]string
	devices        map[string]Device
	commands       map[string]Command
	idempotencyMap map[string]string
}

func NewService() *Service {
	return NewServiceWithClock(realClock{})
}

func NewServiceWithClock(clock Clock) *Service {
	return &Service{
		clock:          clock,
		projects:       map[string]Project{},
		projectsByKey:  map[string]string{},
		devices:        map[string]Device{},
		commands:       map[string]Command{},
		idempotencyMap: map[string]string{},
	}
}

func (s *Service) CreateProject(req CreateProjectRequest) (Project, error) {
	now := s.clock.Now()
	name := strings.TrimSpace(req.Name)
	if name == "" {
		return Project{}, fmt.Errorf("%w: project name is required", ErrInvalidArgument)
	}

	project := Project{
		ID:          newID("prj"),
		Name:        name,
		APIKey:      newAPIKey(),
		WebhookURL:  strings.TrimSpace(req.WebhookURL),
		IPWhitelist: cloneStrings(req.IPWhitelist),
		CreatedAt:   now,
		UpdatedAt:   now,
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	s.projects[project.ID] = project
	s.projectsByKey[project.APIKey] = project.ID
	return project, nil
}

func (s *Service) ListProjects() []Project {
	s.mu.RLock()
	defer s.mu.RUnlock()
	projects := make([]Project, 0, len(s.projects))
	for _, project := range s.projects {
		projects = append(projects, project)
	}
	sort.Slice(projects, func(i, j int) bool {
		return projects[i].CreatedAt.Before(projects[j].CreatedAt)
	})
	return projects
}

func (s *Service) GetProject(projectID string) (Project, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	project, ok := s.projects[projectID]
	if !ok {
		return Project{}, ErrNotFound
	}
	return project, nil
}

func (s *Service) ProjectByAPIKey(apiKey string) (Project, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	projectID, ok := s.projectsByKey[apiKey]
	if !ok {
		return Project{}, ErrNotFound
	}
	return s.projects[projectID], nil
}

func (s *Service) UpdateProject(projectID string, req UpdateProjectRequest) (Project, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	project, ok := s.projects[projectID]
	if !ok {
		return Project{}, ErrNotFound
	}
	if req.Name != nil {
		name := strings.TrimSpace(*req.Name)
		if name == "" {
			return Project{}, fmt.Errorf("%w: project name is required", ErrInvalidArgument)
		}
		project.Name = name
	}
	if req.WebhookURL != nil {
		project.WebhookURL = strings.TrimSpace(*req.WebhookURL)
	}
	if req.IPWhitelist != nil {
		project.IPWhitelist = cloneStrings(req.IPWhitelist)
	}
	project.UpdatedAt = s.clock.Now()
	s.projects[project.ID] = project
	return project, nil
}

func (s *Service) CreateDevice(req CreateDeviceRequest) (Device, error) {
	now := s.clock.Now()
	projectID := strings.TrimSpace(req.ProjectID)
	if projectID == "" {
		return Device{}, fmt.Errorf("%w: project_id is required", ErrInvalidArgument)
	}
	name := strings.TrimSpace(req.Name)
	if name == "" {
		return Device{}, fmt.Errorf("%w: device name is required", ErrInvalidArgument)
	}
	deviceType := strings.TrimSpace(req.DeviceType)
	if deviceType == "" {
		deviceType = "generic"
	}
	accessType := defaultString(strings.TrimSpace(req.AccessType), AccessTypeMockGateway)
	if !validAccessType(accessType) {
		return Device{}, fmt.Errorf("%w: unsupported access_type", ErrInvalidArgument)
	}
	protocol := defaultTransportProtocol(accessType, req.TransportProtocol)
	if !validTransportProtocol(protocol) {
		return Device{}, fmt.Errorf("%w: unsupported transport_protocol", ErrInvalidArgument)
	}
	adapter := defaultAdapter(accessType, req.Adapter)
	if !validAdapter(adapter) {
		return Device{}, fmt.Errorf("%w: unsupported adapter", ErrInvalidArgument)
	}
	if !validAccessAdapterPair(accessType, adapter) {
		return Device{}, fmt.Errorf("%w: adapter does not match access_type", ErrInvalidArgument)
	}
	providerCode := defaultProviderCode(accessType, req.ProviderCode)
	providerDeviceID := strings.TrimSpace(req.ProviderDeviceID)
	if providerDeviceID == "" {
		providerDeviceID = strings.TrimSpace(req.ExternalID)
	}
	connectionStatus := defaultConnectionStatus(req.ConnectionStatus, req.Online)
	if !validConnectionStatus(connectionStatus) {
		return Device{}, fmt.Errorf("%w: unsupported connection_status", ErrInvalidArgument)
	}
	lifecycleStatus := defaultString(strings.TrimSpace(req.LifecycleStatus), LifecycleStatusActive)
	if !validLifecycleStatus(lifecycleStatus) {
		return Device{}, fmt.Errorf("%w: unsupported lifecycle_status", ErrInvalidArgument)
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.projects[projectID]; !ok {
		return Device{}, ErrNotFound
	}

	deviceID := newID("dev")
	if providerDeviceID == "" {
		providerDeviceID = deviceID
	}
	device := Device{
		ID:                deviceID,
		ProjectID:         projectID,
		ExternalID:        strings.TrimSpace(req.ExternalID),
		DeviceTypeID:      strings.TrimSpace(req.DeviceTypeID),
		Name:              name,
		DeviceType:        deviceType,
		ProviderCode:      providerCode,
		ProviderDeviceID:  providerDeviceID,
		AccessType:        accessType,
		TransportProtocol: protocol,
		Adapter:           adapter,
		ConnectionStatus:  connectionStatus,
		LifecycleStatus:   lifecycleStatus,
		Metadata:          cloneMap(req.Metadata),
		Online:            connectionStatus == ConnectionStatusOnline,
		CurrentState:      cloneMap(req.CurrentState),
		CreatedAt:         now,
		UpdatedAt:         now,
	}
	if device.Online {
		device.LastSeenAt = &now
	}
	s.devices[device.ID] = device
	return device, nil
}

func (s *Service) ListDevices(projectID string) []Device {
	s.mu.RLock()
	defer s.mu.RUnlock()
	devices := []Device{}
	for _, device := range s.devices {
		if device.ProjectID == projectID {
			devices = append(devices, cloneDevice(device))
		}
	}
	sort.Slice(devices, func(i, j int) bool {
		return devices[i].CreatedAt.Before(devices[j].CreatedAt)
	})
	return devices
}

func (s *Service) GetDevice(projectID, deviceID string) (Device, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	device, ok := s.devices[deviceID]
	if !ok || device.ProjectID != projectID {
		return Device{}, ErrNotFound
	}
	return cloneDevice(device), nil
}

func (s *Service) SetDeviceOnline(projectID, deviceID string, online bool) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	device, ok := s.devices[deviceID]
	if !ok || device.ProjectID != projectID {
		return ErrNotFound
	}

	now := s.clock.Now()
	device.Online = online
	if online {
		device.ConnectionStatus = ConnectionStatusOnline
	} else {
		device.ConnectionStatus = ConnectionStatusOffline
	}
	device.UpdatedAt = now
	if online {
		device.LastSeenAt = &now
	}
	s.devices[device.ID] = device
	if online {
		s.requeueOfflineLocked(projectID, deviceID, now)
	}
	return nil
}

func (s *Service) CreateCommand(req CreateCommandRequest) (Command, error) {
	now := s.clock.Now()
	projectID := strings.TrimSpace(req.ProjectID)
	deviceID := strings.TrimSpace(req.DeviceID)
	commandType := strings.TrimSpace(req.CommandType)
	if projectID == "" || deviceID == "" || commandType == "" {
		return Command{}, fmt.Errorf("%w: project_id, device_id, and command_type are required", ErrInvalidArgument)
	}
	policy, timeout, lowRisk, err := resolveCommandPolicy(commandType, req.DeliveryPolicy)
	if err != nil {
		return Command{}, err
	}
	expiresAt := now.Add(timeout)
	if req.ExpiresAt != nil {
		expiresAt = req.ExpiresAt.UTC()
	}
	if !expiresAt.After(now) {
		return Command{}, fmt.Errorf("%w: expires_at must be in the future", ErrInvalidArgument)
	}
	requestHash, err := commandRequestHash(deviceID, commandType, req.Payload)
	if err != nil {
		return Command{}, err
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.projects[projectID]; !ok {
		return Command{}, ErrNotFound
	}
	device, ok := s.devices[deviceID]
	if !ok || device.ProjectID != projectID {
		return Command{}, ErrNotFound
	}
	if key := strings.TrimSpace(req.IdempotencyKey); key != "" {
		scope := idempotencyScope(projectID, key)
		if existingID, ok := s.idempotencyMap[scope]; ok {
			existing := s.commands[existingID]
			if existing.RequestHash != requestHash {
				return Command{}, ErrIdempotencyConflict
			}
			return cloneCommand(existing), nil
		}
	}

	command := Command{
		ID:                newID("cmd"),
		ProjectID:         projectID,
		DeviceID:          deviceID,
		CommandType:       commandType,
		Payload:           cloneMap(req.Payload),
		IdempotencyKey:    strings.TrimSpace(req.IdempotencyKey),
		RequestHash:       requestHash,
		DeliveryPolicy:    policy,
		Status:            CommandStatusCreated,
		ExpiresAt:         expiresAt,
		CreatedAt:         now,
		UpdatedAt:         now,
		CompensationUntil: compensationUntil(now, lowRisk),
	}

	if policy == DeliveryPolicyReplaceLatest {
		s.cancelReplaceLatestPredecessorsLocked(command, now)
	}
	command = s.initialDispatchLocked(command, device.Online, now)
	s.commands[command.ID] = command
	if command.IdempotencyKey != "" {
		s.idempotencyMap[idempotencyScope(projectID, command.IdempotencyKey)] = command.ID
	}
	return cloneCommand(command), nil
}

func (s *Service) ListCommands(projectID string) []Command {
	s.mu.RLock()
	defer s.mu.RUnlock()
	commands := []Command{}
	for _, command := range s.commands {
		if command.ProjectID == projectID {
			commands = append(commands, cloneCommand(command))
		}
	}
	sort.Slice(commands, func(i, j int) bool {
		return commands[i].CreatedAt.Before(commands[j].CreatedAt)
	})
	return commands
}

func (s *Service) GetCommand(projectID, commandID string) (Command, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	command, ok := s.commands[commandID]
	if !ok || command.ProjectID != projectID {
		return Command{}, ErrNotFound
	}
	return cloneCommand(command), nil
}

func (s *Service) CancelCommand(projectID, commandID string) (Command, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	command, ok := s.commands[commandID]
	if !ok || command.ProjectID != projectID {
		return Command{}, ErrNotFound
	}
	if !isCancellable(command.Status) {
		return Command{}, ErrInvalidTransition
	}
	now := s.clock.Now()
	command.Status = CommandStatusCancelled
	command.Reason = "cancelled_by_request"
	command.FinishedAt = &now
	command.UpdatedAt = now
	command.Events = append(command.Events, CommandEvent{Type: "command_finished", At: now})
	s.commands[command.ID] = command
	return cloneCommand(command), nil
}

func (s *Service) MarkCommandSent(projectID, commandID string) (Command, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	command, ok := s.commands[commandID]
	if !ok || command.ProjectID != projectID {
		return Command{}, ErrNotFound
	}
	if command.Status != CommandStatusQueued {
		return Command{}, ErrInvalidTransition
	}
	now := s.clock.Now()
	command.Status = CommandStatusSent
	command.SentAt = &now
	command.UpdatedAt = now
	command.Attempts = append(command.Attempts, CommandAttempt{
		AttemptNo: len(command.Attempts) + 1,
		Status:    "sent",
		At:        now,
	})
	s.commands[command.ID] = command
	return cloneCommand(command), nil
}

func (s *Service) AckCommand(projectID, commandID string) (Command, error) {
	return s.transitionCommand(projectID, commandID, CommandStatusAcked, "", false)
}

func (s *Service) SucceedCommand(projectID, commandID string, corrected bool) (Command, error) {
	return s.transitionCommand(projectID, commandID, CommandStatusSuccess, "", corrected)
}

func (s *Service) FailCommand(projectID, commandID, reason string, corrected bool) (Command, error) {
	return s.transitionCommand(projectID, commandID, CommandStatusFailed, reason, corrected)
}

func (s *Service) TimeoutCommand(projectID, commandID string) (Command, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	command, ok := s.commands[commandID]
	if !ok || command.ProjectID != projectID {
		return Command{}, ErrNotFound
	}
	if command.Status != CommandStatusSent && command.Status != CommandStatusAcked {
		return Command{}, ErrInvalidTransition
	}
	now := s.clock.Now()
	command.Status = CommandStatusTimeout
	command.Reason = "command_timeout"
	command.FinishedAt = &now
	command.UpdatedAt = now
	command.Events = append(command.Events, CommandEvent{Type: "command_finished", At: now})
	s.commands[command.ID] = command
	return cloneCommand(command), nil
}

func (s *Service) RequeueOfflineCommands() {
	s.mu.Lock()
	defer s.mu.Unlock()
	now := s.clock.Now()
	for _, device := range s.devices {
		if device.Online {
			s.requeueOfflineLocked(device.ProjectID, device.ID, now)
		}
	}
}

func (s *Service) transitionCommand(projectID, commandID string, next CommandStatus, reason string, corrected bool) (Command, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	command, ok := s.commands[commandID]
	if !ok || command.ProjectID != projectID {
		return Command{}, ErrNotFound
	}
	now := s.clock.Now()
	if !canTransition(command, next, corrected, now) {
		return Command{}, ErrInvalidTransition
	}
	command.Status = next
	command.Reason = reason
	command.Corrected = command.Corrected || corrected
	command.UpdatedAt = now
	if next == CommandStatusSuccess || next == CommandStatusFailed {
		command.FinishedAt = &now
		command.Events = append(command.Events, CommandEvent{
			Type:      "command_finished",
			At:        now,
			Corrected: corrected,
		})
	}
	s.commands[command.ID] = command
	return cloneCommand(command), nil
}

func (s *Service) initialDispatchLocked(command Command, deviceOnline bool, now time.Time) Command {
	if deviceOnline {
		command.Status = CommandStatusQueued
		command.UpdatedAt = now
		return command
	}

	switch command.DeliveryPolicy {
	case DeliveryPolicyOnlineOnly:
		command.Status = CommandStatusFailed
		command.Reason = "device_offline"
		command.FinishedAt = &now
		command.Events = append(command.Events, CommandEvent{Type: "command_finished", At: now})
	case DeliveryPolicyQueueUntilExpire, DeliveryPolicyReplaceLatest:
		command.Status = CommandStatusOffline
	}
	command.UpdatedAt = now
	return command
}

func (s *Service) cancelReplaceLatestPredecessorsLocked(command Command, now time.Time) {
	for id, existing := range s.commands {
		if existing.ProjectID != command.ProjectID ||
			existing.DeviceID != command.DeviceID ||
			existing.CommandType != command.CommandType ||
			existing.DeliveryPolicy != DeliveryPolicyReplaceLatest {
			continue
		}
		if existing.Status != CommandStatusOffline && existing.Status != CommandStatusQueued {
			continue
		}
		existing.Status = CommandStatusCancelled
		existing.Reason = "replaced_by_latest"
		existing.FinishedAt = &now
		existing.UpdatedAt = now
		existing.Events = append(existing.Events, CommandEvent{Type: "command_finished", At: now})
		s.commands[id] = existing
	}
}

func (s *Service) requeueOfflineLocked(projectID, deviceID string, now time.Time) {
	for id, command := range s.commands {
		if command.ProjectID != projectID || command.DeviceID != deviceID || command.Status != CommandStatusOffline {
			continue
		}
		if !command.ExpiresAt.After(now) {
			command.Status = CommandStatusFailed
			command.Reason = "expired_while_offline"
			command.FinishedAt = &now
			command.Events = append(command.Events, CommandEvent{Type: "command_finished", At: now})
		} else {
			command.Status = CommandStatusQueued
			command.Reason = ""
		}
		command.UpdatedAt = now
		s.commands[id] = command
	}
}

func resolveCommandPolicy(commandType string, requested DeliveryPolicy) (DeliveryPolicy, time.Duration, bool, error) {
	defaultPolicy, timeout, lowRisk := commandDefaults(commandType)
	if requested == "" || requested == defaultPolicy {
		return defaultPolicy, timeout, lowRisk, nil
	}
	if !lowRisk {
		return "", 0, false, ErrUnsafeDeliveryOverride
	}
	switch requested {
	case DeliveryPolicyQueueUntilExpire, DeliveryPolicyReplaceLatest:
		return requested, timeout, true, nil
	case DeliveryPolicyOnlineOnly:
		return "", 0, false, ErrUnsafeDeliveryOverride
	default:
		return "", 0, false, fmt.Errorf("%w: unsupported delivery_policy", ErrInvalidArgument)
	}
}

func commandDefaults(commandType string) (DeliveryPolicy, time.Duration, bool) {
	switch commandType {
	case "unlock", "lock":
		return DeliveryPolicyOnlineOnly, 10 * time.Second, false
	case "query_status":
		return DeliveryPolicyQueueUntilExpire, 15 * time.Second, true
	case "set_config":
		return DeliveryPolicyReplaceLatest, 60 * time.Second, true
	case "reboot":
		return DeliveryPolicyQueueUntilExpire, 30 * time.Second, true
	default:
		return DeliveryPolicyQueueUntilExpire, 30 * time.Second, true
	}
}

func canTransition(command Command, next CommandStatus, corrected bool, now time.Time) bool {
	switch next {
	case CommandStatusAcked:
		return command.Status == CommandStatusSent
	case CommandStatusSuccess, CommandStatusFailed:
		switch command.Status {
		case CommandStatusAcked:
			return true
		case CommandStatusSent:
			return next == CommandStatusFailed
		case CommandStatusTimeout:
			return corrected && command.CompensationUntil != nil && now.Before(*command.CompensationUntil)
		default:
			return false
		}
	default:
		return false
	}
}

func isCancellable(status CommandStatus) bool {
	return status == CommandStatusCreated || status == CommandStatusQueued || status == CommandStatusOffline
}

func compensationUntil(now time.Time, lowRisk bool) *time.Time {
	if !lowRisk {
		return nil
	}
	until := now.Add(5 * time.Minute)
	return &until
}

func defaultString(value, fallback string) string {
	if strings.TrimSpace(value) == "" {
		return fallback
	}
	return strings.TrimSpace(value)
}

func defaultTransportProtocol(accessType, requested string) string {
	requested = strings.TrimSpace(requested)
	if requested != "" {
		return requested
	}
	return TransportProtocolSimulator
}

func defaultAdapter(accessType, requested string) string {
	requested = strings.TrimSpace(requested)
	if requested != "" {
		return requested
	}
	return AdapterMockGateway
}

func defaultProviderCode(accessType, requested string) string {
	requested = strings.TrimSpace(requested)
	if requested != "" {
		return requested
	}
	return defaultSimulatorProviderCode
}

func defaultConnectionStatus(requested string, online bool) string {
	requested = strings.TrimSpace(requested)
	if requested != "" {
		return requested
	}
	if online {
		return ConnectionStatusOnline
	}
	return ConnectionStatusUnknown
}

func validAccessType(value string) bool {
	switch value {
	case AccessTypeMockGateway:
		return true
	default:
		return false
	}
}

func validTransportProtocol(value string) bool {
	switch value {
	case TransportProtocolSimulator:
		return true
	default:
		return false
	}
}

func validAdapter(value string) bool {
	switch value {
	case AdapterMockGateway:
		return true
	default:
		return false
	}
}

func validAccessAdapterPair(accessType, adapter string) bool {
	switch accessType {
	case AccessTypeMockGateway:
		return adapter == AdapterMockGateway
	default:
		return false
	}
}

func validConnectionStatus(value string) bool {
	switch value {
	case ConnectionStatusUnknown, ConnectionStatusOnline, ConnectionStatusOffline:
		return true
	default:
		return false
	}
}

func validLifecycleStatus(value string) bool {
	switch value {
	case LifecycleStatusActive, LifecycleStatusDisabled, LifecycleStatusDeleted:
		return true
	default:
		return false
	}
}

func idempotencyScope(projectID, key string) string {
	return projectID + "\x00" + key
}

func commandRequestHash(deviceID, commandType string, payload map[string]any) (string, error) {
	canonical := map[string]any{
		"command_type": commandType,
		"device_id":    deviceID,
		"payload":      cloneMap(payload),
	}
	bytes, err := json.Marshal(canonical)
	if err != nil {
		return "", fmt.Errorf("%w: command payload must be JSON encodable", ErrInvalidArgument)
	}
	sum := sha256.Sum256(bytes)
	return hex.EncodeToString(sum[:]), nil
}

func newID(prefix string) string {
	var b [12]byte
	if _, err := rand.Read(b[:]); err != nil {
		return fmt.Sprintf("%s_%d", prefix, time.Now().UnixNano())
	}
	return prefix + "_" + hex.EncodeToString(b[:])
}

func newAPIKey() string {
	return "dp_" + newID("key")
}

func cloneDevice(device Device) Device {
	device.CurrentState = cloneMap(device.CurrentState)
	return device
}

func cloneCommand(command Command) Command {
	command.Payload = cloneMap(command.Payload)
	command.Attempts = append([]CommandAttempt(nil), command.Attempts...)
	command.Events = append([]CommandEvent(nil), command.Events...)
	return command
}

func cloneStrings(values []string) []string {
	if values == nil {
		return nil
	}
	return append([]string(nil), values...)
}

func cloneMap(values map[string]any) map[string]any {
	if values == nil {
		return nil
	}
	cloned := make(map[string]any, len(values))
	for key, value := range values {
		cloned[key] = value
	}
	return cloned
}
