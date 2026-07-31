//go:build integration

package repository_test

import (
	"bytes"
	"context"
	"database/sql"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/lib/pq"
	"github.com/qiyue2015/device-platform/internal/domain"
	"github.com/qiyue2015/device-platform/internal/storage/repository"
)

const (
	commandProjectID     = "21000000-0000-0000-0000-000000000001"
	commandDeviceID      = "31000000-0000-0000-0000-000000000001"
	commandOtherDeviceID = "31000000-0000-0000-0000-000000000002"
	commandSimulatorID   = "31000000-0000-0000-0000-000000000003"
)

func TestPostgresCommandLifecycleAndEvidence(t *testing.T) {
	withRepositoryTestDatabase(t, func(db *sql.DB) {
		ctx := context.Background()
		store := repository.NewPostgresStore(db)
		now := time.Now().UTC().Truncate(time.Microsecond)
		deviceTypeID := createCommandFixtures(t, ctx, store, now)

		command := testCommand("61000000-0000-0000-0000-000000000001", "request-1", deviceTypeID, now.Add(-20*time.Minute), now.Add(30*time.Minute))
		command.DeviceID = commandSimulatorID
		createdEvent := testCommandEvent("71000000-0000-0000-0000-000000000001", command.ID, "command-created:1", domain.EventTypeCommandCreated, command.QueuedAt)
		createdEvent.DeviceID = stringRef(commandSimulatorID)
		createdAudit := testCommandAudit("81000000-0000-0000-0000-000000000001", command.ID, now)
		if err := store.WithinTransaction(ctx, func(tx *repository.PostgresTx) error {
			if err := tx.Commands().Create(ctx, command); err != nil {
				return err
			}
			if err := tx.Events().Create(ctx, createdEvent); err != nil {
				return err
			}
			return tx.Audits().Create(ctx, createdAudit)
		}); err != nil {
			t.Fatal(err)
		}

		byKey, err := store.Commands().GetByIdempotencyKey(ctx, commandProjectID, command.IdempotencyKey)
		if err != nil || byKey.ID != command.ID || !bytes.Equal(byKey.RequestHash, command.RequestHash) {
			t.Fatalf("idempotency lookup mismatch: command=%+v err=%v", byKey, err)
		}
		if event, err := store.Events().GetByDeduplicationKey(ctx, commandProjectID, createdEvent.DeduplicationKey); err != nil || event.ID != createdEvent.ID {
			t.Fatalf("Event lookup mismatch: event=%+v err=%v", event, err)
		}
		if audit, err := store.Audits().Get(ctx, createdAudit.ID); err != nil || audit.Action != "command.created" || audit.Metadata["idempotency_key"] != command.IdempotencyKey {
			t.Fatalf("Audit lookup mismatch: audit=%+v err=%v", audit, err)
		}

		duplicate := testCommand("61000000-0000-0000-0000-000000000002", command.IdempotencyKey, deviceTypeID, now, now.Add(30*time.Minute))
		duplicate.DeviceID = commandSimulatorID
		err = store.WithinTransaction(ctx, func(tx *repository.PostgresTx) error {
			return tx.Commands().Create(ctx, duplicate)
		})
		var pqErr *pq.Error
		if !errors.As(err, &pqErr) || pqErr.Code != "23505" {
			t.Fatalf("duplicate idempotency error = %v", err)
		}

		claimedCommand, claimedAttempt := claimConcurrently(t, ctx, store, now)
		if claimedCommand.ID != command.ID || claimedAttempt.CommandID != command.ID || claimedAttempt.AttemptNo != 1 {
			t.Fatalf("unexpected concurrent claim: command=%+v attempt=%+v", claimedCommand, claimedAttempt)
		}

		sentAt := now.Add(-2 * time.Minute)
		resultDeadline := sentAt.Add(time.Minute)
		if err := store.WithinTransaction(ctx, func(tx *repository.PostgresTx) error {
			updated, err := tx.Commands().MarkDispatching(ctx, command.ID, claimedAttempt.ID, "00000000-0000-4000-8000-000000000099", time.Minute)
			if err != nil || updated {
				return fmt.Errorf("stale lease dispatched=%v: %w", updated, err)
			}
			updated, err = tx.Commands().MarkDispatching(ctx, command.ID, claimedAttempt.ID, claimedAttempt.LeaseToken, time.Minute)
			if err != nil || !updated {
				return fmt.Errorf("valid lease dispatched=%v: %w", updated, err)
			}
			return nil
		}); err != nil {
			t.Fatal(err)
		}
		databaseTimedCommand, err := store.Commands().Get(ctx, command.ID)
		if err != nil || databaseTimedCommand.SentAt == nil || databaseTimedCommand.ResultDeadlineAt == nil ||
			databaseTimedCommand.ResultDeadlineAt.Sub(*databaseTimedCommand.SentAt) != time.Minute || databaseTimedCommand.SentAt.Before(now) {
			t.Fatalf("database-clock dispatch timing mismatch: command=%+v err=%v", databaseTimedCommand, err)
		}
		if _, err := db.ExecContext(ctx, `
			UPDATE device_commands SET sent_at = $2, result_deadline_at = $3, updated_at = $2
			WHERE id = $1
		`, command.ID, sentAt, resultDeadline); err != nil {
			t.Fatal(err)
		}

		completedAt := sentAt.Add(time.Second)
		if err := store.WithinTransaction(ctx, func(tx *repository.PostgresTx) error {
			completed, err := tx.Commands().CompleteAttempt(ctx, command.ID, claimedAttempt.ID, claimedAttempt.LeaseToken, repository.CompleteCommandAttemptRequest{
				Outcome:           domain.AttemptOutcomeProviderAccepted,
				ConfirmationLevel: domain.ConfirmationProviderAccepted,
				EvidenceStatus:    domain.EvidenceVerified,
				ResponseSummary:   map[string]any{"accepted": true},
			})
			if err != nil || !completed {
				return fmt.Errorf("complete Attempt completed=%v: %w", completed, err)
			}
			updated, err := tx.Commands().UpdateEvidenceFromAttempt(ctx, command.ID, claimedAttempt.ID, claimedAttempt.LeaseToken, domain.CommandStatusSent)
			if err != nil || !updated {
				return fmt.Errorf("apply Provider acceptance updated=%v: %w", updated, err)
			}
			return nil
		}); err != nil {
			t.Fatal(err)
		}

		wrongMessage := domain.RawMessage{
			ID:                "41000000-0000-0000-0000-000000000001",
			DeviceID:          stringRef(commandSimulatorID),
			ProviderCode:      domain.ProviderCodeWWTIOT,
			ProviderDeviceID:  "LOCK-COMMAND-1",
			AccessType:        domain.AccessTypeCloudAPI,
			TransportProtocol: domain.TransportProtocolHTTP,
			Adapter:           domain.AdapterWWTIOTCloudAPI,
			Direction:         domain.RawMessageInbound,
			DeduplicationKey:  claimedAttempt.ProviderRequestKey,
			Headers:           map[string]any{},
			Body:              []byte(`{"result":"ok"}`),
			ReceivedAt:        completedAt.Add(time.Second),
			CreatedAt:         completedAt.Add(time.Second),
		}
		if err := store.WithinTransaction(ctx, func(tx *repository.PostgresTx) error {
			return tx.Messages().Create(ctx, wrongMessage)
		}); err != nil {
			t.Fatal(err)
		}

		verifiedUpdate := repository.VerifiedEvidenceUpdateRequest{
			AttemptID:                  claimedAttempt.ID,
			RawMessageID:               wrongMessage.ID,
			RawMessageDeduplicationKey: wrongMessage.DeduplicationKey,
			AttemptOutcome:             domain.AttemptOutcomeProviderAccepted,
			ResponseSummary:            map[string]any{"accepted": true},
			ExpectedStatus:             domain.CommandStatusSent,
		}
		if err := store.WithinTransaction(ctx, func(tx *repository.PostgresTx) error {
			updated, err := tx.Commands().UpdateProviderAcceptanceFromVerifiedMessage(ctx, command.ID, verifiedUpdate)
			if err != nil || updated {
				return fmt.Errorf("wrong Provider evidence updated=%v: %w", updated, err)
			}
			return nil
		}); err != nil {
			t.Fatal(err)
		}
		attempts, err := store.Commands().ListAttempts(ctx, command.ID)
		if err != nil || len(attempts) != 1 || attempts[0].Outcome == nil || *attempts[0].Outcome != domain.AttemptOutcomeProviderAccepted {
			t.Fatalf("rejected evidence partially changed Attempt: attempts=%+v err=%v", attempts, err)
		}

		trustedMessage := domain.RawMessage{
			ID:                "41000000-0000-0000-0000-000000000002",
			DeviceID:          stringRef(commandSimulatorID),
			ProviderCode:      domain.ProviderCodeSimulator,
			ProviderDeviceID:  commandSimulatorID,
			AccessType:        domain.AccessTypeSimulator,
			TransportProtocol: domain.TransportProtocolInternal,
			Adapter:           domain.AdapterSimulator,
			Direction:         domain.RawMessageInbound,
			DeduplicationKey:  claimedAttempt.ProviderRequestKey,
			Headers:           map[string]any{},
			Body:              []byte(`{"outcome":"provider_accepted"}`),
			ReceivedAt:        completedAt.Add(2 * time.Second),
			CreatedAt:         completedAt.Add(2 * time.Second),
		}
		verifiedUpdate.RawMessageID = trustedMessage.ID
		verifiedUpdate.RawMessageDeduplicationKey = trustedMessage.DeduplicationKey
		if err := store.WithinTransaction(ctx, func(tx *repository.PostgresTx) error {
			if err := tx.Messages().Create(ctx, trustedMessage); err != nil {
				return err
			}
			stored, err := tx.Messages().GetByDeduplicationKey(ctx, domain.ProviderCodeSimulator, trustedMessage.DeduplicationKey)
			if err != nil || stored.ID != trustedMessage.ID {
				return fmt.Errorf("RawMessage lookup: message=%+v: %w", stored, err)
			}
			updated, err := tx.Commands().UpdateProviderAcceptanceFromVerifiedMessage(ctx, command.ID, verifiedUpdate)
			if err != nil || updated {
				return fmt.Errorf("already verified evidence updated=%v: %w", updated, err)
			}
			return nil
		}); err != nil {
			t.Fatal(err)
		}

		verifiedCommand, err := store.Commands().Get(ctx, command.ID)
		if err != nil || verifiedCommand.Status != domain.CommandStatusSent || verifiedCommand.ConfirmationLevel != domain.ConfirmationProviderAccepted || verifiedCommand.EvidenceStatus != domain.EvidenceVerified {
			t.Fatalf("verified Provider acceptance mismatch: command=%+v err=%v", verifiedCommand, err)
		}
		attempts, err = store.Commands().ListAttempts(ctx, command.ID)
		if err != nil || len(attempts) != 1 || attempts[0].Outcome == nil || *attempts[0].Outcome != domain.AttemptOutcomeProviderAccepted || attempts[0].EvidenceStatus != domain.EvidenceVerified {
			t.Fatalf("verified Provider Attempt mismatch: attempts=%+v err=%v", attempts, err)
		}

		timeoutReason := "result_observation_timeout"
		statusEvent := testCommandEvent("71000000-0000-0000-0000-000000000002", command.ID, "command-timeout:1", domain.EventTypeCommandStatusChanged, resultDeadline)
		statusEvent.DeviceID = stringRef(commandSimulatorID)
		statusEvent.Source = domain.EventSourceSystem
		statusEvent.Payload = map[string]any{
			"from":               "sent",
			"to":                 "timeout",
			"reason_code":        timeoutReason,
			"confirmation_level": "provider_accepted",
			"evidence_status":    "verified",
		}
		if err := store.WithinTransaction(ctx, func(tx *repository.PostgresTx) error {
			updated, err := tx.Commands().ExpireResultObservation(ctx, command.ID)
			if err != nil || !updated {
				return fmt.Errorf("result observation timeout updated=%v: %w", updated, err)
			}
			return tx.Events().Create(ctx, statusEvent)
		}); err != nil {
			t.Fatal(err)
		}

		terminalRewrite := verifiedUpdate
		terminalRewrite.ExpectedStatus = domain.CommandStatusTimeout
		if err := store.WithinTransaction(ctx, func(tx *repository.PostgresTx) error {
			updated, err := tx.Commands().UpdateProviderAcceptanceFromVerifiedMessage(ctx, command.ID, terminalRewrite)
			if err != nil || updated {
				return fmt.Errorf("terminal rewrite updated=%v: %w", updated, err)
			}
			return nil
		}); err != nil {
			t.Fatal(err)
		}
		attempts, err = store.Commands().ListAttempts(ctx, command.ID)
		if err != nil || attempts[0].Outcome == nil || *attempts[0].Outcome != domain.AttemptOutcomeProviderAccepted {
			t.Fatalf("terminal rewrite changed Attempt: attempts=%+v err=%v", attempts, err)
		}
		finalCommand, err := store.Commands().Get(ctx, command.ID)
		if err != nil || finalCommand.Status != domain.CommandStatusTimeout || finalCommand.ReasonCode == nil || *finalCommand.ReasonCode != timeoutReason {
			t.Fatalf("final timeout Command mismatch: command=%+v err=%v", finalCommand, err)
		}
		events, err := store.Events().ListByCommand(ctx, command.ID)
		if err != nil || len(events) != 2 || events[0].ID != createdEvent.ID || events[1].ID != statusEvent.ID {
			t.Fatalf("Command Events mismatch: events=%+v err=%v", events, err)
		}
	})
}

func TestPostgresCommandLeaseRecoveryCancelAndExpiry(t *testing.T) {
	withRepositoryTestDatabase(t, func(db *sql.DB) {
		ctx := context.Background()
		store := repository.NewPostgresStore(db)
		now := time.Now().UTC().Truncate(time.Microsecond)
		deviceTypeID := createCommandFixtures(t, ctx, store, now)

		reclaimCommand := testCommand("62000000-0000-0000-0000-000000000001", "reclaim-1", deviceTypeID, now.Add(-20*time.Minute), now.Add(30*time.Minute))
		createCommand(t, ctx, store, reclaimCommand)
		reclaimAttempt := claimOne(t, ctx, store, repository.ClaimCommandRequest{
			WorkerID:      "worker-old",
			LeaseToken:    "02000000-0000-4000-8000-000000000001",
			LeaseDuration: 5 * time.Minute,
			ProviderCode:  domain.ProviderCodeWWTIOT,
			Adapter:       domain.AdapterWWTIOTCloudAPI,
			RequestKey:    "201",
		})
		setAttemptLeaseExpiry(t, db, reclaimAttempt.ID, now.Add(-time.Minute))
		if err := store.WithinTransaction(ctx, func(tx *repository.PostgresTx) error {
			_, updated, err := tx.Commands().ReclaimAttempt(ctx, reclaimAttempt.ID, reclaimAttempt.LeaseToken, repository.ReclaimAttemptRequest{
				WorkerID:      "worker-reusing-token",
				LeaseToken:    reclaimAttempt.LeaseToken,
				LeaseDuration: time.Minute,
			})
			if err != nil || updated {
				return fmt.Errorf("same-token reclaim updated=%v: %w", updated, err)
			}
			dispatched, err := tx.Commands().MarkDispatching(ctx, reclaimCommand.ID, reclaimAttempt.ID, reclaimAttempt.LeaseToken, time.Minute)
			if err != nil || dispatched {
				return fmt.Errorf("expired token after rejected reclaim dispatched=%v: %w", dispatched, err)
			}
			return nil
		}); err != nil {
			t.Fatal(err)
		}
		unchangedAttempts, err := store.Commands().ListAttempts(ctx, reclaimCommand.ID)
		if err != nil || len(unchangedAttempts) != 1 {
			t.Fatalf("same-token reclaim Attempt lookup: attempts=%+v err=%v", unchangedAttempts, err)
		}
		unchangedAttempt := unchangedAttempts[0]
		if unchangedAttempt.LeaseOwner != reclaimAttempt.LeaseOwner || unchangedAttempt.LeaseToken != reclaimAttempt.LeaseToken || unchangedAttempt.LeaseExpiresAt.After(time.Now()) {
			t.Fatalf("same-token reclaim changed Attempt: before=%+v after=%+v", reclaimAttempt, unchangedAttempt)
		}
		var reclaimed domain.CommandAttempt
		if err := store.WithinTransaction(ctx, func(tx *repository.PostgresTx) error {
			var updated bool
			var err error
			reclaimed, updated, err = tx.Commands().ReclaimAttempt(ctx, reclaimAttempt.ID, reclaimAttempt.LeaseToken, repository.ReclaimAttemptRequest{
				WorkerID:      "worker-new",
				LeaseToken:    "02000000-0000-4000-8000-000000000002",
				LeaseDuration: time.Hour,
			})
			if err != nil || !updated {
				return fmt.Errorf("reclaim updated=%v: %w", updated, err)
			}
			stale, err := tx.Commands().MarkDispatching(ctx, reclaimCommand.ID, reclaimAttempt.ID, reclaimAttempt.LeaseToken, time.Minute)
			if err != nil || stale {
				return fmt.Errorf("expired token dispatched=%v: %w", stale, err)
			}
			return nil
		}); err != nil {
			t.Fatal(err)
		}
		if reclaimed.LeaseOwner != "worker-new" || reclaimed.LeaseToken == reclaimAttempt.LeaseToken {
			t.Fatalf("reclaimed Attempt mismatch: %+v", reclaimed)
		}
		if reclaimed.LeaseExpiresAt.After(reclaimCommand.DispatchDeadlineAt) {
			t.Fatalf("reclaimed lease exceeds dispatch deadline: lease=%s deadline=%s", reclaimed.LeaseExpiresAt, reclaimCommand.DispatchDeadlineAt)
		}
		if err := store.WithinTransaction(ctx, func(tx *repository.PostgresTx) error {
			dispatched, err := tx.Commands().MarkDispatching(ctx, reclaimCommand.ID, reclaimed.ID, reclaimed.LeaseToken, time.Minute)
			if err != nil || !dispatched {
				return fmt.Errorf("reclaimed dispatch=%v: %w", dispatched, err)
			}
			return nil
		}); err != nil {
			t.Fatal(err)
		}
		transportErrorCode := "connect_refused"
		providerTransportReason := "provider_transport_error"
		if err := store.WithinTransaction(ctx, func(tx *repository.PostgresTx) error {
			completed, err := tx.Commands().CompleteAttempt(ctx, reclaimCommand.ID, reclaimed.ID, reclaimed.LeaseToken, repository.CompleteCommandAttemptRequest{
				Outcome:           domain.AttemptOutcomeTransportErrorBeforeSend,
				ConfirmationLevel: domain.ConfirmationNone,
				EvidenceStatus:    domain.EvidenceNone,
				ErrorCode:         &transportErrorCode,
			})
			if err != nil || !completed {
				return fmt.Errorf("complete before-send transport error=%v: %w", completed, err)
			}
			transitioned, err := tx.Commands().TransitionFromAttempt(ctx, reclaimCommand.ID, reclaimed.ID, reclaimed.LeaseToken, repository.CommandStatusTransition{
				From:              domain.CommandStatusSent,
				To:                domain.CommandStatusFailed,
				ReasonCode:        &providerTransportReason,
				ConfirmationLevel: domain.ConfirmationNone,
				EvidenceStatus:    domain.EvidenceNone,
			})
			if err != nil || !transitioned {
				return fmt.Errorf("transition before-send transport error=%v: %w", transitioned, err)
			}
			return nil
		}); err != nil {
			t.Fatal(err)
		}
		transportFailed, err := store.Commands().Get(ctx, reclaimCommand.ID)
		if err != nil || transportFailed.Status != domain.CommandStatusFailed || transportFailed.ReasonCode == nil || *transportFailed.ReasonCode != providerTransportReason {
			t.Fatalf("before-send transport failure mismatch: command=%+v err=%v", transportFailed, err)
		}

		cancelCommand := testCommand("62000000-0000-0000-0000-000000000002", "cancel-1", deviceTypeID, now.Add(-20*time.Minute), now.Add(30*time.Minute))
		createCommand(t, ctx, store, cancelCommand)
		cancelAttempt := claimOne(t, ctx, store, repository.ClaimCommandRequest{
			WorkerID:      "worker-cancel",
			LeaseToken:    "02000000-0000-4000-8000-000000000003",
			LeaseDuration: 5 * time.Minute,
			ProviderCode:  domain.ProviderCodeWWTIOT,
			Adapter:       domain.AdapterWWTIOTCloudAPI,
			RequestKey:    "202",
		})
		if err := store.WithinTransaction(ctx, func(tx *repository.PostgresTx) error {
			cancelled, err := tx.Commands().CancelQueued(ctx, cancelCommand.ID, stringRef("operator request"))
			if err != nil || cancelled {
				return fmt.Errorf("valid lease cancellation=%v: %w", cancelled, err)
			}
			return nil
		}); err != nil {
			t.Fatal(err)
		}
		setAttemptLeaseExpiry(t, db, cancelAttempt.ID, now.Add(-time.Minute))
		if err := store.WithinTransaction(ctx, func(tx *repository.PostgresTx) error {
			cancelled, err := tx.Commands().CancelQueued(ctx, cancelCommand.ID, stringRef("operator request"))
			if err != nil || !cancelled {
				return fmt.Errorf("expired lease cancellation=%v: %w", cancelled, err)
			}
			return nil
		}); err != nil {
			t.Fatal(err)
		}
		assertTerminalCommandAndAttempt(t, ctx, store, cancelCommand.ID, domain.CommandStatusCancelled, "cancelled_by_request")

		expireCommand := testCommand("62000000-0000-0000-0000-000000000003", "expire-1", deviceTypeID, now.Add(-20*time.Minute), now.Add(30*time.Minute))
		createCommand(t, ctx, store, expireCommand)
		expireAttempt := claimOne(t, ctx, store, repository.ClaimCommandRequest{
			WorkerID:      "worker-expire",
			LeaseToken:    "02000000-0000-4000-8000-000000000004",
			LeaseDuration: 5 * time.Minute,
			ProviderCode:  domain.ProviderCodeWWTIOT,
			Adapter:       domain.AdapterWWTIOTCloudAPI,
			RequestKey:    "203",
		})
		setAttemptLeaseExpiry(t, db, expireAttempt.ID, now.Add(-time.Minute))
		if _, err := db.ExecContext(ctx, `UPDATE device_commands SET dispatch_deadline_at = $2 WHERE id = $1`, expireCommand.ID, now.Add(-time.Minute)); err != nil {
			t.Fatal(err)
		}
		if err := store.WithinTransaction(ctx, func(tx *repository.PostgresTx) error {
			expired, err := tx.Commands().ExpireQueued(ctx, expireCommand.ID)
			if err != nil || !expired {
				return fmt.Errorf("expire queued=%v: %w", expired, err)
			}
			return nil
		}); err != nil {
			t.Fatal(err)
		}
		assertTerminalCommandAndAttempt(t, ctx, store, expireCommand.ID, domain.CommandStatusTimeout, "dispatch_deadline_exceeded")
	})
}

func TestPostgresCommandExpiredDeadlineCannotReclaimOrDispatch(t *testing.T) {
	withRepositoryTestDatabase(t, func(db *sql.DB) {
		ctx := context.Background()
		store := repository.NewPostgresStore(db)
		now := time.Now().UTC().Truncate(time.Microsecond)
		deviceTypeID := createCommandFixtures(t, ctx, store, now)
		command := testCommand("64000000-0000-0000-0000-000000000001", "expired-deadline-1", deviceTypeID, now.Add(-time.Minute), now.Add(time.Minute))
		createCommand(t, ctx, store, command)
		attempt := claimOne(t, ctx, store, repository.ClaimCommandRequest{
			WorkerID:      "expired-deadline-worker",
			LeaseToken:    "04000000-0000-4000-8000-000000000001",
			LeaseDuration: 10 * time.Minute,
			ProviderCode:  domain.ProviderCodeWWTIOT,
			Adapter:       domain.AdapterWWTIOTCloudAPI,
			RequestKey:    "401",
		})
		if attempt.LeaseExpiresAt.After(command.DispatchDeadlineAt) {
			t.Fatalf("claimed lease exceeds dispatch deadline: lease=%s deadline=%s", attempt.LeaseExpiresAt, command.DispatchDeadlineAt)
		}
		setAttemptLeaseExpiry(t, db, attempt.ID, now.Add(-time.Minute))
		if _, err := db.ExecContext(ctx, `UPDATE device_commands SET dispatch_deadline_at = now() - interval '1 second' WHERE id = $1`, command.ID); err != nil {
			t.Fatal(err)
		}

		if err := store.WithinTransaction(ctx, func(tx *repository.PostgresTx) error {
			_, reclaimed, err := tx.Commands().ReclaimAttempt(ctx, attempt.ID, attempt.LeaseToken, repository.ReclaimAttemptRequest{
				WorkerID:      "late-worker",
				LeaseToken:    "04000000-0000-4000-8000-000000000002",
				LeaseDuration: time.Minute,
			})
			if err != nil || reclaimed {
				return fmt.Errorf("expired Command reclaimed=%v: %w", reclaimed, err)
			}
			dispatched, err := tx.Commands().MarkDispatching(ctx, command.ID, attempt.ID, attempt.LeaseToken, time.Minute)
			if err != nil || dispatched {
				return fmt.Errorf("expired Command dispatched=%v: %w", dispatched, err)
			}
			return nil
		}); err != nil {
			t.Fatal(err)
		}
		if err := store.WithinTransaction(ctx, func(tx *repository.PostgresTx) error {
			expired, err := tx.Commands().ExpireQueued(ctx, command.ID)
			if err != nil || !expired {
				return fmt.Errorf("expire queued updated=%v: %w", expired, err)
			}
			return nil
		}); err != nil {
			t.Fatal(err)
		}
		assertTerminalCommandAndAttempt(t, ctx, store, command.ID, domain.CommandStatusTimeout, "dispatch_deadline_exceeded")
	})
}

func TestPostgresCommandExpiredDispatchingRecoveryFencesWorker(t *testing.T) {
	withRepositoryTestDatabase(t, func(db *sql.DB) {
		ctx := context.Background()
		store := repository.NewPostgresStore(db)
		now := time.Now().UTC().Truncate(time.Microsecond)
		deviceTypeID := createCommandFixtures(t, ctx, store, now)
		command := testCommand("64000000-0000-0000-0000-000000000002", "expired-dispatching-1", deviceTypeID, now.Add(-4*time.Minute), now.Add(time.Minute))
		createCommand(t, ctx, store, command)
		attempt := claimOne(t, ctx, store, repository.ClaimCommandRequest{
			WorkerID:      "crashed-worker",
			LeaseToken:    "04000000-0000-4000-8000-000000000003",
			LeaseDuration: time.Minute,
			ProviderCode:  domain.ProviderCodeWWTIOT,
			Adapter:       domain.AdapterWWTIOTCloudAPI,
			RequestKey:    "402",
		})
		sentAt := now.Add(-2 * time.Minute)
		if err := store.WithinTransaction(ctx, func(tx *repository.PostgresTx) error {
			dispatched, err := tx.Commands().MarkDispatching(ctx, command.ID, attempt.ID, attempt.LeaseToken, time.Minute)
			if err != nil || !dispatched {
				return fmt.Errorf("dispatch updated=%v: %w", dispatched, err)
			}
			return nil
		}); err != nil {
			t.Fatal(err)
		}
		if _, err := db.ExecContext(ctx, `
			UPDATE device_commands SET sent_at = $2, result_deadline_at = $3, updated_at = $2
			WHERE id = $1
		`, command.ID, sentAt, now.Add(-time.Minute)); err != nil {
			t.Fatal(err)
		}
		setAttemptLeaseExpiry(t, db, attempt.ID, now.Add(-time.Minute))
		if err := store.WithinTransaction(ctx, func(tx *repository.PostgresTx) error {
			expired, err := tx.Commands().ExpireResultObservation(ctx, command.ID)
			if err != nil || expired {
				return fmt.Errorf("result scanner bypassed dispatch recovery=%v: %w", expired, err)
			}
			return nil
		}); err != nil {
			t.Fatal(err)
		}

		start := make(chan struct{})
		type recoveryResult struct {
			operation string
			updated   bool
			err       error
		}
		results := make(chan recoveryResult, 3)
		var workers sync.WaitGroup
		workers.Add(3)
		go func() {
			defer workers.Done()
			<-start
			var updated bool
			err := store.WithinTransaction(ctx, func(tx *repository.PostgresTx) error {
				var err error
				updated, err = tx.Commands().CompleteAttempt(ctx, command.ID, attempt.ID, attempt.LeaseToken, repository.CompleteCommandAttemptRequest{
					Outcome:           domain.AttemptOutcomeProviderAccepted,
					ConfirmationLevel: domain.ConfirmationProviderAccepted,
					EvidenceStatus:    domain.EvidenceUnverified,
				})
				return err
			})
			results <- recoveryResult{operation: "stale-complete", updated: updated, err: err}
		}()
		go func() {
			defer workers.Done()
			<-start
			var updated bool
			err := store.WithinTransaction(ctx, func(tx *repository.PostgresTx) error {
				var err error
				updated, err = tx.Commands().RecoverExpiredDispatching(ctx, command.ID, attempt.ID, attempt.LeaseToken)
				return err
			})
			results <- recoveryResult{operation: "recover", updated: updated, err: err}
		}()
		go func() {
			defer workers.Done()
			<-start
			var updated bool
			err := store.WithinTransaction(ctx, func(tx *repository.PostgresTx) error {
				var err error
				updated, err = tx.Commands().ExpireResultObservation(ctx, command.ID)
				return err
			})
			results <- recoveryResult{operation: "result-timeout", updated: updated, err: err}
		}()
		close(start)
		workers.Wait()
		close(results)
		for result := range results {
			if result.err != nil {
				t.Fatalf("%s error: %v", result.operation, result.err)
			}
			want := result.operation == "recover"
			if result.updated != want {
				t.Fatalf("%s updated=%v, want %v", result.operation, result.updated, want)
			}
		}

		recovered, err := store.Commands().Get(ctx, command.ID)
		if err != nil || recovered.Status != domain.CommandStatusUnknown || recovered.ReasonCode == nil || *recovered.ReasonCode != "provider_delivery_unknown" || recovered.ConfirmationLevel != domain.ConfirmationTransportSent || recovered.EvidenceStatus != domain.EvidenceUnverified {
			t.Fatalf("recovered Command mismatch: command=%+v err=%v", recovered, err)
		}
		attempts, err := store.Commands().ListAttempts(ctx, command.ID)
		if err != nil || len(attempts) != 1 || attempts[0].Outcome == nil || *attempts[0].Outcome != domain.AttemptOutcomeTransportErrorAfterSend || attempts[0].ConfirmationLevel != domain.ConfirmationTransportSent || attempts[0].EvidenceStatus != domain.EvidenceUnverified {
			t.Fatalf("recovered Attempt mismatch: attempts=%+v err=%v", attempts, err)
		}
	})
}

func TestPostgresCommandEvidenceAssociationFailsClosed(t *testing.T) {
	withRepositoryTestDatabase(t, func(db *sql.DB) {
		ctx := context.Background()
		store := repository.NewPostgresStore(db)
		now := time.Now().UTC().Truncate(time.Microsecond)
		deviceTypeID := createCommandFixtures(t, ctx, store, now)

		firstCommand, firstAttempt := createCompletedProviderAcceptance(t, ctx, store, deviceTypeID,
			"64000000-0000-0000-0000-000000000003", "sim-evidence-1", commandSimulatorID,
			domain.ProviderCodeSimulator, domain.AdapterSimulator, "sim-evidence-request-1", "04000000-0000-4000-8000-000000000004", now)
		setProviderAcceptanceEvidence(t, db, firstCommand.ID, firstAttempt.ID, domain.EvidenceUnverified)
		trustedMessage := domain.RawMessage{
			ID:                "44000000-0000-0000-0000-000000000001",
			DeviceID:          stringRef(commandSimulatorID),
			ProviderCode:      domain.ProviderCodeSimulator,
			ProviderDeviceID:  commandSimulatorID,
			AccessType:        domain.AccessTypeSimulator,
			TransportProtocol: domain.TransportProtocolInternal,
			Adapter:           domain.AdapterSimulator,
			Direction:         domain.RawMessageInbound,
			DeduplicationKey:  firstAttempt.ProviderRequestKey,
			Headers:           map[string]any{},
			Body:              []byte(`{"outcome":"provider_accepted"}`),
			ReceivedAt:        now,
			CreatedAt:         now,
		}
		if err := store.WithinTransaction(ctx, func(tx *repository.PostgresTx) error {
			if err := tx.Messages().Create(ctx, trustedMessage); err != nil {
				return err
			}
			updated, err := tx.Commands().UpdateProviderAcceptanceFromVerifiedMessage(ctx, firstCommand.ID, repository.VerifiedEvidenceUpdateRequest{
				AttemptID: firstAttempt.ID, RawMessageID: trustedMessage.ID,
				RawMessageDeduplicationKey: trustedMessage.DeduplicationKey,
				AttemptOutcome:             domain.AttemptOutcomeProviderAccepted,
				ExpectedStatus:             domain.CommandStatusSent,
			})
			if err != nil || !updated {
				return fmt.Errorf("first simulator evidence updated=%v: %w", updated, err)
			}
			return nil
		}); err != nil {
			t.Fatal(err)
		}
		if err := store.WithinTransaction(ctx, func(tx *repository.PostgresTx) error {
			updated, err := tx.Commands().UpdateProviderAcceptanceFromVerifiedMessage(ctx, firstCommand.ID, repository.VerifiedEvidenceUpdateRequest{
				AttemptID: firstAttempt.ID, RawMessageID: trustedMessage.ID,
				RawMessageDeduplicationKey: trustedMessage.DeduplicationKey,
				AttemptOutcome:             domain.AttemptOutcomeProviderAccepted,
				ResponseSummary:            map[string]any{"rewrite": true},
				ExpectedStatus:             domain.CommandStatusSent,
			})
			if err != nil || updated {
				return fmt.Errorf("duplicate verified evidence updated=%v: %w", updated, err)
			}
			return nil
		}); err != nil {
			t.Fatal(err)
		}
		firstAttempts, err := store.Commands().ListAttempts(ctx, firstCommand.ID)
		if err != nil || len(firstAttempts) != 1 || firstAttempts[0].ResponseSummary["rewrite"] != nil {
			t.Fatalf("duplicate evidence rewrote Attempt: attempts=%+v err=%v", firstAttempts, err)
		}

		secondCommand, secondAttempt := createCompletedProviderAcceptance(t, ctx, store, deviceTypeID,
			"64000000-0000-0000-0000-000000000004", "sim-evidence-2", commandSimulatorID,
			domain.ProviderCodeSimulator, domain.AdapterSimulator, "sim-evidence-request-2", "04000000-0000-4000-8000-000000000005", now)
		setProviderAcceptanceEvidence(t, db, secondCommand.ID, secondAttempt.ID, domain.EvidenceUnverified)
		if err := store.WithinTransaction(ctx, func(tx *repository.PostgresTx) error {
			updated, err := tx.Commands().UpdateProviderAcceptanceFromVerifiedMessage(ctx, secondCommand.ID, repository.VerifiedEvidenceUpdateRequest{
				AttemptID: secondAttempt.ID, RawMessageID: trustedMessage.ID,
				RawMessageDeduplicationKey: trustedMessage.DeduplicationKey,
				AttemptOutcome:             domain.AttemptOutcomeProviderAccepted,
				ExpectedStatus:             domain.CommandStatusSent,
			})
			if err != nil || updated {
				return fmt.Errorf("reused simulator evidence updated=%v: %w", updated, err)
			}
			return nil
		}); err != nil {
			t.Fatal(err)
		}
		secondStored, err := store.Commands().Get(ctx, secondCommand.ID)
		if err != nil || secondStored.EvidenceStatus != domain.EvidenceUnverified {
			t.Fatalf("rejected reused evidence changed Command: command=%+v err=%v", secondStored, err)
		}

		wwtiotCommand, wwtiotAttempt := createCompletedProviderAcceptance(t, ctx, store, deviceTypeID,
			"64000000-0000-0000-0000-000000000005", "wwtiot-evidence-1", commandDeviceID,
			domain.ProviderCodeWWTIOT, domain.AdapterWWTIOTCloudAPI, "403", "04000000-0000-4000-8000-000000000006", now)
		wwtiotMessage := domain.RawMessage{
			ID:                "44000000-0000-0000-0000-000000000002",
			DeviceID:          stringRef(commandDeviceID),
			ProviderCode:      domain.ProviderCodeWWTIOT,
			ProviderDeviceID:  "LOCK-COMMAND-1",
			AccessType:        domain.AccessTypeCloudAPI,
			TransportProtocol: domain.TransportProtocolHTTP,
			Adapter:           domain.AdapterWWTIOTCloudAPI,
			Direction:         domain.RawMessageInbound,
			DeduplicationKey:  wwtiotAttempt.ProviderRequestKey,
			Headers:           map[string]any{},
			Body:              []byte(`{"result":"ok"}`),
			ReceivedAt:        now,
			CreatedAt:         now,
		}
		if err := store.WithinTransaction(ctx, func(tx *repository.PostgresTx) error {
			if err := tx.Messages().Create(ctx, wwtiotMessage); err != nil {
				return err
			}
			updated, err := tx.Commands().UpdateProviderAcceptanceFromVerifiedMessage(ctx, wwtiotCommand.ID, repository.VerifiedEvidenceUpdateRequest{
				AttemptID: wwtiotAttempt.ID, RawMessageID: wwtiotMessage.ID,
				RawMessageDeduplicationKey: wwtiotMessage.DeduplicationKey,
				AttemptOutcome:             domain.AttemptOutcomeProviderAccepted,
				ExpectedStatus:             domain.CommandStatusSent,
			})
			if err != nil || updated {
				return fmt.Errorf("WWTIOT evidence updated=%v: %w", updated, err)
			}
			return nil
		}); err != nil {
			t.Fatal(err)
		}
		wwtiotStored, err := store.Commands().Get(ctx, wwtiotCommand.ID)
		if err != nil || wwtiotStored.EvidenceStatus != domain.EvidenceUnverified {
			t.Fatalf("fail-closed WWTIOT evidence changed Command: command=%+v err=%v", wwtiotStored, err)
		}
	})
}

func TestPostgresCommandRejectsCrossedAttemptTransition(t *testing.T) {
	withRepositoryTestDatabase(t, func(db *sql.DB) {
		ctx := context.Background()
		store := repository.NewPostgresStore(db)
		now := time.Now().UTC().Truncate(time.Microsecond)
		deviceTypeID := createCommandFixtures(t, ctx, store, now)
		command := testCommand("64000000-0000-0000-0000-000000000006", "crossed-outcome-1", deviceTypeID, now.Add(-time.Minute), now.Add(time.Minute))
		createCommand(t, ctx, store, command)
		attempt := claimOne(t, ctx, store, repository.ClaimCommandRequest{
			WorkerID: "crossed-worker", LeaseToken: "04000000-0000-4000-8000-000000000007",
			LeaseDuration: time.Minute,
			ProviderCode:  domain.ProviderCodeWWTIOT, Adapter: domain.AdapterWWTIOTCloudAPI, RequestKey: "404",
		})
		if err := store.WithinTransaction(ctx, func(tx *repository.PostgresTx) error {
			dispatched, err := tx.Commands().MarkDispatching(ctx, command.ID, attempt.ID, attempt.LeaseToken, time.Minute)
			if err != nil || !dispatched {
				return fmt.Errorf("dispatch updated=%v: %w", dispatched, err)
			}
			completed, err := tx.Commands().CompleteAttempt(ctx, command.ID, attempt.ID, attempt.LeaseToken, repository.CompleteCommandAttemptRequest{
				Outcome: domain.AttemptOutcomeInvalidResponse, ConfirmationLevel: domain.ConfirmationTransportSent,
				EvidenceStatus: domain.EvidenceVerified,
			})
			if err != nil || !completed {
				return fmt.Errorf("complete updated=%v: %w", completed, err)
			}
			providerRejected := "provider_rejected"
			crossed, err := tx.Commands().TransitionFromAttempt(ctx, command.ID, attempt.ID, attempt.LeaseToken, repository.CommandStatusTransition{
				From: domain.CommandStatusSent, To: domain.CommandStatusFailed, ReasonCode: &providerRejected,
				ConfirmationLevel: domain.ConfirmationTransportSent, EvidenceStatus: domain.EvidenceVerified,
			})
			if err != nil || crossed {
				return fmt.Errorf("crossed transition updated=%v: %w", crossed, err)
			}
			return nil
		}); err != nil {
			t.Fatal(err)
		}
		unchanged, err := store.Commands().Get(ctx, command.ID)
		if err != nil || unchanged.Status != domain.CommandStatusSent || unchanged.ReasonCode != nil || unchanged.FinishedAt != nil {
			t.Fatalf("crossed transition changed Command: command=%+v err=%v", unchanged, err)
		}
		providerResponseInvalid := "provider_response_invalid"
		if err := store.WithinTransaction(ctx, func(tx *repository.PostgresTx) error {
			updated, err := tx.Commands().TransitionFromAttempt(ctx, command.ID, attempt.ID, attempt.LeaseToken, repository.CommandStatusTransition{
				From: domain.CommandStatusSent, To: domain.CommandStatusUnknown, ReasonCode: &providerResponseInvalid,
				ConfirmationLevel: domain.ConfirmationTransportSent, EvidenceStatus: domain.EvidenceVerified,
			})
			if err != nil || !updated {
				return fmt.Errorf("valid invalid-response transition updated=%v: %w", updated, err)
			}
			return nil
		}); err != nil {
			t.Fatal(err)
		}
		asserted, err := store.Commands().Get(ctx, command.ID)
		if err != nil || asserted.Status != domain.CommandStatusUnknown || asserted.ReasonCode == nil || *asserted.ReasonCode != providerResponseInvalid {
			t.Fatalf("valid invalid-response transition mismatch: command=%+v err=%v", asserted, err)
		}
	})
}

func TestPostgresCommandCompletionRejectsProviderContractDrift(t *testing.T) {
	withRepositoryTestDatabase(t, func(db *sql.DB) {
		ctx := context.Background()
		store := repository.NewPostgresStore(db)
		now := time.Now().UTC().Truncate(time.Microsecond)
		deviceTypeID := createCommandFixtures(t, ctx, store, now)
		command := testCommand("64000000-0000-0000-0000-000000000008", "provider-contract-1", deviceTypeID, now.Add(-time.Minute), now.Add(time.Minute))
		createCommand(t, ctx, store, command)
		attempt := claimOne(t, ctx, store, repository.ClaimCommandRequest{
			WorkerID: "provider-contract-worker", LeaseToken: "04000000-0000-4000-8000-000000000010",
			LeaseDuration: time.Minute,
			ProviderCode:  domain.ProviderCodeWWTIOT, Adapter: domain.AdapterWWTIOTCloudAPI, RequestKey: "406",
		})
		if err := store.WithinTransaction(ctx, func(tx *repository.PostgresTx) error {
			dispatched, err := tx.Commands().MarkDispatching(ctx, command.ID, attempt.ID, attempt.LeaseToken, time.Minute)
			if err != nil || !dispatched {
				return fmt.Errorf("dispatch updated=%v: %w", dispatched, err)
			}
			for name, request := range map[string]repository.CompleteCommandAttemptRequest{
				"Device final": {
					Outcome: domain.AttemptOutcomeDeviceSucceeded, ConfirmationLevel: domain.ConfirmationDeviceFinal,
					EvidenceStatus: domain.EvidenceVerified,
				},
				"verified WWTIOT acceptance": {
					Outcome: domain.AttemptOutcomeProviderAccepted, ConfirmationLevel: domain.ConfirmationProviderAccepted,
					EvidenceStatus: domain.EvidenceVerified,
				},
			} {
				completed, err := tx.Commands().CompleteAttempt(ctx, command.ID, attempt.ID, attempt.LeaseToken, request)
				if err != nil || completed {
					return fmt.Errorf("%s completed=%v: %w", name, completed, err)
				}
			}
			completed, err := tx.Commands().CompleteAttempt(ctx, command.ID, attempt.ID, attempt.LeaseToken, repository.CompleteCommandAttemptRequest{
				Outcome: domain.AttemptOutcomeProviderAccepted, ConfirmationLevel: domain.ConfirmationProviderAccepted,
				EvidenceStatus: domain.EvidenceUnverified,
			})
			if err != nil || !completed {
				return fmt.Errorf("unverified WWTIOT acceptance completed=%v: %w", completed, err)
			}
			return nil
		}); err != nil {
			t.Fatal(err)
		}
		attempts, err := store.Commands().ListAttempts(ctx, command.ID)
		if err != nil || len(attempts) != 1 || attempts[0].Outcome == nil || *attempts[0].Outcome != domain.AttemptOutcomeProviderAccepted || attempts[0].EvidenceStatus != domain.EvidenceUnverified {
			t.Fatalf("Provider-constrained completion mismatch: attempts=%+v err=%v", attempts, err)
		}
	})
}

func TestPostgresCommandConcurrentReclaimAndQueuedFinish(t *testing.T) {
	for _, test := range []struct {
		name   string
		expire bool
	}{
		{name: "cancel"},
		{name: "expire", expire: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			withRepositoryTestDatabase(t, func(db *sql.DB) {
				ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
				defer cancel()
				store := repository.NewPostgresStore(db)
				now := time.Now().UTC().Truncate(time.Microsecond)
				deviceTypeID := createCommandFixtures(t, ctx, store, now)
				command := testCommand("64000000-0000-0000-0000-000000000007", "concurrent-finish-1", deviceTypeID, now.Add(-time.Minute), now.Add(time.Minute))
				createCommand(t, ctx, store, command)
				attempt := claimOne(t, ctx, store, repository.ClaimCommandRequest{
					WorkerID: "old-worker", LeaseToken: "04000000-0000-4000-8000-000000000008",
					LeaseDuration: time.Minute,
					ProviderCode:  domain.ProviderCodeWWTIOT, Adapter: domain.AdapterWWTIOTCloudAPI, RequestKey: "405",
				})
				setAttemptLeaseExpiry(t, db, attempt.ID, now.Add(-time.Minute))
				if test.expire {
					if _, err := db.ExecContext(ctx, `UPDATE device_commands SET dispatch_deadline_at = now() - interval '1 second' WHERE id = $1`, command.ID); err != nil {
						t.Fatal(err)
					}
				}

				type operationResult struct {
					operation string
					updated   bool
					err       error
				}
				start := make(chan struct{})
				results := make(chan operationResult, 2)
				var workers sync.WaitGroup
				workers.Add(2)
				go func() {
					defer workers.Done()
					<-start
					var updated bool
					err := store.WithinTransaction(ctx, func(tx *repository.PostgresTx) error {
						_, reclaimed, err := tx.Commands().ReclaimAttempt(ctx, attempt.ID, attempt.LeaseToken, repository.ReclaimAttemptRequest{
							WorkerID: "new-worker", LeaseToken: "04000000-0000-4000-8000-000000000009", LeaseDuration: time.Minute,
						})
						updated = reclaimed
						return err
					})
					results <- operationResult{operation: "reclaim", updated: updated, err: err}
				}()
				go func() {
					defer workers.Done()
					<-start
					var updated bool
					err := store.WithinTransaction(ctx, func(tx *repository.PostgresTx) error {
						var err error
						if test.expire {
							updated, err = tx.Commands().ExpireQueued(ctx, command.ID)
						} else {
							updated, err = tx.Commands().CancelQueued(ctx, command.ID, stringRef("operator request"))
						}
						return err
					})
					results <- operationResult{operation: test.name, updated: updated, err: err}
				}()
				close(start)
				workers.Wait()
				close(results)
				updates := 0
				for result := range results {
					if result.err != nil {
						t.Fatalf("%s error: %v", result.operation, result.err)
					}
					if result.updated {
						updates++
					}
				}
				if updates != 1 {
					t.Fatalf("successful concurrent operations = %d, want 1", updates)
				}
			})
		})
	}
}

func TestPostgresCommandRejectsInvalidDurationsWithoutWrites(t *testing.T) {
	withRepositoryTestDatabase(t, func(db *sql.DB) {
		ctx := context.Background()
		store := repository.NewPostgresStore(db)
		now := time.Now().UTC().Truncate(time.Microsecond)
		deviceTypeID := createCommandFixtures(t, ctx, store, now)
		command := testCommand("64000000-0000-0000-0000-000000000008", "invalid-duration-1", deviceTypeID, now.Add(-time.Minute), now.Add(time.Minute))
		createCommand(t, ctx, store, command)

		for _, duration := range []time.Duration{-time.Second, 0, time.Nanosecond} {
			if err := store.WithinTransaction(ctx, func(tx *repository.PostgresTx) error {
				_, _, claimed, err := tx.Commands().ClaimNext(ctx, repository.ClaimCommandRequest{
					WorkerID: "invalid-duration-worker", LeaseToken: "04000000-0000-4000-8000-000000000010",
					LeaseDuration: duration, ProviderCode: domain.ProviderCodeWWTIOT, Adapter: domain.AdapterWWTIOTCloudAPI, RequestKey: "406",
				})
				if err != nil || claimed {
					return fmt.Errorf("claim duration %s updated=%v: %w", duration, claimed, err)
				}
				return nil
			}); err != nil {
				t.Fatal(err)
			}
		}
		attempts, err := store.Commands().ListAttempts(ctx, command.ID)
		if err != nil || len(attempts) != 0 {
			t.Fatalf("invalid claim duration wrote Attempts: attempts=%+v err=%v", attempts, err)
		}

		attempt := claimOne(t, ctx, store, repository.ClaimCommandRequest{
			WorkerID: "valid-duration-worker", LeaseToken: "04000000-0000-4000-8000-000000000011",
			LeaseDuration: time.Minute, ProviderCode: domain.ProviderCodeWWTIOT, Adapter: domain.AdapterWWTIOTCloudAPI, RequestKey: "407",
		})
		for _, duration := range []time.Duration{-time.Second, 0, time.Nanosecond} {
			if err := store.WithinTransaction(ctx, func(tx *repository.PostgresTx) error {
				dispatched, err := tx.Commands().MarkDispatching(ctx, command.ID, attempt.ID, attempt.LeaseToken, duration)
				if err != nil || dispatched {
					return fmt.Errorf("dispatch duration %s updated=%v: %w", duration, dispatched, err)
				}
				return nil
			}); err != nil {
				t.Fatal(err)
			}
		}
		stored, err := store.Commands().Get(ctx, command.ID)
		attempts, attemptsErr := store.Commands().ListAttempts(ctx, command.ID)
		if err != nil || attemptsErr != nil || stored.Status != domain.CommandStatusQueued || stored.SentAt != nil || len(attempts) != 1 || attempts[0].Phase != domain.AttemptPhaseClaimed || attempts[0].DispatchingAt != nil {
			t.Fatalf("invalid dispatch duration changed state: command=%+v attempts=%+v err=%v attemptsErr=%v", stored, attempts, err, attemptsErr)
		}

		setAttemptLeaseExpiry(t, db, attempt.ID, now.Add(-time.Minute))
		for _, duration := range []time.Duration{-time.Second, 0, time.Nanosecond} {
			if err := store.WithinTransaction(ctx, func(tx *repository.PostgresTx) error {
				_, reclaimed, err := tx.Commands().ReclaimAttempt(ctx, attempt.ID, attempt.LeaseToken, repository.ReclaimAttemptRequest{
					WorkerID: "invalid-reclaim-worker", LeaseToken: "04000000-0000-4000-8000-000000000012", LeaseDuration: duration,
				})
				if err != nil || reclaimed {
					return fmt.Errorf("reclaim duration %s updated=%v: %w", duration, reclaimed, err)
				}
				return nil
			}); err != nil {
				t.Fatal(err)
			}
		}
		attempts, err = store.Commands().ListAttempts(ctx, command.ID)
		if err != nil || len(attempts) != 1 || attempts[0].LeaseOwner != attempt.LeaseOwner || attempts[0].LeaseToken != attempt.LeaseToken || attempts[0].LeaseExpiresAt.After(time.Now()) {
			t.Fatalf("invalid reclaim duration changed Attempt: attempts=%+v err=%v", attempts, err)
		}
	})
}

func TestPostgresCommandEventAuditTransactionRollback(t *testing.T) {
	withRepositoryTestDatabase(t, func(db *sql.DB) {
		ctx := context.Background()
		store := repository.NewPostgresStore(db)
		now := time.Now().UTC().Truncate(time.Microsecond)
		deviceTypeID := createCommandFixtures(t, ctx, store, now)
		command := testCommand("63000000-0000-0000-0000-000000000001", "rollback-1", deviceTypeID, now, now.Add(time.Minute))
		event := testCommandEvent("73000000-0000-0000-0000-000000000001", command.ID, "rollback-event:1", domain.EventTypeCommandCreated, now)
		audit := testCommandAudit("83000000-0000-0000-0000-000000000001", command.ID, now)
		sentinel := errors.New("rollback repository unit")
		err := store.WithinTransaction(ctx, func(tx *repository.PostgresTx) error {
			if err := tx.Commands().Create(ctx, command); err != nil {
				return err
			}
			if err := tx.Events().Create(ctx, event); err != nil {
				return err
			}
			if err := tx.Audits().Create(ctx, audit); err != nil {
				return err
			}
			return sentinel
		})
		if !errors.Is(err, sentinel) {
			t.Fatalf("rollback error = %v", err)
		}
		if _, err := store.Commands().Get(ctx, command.ID); !errors.Is(err, sql.ErrNoRows) {
			t.Fatalf("rolled-back Command lookup error = %v", err)
		}
		if _, err := store.Events().Get(ctx, event.ID); !errors.Is(err, sql.ErrNoRows) {
			t.Fatalf("rolled-back Event lookup error = %v", err)
		}
		if _, err := store.Audits().Get(ctx, audit.ID); !errors.Is(err, sql.ErrNoRows) {
			t.Fatalf("rolled-back Audit lookup error = %v", err)
		}
	})
}

func createCommandFixtures(t *testing.T, ctx context.Context, store *repository.PostgresStore, now time.Time) string {
	t.Helper()
	deviceType, err := store.DeviceTypes().GetByCode(ctx, domain.DeviceTypeSmartLock)
	if err != nil {
		t.Fatal(err)
	}
	err = store.WithinTransaction(ctx, func(tx *repository.PostgresTx) error {
		if err := tx.Projects().Create(ctx, domain.Project{
			ID:           commandProjectID,
			Name:         "Command Repository Project",
			APIKeyDigest: bytes.Repeat([]byte{0x31}, 32),
			IPWhitelist:  []string{},
			CreatedAt:    now,
			UpdatedAt:    now,
		}); err != nil {
			return err
		}
		for _, device := range []domain.Device{
			{
				ID:                commandDeviceID,
				ProjectID:         commandProjectID,
				DeviceTypeID:      deviceType.ID,
				Name:              "Command Lock",
				ProviderCode:      domain.ProviderCodeWWTIOT,
				ProviderDeviceID:  "LOCK-COMMAND-1",
				AccessType:        domain.AccessTypeCloudAPI,
				TransportProtocol: domain.TransportProtocolHTTP,
				Adapter:           domain.AdapterWWTIOTCloudAPI,
				ConnectionStatus:  domain.ConnectionStatusUnknown,
				LifecycleStatus:   domain.LifecycleStatusActive,
				CreatedAt:         now,
				UpdatedAt:         now,
			},
			{
				ID:                commandOtherDeviceID,
				ProjectID:         commandProjectID,
				DeviceTypeID:      deviceType.ID,
				Name:              "Other Command Lock",
				ProviderCode:      domain.ProviderCodeWWTIOT,
				ProviderDeviceID:  "LOCK-COMMAND-2",
				AccessType:        domain.AccessTypeCloudAPI,
				TransportProtocol: domain.TransportProtocolHTTP,
				Adapter:           domain.AdapterWWTIOTCloudAPI,
				ConnectionStatus:  domain.ConnectionStatusUnknown,
				LifecycleStatus:   domain.LifecycleStatusActive,
				CreatedAt:         now,
				UpdatedAt:         now,
			},
			{
				ID:                commandSimulatorID,
				ProjectID:         commandProjectID,
				DeviceTypeID:      deviceType.ID,
				Name:              "Simulator Command Lock",
				ProviderCode:      domain.ProviderCodeSimulator,
				ProviderDeviceID:  commandSimulatorID,
				AccessType:        domain.AccessTypeSimulator,
				TransportProtocol: domain.TransportProtocolInternal,
				Adapter:           domain.AdapterSimulator,
				ConnectionStatus:  domain.ConnectionStatusUnknown,
				LifecycleStatus:   domain.LifecycleStatusActive,
				CreatedAt:         now,
				UpdatedAt:         now,
			},
		} {
			if err := tx.Devices().Create(ctx, device); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return deviceType.ID
}

func testCommand(id, key, deviceTypeID string, queuedAt, deadline time.Time) domain.Command {
	return domain.Command{
		ID:                 id,
		ProjectID:          commandProjectID,
		DeviceID:           commandDeviceID,
		DeviceTypeID:       deviceTypeID,
		CommandType:        "unlock",
		Payload:            map[string]any{},
		DeviceTypeRevision: domain.DeviceTypeSmartLockRevision,
		DeliveryPolicy:     domain.DeliveryPolicyDispatchOnce,
		Status:             domain.CommandStatusQueued,
		ConfirmationLevel:  domain.ConfirmationNone,
		EvidenceStatus:     domain.EvidenceNone,
		IdempotencyKey:     key,
		RequestHash:        bytes.Repeat([]byte{byte(len(key))}, 32),
		QueuedAt:           queuedAt,
		DispatchDeadlineAt: deadline,
		CreatedAt:          queuedAt,
		UpdatedAt:          queuedAt,
	}
}

func testCommandEvent(id, commandID, deduplicationKey string, eventType domain.EventType, occurredAt time.Time) domain.Event {
	payload := map[string]any{"command_type": "unlock", "delivery_policy": "dispatch_once", "status": "queued"}
	if eventType == domain.EventTypeCommandStatusChanged {
		payload = map[string]any{"from": "queued", "to": "sent", "reason_code": nil, "confirmation_level": "none", "evidence_status": "none"}
	}
	return domain.Event{
		ID:               id,
		SchemaVersion:    domain.EventSchemaVersion,
		EventType:        eventType,
		ProjectID:        commandProjectID,
		DeviceID:         stringRef(commandDeviceID),
		CommandID:        stringRef(commandID),
		Source:           domain.EventSourceOpenAPI,
		Payload:          payload,
		DeduplicationKey: deduplicationKey,
		OccurredAt:       occurredAt,
		CreatedAt:        occurredAt,
	}
}

func testCommandAudit(id, commandID string, occurredAt time.Time) domain.AuditLog {
	return domain.AuditLog{
		ID:           id,
		ActorType:    domain.ActorTypeProject,
		ActorID:      stringRef(commandProjectID),
		ProjectID:    stringRef(commandProjectID),
		Action:       "command.created",
		Result:       domain.AuditResultSuccess,
		ResourceType: "command",
		ResourceID:   stringRef(commandID),
		RequestID:    stringRef("request-command-1"),
		Metadata:     map[string]any{"idempotency_key": "request-1"},
		OccurredAt:   occurredAt,
	}
}

func createCompletedProviderAcceptance(
	t *testing.T,
	ctx context.Context,
	store *repository.PostgresStore,
	deviceTypeID, commandID, idempotencyKey, deviceID, providerCode string,
	adapter domain.Adapter,
	requestKey, leaseToken string,
	now time.Time,
) (domain.Command, domain.CommandAttempt) {
	t.Helper()
	command := testCommand(commandID, idempotencyKey, deviceTypeID, now.Add(-time.Minute), now.Add(time.Minute))
	command.DeviceID = deviceID
	createCommand(t, ctx, store, command)
	attempt := claimOne(t, ctx, store, repository.ClaimCommandRequest{
		WorkerID:      "evidence-worker-" + requestKey,
		LeaseToken:    leaseToken,
		LeaseDuration: time.Minute,
		ProviderCode:  providerCode,
		Adapter:       adapter,
		RequestKey:    requestKey,
	})
	evidenceStatus := domain.EvidenceUnverified
	if providerCode == domain.ProviderCodeSimulator {
		evidenceStatus = domain.EvidenceVerified
	}
	if err := store.WithinTransaction(ctx, func(tx *repository.PostgresTx) error {
		dispatched, err := tx.Commands().MarkDispatching(ctx, command.ID, attempt.ID, attempt.LeaseToken, time.Minute)
		if err != nil || !dispatched {
			return fmt.Errorf("dispatch Provider acceptance updated=%v: %w", dispatched, err)
		}
		completed, err := tx.Commands().CompleteAttempt(ctx, command.ID, attempt.ID, attempt.LeaseToken, repository.CompleteCommandAttemptRequest{
			Outcome:           domain.AttemptOutcomeProviderAccepted,
			ConfirmationLevel: domain.ConfirmationProviderAccepted,
			EvidenceStatus:    evidenceStatus,
			ResponseSummary:   map[string]any{"accepted": true},
		})
		if err != nil || !completed {
			return fmt.Errorf("complete Provider acceptance updated=%v: %w", completed, err)
		}
		updated, err := tx.Commands().UpdateEvidenceFromAttempt(ctx, command.ID, attempt.ID, attempt.LeaseToken, domain.CommandStatusSent)
		if err != nil || !updated {
			return fmt.Errorf("apply Provider acceptance updated=%v: %w", updated, err)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	return command, attempt
}

func createCommand(t *testing.T, ctx context.Context, store *repository.PostgresStore, command domain.Command) {
	t.Helper()
	if err := store.WithinTransaction(ctx, func(tx *repository.PostgresTx) error {
		return tx.Commands().Create(ctx, command)
	}); err != nil {
		t.Fatal(err)
	}
}

func claimOne(t *testing.T, ctx context.Context, store *repository.PostgresStore, request repository.ClaimCommandRequest) domain.CommandAttempt {
	t.Helper()
	var attempt domain.CommandAttempt
	if err := store.WithinTransaction(ctx, func(tx *repository.PostgresTx) error {
		_, claimedAttempt, claimed, err := tx.Commands().ClaimNext(ctx, request)
		if err != nil {
			return err
		}
		if !claimed {
			return errors.New("expected Command claim")
		}
		attempt = claimedAttempt
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	return attempt
}

func claimConcurrently(t *testing.T, ctx context.Context, store *repository.PostgresStore, now time.Time) (domain.Command, domain.CommandAttempt) {
	t.Helper()
	type result struct {
		command domain.Command
		attempt domain.CommandAttempt
		claimed bool
		err     error
	}
	start := make(chan struct{})
	results := make(chan result, 2)
	var workers sync.WaitGroup
	for index := 0; index < 2; index++ {
		index := index
		workers.Add(1)
		go func() {
			defer workers.Done()
			<-start
			var item result
			item.err = store.WithinTransaction(ctx, func(tx *repository.PostgresTx) error {
				var err error
				item.command, item.attempt, item.claimed, err = tx.Commands().ClaimNext(ctx, repository.ClaimCommandRequest{
					WorkerID:       fmt.Sprintf("worker-%d", index+1),
					LeaseToken:     fmt.Sprintf("01000000-0000-4000-8000-%012d", index+1),
					LeaseDuration:  5 * time.Minute,
					ProviderCode:   domain.ProviderCodeSimulator,
					Adapter:        domain.AdapterSimulator,
					RequestKey:     fmt.Sprintf("simulator-%d", index+1),
					RequestSummary: map[string]any{"action": "unlock"},
				})
				return err
			})
			results <- item
		}()
	}
	close(start)
	workers.Wait()
	close(results)
	var claimed []result
	for item := range results {
		if item.err != nil {
			t.Fatal(item.err)
		}
		if item.claimed {
			claimed = append(claimed, item)
		}
	}
	if len(claimed) != 1 {
		t.Fatalf("concurrent claim owners = %d, want 1", len(claimed))
	}
	return claimed[0].command, claimed[0].attempt
}

func setAttemptLeaseExpiry(t *testing.T, db *sql.DB, attemptID string, expiresAt time.Time) {
	t.Helper()
	if _, err := db.Exec(`
		UPDATE device_command_attempts
			SET claimed_at = LEAST(claimed_at, $2::timestamptz - interval '1 minute'), lease_expires_at = $2
		WHERE id = $1
	`, attemptID, expiresAt); err != nil {
		t.Fatal(err)
	}
}

func setProviderAcceptanceEvidence(t *testing.T, db *sql.DB, commandID, attemptID string, evidence domain.EvidenceStatus) {
	t.Helper()
	if _, err := db.Exec(`UPDATE device_command_attempts SET evidence_status = $3 WHERE id = $1 AND command_id = $2`, attemptID, commandID, evidence); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`UPDATE device_commands SET evidence_status = $2 WHERE id = $1`, commandID, evidence); err != nil {
		t.Fatal(err)
	}
}

func assertTerminalCommandAndAttempt(t *testing.T, ctx context.Context, store *repository.PostgresStore, commandID string, status domain.CommandStatus, reasonCode string) {
	t.Helper()
	command, err := store.Commands().Get(ctx, commandID)
	if err != nil || command.Status != status || command.ReasonCode == nil || *command.ReasonCode != reasonCode || command.FinishedAt == nil {
		t.Fatalf("terminal Command mismatch: command=%+v err=%v", command, err)
	}
	attempts, err := store.Commands().ListAttempts(ctx, commandID)
	if err != nil || len(attempts) != 1 || attempts[0].Phase != domain.AttemptPhaseCompleted || attempts[0].Outcome == nil || *attempts[0].Outcome != domain.AttemptOutcomeNotDispatched {
		t.Fatalf("terminal Attempt mismatch: attempts=%+v err=%v", attempts, err)
	}
}

func stringRef(value string) *string {
	return &value
}
