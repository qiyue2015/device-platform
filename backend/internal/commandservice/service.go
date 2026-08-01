package commandservice

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/qiyue2015/device-platform/internal/domain"
	"github.com/qiyue2015/device-platform/internal/storage/repository"
)

type realClock struct{}

func (realClock) Now() time.Time { return time.Now().UTC() }

type Service struct {
	store     repository.CommandStore
	providers map[string]domain.Provider
	random    io.Reader
	clock     Clock
}

func New(store repository.CommandStore, config Config) (*Service, error) {
	if store == nil {
		return nil, fmt.Errorf("%w: store is required", ErrInvalidRequest)
	}
	providers := make(map[string]domain.Provider, len(config.Providers))
	for _, provider := range config.Providers {
		if strings.TrimSpace(provider.Code) == "" {
			return nil, fmt.Errorf("%w: Provider registry is invalid", ErrInvalidRequest)
		}
		providers[provider.Code] = provider
	}
	if config.Random == nil {
		config.Random = rand.Reader
	}
	if config.Clock == nil {
		config.Clock = realClock{}
	}
	return &Service{store: store, providers: providers, random: config.Random, clock: config.Clock}, nil
}

func (s *Service) Create(ctx context.Context, scope Scope, request CreateRequest, metadata RequestMetadata) (CreateResult, error) {
	scope, err := validateScope(scope)
	if err != nil {
		return CreateResult{}, err
	}
	metadata, err = validateMetadata(scope, metadata)
	if err != nil {
		return CreateResult{}, err
	}
	projectID, err := projectForCreate(scope, request.ProjectID)
	if err != nil {
		return CreateResult{}, err
	}
	deviceID := strings.TrimSpace(request.DeviceID)
	if !validUUID(deviceID) {
		return CreateResult{}, fmt.Errorf("%w: device_id is invalid", ErrInvalidRequest)
	}
	commandType, err := normalizeCommandType(request.CommandType)
	if err != nil {
		return CreateResult{}, err
	}
	payload, err := normalizePayload(request.Payload)
	if err != nil {
		return CreateResult{}, err
	}
	idempotencyKey, err := normalizeIdempotencyKey(request.IdempotencyKey)
	if err != nil {
		return CreateResult{}, err
	}
	var result CreateResult
	err = s.store.TransactCommand(ctx, func(tx repository.CommandTx) error {
		if replay, found, replayErr := findIdempotentReplay(
			ctx, tx.Commands(), projectID, idempotencyKey, deviceID, commandType, payload,
		); replayErr != nil {
			return replayErr
		} else if found {
			result = replay
			return nil
		}
		if _, lockErr := tx.Projects().GetForUpdate(ctx, projectID); errors.Is(lockErr, sql.ErrNoRows) {
			return ErrProjectNotFound
		} else if lockErr != nil {
			return lockErr
		}
		if replay, found, replayErr := findIdempotentReplay(
			ctx, tx.Commands(), projectID, idempotencyKey, deviceID, commandType, payload,
		); replayErr != nil {
			return replayErr
		} else if found {
			result = replay
			return nil
		}
		device, deviceErr := tx.Devices().GetForUpdate(ctx, deviceID)
		if errors.Is(deviceErr, sql.ErrNoRows) || deviceErr == nil && device.ProjectID != projectID {
			return ErrDeviceNotFound
		}
		if deviceErr != nil {
			return deviceErr
		}
		if device.LifecycleStatus != domain.LifecycleStatusActive {
			return ErrDeviceDisabled
		}
		deviceType, typeErr := tx.DeviceTypes().GetByCode(ctx, device.DeviceTypeCode)
		if errors.Is(typeErr, sql.ErrNoRows) || typeErr == nil && deviceType.ID != device.DeviceTypeID {
			return ErrCapabilityUnsupported
		}
		if typeErr != nil {
			return typeErr
		}
		profile, profileErr := tx.DeviceTypes().GetProfile(ctx, deviceType.ID, deviceType.CurrentRevision)
		if profileErr != nil {
			return profileErr
		}
		action, found := findAction(profile, commandType)
		if !found {
			return ErrCapabilityUnsupported
		}
		if err := validateActionPayload(action, payload); err != nil {
			return err
		}
		provider, registered := s.providers[device.ProviderCode]
		if !registered || provider.IntegrationStatus == domain.ProviderIntegrationUnconfigured ||
			provider.Code != device.ProviderCode || provider.Adapter != device.Adapter {
			return ErrProviderNotConfigured
		}
		profileActions, registeredProfile := provider.ProfileActions[device.ProviderProfile]
		if !registeredProfile {
			return ErrProviderNotConfigured
		}
		switch profileActions[commandType] {
		case domain.ProviderActionSupported:
		case domain.ProviderActionUnsupported:
			return ErrProviderActionUnsupported
		case domain.ProviderActionMappingUnknown:
			return ErrProviderMappingUnknown
		default:
			return ErrProviderNotConfigured
		}
		requestHash, hashErr := canonicalRequestHash(device, commandType, payload, profile.Revision, action)
		if hashErr != nil {
			return hashErr
		}
		if action.DeliveryPolicy == domain.DeliveryPolicyOnlineOnly && device.ConnectionStatus != domain.ConnectionStatusOnline {
			return ErrDeviceNotOnline
		}
		commandID, idErr := randomUUID(s.random)
		if idErr != nil {
			return idErr
		}
		now := s.clock.Now().UTC()
		command := domain.Command{
			ID: commandID, ProjectID: projectID, DeviceID: device.ID, DeviceTypeID: deviceType.ID,
			DeviceTypeCode: deviceType.Code, ProviderCode: device.ProviderCode,
			ProviderProfile: device.ProviderProfile, ProviderDeviceID: device.ProviderDeviceID, Adapter: device.Adapter,
			CommandType: commandType, Payload: payload, DeviceTypeRevision: profile.Revision,
			DeliveryPolicy: action.DeliveryPolicy, DispatchDeadline: action.DispatchDeadline,
			ProviderTimeout: action.ProviderRequestTimeout, ResultTimeout: action.ResultObservationTimeout,
			RetryAllowed: action.RetryAllowed, Status: domain.CommandStatusQueued,
			ConfirmationLevel: domain.ConfirmationNone, EvidenceStatus: domain.EvidenceNone,
			IdempotencyKey: idempotencyKey, RequestHash: requestHash, QueuedAt: now,
			DispatchDeadlineAt: now.Add(action.DispatchDeadline), CreatedAt: now, UpdatedAt: now,
		}
		if createErr := tx.Commands().Create(ctx, command); createErr != nil {
			return createErr
		}
		if eventErr := s.createEvent(ctx, tx, metadata, command, domain.CommandStatus(""), domain.CommandStatusQueued, nil); eventErr != nil {
			return eventErr
		}
		if auditErr := s.createAudit(ctx, tx, metadata, command, "command.created", map[string]any{
			"command_type": command.CommandType, "delivery_policy": command.DeliveryPolicy,
		}); auditErr != nil {
			return auditErr
		}
		result = CreateResult{Command: command}
		return nil
	})
	return result, err
}

func findIdempotentReplay(
	ctx context.Context,
	commands repository.CommandQueries,
	projectID, idempotencyKey, deviceID string,
	commandType domain.ActionIdentifier,
	payload map[string]any,
) (CreateResult, bool, error) {
	existing, err := commands.GetByIdempotencyKey(ctx, projectID, idempotencyKey)
	if errors.Is(err, sql.ErrNoRows) {
		return CreateResult{}, false, nil
	}
	if err != nil {
		return CreateResult{}, false, err
	}
	existingPayload, err := json.Marshal(existing.Payload)
	if err != nil {
		return CreateResult{}, false, fmt.Errorf("encode historical Command payload: %w", err)
	}
	requestPayload, err := json.Marshal(payload)
	if err != nil {
		return CreateResult{}, false, fmt.Errorf("encode replay Command payload: %w", err)
	}
	if existing.DeviceID != deviceID || existing.CommandType != commandType || !bytes.Equal(existingPayload, requestPayload) {
		return CreateResult{}, false, ErrIdempotencyKeyConflict
	}
	return CreateResult{Command: existing, IdempotentReplay: true}, true, nil
}

func (s *Service) List(ctx context.Context, scope Scope, request ListRequest) (ListResult, error) {
	scope, err := validateScope(scope)
	if err != nil {
		return ListResult{}, err
	}
	request, err = validateListRequest(scope, request)
	if err != nil {
		return ListResult{}, err
	}
	items, total, err := s.store.Commands().List(ctx, repository.ListCommandsRequest{
		ProjectID: request.ProjectID, DeviceID: request.DeviceID, CommandType: request.CommandType,
		Status: request.Status, Limit: request.PageSize, Offset: (request.Page - 1) * request.PageSize,
	})
	if err != nil {
		return ListResult{}, err
	}
	return ListResult{Items: items, Page: request.Page, PageSize: request.PageSize, Total: total}, nil
}

func (s *Service) Get(ctx context.Context, scope Scope, commandID string) (Detail, error) {
	scope, err := validateScope(scope)
	if err != nil || !validUUID(commandID) {
		return Detail{}, ErrInvalidRequest
	}
	command, err := s.store.Commands().Get(ctx, commandID)
	if errors.Is(err, sql.ErrNoRows) || err == nil && scope.Kind == ScopeProject && command.ProjectID != scope.ProjectID {
		return Detail{}, ErrCommandNotFound
	}
	if err != nil {
		return Detail{}, err
	}
	attempts, err := s.store.Commands().ListAttempts(ctx, commandID)
	if err != nil {
		return Detail{}, err
	}
	results, err := s.store.Commands().ListResults(ctx, commandID)
	if err != nil {
		return Detail{}, err
	}
	events, err := s.store.Events().ListByCommand(ctx, commandID)
	if err != nil {
		return Detail{}, err
	}
	return Detail{Command: command, Attempts: attempts, Results: results, Events: events}, nil
}

func (s *Service) Cancel(ctx context.Context, scope Scope, commandID string, metadata RequestMetadata) (domain.Command, error) {
	scope, err := validateScope(scope)
	if err != nil || !validUUID(commandID) {
		return domain.Command{}, ErrInvalidRequest
	}
	metadata, err = validateMetadata(scope, metadata)
	if err != nil {
		return domain.Command{}, err
	}
	var cancelled domain.Command
	err = s.store.TransactCommand(ctx, func(tx repository.CommandTx) error {
		current, lockErr := tx.Commands().GetForUpdate(ctx, commandID)
		if errors.Is(lockErr, sql.ErrNoRows) || lockErr == nil && scope.Kind == ScopeProject && current.ProjectID != scope.ProjectID {
			return ErrCommandNotFound
		}
		if lockErr != nil {
			return lockErr
		}
		updated, cancelErr := tx.Commands().CancelQueued(ctx, commandID, nil)
		if cancelErr != nil {
			return cancelErr
		}
		if !updated {
			return ErrCommandNotCancellable
		}
		cancelled, err = tx.Commands().Get(ctx, commandID)
		if err != nil {
			return err
		}
		if eventErr := s.createEvent(ctx, tx, metadata, cancelled, current.Status, cancelled.Status, cancelled.ReasonCode); eventErr != nil {
			return eventErr
		}
		return s.createAudit(ctx, tx, metadata, cancelled, "command.cancelled", map[string]any{
			"reason_code": cancelled.ReasonCode,
		})
	})
	return cancelled, err
}

func findAction(profile domain.DeviceTypeProfile, commandType domain.ActionIdentifier) (domain.CapabilityAction, bool) {
	for _, action := range profile.Actions {
		if action.Identifier == commandType {
			return action, true
		}
	}
	return domain.CapabilityAction{}, false
}

func validateActionPayload(action domain.CapabilityAction, payload map[string]any) error {
	objectType, typeOK := action.PayloadSchema["type"].(string)
	additional, additionalOK := action.PayloadSchema["additionalProperties"].(bool)
	maxProperties, maxOK := action.PayloadSchema["maxProperties"].(float64)
	if !maxOK {
		if value, ok := action.PayloadSchema["maxProperties"].(int); ok {
			maxProperties, maxOK = float64(value), true
		}
	}
	if objectType != "object" || !typeOK || !additionalOK || additional || !maxOK || maxProperties != 0 || len(payload) != 0 {
		return ErrPayloadInvalid
	}
	return nil
}

func canonicalRequestHash(device domain.Device, commandType domain.ActionIdentifier, payload map[string]any, revision int, action domain.CapabilityAction) ([]byte, error) {
	encoded, err := json.Marshal(map[string]any{
		"command_type":                  commandType,
		"delivery_policy":               action.DeliveryPolicy,
		"device_id":                     device.ID,
		"device_type_code":              device.DeviceTypeCode,
		"device_type_revision":          revision,
		"dispatch_deadline_ms":          action.DispatchDeadline.Milliseconds(),
		"normalized_payload":            payload,
		"provider_code":                 device.ProviderCode,
		"provider_profile":              device.ProviderProfile,
		"provider_device_id":            device.ProviderDeviceID,
		"provider_request_timeout_ms":   action.ProviderRequestTimeout.Milliseconds(),
		"result_observation_timeout_ms": action.ResultObservationTimeout.Milliseconds(),
		"retry_allowed":                 action.RetryAllowed,
	})
	if err != nil {
		return nil, fmt.Errorf("encode canonical Command request: %w", err)
	}
	digest := sha256.Sum256(encoded)
	return digest[:], nil
}

func (s *Service) createEvent(ctx context.Context, tx repository.CommandTx, metadata RequestMetadata, command domain.Command, from, to domain.CommandStatus, reasonCode *string) error {
	eventID, err := randomUUID(s.random)
	if err != nil {
		return err
	}
	deviceID, commandID := command.DeviceID, command.ID
	eventType := domain.EventTypeCommandStatusChanged
	payload := map[string]any{
		"from": from, "to": to, "reason_code": reasonCode,
		"confirmation_level": command.ConfirmationLevel, "evidence_status": command.EvidenceStatus,
	}
	deduplicationKey := "command.status_changed:" + command.ID + ":" + string(to)
	if from == "" {
		eventType = domain.EventTypeCommandCreated
		payload = map[string]any{
			"command_type": command.CommandType, "delivery_policy": command.DeliveryPolicy, "status": command.Status,
		}
		deduplicationKey = "command.created:" + command.ID
	}
	now := s.clock.Now().UTC()
	source := domain.EventSourceAdmin
	if metadata.ActorType == domain.ActorTypeProject {
		source = domain.EventSourceOpenAPI
	}
	event := domain.Event{
		ID: eventID, SchemaVersion: domain.EventSchemaVersion, EventType: eventType,
		ProjectID: command.ProjectID, DeviceID: &deviceID, CommandID: &commandID, Source: source,
		Payload: payload, DeduplicationKey: deduplicationKey, OccurredAt: now, CreatedAt: now,
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
		return fmt.Errorf("encode Command Event envelope: %w", err)
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

func (s *Service) createAudit(ctx context.Context, tx repository.CommandTx, metadata RequestMetadata, command domain.Command, action string, fields map[string]any) error {
	id, err := randomUUID(s.random)
	if err != nil {
		return err
	}
	projectID, resourceID := command.ProjectID, command.ID
	return tx.Audits().Create(ctx, domain.AuditLog{
		ID: id, ActorType: metadata.ActorType, ActorID: optionalString(metadata.ActorID), ProjectID: &projectID,
		Action: action, Result: domain.AuditResultSuccess, ResourceType: "command", ResourceID: &resourceID,
		IPAddress: optionalString(metadata.IPAddress), RequestID: optionalString(metadata.RequestID),
		Metadata: fields, OccurredAt: s.clock.Now().UTC(),
	})
}

func randomUUID(reader io.Reader) (string, error) {
	var value [16]byte
	if _, err := io.ReadFull(reader, value[:]); err != nil {
		return "", fmt.Errorf("%w: %v", ErrIdentifierGeneration, err)
	}
	value[6] = value[6]&0x0f | 0x40
	value[8] = value[8]&0x3f | 0x80
	encoded := hex.EncodeToString(value[:])
	return encoded[0:8] + "-" + encoded[8:12] + "-" + encoded[12:16] + "-" + encoded[16:20] + "-" + encoded[20:32], nil
}

func optionalString(value string) *string {
	if strings.TrimSpace(value) == "" {
		return nil
	}
	copy := value
	return &copy
}
