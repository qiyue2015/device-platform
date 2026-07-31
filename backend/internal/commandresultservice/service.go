package commandresultservice

import (
	"context"
	"crypto/rand"
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
	store  repository.CommandStore
	random io.Reader
	clock  Clock
}

func New(store repository.CommandStore, config Config) (*Service, error) {
	if store == nil {
		return nil, ErrInvalidResult
	}
	if config.Random == nil {
		config.Random = rand.Reader
	}
	if config.Clock == nil {
		config.Clock = realClock{}
	}
	return &Service{store: store, random: config.Random, clock: config.Clock}, nil
}

func (s *Service) Record(ctx context.Context, request RecordRequest) (RecordResult, error) {
	request, confirmation, err := normalize(request, s.clock.Now().UTC())
	if err != nil {
		return RecordResult{}, err
	}
	resultID, err := randomUUID(s.random)
	if err != nil {
		return RecordResult{}, err
	}
	eventID, err := randomUUID(s.random)
	if err != nil {
		return RecordResult{}, err
	}
	deliveryID, err := randomUUID(s.random)
	if err != nil {
		return RecordResult{}, err
	}

	var recorded RecordResult
	err = s.store.TransactCommand(ctx, func(tx repository.CommandTx) error {
		command, lockErr := tx.Commands().GetForUpdate(ctx, request.CommandID)
		if errors.Is(lockErr, sql.ErrNoRows) {
			return ErrCommandMissing
		}
		if lockErr != nil {
			return lockErr
		}
		existing, existingErr := tx.Results().GetByDeduplicationKey(ctx, request.Source, request.DeduplicationKey)
		if existingErr == nil {
			if !sameResult(existing, request, confirmation) {
				return ErrResultConflict
			}
			recorded = RecordResult{Result: existing, Command: command, IdempotentReplay: true}
			return nil
		}
		if !errors.Is(existingErr, sql.ErrNoRows) {
			return existingErr
		}
		late := command.Status.IsTerminal()
		result := domain.CommandResult{
			ID: resultID, CommandID: command.ID, AttemptID: request.AttemptID,
			Source: request.Source, Outcome: request.Outcome, ConfirmationLevel: confirmation,
			EvidenceStatus: domain.EvidenceVerified, DeduplicationKey: request.DeduplicationKey,
			ReportedAt: request.ReportedAt, ObservedAt: request.ObservedAt, Late: late,
			Payload: request.Payload, CreatedAt: request.ObservedAt,
		}
		if createErr := tx.Results().Create(ctx, result); createErr != nil {
			if errors.Is(createErr, repository.ErrCommandResultConflict) {
				return ErrResultConflict
			}
			return createErr
		}
		aggregation, applyErr := tx.Commands().ApplyResult(ctx, result)
		if applyErr != nil {
			return applyErr
		}
		if eventErr := createResultEvent(ctx, tx, eventID, deliveryID, result, aggregation.Command); eventErr != nil {
			return eventErr
		}
		recorded = RecordResult{Result: result, Command: aggregation.Command}
		return nil
	})
	return recorded, err
}

func normalize(request RecordRequest, now time.Time) (RecordRequest, domain.ConfirmationLevel, error) {
	request.CommandID = strings.TrimSpace(request.CommandID)
	request.DeduplicationKey = strings.TrimSpace(request.DeduplicationKey)
	if request.CommandID == "" || request.DeduplicationKey == "" || len(request.DeduplicationKey) > 256 {
		return RecordRequest{}, "", ErrInvalidResult
	}
	if request.AttemptID != nil {
		value := strings.TrimSpace(*request.AttemptID)
		if value == "" {
			return RecordRequest{}, "", ErrInvalidResult
		}
		request.AttemptID = &value
	}
	switch request.Source {
	case domain.EventSourceProviderCallback, domain.EventSourceSimulator, domain.EventSourceSystem:
	default:
		return RecordRequest{}, "", ErrInvalidResult
	}
	var confirmation domain.ConfirmationLevel
	switch request.Outcome {
	case domain.ResultOutcomeDeviceAcked:
		confirmation = domain.ConfirmationDeviceAcked
	case domain.ResultOutcomeDeviceSucceeded, domain.ResultOutcomeDeviceFailed:
		confirmation = domain.ConfirmationDeviceFinal
	default:
		return RecordRequest{}, "", ErrInvalidResult
	}
	if request.ObservedAt.IsZero() {
		request.ObservedAt = now
	}
	request.ObservedAt = request.ObservedAt.UTC()
	if request.ReportedAt != nil {
		reported := request.ReportedAt.UTC()
		if reported.After(request.ObservedAt) {
			return RecordRequest{}, "", ErrInvalidResult
		}
		request.ReportedAt = &reported
	}
	if request.Payload == nil {
		request.Payload = map[string]any{}
	}
	if _, err := json.Marshal(request.Payload); err != nil {
		return RecordRequest{}, "", ErrInvalidResult
	}
	return request, confirmation, nil
}

func sameResult(existing domain.CommandResult, request RecordRequest, confirmation domain.ConfirmationLevel) bool {
	if existing.CommandID != request.CommandID || existing.Source != request.Source || existing.Outcome != request.Outcome ||
		existing.ConfirmationLevel != confirmation || existing.EvidenceStatus != domain.EvidenceVerified ||
		!sameOptionalString(existing.AttemptID, request.AttemptID) {
		return false
	}
	left, leftErr := json.Marshal(existing.Payload)
	right, rightErr := json.Marshal(request.Payload)
	return leftErr == nil && rightErr == nil && string(left) == string(right)
}

func sameOptionalString(left, right *string) bool {
	return left == nil && right == nil || left != nil && right != nil && *left == *right
}

func createResultEvent(ctx context.Context, tx repository.CommandTx, eventID, deliveryID string, result domain.CommandResult, command domain.Command) error {
	deviceID, commandID := command.DeviceID, command.ID
	event := domain.Event{
		ID: eventID, SchemaVersion: domain.EventSchemaVersion, EventType: domain.EventTypeCommandResultRecorded,
		ProjectID: command.ProjectID, DeviceID: &deviceID, CommandID: &commandID, Source: result.Source,
		Payload: map[string]any{
			"status": command.Status, "result_id": result.ID, "outcome": result.Outcome,
			"confirmation_level": result.ConfirmationLevel, "evidence_status": result.EvidenceStatus, "late": result.Late,
		},
		DeduplicationKey: "command.result_recorded:" + command.ID + ":result:" + result.ID,
		OccurredAt:       result.ObservedAt, CreatedAt: result.ObservedAt,
	}
	if err := tx.Events().Create(ctx, event); err != nil {
		return err
	}
	rawBody, err := json.Marshal(struct {
		EventID       string             `json:"event_id"`
		SchemaVersion int                `json:"schema_version"`
		EventType     domain.EventType   `json:"event_type"`
		ProjectID     string             `json:"project_id"`
		DeviceID      *string            `json:"device_id"`
		CommandID     *string            `json:"command_id"`
		OccurredAt    time.Time          `json:"occurred_at"`
		Source        domain.EventSource `json:"source"`
		Payload       map[string]any     `json:"payload"`
	}{event.ID, event.SchemaVersion, event.EventType, event.ProjectID, event.DeviceID, event.CommandID, event.OccurredAt, event.Source, event.Payload})
	if err != nil {
		return fmt.Errorf("encode CommandResult Event: %w", err)
	}
	_, _, err = tx.Webhooks().CreateDelivery(ctx, repository.CreateWebhookDeliveryRequest{ID: deliveryID, EventID: event.ID, RawBody: rawBody})
	return err
}

func randomUUID(reader io.Reader) (string, error) {
	var value [16]byte
	if _, err := io.ReadFull(reader, value[:]); err != nil {
		return "", err
	}
	value[6] = value[6]&0x0f | 0x40
	value[8] = value[8]&0x3f | 0x80
	encoded := hex.EncodeToString(value[:])
	return encoded[0:8] + "-" + encoded[8:12] + "-" + encoded[12:16] + "-" + encoded[16:20] + "-" + encoded[20:32], nil
}
