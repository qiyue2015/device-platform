package deviceservice

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"strings"
	"time"

	"github.com/lib/pq"
	"github.com/qiyue2015/device-platform/internal/domain"
	"github.com/qiyue2015/device-platform/internal/storage/repository"
)

type realClock struct{}

func (realClock) Now() time.Time { return time.Now().UTC() }

type Service struct {
	store         repository.DeviceStore
	providers     map[string]ProviderRegistration
	providerOrder []string
	random        io.Reader
	clock         Clock
}

func New(store repository.DeviceStore, config Config) (*Service, error) {
	if store == nil {
		return nil, fmt.Errorf("%w: store is required", ErrInvalidRequest)
	}
	random := config.Random
	if random == nil {
		random = rand.Reader
	}
	clock := config.Clock
	if clock == nil {
		clock = realClock{}
	}
	providers := make(map[string]ProviderRegistration, len(config.Providers))
	providerOrder := make([]string, 0, len(config.Providers))
	for _, registration := range config.Providers {
		provider := cloneProvider(registration.Provider)
		provider.Code = strings.TrimSpace(provider.Code)
		if provider.Code == "" || registration.IdentityPolicy == nil || len(provider.Profiles) == 0 {
			return nil, fmt.Errorf("%w: Provider registration is incomplete", ErrInvalidRequest)
		}
		if _, exists := providers[provider.Code]; exists {
			return nil, fmt.Errorf("%w: Provider registration is duplicated", ErrInvalidRequest)
		}
		for _, profile := range provider.Profiles {
			if strings.TrimSpace(profile) == "" {
				return nil, fmt.Errorf("%w: Provider profile is invalid", ErrInvalidRequest)
			}
			if _, exists := provider.ProfileActions[profile]; !exists {
				return nil, fmt.Errorf("%w: Provider profile actions are missing", ErrInvalidRequest)
			}
		}
		registration.Provider = provider
		providers[provider.Code] = registration
		providerOrder = append(providerOrder, provider.Code)
	}
	if len(providers) == 0 {
		return nil, fmt.Errorf("%w: at least one Provider registration is required", ErrInvalidRequest)
	}
	return &Service{store: store, providers: providers, providerOrder: providerOrder, random: random, clock: clock}, nil
}

func (s *Service) ListProviders() []Provider {
	providers := make([]Provider, 0, len(s.providerOrder))
	for _, code := range s.providerOrder {
		providers = append(providers, cloneProvider(s.providers[code].Provider))
	}
	return providers
}

func (s *Service) ListDeviceTypes(ctx context.Context) ([]DeviceType, error) {
	types, err := s.store.DeviceTypes().List(ctx)
	if err != nil {
		return nil, err
	}
	result := make([]DeviceType, 0, len(types))
	for _, deviceType := range types {
		mapped, err := s.loadDeviceType(ctx, deviceType)
		if err != nil {
			return nil, err
		}
		result = append(result, mapped)
	}
	return result, nil
}

func (s *Service) GetDeviceType(ctx context.Context, code string) (DeviceType, error) {
	if code != domain.DeviceTypeSmartLock {
		return DeviceType{}, ErrDeviceTypeNotFound
	}
	deviceType, err := s.store.DeviceTypes().GetByCode(ctx, code)
	if errors.Is(err, sql.ErrNoRows) {
		return DeviceType{}, ErrDeviceTypeNotFound
	}
	if err != nil {
		return DeviceType{}, err
	}
	return s.loadDeviceType(ctx, deviceType)
}

func (s *Service) Create(ctx context.Context, scope Scope, request CreateRequest, metadata RequestMetadata) (Device, error) {
	scope, err := validateScope(scope)
	if err != nil {
		return Device{}, err
	}
	if scope.Kind != ScopeAdmin {
		return Device{}, fmt.Errorf("%w: Device creation requires admin scope", ErrInvalidRequest)
	}
	metadata, err = validateMetadata(metadata)
	if err != nil {
		return Device{}, err
	}
	if err := validateWriteActor(scope, metadata); err != nil {
		return Device{}, err
	}
	projectID, err := projectForCreate(scope, request.ProjectID)
	if err != nil {
		return Device{}, err
	}
	name, err := normalizeName(request.Name)
	if err != nil {
		return Device{}, err
	}
	deviceTypeCode := strings.TrimSpace(request.DeviceTypeCode)
	if deviceTypeCode == "" {
		return Device{}, fmt.Errorf("%w: device_type_code is required", ErrInvalidRequest)
	}
	if deviceTypeCode != domain.DeviceTypeSmartLock {
		return Device{}, ErrDeviceTypeNotFound
	}
	providerCode := strings.TrimSpace(request.ProviderCode)
	if providerCode == "" {
		return Device{}, fmt.Errorf("%w: provider_code is required", ErrInvalidRequest)
	}
	registration, exists := s.providers[providerCode]
	if !exists {
		return Device{}, ErrProviderNotFound
	}
	provider := registration.Provider
	providerProfile := strings.TrimSpace(request.ProviderProfile)
	if providerProfile == "" {
		return Device{}, fmt.Errorf("%w: provider_profile is required", ErrInvalidRequest)
	}
	if _, exists := provider.ProfileActions[providerProfile]; !exists {
		return Device{}, ErrProviderProfileNotFound
	}
	deviceID, err := randomUUID(s.random)
	if err != nil {
		return Device{}, err
	}
	providerDeviceID, err := registration.IdentityPolicy.NormalizeDeviceIdentity(request.ProviderDeviceID, deviceID)
	if err != nil {
		return Device{}, fmt.Errorf("%w: %v", ErrInvalidRequest, err)
	}
	now := s.clock.Now().UTC()
	device := domain.Device{
		ID: deviceID, ProjectID: projectID, Name: name, ProviderCode: provider.Code, ProviderProfile: providerProfile,
		ProviderDeviceID: providerDeviceID, AccessType: provider.AccessType,
		TransportProtocol: provider.TransportProtocol, Adapter: provider.Adapter,
		ConnectionStatus: domain.ConnectionStatusUnknown, LifecycleStatus: domain.LifecycleStatusActive,
		CreatedAt: now, UpdatedAt: now,
	}
	err = s.store.TransactDevice(ctx, func(tx repository.DeviceTx) error {
		if _, projectErr := tx.Projects().Get(ctx, projectID); errors.Is(projectErr, sql.ErrNoRows) {
			return ErrProjectNotFound
		} else if projectErr != nil {
			return projectErr
		}
		deviceType, typeErr := tx.DeviceTypes().GetByCode(ctx, deviceTypeCode)
		if errors.Is(typeErr, sql.ErrNoRows) {
			return ErrDeviceTypeNotFound
		}
		if typeErr != nil {
			return typeErr
		}
		if _, profileErr := tx.DeviceTypes().GetProfile(ctx, deviceType.ID, deviceType.CurrentRevision); profileErr != nil {
			return profileErr
		}
		device.DeviceTypeID = deviceType.ID
		device.DeviceTypeCode = deviceType.Code
		if createErr := tx.Devices().Create(ctx, device); providerIdentityConflict(createErr) {
			return ErrProviderDeviceConflict
		} else if createErr != nil {
			return createErr
		}
		if eventErr := s.createDeviceEvent(ctx, tx, metadata, device, domain.EventTypeDeviceCreated, map[string]any{
			"device_type_code": device.DeviceTypeCode,
			"provider_code":    device.ProviderCode,
			"provider_profile": device.ProviderProfile,
			"lifecycle_status": device.LifecycleStatus,
		}); eventErr != nil {
			return eventErr
		}
		return s.createAudit(ctx, tx, metadata, device, "device.created", map[string]any{
			"device_type_code": device.DeviceTypeCode,
			"provider_code":    device.ProviderCode,
			"provider_profile": device.ProviderProfile,
		})
	})
	if err != nil {
		return Device{}, err
	}
	return safeDevice(device, nil), nil
}

func (s *Service) Get(ctx context.Context, scope Scope, deviceID string) (Device, error) {
	scope, err := validateScope(scope)
	if err != nil || !validUUID(deviceID) {
		return Device{}, ErrInvalidRequest
	}
	var device domain.Device
	if scope.Kind == ScopeProject {
		device, err = s.store.Devices().GetByProject(ctx, scope.ProjectID, deviceID)
	} else {
		device, err = s.store.Devices().Get(ctx, deviceID)
	}
	if errors.Is(err, sql.ErrNoRows) {
		return Device{}, ErrDeviceNotFound
	}
	if err != nil {
		return Device{}, err
	}
	return s.withState(ctx, device)
}

func (s *Service) List(ctx context.Context, scope Scope, request ListRequest) (ListResult, error) {
	scope, err := validateScope(scope)
	if err != nil {
		return ListResult{}, err
	}
	request, err = s.validateListRequest(scope, request)
	if err != nil {
		return ListResult{}, err
	}
	items, total, err := s.store.Devices().List(ctx, repository.ListDevicesRequest{
		ProjectID: request.ProjectID, DeviceTypeCode: request.DeviceTypeCode, ProviderCode: request.ProviderCode,
		ConnectionStatus: request.ConnectionStatus, LifecycleStatus: request.LifecycleStatus,
		Limit: request.PageSize, Offset: (request.Page - 1) * request.PageSize,
	})
	if err != nil {
		return ListResult{}, err
	}
	result := ListResult{Items: make([]Device, 0, len(items)), Page: request.Page, PageSize: request.PageSize, Total: total}
	for _, item := range items {
		mapped, stateErr := s.withState(ctx, item)
		if stateErr != nil {
			return ListResult{}, stateErr
		}
		result.Items = append(result.Items, mapped)
	}
	return result, nil
}

func (s *Service) Update(ctx context.Context, scope Scope, deviceID string, request UpdateRequest, metadata RequestMetadata) (Device, error) {
	scope, err := validateScope(scope)
	if err != nil || !validUUID(deviceID) {
		return Device{}, ErrInvalidRequest
	}
	if scope.Kind != ScopeAdmin {
		return Device{}, fmt.Errorf("%w: Device update requires admin scope", ErrInvalidRequest)
	}
	metadata, err = validateMetadata(metadata)
	if err != nil {
		return Device{}, err
	}
	if err := validateWriteActor(scope, metadata); err != nil {
		return Device{}, err
	}
	if request.Name == nil && request.LifecycleStatus == nil {
		return Device{}, fmt.Errorf("%w: update has no fields", ErrInvalidRequest)
	}
	var name *string
	if request.Name != nil {
		normalized, normalizeErr := normalizeName(*request.Name)
		if normalizeErr != nil {
			return Device{}, normalizeErr
		}
		name = &normalized
	}
	if request.LifecycleStatus != nil && !validLifecycleStatus(*request.LifecycleStatus) {
		return Device{}, fmt.Errorf("%w: lifecycle_status is invalid", ErrInvalidRequest)
	}
	if name != nil && request.LifecycleStatus != nil && *request.LifecycleStatus == domain.LifecycleStatusDeleted {
		return Device{}, fmt.Errorf("%w: name and deleted lifecycle cannot be updated together", ErrInvalidRequest)
	}
	var updated domain.Device
	err = s.store.TransactDevice(ctx, func(tx repository.DeviceTx) error {
		current, lockErr := tx.Devices().GetForUpdate(ctx, deviceID)
		if errors.Is(lockErr, sql.ErrNoRows) || lockErr == nil && scope.Kind == ScopeProject && current.ProjectID != scope.ProjectID {
			return ErrDeviceNotFound
		}
		if lockErr != nil {
			return lockErr
		}
		if current.LifecycleStatus == domain.LifecycleStatusDeleted {
			return ErrDeviceImmutable
		}
		if name != nil {
			if current.Name != *name {
				if renameErr := tx.Devices().Rename(ctx, deviceID, *name); renameErr != nil {
					return renameErr
				}
				if auditErr := s.createAudit(ctx, tx, metadata, current, "device.updated", map[string]any{"fields": []string{"name"}}); auditErr != nil {
					return auditErr
				}
			}
		}
		if request.LifecycleStatus != nil {
			target := *request.LifecycleStatus
			if !canTransitionLifecycle(current.LifecycleStatus, target) {
				return ErrLifecycleTransition
			}
			changed, transitionErr := tx.Devices().SetLifecycleStatus(ctx, deviceID, current.LifecycleStatus, target)
			if transitionErr != nil {
				return transitionErr
			}
			if !changed {
				return ErrLifecycleTransition
			}
			if eventErr := s.createDeviceEvent(ctx, tx, metadata, current, domain.EventTypeDeviceLifecycleChanged, map[string]any{
				"from": current.LifecycleStatus, "to": target, "reason_code": "admin_requested",
			}); eventErr != nil {
				return eventErr
			}
			if auditErr := s.createAudit(ctx, tx, metadata, current, "device.lifecycle_changed", map[string]any{
				"from": current.LifecycleStatus, "to": target,
			}); auditErr != nil {
				return auditErr
			}
		}
		updated, err = tx.Devices().Get(ctx, deviceID)
		return err
	})
	if err != nil {
		return Device{}, err
	}
	return s.withState(ctx, updated)
}

func (s *Service) loadDeviceType(ctx context.Context, deviceType domain.DeviceType) (DeviceType, error) {
	if deviceType.Code != domain.DeviceTypeSmartLock || deviceType.CurrentRevision != domain.DeviceTypeSmartLockRevision {
		return DeviceType{}, fmt.Errorf("%w: unsupported published profile", ErrDeviceTypeNotFound)
	}
	profile, err := s.store.DeviceTypes().GetProfile(ctx, deviceType.ID, deviceType.CurrentRevision)
	if err != nil {
		return DeviceType{}, err
	}
	actions := make([]CapabilityAction, 0, len(profile.Actions))
	for _, action := range profile.Actions {
		actions = append(actions, CapabilityAction{
			Identifier: action.Identifier, PayloadSchema: cloneMap(action.PayloadSchema), RiskLevel: action.RiskLevel,
			DeliveryPolicy: action.DeliveryPolicy, DispatchDeadlineMS: action.DispatchDeadline.Milliseconds(),
			ProviderRequestTimeoutMS:   action.ProviderRequestTimeout.Milliseconds(),
			ResultObservationTimeoutMS: action.ResultObservationTimeout.Milliseconds(), RetryAllowed: action.RetryAllowed,
			DeliveryPolicyOverrideAllowed: action.DeliveryPolicyOverrideAllowed,
		})
	}
	return DeviceType{Code: deviceType.Code, Revision: profile.Revision, Name: deviceType.Name, Actions: actions}, nil
}

func (s *Service) withState(ctx context.Context, device domain.Device) (Device, error) {
	state, err := s.store.Devices().GetCurrentState(ctx, device.ID)
	if errors.Is(err, sql.ErrNoRows) {
		return safeDevice(device, nil), nil
	}
	if err != nil {
		return Device{}, err
	}
	if state.EvidenceStatus != domain.EvidenceVerified {
		return safeDevice(device, nil), nil
	}
	return safeDevice(device, &state), nil
}

func (s *Service) createDeviceEvent(ctx context.Context, tx repository.DeviceTx, metadata RequestMetadata, device domain.Device, eventType domain.EventType, payload map[string]any) error {
	id, err := randomUUID(s.random)
	if err != nil {
		return err
	}
	deviceID := device.ID
	now := s.clock.Now().UTC()
	source := domain.EventSourceAdmin
	if metadata.ActorType == domain.ActorTypeProject {
		source = domain.EventSourceOpenAPI
	}
	event := domain.Event{
		ID: id, SchemaVersion: domain.EventSchemaVersion, EventType: eventType,
		ProjectID: device.ProjectID, DeviceID: &deviceID, Source: source, Payload: payload,
		DeduplicationKey: string(eventType) + ":" + id, OccurredAt: now, CreatedAt: now,
	}
	if err := tx.Events().Create(ctx, event); err != nil {
		return err
	}
	rawBody, err := json.Marshal(eventEnvelope{
		EventID: event.ID, SchemaVersion: event.SchemaVersion, EventType: event.EventType,
		ProjectID: event.ProjectID, DeviceID: event.DeviceID, CommandID: event.CommandID,
		OccurredAt: event.OccurredAt, Source: event.Source, Payload: event.Payload,
	})
	if err != nil {
		return fmt.Errorf("encode Device Event envelope: %w", err)
	}
	deliveryID, err := randomUUID(s.random)
	if err != nil {
		return err
	}
	_, _, err = tx.Webhooks().CreateDelivery(ctx, repository.CreateWebhookDeliveryRequest{
		ID: deliveryID, EventID: event.ID, RawBody: rawBody,
	})
	return err
}

type eventEnvelope struct {
	EventID       string             `json:"event_id"`
	SchemaVersion int                `json:"schema_version"`
	EventType     domain.EventType   `json:"event_type"`
	ProjectID     string             `json:"project_id"`
	DeviceID      *string            `json:"device_id"`
	CommandID     *string            `json:"command_id"`
	OccurredAt    time.Time          `json:"occurred_at"`
	Source        domain.EventSource `json:"source"`
	Payload       map[string]any     `json:"payload"`
}

func (s *Service) createAudit(ctx context.Context, tx repository.DeviceTx, metadata RequestMetadata, device domain.Device, action string, fields map[string]any) error {
	id, err := randomUUID(s.random)
	if err != nil {
		return err
	}
	projectID, resourceID := device.ProjectID, device.ID
	return tx.Audits().Create(ctx, domain.AuditLog{
		ID: id, ActorType: metadata.ActorType, ActorID: optionalString(metadata.ActorID), ProjectID: &projectID,
		Action: action, Result: domain.AuditResultSuccess, ResourceType: "device", ResourceID: &resourceID,
		IPAddress: optionalString(metadata.IPAddress), RequestID: optionalString(metadata.RequestID),
		Metadata: fields, OccurredAt: s.clock.Now().UTC(),
	})
}

func projectForCreate(scope Scope, requested string) (string, error) {
	requested = strings.TrimSpace(requested)
	if scope.Kind == ScopeProject {
		if requested != "" {
			return "", fmt.Errorf("%w: Project-scoped create cannot override project_id", ErrInvalidRequest)
		}
		return scope.ProjectID, nil
	}
	if !validUUID(requested) {
		return "", fmt.Errorf("%w: project_id is invalid", ErrInvalidRequest)
	}
	return requested, nil
}

func (s *Service) validateListRequest(scope Scope, request ListRequest) (ListRequest, error) {
	if request.Page == 0 {
		request.Page = 1
	}
	if request.PageSize == 0 {
		request.PageSize = 20
	}
	if request.Page < 1 || request.PageSize < 1 || request.PageSize > 100 || request.Page-1 > math.MaxInt/request.PageSize {
		return ListRequest{}, fmt.Errorf("%w: pagination is invalid", ErrInvalidRequest)
	}
	if scope.Kind == ScopeProject {
		if request.ProjectID != nil {
			return ListRequest{}, fmt.Errorf("%w: Project scope cannot override project_id", ErrInvalidRequest)
		}
		projectID := scope.ProjectID
		request.ProjectID = &projectID
	} else if request.ProjectID != nil && !validUUID(*request.ProjectID) {
		return ListRequest{}, fmt.Errorf("%w: project_id filter is invalid", ErrInvalidRequest)
	}
	if request.DeviceTypeCode != nil && *request.DeviceTypeCode != domain.DeviceTypeSmartLock {
		return ListRequest{}, fmt.Errorf("%w: device_type_code filter is invalid", ErrInvalidRequest)
	}
	if request.ProviderCode != nil {
		providerCode := strings.TrimSpace(*request.ProviderCode)
		if _, exists := s.providers[providerCode]; !exists {
			return ListRequest{}, fmt.Errorf("%w: provider_code filter is invalid", ErrInvalidRequest)
		}
		request.ProviderCode = &providerCode
	}
	if request.ConnectionStatus != nil && !validConnectionStatus(*request.ConnectionStatus) {
		return ListRequest{}, fmt.Errorf("%w: connection_status filter is invalid", ErrInvalidRequest)
	}
	if request.LifecycleStatus != nil && !validLifecycleStatus(*request.LifecycleStatus) {
		return ListRequest{}, fmt.Errorf("%w: lifecycle_status filter is invalid", ErrInvalidRequest)
	}
	return request, nil
}

func safeDevice(device domain.Device, state *domain.DeviceState) Device {
	result := Device{
		ID: device.ID, ProjectID: device.ProjectID, DeviceTypeCode: device.DeviceTypeCode,
		Name: device.Name, ProviderCode: device.ProviderCode, ProviderProfile: device.ProviderProfile,
		ProviderDeviceID: device.ProviderDeviceID,
		AccessType:       device.AccessType, TransportProtocol: device.TransportProtocol, Adapter: device.Adapter,
		ConnectionStatus: device.ConnectionStatus, LifecycleStatus: device.LifecycleStatus,
		CreatedAt: device.CreatedAt, UpdatedAt: device.UpdatedAt,
	}
	if state != nil {
		var reportedAt *time.Time
		if state.ReportedAt != nil {
			value := state.ReportedAt.UTC()
			reportedAt = &value
		}
		result.CurrentState = &DeviceState{
			State:          cloneMap(state.State),
			EvidenceStatus: state.EvidenceStatus,
			ReportedAt:     reportedAt,
			ObservedAt:     state.ObservedAt.UTC(),
		}
		lastSeen := state.ObservedAt.UTC()
		result.LastSeenAt = &lastSeen
	}
	return result
}

func cloneProvider(provider Provider) Provider {
	provider.Profiles = append([]string(nil), provider.Profiles...)
	clonedActions := make(map[string]map[domain.ActionIdentifier]domain.ProviderActionAvailability, len(provider.ProfileActions))
	for profile, actions := range provider.ProfileActions {
		cloned := make(map[domain.ActionIdentifier]domain.ProviderActionAvailability, len(actions))
		for action, availability := range actions {
			cloned[action] = availability
		}
		clonedActions[profile] = cloned
	}
	provider.ProfileActions = clonedActions
	return provider
}

func cloneMap(value map[string]any) map[string]any {
	if value == nil {
		return nil
	}
	result := make(map[string]any, len(value))
	for key, item := range value {
		result[key] = item
	}
	return result
}

func optionalString(value string) *string {
	if value == "" {
		return nil
	}
	copy := value
	return &copy
}

func providerIdentityConflict(err error) bool {
	var pqError *pq.Error
	if !errors.As(err, &pqError) {
		return false
	}
	return pqError.Constraint == "uq_devices_provider_identity" || pqError.Constraint == "uq_devices_active_provider_identity"
}

func randomUUID(reader io.Reader) (string, error) {
	value := make([]byte, 16)
	if _, err := io.ReadFull(reader, value); err != nil {
		return "", fmt.Errorf("%w: %v", ErrIdentifierGeneration, err)
	}
	value[6] = (value[6] & 0x0f) | 0x40
	value[8] = (value[8] & 0x3f) | 0x80
	return fmt.Sprintf("%08x-%04x-%04x-%04x-%012x", value[0:4], value[4:6], value[6:8], value[8:10], value[10:16]), nil
}
