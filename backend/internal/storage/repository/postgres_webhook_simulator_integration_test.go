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

	"github.com/qiyue2015/device-platform/internal/domain"
	"github.com/qiyue2015/device-platform/internal/storage/repository"
)

func TestPostgresEventCommitsWithoutWebhookConfiguration(t *testing.T) {
	withRepositoryTestDatabase(t, func(db *sql.DB) {
		ctx := context.Background()
		store := repository.NewPostgresStore(db)
		now := time.Now().UTC().Truncate(time.Microsecond)
		createCommandFixtures(t, ctx, store, now)
		event := domain.Event{
			ID:               "75000000-0000-0000-0000-000000000099",
			SchemaVersion:    domain.EventSchemaVersion,
			EventType:        domain.EventTypeDeviceCreated,
			ProjectID:        commandProjectID,
			DeviceID:         stringRef(commandSimulatorID),
			Source:           domain.EventSourceAdmin,
			Payload:          map[string]any{"device_type_code": "smart-lock", "provider_code": "simulator", "lifecycle_status": "active"},
			DeduplicationKey: "webhook-event-without-endpoint",
			OccurredAt:       now,
			CreatedAt:        now,
		}
		deliveryID := "65000000-0000-0000-0000-000000000099"
		if err := store.WithinTransaction(ctx, func(tx *repository.PostgresTx) error {
			if err := tx.Events().Create(ctx, event); err != nil {
				return err
			}
			_, created, err := tx.Webhooks().CreateDelivery(ctx, testWebhookDeliveryRequest(deliveryID, event.ID))
			if err != nil {
				return err
			}
			if created {
				return errors.New("Webhook Delivery created without an endpoint")
			}
			return nil
		}); err != nil {
			t.Fatal(err)
		}
		storedEvent, err := store.Events().Get(ctx, event.ID)
		if err != nil || storedEvent.ID != event.ID {
			t.Fatalf("Event was not committed without Webhook configuration: event=%+v err=%v", storedEvent, err)
		}
		if _, err := store.Webhooks().GetDelivery(ctx, deliveryID); !errors.Is(err, sql.ErrNoRows) {
			t.Fatalf("unexpected Delivery without Webhook configuration: %v", err)
		}
	})
}

func TestPostgresWebhookLifecycleRecoveryAndReplay(t *testing.T) {
	withRepositoryTestDatabase(t, func(db *sql.DB) {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		store := repository.NewPostgresStore(db)
		now := time.Now().UTC().Truncate(time.Microsecond)
		eventID := createWebhookFixtures(t, ctx, store, now)
		delivery := testWebhookDeliveryRequest("65000000-0000-0000-0000-000000000001", eventID)
		var createdDelivery domain.WebhookDelivery
		if err := store.WithinTransaction(ctx, func(tx *repository.PostgresTx) error {
			var err error
			var created bool
			createdDelivery, created, err = tx.Webhooks().CreateDelivery(ctx, delivery)
			if err == nil && !created {
				return errors.New("expected Webhook Delivery creation")
			}
			return err
		}); err != nil {
			t.Fatal(err)
		}
		if createdDelivery.TargetURL != "https://example.test/hook" || createdDelivery.WebhookConfigVersion != 1 || createdDelivery.WebhookSecretVersion != 1 || createdDelivery.NextAttemptAt == nil || !createdDelivery.NextAttemptAt.Equal(createdDelivery.CreatedAt) || !createdDelivery.UpdatedAt.Equal(createdDelivery.CreatedAt) {
			t.Fatalf("database-derived Webhook snapshot mismatch: %+v", createdDelivery)
		}
		for name, test := range map[string]struct {
			request repository.CreateWebhookReplayRequest
			want    error
		}{
			"invalid": {
				request: repository.CreateWebhookReplayRequest{},
				want:    repository.ErrInvalidRepositoryRequest,
			},
			"missing": {
				request: repository.CreateWebhookReplayRequest{ID: "65000000-0000-0000-0000-000000000090", ReplayOfDeliveryID: "65000000-0000-0000-0000-000000000099"},
				want:    repository.ErrWebhookDeliveryNotFound,
			},
			"not dead": {
				request: repository.CreateWebhookReplayRequest{ID: "65000000-0000-0000-0000-000000000091", ReplayOfDeliveryID: delivery.ID},
				want:    repository.ErrWebhookDeliveryNotDead,
			},
		} {
			t.Run("replay "+name, func(t *testing.T) {
				err := store.WithinTransaction(ctx, func(tx *repository.PostgresTx) error {
					_, err := tx.Webhooks().CreateReplay(ctx, test.request)
					return err
				})
				if !errors.Is(err, test.want) {
					t.Fatalf("CreateReplay error=%v, want %v", err, test.want)
				}
			})
		}
		if err := store.WithinTransaction(ctx, func(tx *repository.PostgresTx) error {
			_, _, claimed, err := tx.Webhooks().ClaimDue(ctx, repository.ClaimWebhookRequest{
				WorkerID: "invalid-webhook-worker", LeaseToken: "05000000-0000-4000-8000-000000000098",
				LeaseDuration: time.Nanosecond, MaxAttempts: 3,
			})
			if err != nil || claimed {
				return fmt.Errorf("invalid Webhook claim=%v: %w", claimed, err)
			}
			return nil
		}); err != nil {
			t.Fatal(err)
		}
		unchanged, err := store.Webhooks().GetDelivery(ctx, delivery.ID)
		unchangedAttempts, attemptsErr := store.Webhooks().ListAttempts(ctx, delivery.ID)
		if err != nil || attemptsErr != nil || unchanged.Status != domain.WebhookDeliveryStatusPending || unchanged.AttemptCount != 0 || len(unchangedAttempts) != 0 {
			t.Fatalf("invalid Webhook claim wrote state: delivery=%+v attempts=%+v err=%v attemptsErr=%v", unchanged, unchangedAttempts, err, attemptsErr)
		}

		type claimResult struct {
			delivery domain.WebhookDelivery
			attempt  domain.WebhookDeliveryAttempt
			claimed  bool
			err      error
		}
		start := make(chan struct{})
		claims := make(chan claimResult, 2)
		var claimers sync.WaitGroup
		for index := 0; index < 2; index++ {
			index := index
			claimers.Add(1)
			go func() {
				defer claimers.Done()
				<-start
				var result claimResult
				result.err = store.WithinTransaction(ctx, func(tx *repository.PostgresTx) error {
					var err error
					result.delivery, result.attempt, result.claimed, err = tx.Webhooks().ClaimDue(ctx, repository.ClaimWebhookRequest{
						WorkerID:      fmt.Sprintf("webhook-worker-%d", index+1),
						LeaseToken:    fmt.Sprintf("05000000-0000-4000-8000-%012d", index+1),
						LeaseDuration: time.Minute,
						MaxAttempts:   3,
					})
					return err
				})
				claims <- result
			}()
		}
		close(start)
		claimers.Wait()
		close(claims)
		var first claimResult
		claimedCount := 0
		for result := range claims {
			if result.err != nil {
				t.Fatal(result.err)
			}
			if result.claimed {
				claimedCount++
				first = result
			}
		}
		if claimedCount != 1 {
			t.Fatalf("concurrent Webhook claims = %d, want 1", claimedCount)
		}
		if first.delivery.AttemptCount != 1 || first.delivery.Status != domain.WebhookDeliveryStatusSending || first.attempt.AttemptNo != 1 || first.attempt.CompletedAt != nil || first.delivery.LeaseExpiresAt == nil {
			t.Fatalf("first Webhook claim mismatch: delivery=%+v attempt=%+v", first.delivery, first.attempt)
		}
		if first.attempt.RequestTimestamp != first.attempt.StartedAt.Unix() || first.delivery.LeaseExpiresAt.Sub(first.attempt.StartedAt) != time.Minute {
			t.Fatalf("database-clock Webhook timing mismatch: delivery=%+v attempt=%+v", first.delivery, first.attempt)
		}

		status503 := 503
		httpError := "http_status"
		detail := "service unavailable"
		if err := store.WithinTransaction(ctx, func(tx *repository.PostgresTx) error {
			stale, err := tx.Webhooks().CompleteAttempt(ctx, delivery.ID, "05000000-0000-4000-8000-000000000099", repository.CompleteWebhookAttemptRequest{
				AttemptID: first.attempt.ID, HTTPStatus: &status503, ErrorCode: &httpError, ErrorDetail: &detail,
				RetryDelay: time.Second, MaxAttempts: 3,
			})
			if err != nil || stale {
				return fmt.Errorf("stale Webhook completion=%v: %w", stale, err)
			}
			invalid, err := tx.Webhooks().CompleteAttempt(ctx, delivery.ID, *first.delivery.LeaseToken, repository.CompleteWebhookAttemptRequest{
				AttemptID: first.attempt.ID, HTTPStatus: &status503, ErrorCode: &httpError, ErrorDetail: &detail,
				RetryDelay: time.Nanosecond, MaxAttempts: 3,
			})
			if err != nil || invalid {
				return fmt.Errorf("invalid Webhook completion=%v: %w", invalid, err)
			}
			completed, err := tx.Webhooks().CompleteAttempt(ctx, delivery.ID, *first.delivery.LeaseToken, repository.CompleteWebhookAttemptRequest{
				AttemptID: first.attempt.ID, HTTPStatus: &status503, ErrorCode: &httpError, ErrorDetail: &detail,
				RetryDelay: time.Second, MaxAttempts: 3,
			})
			if err != nil || !completed {
				return fmt.Errorf("first Webhook completion=%v: %w", completed, err)
			}
			return nil
		}); err != nil {
			t.Fatal(err)
		}
		failed, err := store.Webhooks().GetDelivery(ctx, delivery.ID)
		attempts, attemptsErr := store.Webhooks().ListAttempts(ctx, delivery.ID)
		if err != nil || attemptsErr != nil || failed.Status != domain.WebhookDeliveryStatusFailed || failed.NextAttemptAt == nil || len(attempts) != 1 || attempts[0].CompletedAt == nil || failed.NextAttemptAt.Sub(*attempts[0].CompletedAt) != time.Second {
			t.Fatalf("failed Webhook mismatch: delivery=%+v attempts=%+v err=%v attemptsErr=%v", failed, attempts, err, attemptsErr)
		}

		backdateWebhookNextAttempt(t, db, delivery.ID)
		secondDelivery, secondAttempt := claimWebhook(t, ctx, store, repository.ClaimWebhookRequest{
			WorkerID: "webhook-worker-2", LeaseToken: "05000000-0000-4000-8000-000000000003",
			LeaseDuration: time.Minute, MaxAttempts: 3,
		})
		if secondDelivery.AttemptCount != 2 || secondAttempt.AttemptNo != 2 {
			t.Fatalf("second Webhook claim mismatch: delivery=%+v attempt=%+v", secondDelivery, secondAttempt)
		}
		setWebhookLeaseExpiry(t, db, delivery.ID, now.Add(-time.Minute))
		if err := store.WithinTransaction(ctx, func(tx *repository.PostgresTx) error {
			_, recovered, err := tx.Webhooks().RecoverNextExpiredSending(ctx, repository.RecoverExpiredWebhookRequest{
				ErrorCode: "worker_lease_expired", ErrorDetail: "worker did not persist a result",
				RetrySchedule: []time.Duration{time.Second}, MaxAttempts: 3,
			})
			if err != nil || recovered {
				return fmt.Errorf("invalid Webhook recovery=%v: %w", recovered, err)
			}
			return nil
		}); err != nil {
			t.Fatal(err)
		}

		type completionResult struct {
			operation string
			updated   bool
			err       error
		}
		completionStart := make(chan struct{})
		results := make(chan completionResult, 3)
		var workers sync.WaitGroup
		workers.Add(3)
		go func() {
			defer workers.Done()
			<-completionStart
			var updated bool
			err := store.WithinTransaction(ctx, func(tx *repository.PostgresTx) error {
				var err error
				updated, err = tx.Webhooks().CompleteAttempt(ctx, delivery.ID, *secondDelivery.LeaseToken, repository.CompleteWebhookAttemptRequest{
					AttemptID: secondAttempt.ID, HTTPStatus: &status503, ErrorCode: &httpError, ErrorDetail: &detail,
					RetryDelay: 5 * time.Second, MaxAttempts: 3,
				})
				return err
			})
			results <- completionResult{operation: "expired-complete", updated: updated, err: err}
		}()
		for index := 0; index < 2; index++ {
			index := index
			go func() {
				defer workers.Done()
				<-completionStart
				var updated bool
				err := store.WithinTransaction(ctx, func(tx *repository.PostgresTx) error {
					var err error
					recovered, ok, err := tx.Webhooks().RecoverNextExpiredSending(ctx, repository.RecoverExpiredWebhookRequest{
						ErrorCode: "worker_lease_expired", ErrorDetail: "worker did not persist a result",
						RetrySchedule: []time.Duration{time.Second, 5 * time.Second}, MaxAttempts: 3,
					})
					updated = ok && recovered.ID == delivery.ID
					return err
				})
				results <- completionResult{operation: fmt.Sprintf("recover-%d", index+1), updated: updated, err: err}
			}()
		}
		close(completionStart)
		workers.Wait()
		close(results)
		recoveryCount := 0
		for result := range results {
			if result.err != nil {
				t.Fatalf("%s error: %v", result.operation, result.err)
			}
			if result.operation == "expired-complete" && result.updated {
				t.Fatal("expired Webhook completion must lose ownership to recovery")
			}
			if result.operation != "expired-complete" && result.updated {
				recoveryCount++
			}
		}
		if recoveryCount != 1 {
			t.Fatalf("concurrent Webhook recoveries = %d, want 1", recoveryCount)
		}

		backdateWebhookNextAttempt(t, db, delivery.ID)
		_, thirdAttempt := claimWebhook(t, ctx, store, repository.ClaimWebhookRequest{
			WorkerID: "webhook-worker-3", LeaseToken: "05000000-0000-4000-8000-000000000004",
			LeaseDuration: time.Minute, MaxAttempts: 3,
		})
		setWebhookLeaseExpiry(t, db, delivery.ID, now.Add(-time.Minute))
		if err := store.WithinTransaction(ctx, func(tx *repository.PostgresTx) error {
			recovered, ok, err := tx.Webhooks().RecoverNextExpiredSending(ctx, repository.RecoverExpiredWebhookRequest{
				ErrorCode: "worker_lease_expired", ErrorDetail: "worker did not persist a result",
				RetrySchedule: []time.Duration{time.Second, 5 * time.Second}, MaxAttempts: 3,
			})
			if err != nil || !ok || recovered.ID != delivery.ID {
				return fmt.Errorf("terminal Webhook recovery=%v delivery=%s: %w", ok, recovered.ID, err)
			}
			return nil
		}); err != nil {
			t.Fatal(err)
		}
		dead, err := store.Webhooks().GetDelivery(ctx, delivery.ID)
		attempts, attemptsErr = store.Webhooks().ListAttempts(ctx, delivery.ID)
		if err != nil || attemptsErr != nil || dead.Status != domain.WebhookDeliveryStatusDead || dead.AttemptCount != 3 || dead.NextAttemptAt != nil || len(attempts) != 3 || attempts[2].ID != thirdAttempt.ID || attempts[2].CompletedAt == nil {
			t.Fatalf("dead Webhook mismatch: delivery=%+v attempts=%+v err=%v attemptsErr=%v", dead, attempts, err, attemptsErr)
		}

		secretVersion2 := 2
		if err := store.WithinTransaction(ctx, func(tx *repository.PostgresTx) error {
			return tx.Projects().CreateWebhookSecretVersion(ctx, domain.WebhookSecretVersion{
				ProjectID: commandProjectID, Version: secretVersion2, Ciphertext: bytes.Repeat([]byte{0x43}, 17),
				Nonce: bytes.Repeat([]byte{0x44}, 12), EncryptionKeyVersion: 1, CreatedAt: now,
			})
		}); err != nil {
			t.Fatal(err)
		}
		rotationTx, err := db.BeginTx(ctx, nil)
		if err != nil {
			t.Fatal(err)
		}
		defer rotationTx.Rollback()
		if _, err := rotationTx.ExecContext(ctx, `
			UPDATE projects
			SET webhook_url = 'https://example.test/hook-v2', webhook_config_version = 2,
				current_webhook_secret_version = 2, updated_at = now()
			WHERE id = $1
		`, commandProjectID); err != nil {
			t.Fatal(err)
		}
		replayRequest := repository.CreateWebhookReplayRequest{
			ID: "65000000-0000-0000-0000-000000000002", ReplayOfDeliveryID: delivery.ID,
		}
		type replayResult struct {
			delivery domain.WebhookDelivery
			err      error
		}
		replayResults := make(chan replayResult, 1)
		go func() {
			var result replayResult
			result.err = store.WithinTransaction(ctx, func(tx *repository.PostgresTx) error {
				var err error
				result.delivery, err = tx.Webhooks().CreateReplay(ctx, replayRequest)
				return err
			})
			replayResults <- result
		}()
		waitForWebhookReplayProjectLock(t, ctx, db)
		select {
		case result := <-replayResults:
			t.Fatalf("Webhook replay bypassed the uncommitted Project rotation: %+v", result)
		default:
		}
		if err := rotationTx.Commit(); err != nil {
			t.Fatal(err)
		}
		var replay domain.WebhookDelivery
		select {
		case result := <-replayResults:
			if result.err != nil {
				t.Fatal(result.err)
			}
			replay = result.delivery
		case <-ctx.Done():
			t.Fatalf("Webhook replay did not resume after Project rotation: %v", ctx.Err())
		}
		if replay.TargetURL != "https://example.test/hook-v2" || replay.WebhookConfigVersion != 2 || replay.WebhookSecretVersion != 2 || replay.EventID != eventID || !bytes.Equal(replay.RawBody, delivery.RawBody) {
			t.Fatalf("Webhook replay did not derive current Project snapshot: %+v", replay)
		}
		replayedDelivery, replayAttempt := claimWebhook(t, ctx, store, repository.ClaimWebhookRequest{
			WorkerID: "webhook-replay-worker", LeaseToken: "05000000-0000-4000-8000-000000000005",
			LeaseDuration: time.Minute, MaxAttempts: 3,
		})
		status204 := 204
		if err := store.WithinTransaction(ctx, func(tx *repository.PostgresTx) error {
			completed, err := tx.Webhooks().CompleteAttempt(ctx, replay.ID, *replayedDelivery.LeaseToken, repository.CompleteWebhookAttemptRequest{
				AttemptID: replayAttempt.ID, HTTPStatus: &status204, MaxAttempts: 3,
			})
			if err != nil || !completed {
				return fmt.Errorf("complete Webhook replay=%v: %w", completed, err)
			}
			return nil
		}); err != nil {
			t.Fatal(err)
		}
		storedReplay, err := store.Webhooks().GetDelivery(ctx, replayRequest.ID)
		original, originalErr := store.Webhooks().GetDelivery(ctx, delivery.ID)
		if err != nil || originalErr != nil || storedReplay.Status != domain.WebhookDeliveryStatusDelivered || storedReplay.DeliveredAt == nil || !bytes.Equal(storedReplay.RawBody, delivery.RawBody) || original.Status != domain.WebhookDeliveryStatusDead {
			t.Fatalf("Webhook replay changed history: replay=%+v original=%+v err=%v originalErr=%v", storedReplay, original, err, originalErr)
		}

		exhaustEventID := "75000000-0000-0000-0000-000000000002"
		if err := store.WithinTransaction(ctx, func(tx *repository.PostgresTx) error {
			return tx.Events().Create(ctx, domain.Event{
				ID: exhaustEventID, SchemaVersion: domain.EventSchemaVersion, EventType: domain.EventTypeDeviceCreated,
				ProjectID: commandProjectID, DeviceID: stringRef(commandSimulatorID), Source: domain.EventSourceAdmin,
				Payload:          map[string]any{"device_type_code": "smart-lock", "provider_code": "simulator", "lifecycle_status": "active"},
				DeduplicationKey: "webhook-event-2", OccurredAt: now, CreatedAt: now,
			})
		}); err != nil {
			t.Fatal(err)
		}
		exhaustDelivery := testWebhookDeliveryRequest("65000000-0000-0000-0000-000000000003", exhaustEventID)
		if err := store.WithinTransaction(ctx, func(tx *repository.PostgresTx) error {
			_, created, err := tx.Webhooks().CreateDelivery(ctx, exhaustDelivery)
			if err == nil && !created {
				return errors.New("expected Webhook Delivery creation")
			}
			return err
		}); err != nil {
			t.Fatal(err)
		}
		claimedDelivery, claimedAttempt := claimWebhook(t, ctx, store, repository.ClaimWebhookRequest{
			WorkerID: "webhook-budget-worker", LeaseToken: "05000000-0000-4000-8000-000000000006",
			LeaseDuration: time.Minute, MaxAttempts: 5,
		})
		if err := store.WithinTransaction(ctx, func(tx *repository.PostgresTx) error {
			completed, err := tx.Webhooks().CompleteAttempt(ctx, exhaustDelivery.ID, *claimedDelivery.LeaseToken, repository.CompleteWebhookAttemptRequest{
				AttemptID: claimedAttempt.ID, HTTPStatus: &status503, ErrorCode: &httpError, ErrorDetail: &detail,
				RetryDelay: time.Second, MaxAttempts: 5,
			})
			if err != nil || !completed {
				return fmt.Errorf("complete Webhook before budget reduction=%v: %w", completed, err)
			}
			return nil
		}); err != nil {
			t.Fatal(err)
		}

		exhaustStart := make(chan struct{})
		exhaustResults := make(chan bool, 2)
		var exhausters sync.WaitGroup
		for index := 0; index < 2; index++ {
			exhausters.Add(1)
			go func() {
				defer exhausters.Done()
				<-exhaustStart
				var exhausted bool
				err := store.WithinTransaction(ctx, func(tx *repository.PostgresTx) error {
					delivery, ok, err := tx.Webhooks().ExhaustRetryBudget(ctx, 1)
					exhausted = ok && delivery.ID == exhaustDelivery.ID
					return err
				})
				if err != nil {
					t.Errorf("exhaust Webhook retry budget: %v", err)
				}
				exhaustResults <- exhausted
			}()
		}
		close(exhaustStart)
		exhausters.Wait()
		close(exhaustResults)
		exhaustedCount := 0
		for exhausted := range exhaustResults {
			if exhausted {
				exhaustedCount++
			}
		}
		if exhaustedCount != 1 {
			t.Fatalf("concurrent retry-budget exhaustions = %d, want 1", exhaustedCount)
		}
		exhausted, err := store.Webhooks().GetDelivery(ctx, exhaustDelivery.ID)
		exhaustAttempts, attemptsErr := store.Webhooks().ListAttempts(ctx, exhaustDelivery.ID)
		if err != nil || attemptsErr != nil || exhausted.Status != domain.WebhookDeliveryStatusDead || exhausted.AttemptCount != 1 || exhausted.NextAttemptAt != nil || len(exhaustAttempts) != 1 || exhaustAttempts[0].ID != claimedAttempt.ID || exhaustAttempts[0].CompletedAt == nil {
			t.Fatalf("reduced Webhook retry budget changed history: delivery=%+v attempts=%+v err=%v attemptsErr=%v", exhausted, exhaustAttempts, err, attemptsErr)
		}

		if err := store.WithinTransaction(ctx, func(tx *repository.PostgresTx) error {
			if err := tx.Projects().SetWebhookConfiguration(ctx, commandProjectID, nil, 3, &secretVersion2); err != nil {
				return err
			}
			_, err := tx.Webhooks().CreateReplay(ctx, repository.CreateWebhookReplayRequest{
				ID: "65000000-0000-0000-0000-000000000004", ReplayOfDeliveryID: delivery.ID,
			})
			if !errors.Is(err, repository.ErrWebhookNotConfigured) {
				return fmt.Errorf("unconfigured Webhook replay: %w", err)
			}
			return nil
		}); err != nil {
			t.Fatal(err)
		}
		if _, err := store.Webhooks().GetDelivery(ctx, "65000000-0000-0000-0000-000000000004"); !errors.Is(err, sql.ErrNoRows) {
			t.Fatalf("unconfigured Webhook replay wrote a Delivery: %v", err)
		}
		disabledProject, err := store.Projects().Get(ctx, commandProjectID)
		if err != nil || disabledProject.WebhookURL != nil || disabledProject.WebhookConfigVersion != 3 || disabledProject.CurrentWebhookSecretVersion == nil || *disabledProject.CurrentWebhookSecretVersion != secretVersion2 {
			t.Fatalf("disabling Webhook endpoint did not preserve current secret: project=%+v err=%v", disabledProject, err)
		}
	})
}

func TestPostgresSimulatorCompareAndSwapAndRollback(t *testing.T) {
	withRepositoryTestDatabase(t, func(db *sql.DB) {
		ctx := context.Background()
		store := repository.NewPostgresStore(db)
		initial, err := store.Simulator().Get(ctx)
		if err != nil || initial.Outcome != domain.SimulatorOutcomeProviderAccepted || initial.Delay != 0 || initial.Version != 1 {
			t.Fatalf("initial Simulator config=%+v err=%v", initial, err)
		}
		if err := store.WithinTransaction(ctx, func(tx *repository.PostgresTx) error {
			updated, err := tx.Simulator().Update(ctx, initial.Version, repository.UpdateSimulatorRequest{
				Outcome: domain.SimulatorOutcomeProviderAccepted, Delay: time.Nanosecond,
			})
			if err != nil || updated {
				return fmt.Errorf("invalid Simulator update=%v: %w", updated, err)
			}
			return nil
		}); err != nil {
			t.Fatal(err)
		}
		unchanged, err := store.Simulator().Get(ctx)
		if err != nil || unchanged != initial {
			t.Fatalf("invalid Simulator update wrote state: before=%+v after=%+v err=%v", initial, unchanged, err)
		}

		start := make(chan struct{})
		results := make(chan bool, 2)
		var workers sync.WaitGroup
		for index, outcome := range []domain.SimulatorOutcome{domain.SimulatorOutcomeProviderRejected, domain.SimulatorOutcomeInvalidResponse} {
			index := index
			outcome := outcome
			workers.Add(1)
			go func() {
				defer workers.Done()
				<-start
				var updated bool
				err := store.WithinTransaction(ctx, func(tx *repository.PostgresTx) error {
					var err error
					updated, err = tx.Simulator().Update(ctx, initial.Version, repository.UpdateSimulatorRequest{Outcome: outcome, Delay: time.Duration(index+1) * time.Second})
					return err
				})
				if err != nil {
					t.Errorf("Simulator CAS: %v", err)
				}
				results <- updated
			}()
		}
		close(start)
		workers.Wait()
		close(results)
		updates := 0
		for updated := range results {
			if updated {
				updates++
			}
		}
		if updates != 1 {
			t.Fatalf("Simulator CAS updates=%d, want 1", updates)
		}
		current, err := store.Simulator().Get(ctx)
		if err != nil || current.Version != 2 || current.UpdatedAt.Before(initial.UpdatedAt) {
			t.Fatalf("updated Simulator config=%+v err=%v", current, err)
		}

		sentinel := errors.New("rollback Simulator update")
		err = store.WithinTransaction(ctx, func(tx *repository.PostgresTx) error {
			updated, err := tx.Simulator().Update(ctx, current.Version, repository.UpdateSimulatorRequest{
				Outcome: domain.SimulatorOutcomeTransportErrorAfterSend, Delay: 3 * time.Second,
			})
			if err != nil || !updated {
				return fmt.Errorf("rollback update=%v: %w", updated, err)
			}
			return sentinel
		})
		if !errors.Is(err, sentinel) {
			t.Fatalf("Simulator rollback error=%v", err)
		}
		afterRollback, err := store.Simulator().Get(ctx)
		if err != nil || afterRollback != current {
			t.Fatalf("Simulator rollback changed config: before=%+v after=%+v err=%v", current, afterRollback, err)
		}
	})
}

func createWebhookFixtures(t *testing.T, ctx context.Context, store *repository.PostgresStore, now time.Time) string {
	t.Helper()
	createCommandFixtures(t, ctx, store, now)
	eventID := "75000000-0000-0000-0000-000000000001"
	secretVersion := 1
	if err := store.WithinTransaction(ctx, func(tx *repository.PostgresTx) error {
		if err := tx.Projects().CreateWebhookSecretVersion(ctx, domain.WebhookSecretVersion{
			ProjectID: commandProjectID, Version: secretVersion, Ciphertext: bytes.Repeat([]byte{0x41}, 17),
			Nonce: bytes.Repeat([]byte{0x42}, 12), EncryptionKeyVersion: 1, CreatedAt: now,
		}); err != nil {
			return err
		}
		if err := tx.Projects().SetWebhookConfiguration(ctx, commandProjectID, stringRef("https://example.test/hook"), 1, &secretVersion); err != nil {
			return err
		}
		return tx.Events().Create(ctx, domain.Event{
			ID: eventID, SchemaVersion: domain.EventSchemaVersion, EventType: domain.EventTypeDeviceCreated,
			ProjectID: commandProjectID, DeviceID: stringRef(commandSimulatorID), Source: domain.EventSourceAdmin,
			Payload:          map[string]any{"device_type_code": "smart-lock", "provider_code": "simulator", "lifecycle_status": "active"},
			DeduplicationKey: "webhook-event-1", OccurredAt: now, CreatedAt: now,
		})
	}); err != nil {
		t.Fatal(err)
	}
	return eventID
}

func waitForWebhookReplayProjectLock(t *testing.T, ctx context.Context, db *sql.DB) {
	t.Helper()
	deadline := time.NewTimer(2 * time.Second)
	defer deadline.Stop()
	ticker := time.NewTicker(10 * time.Millisecond)
	defer ticker.Stop()
	for {
		var waiting bool
		if err := db.QueryRowContext(ctx, `
			SELECT EXISTS (
				SELECT 1
				FROM pg_stat_activity
				WHERE pid <> pg_backend_pid()
					AND datname = current_database()
					AND wait_event_type = 'Lock'
					AND query LIKE '%lock_current_webhook_config%'
			)
		`).Scan(&waiting); err != nil {
			t.Fatal(err)
		}
		if waiting {
			return
		}
		select {
		case <-ticker.C:
		case <-deadline.C:
			t.Fatal("Webhook replay did not wait on the Project configuration lock")
		case <-ctx.Done():
			t.Fatalf("waiting for Webhook replay Project lock: %v", ctx.Err())
		}
	}
}

func testWebhookDeliveryRequest(id, eventID string) repository.CreateWebhookDeliveryRequest {
	return repository.CreateWebhookDeliveryRequest{
		ID: id, EventID: eventID, RawBody: []byte(`{"schema_version":1,"event_id":"` + eventID + `"}`),
	}
}

func claimWebhook(t *testing.T, ctx context.Context, store *repository.PostgresStore, request repository.ClaimWebhookRequest) (domain.WebhookDelivery, domain.WebhookDeliveryAttempt) {
	t.Helper()
	var delivery domain.WebhookDelivery
	var attempt domain.WebhookDeliveryAttempt
	if err := store.WithinTransaction(ctx, func(tx *repository.PostgresTx) error {
		var claimed bool
		var err error
		delivery, attempt, claimed, err = tx.Webhooks().ClaimDue(ctx, request)
		if err != nil {
			return err
		}
		if !claimed {
			return errors.New("expected Webhook claim")
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	return delivery, attempt
}

func backdateWebhookNextAttempt(t *testing.T, db *sql.DB, deliveryID string) {
	t.Helper()
	if _, err := db.Exec(`UPDATE webhook_deliveries SET next_attempt_at = now() - interval '1 second' WHERE id = $1`, deliveryID); err != nil {
		t.Fatal(err)
	}
}

func setWebhookLeaseExpiry(t *testing.T, db *sql.DB, deliveryID string, expiresAt time.Time) {
	t.Helper()
	if _, err := db.Exec(`UPDATE webhook_deliveries SET lease_expires_at = $2 WHERE id = $1`, deliveryID, expiresAt); err != nil {
		t.Fatal(err)
	}
}
