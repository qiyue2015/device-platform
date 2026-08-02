package webhookaudit

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"math"
	"net/netip"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/qiyue2015/device-platform/internal/access"
	"github.com/qiyue2015/device-platform/internal/domain"
	"github.com/qiyue2015/device-platform/internal/storage/repository"
)

type PersistentService struct {
	store  repository.WebhookAuditStore
	random io.Reader
	clock  Clock
}

type realClock struct{}

func (realClock) Now() time.Time { return time.Now().UTC() }

func NewPersistentService(store repository.WebhookAuditStore) *PersistentService {
	return &PersistentService{store: store, random: rand.Reader, clock: realClock{}}
}

func newPersistentService(store repository.WebhookAuditStore, config PersistentConfig) (*PersistentService, error) {
	if store == nil {
		return nil, fmt.Errorf("%w: store is required", ErrInvalidRequest)
	}
	if config.Random == nil {
		config.Random = rand.Reader
	}
	if config.Clock == nil {
		config.Clock = realClock{}
	}
	return &PersistentService{store: store, random: config.Random, clock: config.Clock}, nil
}

func (s *PersistentService) ListEvents(ctx context.Context, scope Scope, request EventListRequest) (ListResult[PersistentEvent], error) {
	if err := validateHumanScope(scope); err != nil {
		return ListResult[PersistentEvent]{}, err
	}
	page, pageSize, err := normalizePagination(request.Page, request.PageSize)
	if err != nil {
		return ListResult[PersistentEvent]{}, err
	}
	if !validOptionalUUID(request.ProjectID) || !validOptionalUUID(request.DeviceID) || !validOptionalUUID(request.CommandID) {
		return ListResult[PersistentEvent]{}, ErrInvalidRequest
	}
	if err := s.authorizeProjectFilter(ctx, scope, request.ProjectID); err != nil {
		return ListResult[PersistentEvent]{}, err
	}
	var eventType *domain.EventType
	if request.EventType != nil {
		value := domain.EventType(*request.EventType)
		if !validEventType(value) {
			return ListResult[PersistentEvent]{}, ErrInvalidRequest
		}
		eventType = &value
	}
	items, total, err := s.store.Events().List(ctx, repository.ListEventsRequest{
		ProjectID: request.ProjectID, ManagerUserID: scope.ManagerUserID(), DeviceID: request.DeviceID, CommandID: request.CommandID,
		EventType: eventType, Limit: pageSize, Offset: (page - 1) * pageSize,
	})
	if err != nil {
		return ListResult[PersistentEvent]{}, err
	}
	result := make([]PersistentEvent, 0, len(items))
	for _, item := range items {
		result = append(result, safeEvent(item))
	}
	return ListResult[PersistentEvent]{Items: result, Page: page, PageSize: pageSize, Total: total}, nil
}

func (s *PersistentService) GetEvent(ctx context.Context, scope Scope, eventID string) (PersistentEvent, error) {
	if validateHumanScope(scope) != nil || !validUUID(eventID) {
		return PersistentEvent{}, ErrInvalidRequest
	}
	event, err := s.store.Events().Get(ctx, eventID)
	if errors.Is(err, sql.ErrNoRows) {
		return PersistentEvent{}, ErrResourceNotFound
	}
	if err != nil {
		return PersistentEvent{}, err
	}
	if err := s.authorizeProject(ctx, scope, event.ProjectID); err != nil {
		return PersistentEvent{}, err
	}
	return safeEvent(event), nil
}

func (s *PersistentService) ListDeliveries(ctx context.Context, scope Scope, request DeliveryListRequest) (ListResult[PersistentDelivery], error) {
	if err := validateHumanScope(scope); err != nil {
		return ListResult[PersistentDelivery]{}, err
	}
	page, pageSize, err := normalizePagination(request.Page, request.PageSize)
	if err != nil {
		return ListResult[PersistentDelivery]{}, err
	}
	if !validOptionalUUID(request.ProjectID) || !validOptionalUUID(request.EventID) {
		return ListResult[PersistentDelivery]{}, ErrInvalidRequest
	}
	if err := s.authorizeProjectFilter(ctx, scope, request.ProjectID); err != nil {
		return ListResult[PersistentDelivery]{}, err
	}
	var status *domain.WebhookDeliveryStatus
	if request.Status != nil {
		value := domain.WebhookDeliveryStatus(*request.Status)
		if !validDeliveryStatus(value) {
			return ListResult[PersistentDelivery]{}, ErrInvalidRequest
		}
		status = &value
	}
	items, total, err := s.store.Webhooks().ListDeliveries(ctx, repository.ListWebhookDeliveriesRequest{
		ProjectID: request.ProjectID, ManagerUserID: scope.ManagerUserID(), EventID: request.EventID, Status: status,
		Limit: pageSize, Offset: (page - 1) * pageSize,
	})
	if err != nil {
		return ListResult[PersistentDelivery]{}, err
	}
	result := make([]PersistentDelivery, 0, len(items))
	for _, item := range items {
		result = append(result, safeDelivery(item, nil))
	}
	return ListResult[PersistentDelivery]{Items: result, Page: page, PageSize: pageSize, Total: total}, nil
}

func (s *PersistentService) GetDelivery(ctx context.Context, scope Scope, deliveryID string) (PersistentDelivery, error) {
	if validateHumanScope(scope) != nil || !validUUID(deliveryID) {
		return PersistentDelivery{}, ErrInvalidRequest
	}
	delivery, err := s.store.Webhooks().GetDelivery(ctx, deliveryID)
	if errors.Is(err, sql.ErrNoRows) {
		return PersistentDelivery{}, ErrResourceNotFound
	}
	if err != nil {
		return PersistentDelivery{}, err
	}
	if err := s.authorizeProject(ctx, scope, delivery.ProjectID); err != nil {
		return PersistentDelivery{}, err
	}
	attempts, err := s.store.Webhooks().ListAttempts(ctx, deliveryID)
	if err != nil {
		return PersistentDelivery{}, err
	}
	return safeDelivery(delivery, attempts), nil
}

func (s *PersistentService) ReplayDead(ctx context.Context, scope Scope, deliveryID string, request ReplayRequest) (PersistentDelivery, error) {
	if validateHumanScope(scope) != nil || !validUUID(deliveryID) {
		return PersistentDelivery{}, ErrInvalidRequest
	}
	request, err := normalizeReplayRequest(request)
	if err != nil {
		return PersistentDelivery{}, err
	}
	current, err := s.store.Webhooks().GetDelivery(ctx, deliveryID)
	if errors.Is(err, sql.ErrNoRows) {
		return PersistentDelivery{}, ErrResourceNotFound
	}
	if err != nil {
		return PersistentDelivery{}, err
	}
	if err := s.authorizeProject(ctx, scope, current.ProjectID); err != nil {
		return PersistentDelivery{}, err
	}
	if !scope.IsSuperAdmin() {
		return PersistentDelivery{}, ErrForbidden
	}
	if scope.UserID != "" && scope.UserID != request.ActorUserID {
		return PersistentDelivery{}, ErrInvalidRequest
	}
	deliveryIDNew, err := randomUUID(s.random)
	if err != nil {
		return PersistentDelivery{}, err
	}
	auditID, err := randomUUID(s.random)
	if err != nil {
		return PersistentDelivery{}, err
	}
	var replay domain.WebhookDelivery
	err = s.store.TransactWebhookAudit(ctx, func(tx repository.WebhookAuditTx) error {
		var createErr error
		replay, createErr = tx.Webhooks().CreateReplay(ctx, repository.CreateWebhookReplayRequest{
			ID: deliveryIDNew, ReplayOfDeliveryID: deliveryID,
		})
		if createErr != nil {
			return createErr
		}
		projectID, resourceID := replay.ProjectID, replay.ID
		return tx.Audits().Create(ctx, domain.AuditLog{
			ID: auditID, ActorType: domain.ActorTypeUser, ActorUserID: optionalString(request.ActorUserID), ProjectID: &projectID,
			Action: "webhook.delivery_replayed", Result: domain.AuditResultSuccess,
			ResourceType: "webhook_delivery", ResourceID: &resourceID,
			IPAddress: optionalString(request.IPAddress), RequestID: optionalString(request.RequestID),
			Metadata:   map[string]any{"original_delivery_id": deliveryID, "event_id": replay.EventID},
			OccurredAt: s.clock.Now().UTC(),
		})
	})
	if errors.Is(err, repository.ErrWebhookDeliveryNotFound) {
		return PersistentDelivery{}, ErrResourceNotFound
	}
	if errors.Is(err, repository.ErrWebhookDeliveryNotDead) {
		return PersistentDelivery{}, ErrWebhookDeliveryNotDead
	}
	if errors.Is(err, repository.ErrWebhookNotConfigured) {
		return PersistentDelivery{}, ErrWebhookNotConfigured
	}
	if err != nil {
		return PersistentDelivery{}, err
	}
	return safeDelivery(replay, nil), nil
}

func (s *PersistentService) ListAudits(ctx context.Context, scope Scope, request AuditListRequest) (ListResult[PersistentAudit], error) {
	if err := validateHumanScope(scope); err != nil {
		return ListResult[PersistentAudit]{}, err
	}
	page, pageSize, err := normalizePagination(request.Page, request.PageSize)
	if err != nil {
		return ListResult[PersistentAudit]{}, err
	}
	if !validOptionalUUID(request.ProjectID) || !validOptionalText(request.ResourceType, 120) || !validOptionalText(request.ResourceID, 255) {
		return ListResult[PersistentAudit]{}, ErrInvalidRequest
	}
	if err := s.authorizeProjectFilter(ctx, scope, request.ProjectID); err != nil {
		return ListResult[PersistentAudit]{}, err
	}
	var actorType *domain.ActorType
	if request.ActorType != nil {
		value := domain.ActorType(*request.ActorType)
		if !validActorType(value) {
			return ListResult[PersistentAudit]{}, ErrInvalidRequest
		}
		actorType = &value
	}
	if request.Action != nil && !validAuditAction(*request.Action) {
		return ListResult[PersistentAudit]{}, ErrInvalidRequest
	}
	var result *domain.AuditResult
	if request.Result != nil {
		value := domain.AuditResult(*request.Result)
		if value != domain.AuditResultSuccess && value != domain.AuditResultFailure {
			return ListResult[PersistentAudit]{}, ErrInvalidRequest
		}
		result = &value
	}
	items, total, err := s.store.Audits().List(ctx, repository.ListAuditsRequest{
		ProjectID: request.ProjectID, ManagerUserID: scope.ManagerUserID(), ActorType: actorType, Action: request.Action, Result: result,
		ResourceType: request.ResourceType, ResourceID: request.ResourceID, Limit: pageSize, Offset: (page - 1) * pageSize,
	})
	if err != nil {
		return ListResult[PersistentAudit]{}, err
	}
	mapped := make([]PersistentAudit, 0, len(items))
	for _, item := range items {
		mapped = append(mapped, safeAudit(item))
	}
	return ListResult[PersistentAudit]{Items: mapped, Page: page, PageSize: pageSize, Total: total}, nil
}

func (s *PersistentService) GetAudit(ctx context.Context, scope Scope, auditID string) (PersistentAudit, error) {
	if validateHumanScope(scope) != nil || !validUUID(auditID) {
		return PersistentAudit{}, ErrInvalidRequest
	}
	audit, err := s.store.Audits().Get(ctx, auditID)
	if errors.Is(err, sql.ErrNoRows) {
		return PersistentAudit{}, ErrResourceNotFound
	}
	if err != nil {
		return PersistentAudit{}, err
	}
	if audit.ProjectID == nil {
		if !scope.IsSuperAdmin() {
			return PersistentAudit{}, ErrResourceNotFound
		}
	} else if err := s.authorizeProject(ctx, scope, *audit.ProjectID); err != nil {
		return PersistentAudit{}, err
	}
	return safeAudit(audit), nil
}

func safeEvent(event domain.Event) PersistentEvent {
	return PersistentEvent{
		EventID: event.ID, SchemaVersion: event.SchemaVersion, EventType: event.EventType,
		ProjectID: event.ProjectID, DeviceID: event.DeviceID, CommandID: event.CommandID,
		OccurredAt: event.OccurredAt, Source: event.Source, Payload: cloneMap(event.Payload),
	}
}

func safeDelivery(delivery domain.WebhookDelivery, attempts []domain.WebhookDeliveryAttempt) PersistentDelivery {
	result := PersistentDelivery{
		ID: delivery.ID, EventID: delivery.EventID, ProjectID: delivery.ProjectID, TargetURL: delivery.TargetURL,
		WebhookConfigVersion: delivery.WebhookConfigVersion, Status: delivery.Status, AttemptCount: delivery.AttemptCount,
		NextAttemptAt: delivery.NextAttemptAt, ReplayOfDeliveryID: delivery.ReplayOfDeliveryID, DeliveredAt: delivery.DeliveredAt,
		CreatedAt: delivery.CreatedAt, UpdatedAt: delivery.UpdatedAt,
	}
	if attempts != nil {
		mapped := make([]PersistentDeliveryAttempt, 0, len(attempts))
		for _, attempt := range attempts {
			mapped = append(mapped, PersistentDeliveryAttempt{
				AttemptNo: attempt.AttemptNo, StartedAt: attempt.StartedAt, CompletedAt: attempt.CompletedAt,
				HTTPStatus: attempt.HTTPStatus, ResponseSummary: attempt.ResponseSummary,
				ErrorCode: attempt.ErrorCode, ErrorDetail: attempt.ErrorDetail,
			})
		}
		result.Attempts = &mapped
	}
	return result
}

func safeAudit(audit domain.AuditLog) PersistentAudit {
	return PersistentAudit{
		ID: audit.ID, ActorType: audit.ActorType, ActorUserID: audit.ActorUserID, ActorID: audit.ActorID, ProjectID: audit.ProjectID,
		Action: audit.Action, Result: audit.Result, ResourceType: audit.ResourceType, ResourceID: audit.ResourceID,
		IPAddress: audit.IPAddress, RequestID: audit.RequestID, Metadata: cloneMap(audit.Metadata), OccurredAt: audit.OccurredAt,
	}
}

func validateHumanScope(scope Scope) error {
	if scope.Validate() != nil || !scope.IsHuman() || scope.UserID != "" && !validUUID(scope.UserID) {
		return ErrInvalidRequest
	}
	return nil
}

func (s *PersistentService) authorizeProjectFilter(ctx context.Context, scope Scope, projectID *string) error {
	if scope.Kind != access.ScopeUser || projectID == nil {
		return nil
	}
	return s.authorizeProject(ctx, scope, *projectID)
}

func (s *PersistentService) authorizeProject(ctx context.Context, scope Scope, projectID string) error {
	if scope.IsSuperAdmin() {
		return nil
	}
	project, err := s.store.Projects().Get(ctx, projectID)
	if errors.Is(err, sql.ErrNoRows) || err == nil && !scope.CanAccessProject(project) {
		return ErrResourceNotFound
	}
	return err
}

func normalizePagination(page, pageSize int) (int, int, error) {
	if page == 0 {
		page = 1
	}
	if pageSize == 0 {
		pageSize = 20
	}
	if page < 1 || pageSize < 1 || pageSize > 100 || page-1 > math.MaxInt/pageSize {
		return 0, 0, ErrInvalidRequest
	}
	return page, pageSize, nil
}

func normalizeReplayRequest(request ReplayRequest) (ReplayRequest, error) {
	request.ActorUserID = strings.TrimSpace(request.ActorUserID)
	request.RequestID = strings.TrimSpace(request.RequestID)
	request.IPAddress = strings.TrimSpace(request.IPAddress)
	if !validUUID(request.ActorUserID) || request.RequestID == "" || len(request.RequestID) > 255 {
		return ReplayRequest{}, ErrInvalidRequest
	}
	if request.IPAddress != "" {
		address, err := netip.ParseAddr(request.IPAddress)
		if err != nil || address.Zone() != "" {
			return ReplayRequest{}, ErrInvalidRequest
		}
		request.IPAddress = address.Unmap().String()
	}
	return request, nil
}

func validOptionalUUID(value *string) bool { return value == nil || validUUID(*value) }

func validUUID(value string) bool {
	if len(value) != 36 || value[8] != '-' || value[13] != '-' || value[18] != '-' || value[23] != '-' || value != strings.ToLower(value) {
		return false
	}
	decoded, err := hex.DecodeString(strings.ReplaceAll(value, "-", ""))
	return err == nil && len(decoded) == 16
}

func validOptionalText(value *string, maximum int) bool {
	return value == nil || (*value != "" && utf8.ValidString(*value) && utf8.RuneCountInString(*value) <= maximum)
}

func validEventType(value domain.EventType) bool {
	switch value {
	case domain.EventTypeDeviceCreated, domain.EventTypeDeviceLifecycleChanged, domain.EventTypeDeviceConnectionChanged,
		domain.EventTypeDeviceStateUpdated, domain.EventTypeCommandCreated, domain.EventTypeCommandStatusChanged,
		domain.EventTypeCommandEvidenceUpdated:
		return true
	default:
		return false
	}
}

func validDeliveryStatus(value domain.WebhookDeliveryStatus) bool {
	switch value {
	case domain.WebhookDeliveryStatusPending, domain.WebhookDeliveryStatusSending, domain.WebhookDeliveryStatusDelivered,
		domain.WebhookDeliveryStatusFailed, domain.WebhookDeliveryStatusDead:
		return true
	default:
		return false
	}
}

func validActorType(value domain.ActorType) bool {
	switch value {
	case domain.ActorTypeUser, domain.ActorTypeProject, domain.ActorTypeProvider, domain.ActorTypeSystem:
		return true
	default:
		return false
	}
}

func validAuditAction(value string) bool {
	switch value {
	case "auth.login", "auth.refresh", "auth.logout", "user.created", "user.status_changed",
		"project.created", "project.updated", "project.transferred", "project.api_key_rotated", "project.webhook_secret_rotated",
		"project.webhook_secret_decryption_failed", "device.created", "device.updated", "device.lifecycle_changed",
		"command.created", "command.cancelled", "provider.callback_rejected", "provider.message_received",
		"provider.message_rejected", "webhook.delivery_replayed", "simulator.updated":
		return true
	default:
		return false
	}
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
