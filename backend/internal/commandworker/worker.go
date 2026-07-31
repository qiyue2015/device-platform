package commandworker

import (
	"context"
	"crypto/rand"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/qiyue2015/device-platform/internal/domain"
	"github.com/qiyue2015/device-platform/internal/provideradapter"
	"github.com/qiyue2015/device-platform/internal/storage/repository"
)

const (
	defaultLeaseDuration = 15 * time.Second
	defaultPollInterval  = 100 * time.Millisecond
	requestKeyAttempts   = 8
)

type Worker struct {
	store         Store
	workerID      string
	leaseDuration time.Duration
	pollInterval  time.Duration
	adapters      []AdapterRegistration
	random        io.Reader
	randomMu      sync.Mutex
}

func New(store Store, config Config) (*Worker, error) {
	if store == nil || len(config.Adapters) == 0 {
		return nil, ErrInvalidConfig
	}
	if config.Random == nil {
		config.Random = rand.Reader
	}
	if config.LeaseDuration == 0 {
		config.LeaseDuration = defaultLeaseDuration
	}
	if config.PollInterval == 0 {
		config.PollInterval = defaultPollInterval
	}
	if config.LeaseDuration < time.Microsecond || config.PollInterval < time.Millisecond {
		return nil, ErrInvalidConfig
	}
	workerID := strings.TrimSpace(config.WorkerID)
	if workerID == "" {
		id, err := randomUUID(config.Random)
		if err != nil {
			return nil, fmt.Errorf("generate Command worker ID: %w", err)
		}
		workerID = "command-worker-" + id
	}
	seen := make(map[string]struct{}, len(config.Adapters))
	registrations := make([]AdapterRegistration, len(config.Adapters))
	for index, registration := range config.Adapters {
		registration.ProviderCode = strings.TrimSpace(registration.ProviderCode)
		if registration.ProviderCode == "" || registration.AdapterCode == "" || registration.Adapter == nil {
			return nil, ErrInvalidConfig
		}
		key := registration.ProviderCode + "\x00" + string(registration.AdapterCode)
		if _, exists := seen[key]; exists {
			return nil, ErrInvalidConfig
		}
		seen[key] = struct{}{}
		if registration.ResultSource == "" {
			registration.ResultSource = domain.EventSourceSystem
		}
		registrations[index] = registration
	}
	return &Worker{
		store: store, workerID: workerID, leaseDuration: config.LeaseDuration,
		pollInterval: config.PollInterval, adapters: registrations, random: config.Random,
	}, nil
}

func (w *Worker) DispatchNext(ctx context.Context) (bool, error) {
	for _, registration := range w.adapters {
		command, attempt, claimed, err := w.claimNext(ctx, registration)
		if err != nil {
			return false, err
		}
		if !claimed {
			continue
		}
		return true, w.dispatchClaimed(ctx, registration, command, attempt)
	}
	return false, nil
}

func (w *Worker) RecoverNext(ctx context.Context) (bool, error) {
	var recovered bool
	err := w.store.TransactCommand(ctx, func(tx repository.CommandTx) error {
		command, _, updated, err := tx.Commands().RecoverNextExpiredDispatching(ctx)
		if err != nil || !updated {
			return err
		}
		recovered = true
		return w.createStatusEvent(ctx, tx, command, domain.CommandStatusSent, domain.EventSourceSystem,
			"command.status_changed:"+command.ID+":"+string(command.Status))
	})
	return recovered, err
}

func (w *Worker) ExpireNextQueued(ctx context.Context) (bool, error) {
	var expired bool
	err := w.store.TransactCommand(ctx, func(tx repository.CommandTx) error {
		command, updated, err := tx.Commands().ExpireNextQueued(ctx)
		if err != nil || !updated {
			return err
		}
		expired = true
		return w.createStatusEvent(ctx, tx, command, domain.CommandStatusQueued, domain.EventSourceSystem,
			"command.status_changed:"+command.ID+":"+string(command.Status))
	})
	return expired, err
}

func (w *Worker) ExpireNextResultObservation(ctx context.Context) (bool, error) {
	var expired bool
	err := w.store.TransactCommand(ctx, func(tx repository.CommandTx) error {
		command, previousStatus, updated, err := tx.Commands().ExpireNextResultObservation(ctx)
		if err != nil || !updated {
			return err
		}
		expired = true
		return w.createStatusEvent(ctx, tx, command, previousStatus, domain.EventSourceSystem,
			"command.status_changed:"+command.ID+":"+string(command.Status))
	})
	return expired, err
}

func (w *Worker) Run(ctx context.Context, report ErrorReporter) {
	if report == nil {
		report = func(error) {}
	}
	var group sync.WaitGroup
	group.Add(2)
	go func() {
		defer group.Done()
		w.poll(ctx, report, func(ctx context.Context) (bool, error) {
			return w.DispatchNext(ctx)
		})
	}()
	go func() {
		defer group.Done()
		w.poll(ctx, report, func(ctx context.Context) (bool, error) {
			if recovered, err := w.RecoverNext(ctx); err != nil || recovered {
				return recovered, err
			}
			if expired, err := w.ExpireNextQueued(ctx); err != nil || expired {
				return expired, err
			}
			return w.ExpireNextResultObservation(ctx)
		})
	}()
	group.Wait()
}

func (w *Worker) poll(ctx context.Context, report ErrorReporter, work func(context.Context) (bool, error)) {
	timer := time.NewTimer(0)
	defer timer.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-timer.C:
		}
		for {
			worked, err := work(ctx)
			if err != nil {
				if !errors.Is(err, context.Canceled) {
					report(err)
				}
				break
			}
			if !worked || ctx.Err() != nil {
				break
			}
		}
		timer.Reset(w.pollInterval)
	}
}

func (w *Worker) claimNext(ctx context.Context, registration AdapterRegistration) (domain.Command, domain.CommandAttempt, bool, error) {
	for attemptIndex := 0; attemptIndex < requestKeyAttempts; attemptIndex++ {
		leaseToken, requestKey, serial, err := w.newLeaseIdentity()
		if err != nil {
			return domain.Command{}, domain.CommandAttempt{}, false, err
		}
		var command domain.Command
		var attempt domain.CommandAttempt
		var claimed bool
		err = w.store.TransactCommand(ctx, func(tx repository.CommandTx) error {
			var claimErr error
			command, attempt, claimed, claimErr = tx.Commands().ClaimNext(ctx, repository.ClaimCommandRequest{
				WorkerID: w.workerID, LeaseToken: leaseToken, LeaseDuration: w.leaseDuration,
				ProviderCode: registration.ProviderCode, Adapter: registration.AdapterCode,
				RequestKey: requestKey, RequestSummary: map[string]any{"serialnum": serial},
			})
			return claimErr
		})
		if errors.Is(err, repository.ErrProviderRequestKeyConflict) {
			continue
		}
		return command, attempt, claimed, err
	}
	return domain.Command{}, domain.CommandAttempt{}, false, repository.ErrProviderRequestKeyConflict
}

func (w *Worker) dispatchClaimed(ctx context.Context, registration AdapterRegistration, command domain.Command, attempt domain.CommandAttempt) error {
	device, err := w.store.Devices().Get(ctx, command.DeviceID)
	if err != nil {
		return fmt.Errorf("load claimed Command Device: %w", err)
	}
	if device.ProjectID != command.ProjectID || device.DeviceTypeID != command.DeviceTypeID ||
		device.ProviderCode != registration.ProviderCode || device.Adapter != registration.AdapterCode ||
		attempt.ProviderCode != registration.ProviderCode || attempt.Adapter != registration.AdapterCode {
		return ErrRuntimeState
	}
	profile, err := w.store.DeviceTypes().GetProfile(ctx, command.DeviceTypeID, command.DeviceTypeRevision)
	if err != nil {
		return fmt.Errorf("load claimed Command profile: %w", err)
	}
	action, ok := actionFor(profile, command.CommandType)
	if !ok || action.DeliveryPolicy != command.DeliveryPolicy || action.RetryAllowed {
		return ErrRuntimeState
	}
	request := provideradapter.DispatchRequest{
		ProviderDeviceID: device.ProviderDeviceID, Action: command.CommandType,
		Payload: command.Payload, ProviderRequestKey: attempt.ProviderRequestKey,
	}
	if !registration.Adapter.Configured() {
		return w.failBeforeDispatch(ctx, command, attempt, "Provider is not configured")
	}
	prepared, err := registration.Adapter.Prepare(request)
	if err != nil {
		return w.failBeforeDispatch(ctx, command, attempt, err.Error())
	}
	dispatchLeaseDuration := max(w.leaseDuration, action.ProviderRequestTimeout+5*time.Second)
	if err := w.commitDispatch(ctx, command, attempt, action.ResultObservationTimeout, dispatchLeaseDuration, prepared.RequestSummary()); err != nil {
		return err
	}
	dispatchContext, cancel := context.WithTimeout(ctx, action.ProviderRequestTimeout)
	defer cancel()
	result := prepared.Dispatch(dispatchContext)
	return w.persistResult(ctx, registration, command, attempt, result)
}

func (w *Worker) failBeforeDispatch(ctx context.Context, command domain.Command, attempt domain.CommandAttempt, detail string) error {
	reasonCode := "provider_not_configured"
	detail = truncate(detail, 4096)
	return w.store.TransactCommand(ctx, func(tx repository.CommandTx) error {
		completed, err := tx.Commands().CompleteAttempt(ctx, command.ID, attempt.ID, attempt.LeaseToken, repository.CompleteCommandAttemptRequest{
			Outcome: domain.AttemptOutcomeInvalidRequest, ConfirmationLevel: domain.ConfirmationNone,
			EvidenceStatus: domain.EvidenceNone, ResponseSummary: map[string]any{},
			ErrorCode: &reasonCode, ErrorDetail: optional(detail),
		})
		if err != nil {
			return err
		}
		if !completed {
			return ErrLeaseLost
		}
		transitioned, err := tx.Commands().TransitionFromAttempt(ctx, command.ID, attempt.ID, attempt.LeaseToken, repository.CommandStatusTransition{
			From: domain.CommandStatusQueued, To: domain.CommandStatusFailed, ReasonCode: &reasonCode,
			ReasonDetail: optional(detail), ConfirmationLevel: domain.ConfirmationNone, EvidenceStatus: domain.EvidenceNone,
		})
		if err != nil {
			return err
		}
		if !transitioned {
			return ErrLeaseLost
		}
		updated, err := tx.Commands().Get(ctx, command.ID)
		if err != nil {
			return err
		}
		return w.createStatusEvent(ctx, tx, updated, domain.CommandStatusQueued, domain.EventSourceSystem,
			"command.status_changed:"+command.ID+":"+string(updated.Status))
	})
}

func (w *Worker) commitDispatch(ctx context.Context, command domain.Command, attempt domain.CommandAttempt, resultTimeout, dispatchLeaseDuration time.Duration, requestSummary map[string]any) error {
	return w.store.TransactCommand(ctx, func(tx repository.CommandTx) error {
		updated, err := tx.Commands().MarkDispatching(ctx, command.ID, attempt.ID, attempt.LeaseToken, repository.MarkDispatchingRequest{
			ResultObservationTimeout: resultTimeout, DispatchLeaseDuration: dispatchLeaseDuration,
			RequestSummary: requestSummary,
		})
		if err != nil {
			return err
		}
		if !updated {
			return ErrLeaseLost
		}
		sent, err := tx.Commands().Get(ctx, command.ID)
		if err != nil {
			return err
		}
		return w.createStatusEvent(ctx, tx, sent, domain.CommandStatusQueued, domain.EventSourceSystem,
			"command.status_changed:"+command.ID+":"+string(sent.Status))
	})
}

func (w *Worker) persistResult(ctx context.Context, registration AdapterRegistration, command domain.Command, attempt domain.CommandAttempt, result provideradapter.DispatchResult) error {
	return w.store.TransactCommand(ctx, func(tx repository.CommandTx) error {
		reasonCode, target, err := resultTransition(result.Outcome)
		if err != nil {
			return err
		}
		completed, err := tx.Commands().CompleteAttempt(ctx, command.ID, attempt.ID, attempt.LeaseToken, repository.CompleteCommandAttemptRequest{
			Outcome: result.Outcome, ConfirmationLevel: result.ConfirmationLevel,
			EvidenceStatus: result.EvidenceStatus, ResponseSummary: result.ResponseSummary,
			ErrorCode: reasonCode, ErrorDetail: optional(truncate(result.ErrorDetail, 4096)),
		})
		if err != nil {
			return err
		}
		if !completed {
			return ErrLeaseLost
		}
		if result.Outcome == domain.AttemptOutcomeProviderAccepted {
			updated, updateErr := tx.Commands().UpdateEvidenceFromAttempt(ctx, command.ID, attempt.ID, attempt.LeaseToken, domain.CommandStatusSent)
			if updateErr != nil {
				return updateErr
			}
			if !updated {
				return ErrLeaseLost
			}
			current, getErr := tx.Commands().Get(ctx, command.ID)
			if getErr != nil {
				return getErr
			}
			return w.createStatusEvent(ctx, tx, current, domain.CommandStatusSent, registration.ResultSource,
				"command.status_changed:"+command.ID+":attempt:"+attempt.ID+":provider_accepted")
		}
		transitioned, transitionErr := tx.Commands().TransitionFromAttempt(ctx, command.ID, attempt.ID, attempt.LeaseToken, repository.CommandStatusTransition{
			From: domain.CommandStatusSent, To: target, ReasonCode: reasonCode,
			ReasonDetail:      optional(truncate(result.ErrorDetail, 4096)),
			ConfirmationLevel: result.ConfirmationLevel, EvidenceStatus: result.EvidenceStatus,
		})
		if transitionErr != nil {
			return transitionErr
		}
		if !transitioned {
			return ErrLeaseLost
		}
		current, getErr := tx.Commands().Get(ctx, command.ID)
		if getErr != nil {
			return getErr
		}
		return w.createStatusEvent(ctx, tx, current, domain.CommandStatusSent, registration.ResultSource,
			"command.status_changed:"+command.ID+":"+string(current.Status))
	})
}

func resultTransition(outcome domain.AttemptOutcome) (*string, domain.CommandStatus, error) {
	var reason string
	var target domain.CommandStatus
	switch outcome {
	case domain.AttemptOutcomeProviderAccepted:
		return nil, domain.CommandStatusSent, nil
	case domain.AttemptOutcomeTransportErrorBeforeSend:
		reason, target = "provider_transport_error", domain.CommandStatusFailed
	case domain.AttemptOutcomeTransportErrorAfterSend:
		reason, target = "provider_delivery_unknown", domain.CommandStatusUnknown
	case domain.AttemptOutcomeInvalidResponse:
		reason, target = "provider_response_invalid", domain.CommandStatusUnknown
	case domain.AttemptOutcomeProviderRejected:
		reason, target = "provider_rejected", domain.CommandStatusFailed
	default:
		return nil, "", ErrRuntimeState
	}
	return &reason, target, nil
}

func actionFor(profile domain.DeviceTypeProfile, identifier domain.ActionIdentifier) (domain.CapabilityAction, bool) {
	for _, action := range profile.Actions {
		if action.Identifier == identifier {
			return action, true
		}
	}
	return domain.CapabilityAction{}, false
}

func (w *Worker) createStatusEvent(ctx context.Context, tx repository.CommandTx, command domain.Command, from domain.CommandStatus, source domain.EventSource, deduplicationKey string) error {
	eventID, deliveryID, err := w.newEventIdentities()
	if err != nil {
		return err
	}
	deviceID, commandID := command.DeviceID, command.ID
	occurredAt := command.UpdatedAt.UTC()
	if occurredAt.IsZero() {
		occurredAt = time.Now().UTC()
	}
	event := domain.Event{
		ID: eventID, SchemaVersion: domain.EventSchemaVersion, EventType: domain.EventTypeCommandStatusChanged,
		ProjectID: command.ProjectID, DeviceID: &deviceID, CommandID: &commandID, Source: source,
		Payload: map[string]any{
			"from": from, "to": command.Status, "reason_code": command.ReasonCode,
			"confirmation_level": command.ConfirmationLevel, "evidence_status": command.EvidenceStatus,
		},
		DeduplicationKey: deduplicationKey, OccurredAt: occurredAt, CreatedAt: occurredAt,
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
		return fmt.Errorf("encode Command status Event: %w", err)
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

func (w *Worker) newLeaseIdentity() (string, string, int64, error) {
	w.randomMu.Lock()
	defer w.randomMu.Unlock()
	leaseToken, err := randomUUID(w.random)
	if err != nil {
		return "", "", 0, err
	}
	var value [4]byte
	if _, err := io.ReadFull(w.random, value[:]); err != nil {
		return "", "", 0, err
	}
	serial := int64(binary.BigEndian.Uint32(value[:])%999999999) + 1
	return leaseToken, strconv.FormatInt(serial, 10), serial, nil
}

func (w *Worker) newEventIdentities() (string, string, error) {
	w.randomMu.Lock()
	defer w.randomMu.Unlock()
	eventID, err := randomUUID(w.random)
	if err != nil {
		return "", "", err
	}
	deliveryID, err := randomUUID(w.random)
	return eventID, deliveryID, err
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

func optional(value string) *string {
	if value == "" {
		return nil
	}
	copy := value
	return &copy
}

func truncate(value string, limit int) string {
	if len(value) <= limit {
		return value
	}
	value = value[:limit]
	for !utf8.ValidString(value) {
		value = value[:len(value)-1]
	}
	return value
}
