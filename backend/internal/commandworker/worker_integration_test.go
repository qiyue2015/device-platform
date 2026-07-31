//go:build integration

package commandworker

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	_ "github.com/lib/pq"
	"github.com/qiyue2015/device-platform/internal/domain"
	"github.com/qiyue2015/device-platform/internal/provideradapter"
	simulatorruntime "github.com/qiyue2015/device-platform/internal/simulator"
	"github.com/qiyue2015/device-platform/internal/storage"
	"github.com/qiyue2015/device-platform/internal/storage/repository"
)

const (
	workerProjectID = "91000000-0000-4000-8000-000000000001"
	workerDeviceID  = "92000000-0000-4000-8000-000000000001"
	workerCommandID = "93000000-0000-4000-8000-000000000001"
)

func TestPersistentWorkerWWTIOTResultMatrix(t *testing.T) {
	tests := []struct {
		name       string
		result     provideradapter.DispatchResult
		status     domain.CommandStatus
		reasonCode string
	}{
		{
			name: "provider accepted remains sent",
			result: provideradapter.DispatchResult{
				Outcome: domain.AttemptOutcomeProviderAccepted, ConfirmationLevel: domain.ConfirmationProviderAccepted,
				EvidenceStatus: domain.EvidenceUnverified, ResponseSummary: map[string]any{"result": "ok"},
			},
			status: domain.CommandStatusSent,
		},
		{
			name: "provider rejected fails",
			result: provideradapter.DispatchResult{
				Outcome: domain.AttemptOutcomeProviderRejected, ConfirmationLevel: domain.ConfirmationTransportSent,
				EvidenceStatus: domain.EvidenceUnverified, ResponseSummary: map[string]any{"result": "denied"}, ErrorDetail: "denied",
			},
			status: domain.CommandStatusFailed, reasonCode: "provider_rejected",
		},
		{
			name: "before send transport fails",
			result: provideradapter.DispatchResult{
				Outcome: domain.AttemptOutcomeTransportErrorBeforeSend, ConfirmationLevel: domain.ConfirmationNone,
				EvidenceStatus: domain.EvidenceNone, ResponseSummary: map[string]any{}, ErrorDetail: "dial failed",
			},
			status: domain.CommandStatusFailed, reasonCode: "provider_transport_error",
		},
		{
			name: "after send transport is unknown",
			result: provideradapter.DispatchResult{
				Outcome: domain.AttemptOutcomeTransportErrorAfterSend, ConfirmationLevel: domain.ConfirmationTransportSent,
				EvidenceStatus: domain.EvidenceVerified, ResponseSummary: map[string]any{}, ErrorDetail: "connection closed",
			},
			status: domain.CommandStatusUnknown, reasonCode: "provider_delivery_unknown",
		},
		{
			name: "invalid response is unknown",
			result: provideradapter.DispatchResult{
				Outcome: domain.AttemptOutcomeInvalidResponse, ConfirmationLevel: domain.ConfirmationTransportSent,
				EvidenceStatus: domain.EvidenceVerified, ResponseSummary: map[string]any{"result": "ok"}, ErrorDetail: "echo mismatch",
			},
			status: domain.CommandStatusUnknown, reasonCode: "provider_response_invalid",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			withWorkerDatabase(t, func(db *sql.DB, store *repository.PostgresStore) {
				seedWorkerCommand(t, store, time.Now().UTC().Add(30*time.Second), false)
				adapter := &fakeAdapter{configured: true, result: test.result}
				worker := newTestWorker(t, store, adapter, nil)
				worked, err := worker.DispatchNext(context.Background())
				if err != nil || !worked {
					t.Fatalf("DispatchNext worked=%v err=%v", worked, err)
				}
				command, err := store.Commands().Get(context.Background(), workerCommandID)
				if err != nil {
					t.Fatal(err)
				}
				if command.Status != test.status || pointerValue(command.ReasonCode) != test.reasonCode {
					t.Fatalf("Command status/reason=%s/%q, want %s/%q", command.Status, pointerValue(command.ReasonCode), test.status, test.reasonCode)
				}
				if command.Status == domain.CommandStatusAcked || command.Status == domain.CommandStatusSuccess {
					t.Fatalf("Provider response fabricated Device result: %s", command.Status)
				}
				attempts, err := store.Commands().ListAttempts(context.Background(), workerCommandID)
				if err != nil || len(attempts) != 1 {
					t.Fatalf("Attempts=%+v err=%v", attempts, err)
				}
				attempt := attempts[0]
				if attempt.Phase != domain.AttemptPhaseCompleted || attempt.Outcome == nil || *attempt.Outcome != test.result.Outcome {
					t.Fatalf("Attempt=%+v", attempt)
				}
				if attempt.ProviderRequestKey == "" || len(attempt.ProviderRequestKey) > 9 || attempt.RequestSummary["cmd"] != "open" {
					t.Fatalf("persisted provider request identity/summary=%q/%+v", attempt.ProviderRequestKey, attempt.RequestSummary)
				}
				events, err := store.Events().ListByCommand(context.Background(), workerCommandID)
				if err != nil || len(events) != 2 {
					t.Fatalf("status Events=%+v err=%v", events, err)
				}
				if events[0].Payload["from"] != "queued" || events[0].Payload["to"] != "sent" || events[1].Payload["to"] != string(test.status) {
					t.Fatalf("status Event history=%+v", events)
				}
				if adapter.calls.Load() != 1 {
					t.Fatalf("adapter calls=%d", adapter.calls.Load())
				}
				assertWorkerTableCount(t, db, "webhook_deliveries", 0)
			})
		})
	}
}

func TestPersistentWorkerSimulatorResultMatrix(t *testing.T) {
	tests := []struct {
		outcome      domain.SimulatorOutcome
		status       domain.CommandStatus
		reason       string
		confirmation domain.ConfirmationLevel
		evidence     domain.EvidenceStatus
	}{
		{domain.SimulatorOutcomeProviderAccepted, domain.CommandStatusSent, "", domain.ConfirmationProviderAccepted, domain.EvidenceVerified},
		{domain.SimulatorOutcomeProviderRejected, domain.CommandStatusFailed, "provider_rejected", domain.ConfirmationTransportSent, domain.EvidenceVerified},
		{domain.SimulatorOutcomeTransportErrorBeforeSend, domain.CommandStatusFailed, "provider_transport_error", domain.ConfirmationNone, domain.EvidenceNone},
		{domain.SimulatorOutcomeTransportErrorAfterSend, domain.CommandStatusUnknown, "provider_delivery_unknown", domain.ConfirmationTransportSent, domain.EvidenceVerified},
		{domain.SimulatorOutcomeInvalidResponse, domain.CommandStatusUnknown, "provider_response_invalid", domain.ConfirmationTransportSent, domain.EvidenceVerified},
	}
	for _, test := range tests {
		t.Run(string(test.outcome), func(t *testing.T) {
			withWorkerDatabase(t, func(db *sql.DB, store *repository.PostgresStore) {
				setSimulatorConfig(t, store, test.outcome, 0)
				seedWorkerCommand(t, store, time.Now().UTC().Add(30*time.Second), true)
				makeWorkerDeviceSimulator(t, db)
				worker := newSimulatorWorker(t, store, Config{WorkerID: "simulator-matrix-worker"})
				worked, err := worker.DispatchNext(context.Background())
				if err != nil || !worked {
					t.Fatalf("DispatchNext worked=%v err=%v", worked, err)
				}
				command, err := store.Commands().Get(context.Background(), workerCommandID)
				if err != nil || command.Status != test.status || pointerValue(command.ReasonCode) != test.reason ||
					command.ConfirmationLevel != test.confirmation || command.EvidenceStatus != test.evidence {
					t.Fatalf("Simulator Command=%+v err=%v", command, err)
				}
				if command.Status == domain.CommandStatusAcked || command.Status == domain.CommandStatusSuccess {
					t.Fatalf("Simulator fabricated Device result: %s", command.Status)
				}
				attempts, err := store.Commands().ListAttempts(context.Background(), workerCommandID)
				if err != nil || len(attempts) != 1 || attempts[0].Outcome == nil || *attempts[0].Outcome != domain.AttemptOutcome(test.outcome) {
					t.Fatalf("Simulator Attempts=%+v err=%v", attempts, err)
				}
				attempt := attempts[0]
				if attempt.RequestSummary["simulator_outcome"] != string(test.outcome) || attempt.RequestSummary["simulator_config_version"] != float64(2) {
					t.Fatalf("Simulator request snapshot=%+v", attempt.RequestSummary)
				}
				events, err := store.Events().ListByCommand(context.Background(), workerCommandID)
				if err != nil || len(events) != 2 || events[0].Source != domain.EventSourceSimulator || events[1].Source != domain.EventSourceSimulator {
					t.Fatalf("Simulator Events=%+v err=%v", events, err)
				}
				assertWorkerTableCount(t, db, "webhook_deliveries", 2)
			})
		})
	}
}

func TestPersistentWorkerSimulatorClaimSnapshotSurvivesConfigChangeAndReclaim(t *testing.T) {
	withWorkerDatabase(t, func(db *sql.DB, store *repository.PostgresStore) {
		setSimulatorConfig(t, store, domain.SimulatorOutcomeProviderRejected, 0)
		seedWorkerCommand(t, store, time.Now().UTC().Add(30*time.Second), false)
		makeWorkerDeviceSimulator(t, db)
		first := newSimulatorWorker(t, store, Config{WorkerID: "simulator-first", LeaseDuration: time.Second})
		registration := first.adapters[0]
		command, attempt, claimed, err := first.claimNext(context.Background(), registration)
		if err != nil || !claimed {
			t.Fatalf("first claim=%v err=%v", claimed, err)
		}
		if attempt.RequestSummary["simulator_outcome"] != string(domain.SimulatorOutcomeProviderRejected) {
			t.Fatalf("first claim snapshot=%+v", attempt.RequestSummary)
		}
		setSimulatorConfig(t, store, domain.SimulatorOutcomeProviderAccepted, 0)
		if _, err := db.Exec(`
			UPDATE device_command_attempts
			SET claimed_at = now() - interval '2 seconds', lease_expires_at = now() - interval '1 second'
			WHERE id = $1
		`, attempt.ID); err != nil {
			t.Fatal(err)
		}

		second := newSimulatorWorker(t, store, Config{WorkerID: "simulator-second", LeaseDuration: time.Second})
		secondRegistration := second.adapters[0]
		reclaimedCommand, reclaimedAttempt, reclaimed, err := second.claimNext(context.Background(), secondRegistration)
		if err != nil || !reclaimed || reclaimedAttempt.ID != attempt.ID || reclaimedCommand.ID != command.ID {
			t.Fatalf("reclaim=%v Command=%+v Attempt=%+v err=%v", reclaimed, reclaimedCommand, reclaimedAttempt, err)
		}
		if reclaimedAttempt.RequestSummary["simulator_outcome"] != string(domain.SimulatorOutcomeProviderRejected) ||
			reclaimedAttempt.RequestSummary["simulator_config_version"] != float64(2) {
			t.Fatalf("reclaim applied live Simulator config: %+v", reclaimedAttempt.RequestSummary)
		}
		if err := second.dispatchClaimed(context.Background(), secondRegistration, reclaimedCommand, reclaimedAttempt); err != nil {
			t.Fatal(err)
		}
		assertCommandStatus(t, store, domain.CommandStatusFailed, "provider_rejected")
	})
}

func TestPersistentWorkerSimulatorProfileTimeoutIsAfterSend(t *testing.T) {
	withWorkerDatabase(t, func(db *sql.DB, store *repository.PostgresStore) {
		setSimulatorConfig(t, store, domain.SimulatorOutcomeTransportErrorBeforeSend, 100*time.Millisecond)
		seedWorkerCommand(t, store, time.Now().UTC().Add(30*time.Second), false)
		makeWorkerDeviceSimulator(t, db)
		timeoutStore := &providerTimeoutStore{PostgresStore: store, timeout: time.Millisecond}
		worker := newSimulatorWorker(t, timeoutStore, Config{WorkerID: "simulator-timeout-worker"})
		worked, err := worker.DispatchNext(context.Background())
		if err != nil || !worked {
			t.Fatalf("DispatchNext worked=%v err=%v", worked, err)
		}
		assertCommandStatus(t, store, domain.CommandStatusUnknown, "provider_delivery_unknown")
		attempts, err := store.Commands().ListAttempts(context.Background(), workerCommandID)
		if err != nil || len(attempts) != 1 || attempts[0].Outcome == nil ||
			*attempts[0].Outcome != domain.AttemptOutcomeTransportErrorAfterSend ||
			attempts[0].ConfirmationLevel != domain.ConfirmationTransportSent || attempts[0].EvidenceStatus != domain.EvidenceVerified {
			t.Fatalf("timeout Attempt=%+v err=%v", attempts, err)
		}
	})
}

func TestPersistentWorkerSimulatorSnapshotLockOrdersClaimAndConfigCommit(t *testing.T) {
	t.Run("claim lock commits before config update", func(t *testing.T) {
		withWorkerDatabase(t, func(db *sql.DB, store *repository.PostgresStore) {
			setSimulatorConfig(t, store, domain.SimulatorOutcomeProviderRejected, 0)
			seedWorkerCommand(t, store, time.Now().UTC().Add(30*time.Second), false)
			makeWorkerDeviceSimulator(t, db)
			locked, release := make(chan struct{}), make(chan struct{})
			defer closeIfOpen(release)
			worker := newSimulatorWorkerWithSnapshot(t, store, Config{WorkerID: "claim-lock-worker"}, func(ctx context.Context, tx repository.CommandTx) (map[string]any, error) {
				snapshot, err := simulatorruntime.ClaimSnapshot(ctx, tx)
				close(locked)
				<-release
				return snapshot, err
			})
			type claimResult struct {
				attempt domain.CommandAttempt
				err     error
			}
			claimed := make(chan claimResult, 1)
			go func() {
				_, attempt, _, err := worker.claimNext(context.Background(), worker.adapters[0])
				claimed <- claimResult{attempt: attempt, err: err}
			}()
			<-locked
			updateStarted, updateDone := make(chan struct{}), make(chan error, 1)
			go func() {
				close(updateStarted)
				updateDone <- updateSimulatorConfig(store, domain.SimulatorOutcomeInvalidResponse, 0)
			}()
			<-updateStarted
			waitForSimulatorLockWait(t, db)
			close(release)
			claim := <-claimed
			if claim.err != nil || claim.attempt.RequestSummary["simulator_outcome"] != string(domain.SimulatorOutcomeProviderRejected) {
				t.Fatalf("claim result=%+v", claim)
			}
			if err := <-updateDone; err != nil {
				t.Fatal(err)
			}
		})
	})

	t.Run("config lock commits before claim snapshot", func(t *testing.T) {
		withWorkerDatabase(t, func(db *sql.DB, store *repository.PostgresStore) {
			seedWorkerCommand(t, store, time.Now().UTC().Add(30*time.Second), false)
			makeWorkerDeviceSimulator(t, db)
			updateLocked, releaseUpdate, updateDone := make(chan struct{}), make(chan struct{}), make(chan error, 1)
			defer closeIfOpen(releaseUpdate)
			go func() {
				updateDone <- store.TransactSimulator(context.Background(), func(tx repository.SimulatorTx) error {
					current, err := tx.Simulator().GetForUpdate(context.Background())
					if err != nil {
						return err
					}
					updated, err := tx.Simulator().Update(context.Background(), current.Version, repository.UpdateSimulatorRequest{
						Outcome: domain.SimulatorOutcomeInvalidResponse, Delay: 5 * time.Millisecond,
					})
					if err != nil || !updated {
						return fmt.Errorf("update=%v: %w", updated, err)
					}
					close(updateLocked)
					<-releaseUpdate
					return nil
				})
			}()
			<-updateLocked
			worker := newSimulatorWorker(t, store, Config{WorkerID: "update-lock-worker"})
			claimed := make(chan domain.CommandAttempt, 1)
			claimErrors := make(chan error, 1)
			go func() {
				_, attempt, _, err := worker.claimNext(context.Background(), worker.adapters[0])
				claimed <- attempt
				claimErrors <- err
			}()
			waitForSimulatorLockWait(t, db)
			close(releaseUpdate)
			if err := <-updateDone; err != nil {
				t.Fatal(err)
			}
			attempt := <-claimed
			if err := <-claimErrors; err != nil {
				t.Fatal(err)
			}
			if attempt.RequestSummary["simulator_outcome"] != string(domain.SimulatorOutcomeInvalidResponse) ||
				attempt.RequestSummary["simulator_delay_ms"] != float64(5) || attempt.RequestSummary["simulator_config_version"] != float64(2) {
				t.Fatalf("claim did not observe committed config=%+v", attempt.RequestSummary)
			}
		})
	})
}

func TestPersistentWorkerRotatesProviderClaimPriority(t *testing.T) {
	withWorkerDatabase(t, func(_ *sql.DB, store *repository.PostgresStore) {
		seedWorkerCommand(t, store, time.Now().UTC().Add(30*time.Second), false)
		setSimulatorConfig(t, store, domain.SimulatorOutcomeProviderAccepted, 0)
		seedFairnessCommands(t, store)
		wwtiotAdapter := &fakeAdapter{configured: true, result: acceptedResult()}
		worker, err := New(store, Config{WorkerID: "provider-fairness-worker", Adapters: []AdapterRegistration{
			{ProviderCode: domain.ProviderCodeWWTIOT, AdapterCode: domain.AdapterWWTIOTCloudAPI, Adapter: wwtiotAdapter, ResultSource: domain.EventSourceSystem},
			{ProviderCode: domain.ProviderCodeSimulator, AdapterCode: domain.AdapterSimulator, Adapter: simulatorruntime.NewAdapter(), ResultSource: domain.EventSourceSimulator, ClaimSnapshot: simulatorruntime.ClaimSnapshot},
		}})
		if err != nil {
			t.Fatal(err)
		}
		for range 2 {
			worked, dispatchErr := worker.DispatchNext(context.Background())
			if dispatchErr != nil || !worked {
				t.Fatalf("DispatchNext worked=%v err=%v", worked, dispatchErr)
			}
		}
		simulatorCommand, err := store.Commands().Get(context.Background(), "93000000-0000-4000-8000-000000000003")
		if err != nil || simulatorCommand.Status != domain.CommandStatusSent {
			t.Fatalf("Simulator Command starved=%+v err=%v", simulatorCommand, err)
		}
		secondWWTIOT, err := store.Commands().Get(context.Background(), "93000000-0000-4000-8000-000000000002")
		if err != nil || secondWWTIOT.Status != domain.CommandStatusQueued {
			t.Fatalf("second WWTIOT Command should remain queued after rotated claim=%+v err=%v", secondWWTIOT, err)
		}
	})
}

func TestPersistentWorkerClaimsOnceAndReclaimsExpiredClaim(t *testing.T) {
	withWorkerDatabase(t, func(_ *sql.DB, store *repository.PostgresStore) {
		seedWorkerCommand(t, store, time.Now().UTC().Add(30*time.Second), false)
		adapter := &fakeAdapter{configured: true, result: acceptedResult()}
		first := newTestWorkerWithConfig(t, store, adapter, Config{WorkerID: "worker-first", LeaseDuration: 20 * time.Millisecond})
		second := newTestWorkerWithConfig(t, store, adapter, Config{WorkerID: "worker-second", LeaseDuration: time.Second})
		registration := first.adapters[0]
		command, claimedAttempt, claimed, err := first.claimNext(context.Background(), registration)
		if err != nil || !claimed {
			t.Fatalf("initial claim=%v err=%v", claimed, err)
		}
		time.Sleep(30 * time.Millisecond)
		reclaimedCommand, reclaimedAttempt, reclaimed, err := second.claimNext(context.Background(), second.adapters[0])
		if err != nil || !reclaimed {
			t.Fatalf("reclaim=%v err=%v", reclaimed, err)
		}
		if reclaimedCommand.ID != command.ID || reclaimedAttempt.ID != claimedAttempt.ID || reclaimedAttempt.ProviderRequestKey != claimedAttempt.ProviderRequestKey || reclaimedAttempt.LeaseToken == claimedAttempt.LeaseToken {
			t.Fatalf("reclaim changed frozen Attempt identity: before=%+v after=%+v", claimedAttempt, reclaimedAttempt)
		}

		var outcomes [2]struct {
			worked bool
			err    error
		}
		var wait sync.WaitGroup
		wait.Add(2)
		for index, worker := range []*Worker{first, second} {
			go func(index int, worker *Worker) {
				defer wait.Done()
				outcomes[index].worked, outcomes[index].err = worker.DispatchNext(context.Background())
			}(index, worker)
		}
		wait.Wait()
		if outcomes[0].worked || outcomes[1].worked {
			t.Fatalf("valid reclaimed lease must exclude another claim: %+v", outcomes)
		}
	})
}

func TestPersistentWorkerRetriesProviderRequestKeyConflict(t *testing.T) {
	withWorkerDatabase(t, func(db *sql.DB, store *repository.PostgresStore) {
		seedWorkerCommand(t, store, time.Now().UTC().Add(30*time.Second), false)
		if _, err := db.Exec(`
			INSERT INTO device_command_attempts (
				id, command_id, attempt_no, phase, provider_code, adapter,
				provider_request_key, outcome, request_summary, response_summary,
				confirmation_level, evidence_status, lease_token, lease_owner,
				lease_expires_at, claimed_at, completed_at
			) VALUES (
				'97000000-0000-4000-8000-000000000001', $1, 1, 'completed', 'wwtiot',
				'wwtiot_cloud_api', '1', 'not_dispatched', '{"serialnum":1}'::jsonb, '{}'::jsonb,
				'none', 'none', '98000000-0000-4000-8000-000000000001', 'fixture',
				now() - interval '1 second', now() - interval '2 seconds', now() - interval '1 second'
			)
		`, workerCommandID); err != nil {
			t.Fatal(err)
		}
		reader := &serialReader{serials: []uint32{0, 1}}
		adapter := &fakeAdapter{configured: true, result: acceptedResult()}
		worker := newTestWorker(t, store, adapter, reader)
		worked, err := worker.DispatchNext(context.Background())
		if err != nil || !worked {
			t.Fatalf("DispatchNext worked=%v err=%v", worked, err)
		}
		attempts, err := store.Commands().ListAttempts(context.Background(), workerCommandID)
		if err != nil || len(attempts) != 2 || attempts[1].ProviderRequestKey != "2" {
			t.Fatalf("request-key retry Attempts=%+v err=%v", attempts, err)
		}
	})
}

func TestPersistentWorkerFailsClosedBeforeDispatch(t *testing.T) {
	tests := []struct {
		name    string
		adapter *fakeAdapter
	}{
		{name: "configuration drift", adapter: &fakeAdapter{configured: false}},
		{name: "prepare failure", adapter: &fakeAdapter{configured: true, prepareErr: errors.New("adapter preflight failed")}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			withWorkerDatabase(t, func(_ *sql.DB, store *repository.PostgresStore) {
				seedWorkerCommand(t, store, time.Now().UTC().Add(30*time.Second), false)
				worker := newTestWorker(t, store, test.adapter, nil)
				worked, err := worker.DispatchNext(context.Background())
				if err != nil || !worked {
					t.Fatalf("DispatchNext worked=%v err=%v", worked, err)
				}
				assertCommandStatus(t, store, domain.CommandStatusFailed, "provider_not_configured")
				attempts, err := store.Commands().ListAttempts(context.Background(), workerCommandID)
				if err != nil || len(attempts) != 1 || attempts[0].Outcome == nil || *attempts[0].Outcome != domain.AttemptOutcomeInvalidRequest || attempts[0].DispatchingAt != nil {
					t.Fatalf("preflight Attempt=%+v err=%v", attempts, err)
				}
				if test.adapter.calls.Load() != 0 {
					t.Fatalf("preflight failure performed %d external calls", test.adapter.calls.Load())
				}
			})
		})
	}
}

func TestPersistentWorkerFailsClosedForInvalidSimulatorClaimSnapshot(t *testing.T) {
	withWorkerDatabase(t, func(db *sql.DB, store *repository.PostgresStore) {
		seedWorkerCommand(t, store, time.Now().UTC().Add(30*time.Second), false)
		makeWorkerDeviceSimulator(t, db)
		worker := newSimulatorWorker(t, store, Config{WorkerID: "invalid-simulator-snapshot"})
		registration := worker.adapters[0]

		command, attempt, claimed, err := worker.claimNext(context.Background(), registration)
		if err != nil || !claimed {
			t.Fatalf("claimNext claimed=%v err=%v", claimed, err)
		}
		if _, err := db.Exec(`UPDATE device_command_attempts SET request_summary = '{}'::jsonb WHERE id = $1`, attempt.ID); err != nil {
			t.Fatal(err)
		}
		attempt.RequestSummary = map[string]any{}

		if err := worker.dispatchClaimed(context.Background(), registration, command, attempt); err != nil {
			t.Fatalf("dispatchClaimed: %v", err)
		}
		assertCommandStatus(t, store, domain.CommandStatusFailed, "provider_not_configured")
		attempts, err := store.Commands().ListAttempts(context.Background(), workerCommandID)
		if err != nil || len(attempts) != 1 || attempts[0].Phase != domain.AttemptPhaseCompleted ||
			attempts[0].Outcome == nil || *attempts[0].Outcome != domain.AttemptOutcomeInvalidRequest || attempts[0].DispatchingAt != nil {
			t.Fatalf("invalid snapshot Attempt=%+v err=%v", attempts, err)
		}
		worked, err := worker.DispatchNext(context.Background())
		if err != nil || worked {
			t.Fatalf("completed invalid snapshot was reclaimed: worked=%v err=%v", worked, err)
		}
	})
}

func TestPersistentWorkerNearDispatchDeadlineKeepsDispatchLease(t *testing.T) {
	withWorkerDatabase(t, func(_ *sql.DB, store *repository.PostgresStore) {
		seedWorkerCommand(t, store, time.Now().UTC().Add(time.Second), false)
		adapter := &fakeAdapter{configured: true, result: acceptedResult(), delay: 1200 * time.Millisecond}
		worker := newTestWorkerWithConfig(t, store, adapter, Config{WorkerID: "near-deadline", LeaseDuration: 100 * time.Millisecond})
		worked, err := worker.DispatchNext(context.Background())
		if err != nil || !worked {
			t.Fatalf("DispatchNext worked=%v err=%v", worked, err)
		}
		command, err := store.Commands().Get(context.Background(), workerCommandID)
		if err != nil || command.Status != domain.CommandStatusSent || command.ConfirmationLevel != domain.ConfirmationProviderAccepted {
			t.Fatalf("near-deadline response was not persisted: command=%+v err=%v", command, err)
		}
		attempts, _ := store.Commands().ListAttempts(context.Background(), workerCommandID)
		if len(attempts) != 1 || attempts[0].LeaseExpiresAt.Before(*attempts[0].DispatchingAt) || attempts[0].LeaseExpiresAt.Before(time.Now().UTC()) {
			t.Fatalf("dispatch lease was not renewed beyond old deadline: %+v", attempts)
		}
	})
}

func TestPersistentWorkerEnforcesProfileProviderRequestTimeout(t *testing.T) {
	withWorkerDatabase(t, func(_ *sql.DB, store *repository.PostgresStore) {
		seedWorkerCommand(t, store, time.Now().UTC().Add(30*time.Second), false)
		adapter := &fakeAdapter{configured: true, result: acceptedResult(), delay: time.Second}
		profileStore := &providerTimeoutStore{PostgresStore: store, timeout: 20 * time.Millisecond}
		worker := newTestWorker(t, profileStore, adapter, nil)

		startedAt := time.Now()
		worked, err := worker.DispatchNext(context.Background())
		if err != nil || !worked {
			t.Fatalf("DispatchNext worked=%v err=%v", worked, err)
		}
		if elapsed := time.Since(startedAt); elapsed >= time.Second {
			t.Fatalf("Provider request ignored profile timeout: elapsed=%s", elapsed)
		}
		assertCommandStatus(t, store, domain.CommandStatusFailed, "provider_transport_error")
		attempts, err := store.Commands().ListAttempts(context.Background(), workerCommandID)
		if err != nil || len(attempts) != 1 || attempts[0].Outcome == nil || *attempts[0].Outcome != domain.AttemptOutcomeTransportErrorBeforeSend {
			t.Fatalf("timed-out Attempt=%+v err=%v", attempts, err)
		}
		if adapter.calls.Load() != 1 {
			t.Fatalf("timed-out Adapter calls=%d", adapter.calls.Load())
		}
	})
}

func TestPersistentWorkerCrashRecoveryAndLateResultFencing(t *testing.T) {
	withWorkerDatabase(t, func(db *sql.DB, store *repository.PostgresStore) {
		seedWorkerCommand(t, store, time.Now().UTC().Add(30*time.Second), false)
		release := make(chan struct{})
		started := make(chan struct{})
		adapter := &fakeAdapter{configured: true, result: acceptedResult(), started: started, release: release}
		worker := newTestWorker(t, store, adapter, nil)
		result := make(chan error, 1)
		go func() {
			_, err := worker.DispatchNext(context.Background())
			result <- err
		}()
		<-started
		if _, err := db.Exec(`
			UPDATE device_command_attempts
			SET claimed_at = now() - interval '2 seconds',
				dispatching_at = now() - interval '2 seconds',
				lease_expires_at = now() - interval '1 second'
			WHERE command_id = $1
		`, workerCommandID); err != nil {
			t.Fatal(err)
		}
		recovered, err := worker.RecoverNext(context.Background())
		if err != nil || !recovered {
			t.Fatalf("RecoverNext recovered=%v err=%v", recovered, err)
		}
		close(release)
		if err := <-result; !errors.Is(err, ErrLeaseLost) {
			t.Fatalf("late result error=%v, want ErrLeaseLost", err)
		}
		command, err := store.Commands().Get(context.Background(), workerCommandID)
		if err != nil || command.Status != domain.CommandStatusUnknown || pointerValue(command.ReasonCode) != "provider_delivery_unknown" {
			t.Fatalf("recovered Command=%+v err=%v", command, err)
		}
		attempts, _ := store.Commands().ListAttempts(context.Background(), workerCommandID)
		if len(attempts) != 1 || attempts[0].Outcome == nil || *attempts[0].Outcome != domain.AttemptOutcomeTransportErrorAfterSend || adapter.calls.Load() != 1 {
			t.Fatalf("crash evidence/replay mismatch: attempts=%+v calls=%d", attempts, adapter.calls.Load())
		}
		again, err := worker.RecoverNext(context.Background())
		if err != nil || again {
			t.Fatalf("repeated recovery changed terminal result: recovered=%v err=%v", again, err)
		}
	})
}

func TestPersistentWorkerCrashAfterSentCommitDoesNotDispatch(t *testing.T) {
	withWorkerDatabase(t, func(_ *sql.DB, store *repository.PostgresStore) {
		seedWorkerCommand(t, store, time.Now().UTC().Add(30*time.Second), false)
		adapter := &fakeAdapter{configured: true, result: acceptedResult()}
		worker := newTestWorkerWithConfig(t, store, adapter, Config{
			WorkerID: "crashed-worker", LeaseDuration: time.Second,
		})
		registration := worker.adapters[0]
		command, attempt, claimed, err := worker.claimNext(context.Background(), registration)
		if err != nil || !claimed {
			t.Fatalf("claimNext=%v err=%v", claimed, err)
		}
		prepared, err := adapter.Prepare(provideradapter.DispatchRequest{
			ProviderDeviceID: "LOCK-WORKER-1", Action: command.CommandType,
			Payload: command.Payload, ProviderRequestKey: attempt.ProviderRequestKey,
		})
		if err != nil {
			t.Fatal(err)
		}
		if err := worker.commitDispatch(context.Background(), command, attempt, domain.EventSourceSystem, time.Minute, 20*time.Millisecond, prepared.RequestSummary()); err != nil {
			t.Fatal(err)
		}
		if calls := adapter.calls.Load(); calls != 0 {
			t.Fatalf("committing sent state performed %d Adapter dispatch calls", calls)
		}

		time.Sleep(25 * time.Millisecond)
		recovery := newTestWorker(t, store, adapter, nil)
		recovered, err := recovery.RecoverNext(context.Background())
		if err != nil || !recovered {
			t.Fatalf("RecoverNext=%v err=%v", recovered, err)
		}
		assertCommandStatus(t, store, domain.CommandStatusUnknown, "provider_delivery_unknown")
		current, err := store.Commands().Get(context.Background(), workerCommandID)
		if err != nil || current.ConfirmationLevel != domain.ConfirmationTransportSent || current.EvidenceStatus != domain.EvidenceUnverified {
			t.Fatalf("recovered Command evidence=%+v err=%v", current, err)
		}
		if calls := adapter.calls.Load(); calls != 0 {
			t.Fatalf("recovery performed %d Adapter dispatch calls", calls)
		}
	})
}

func TestPersistentWorkerRunResumesQueuedAndExpiredDispatching(t *testing.T) {
	t.Run("queued", func(t *testing.T) {
		withWorkerDatabase(t, func(_ *sql.DB, store *repository.PostgresStore) {
			seedWorkerCommand(t, store, time.Now().UTC().Add(30*time.Second), false)
			adapter := &fakeAdapter{configured: true, result: acceptedResult()}
			worker := newTestWorkerWithConfig(t, store, adapter, Config{
				WorkerID: "run-queued", LeaseDuration: time.Second, PollInterval: 5 * time.Millisecond,
			})
			runWorkerUntil(t, worker, func() bool {
				command, err := store.Commands().Get(context.Background(), workerCommandID)
				return err == nil && command.Status == domain.CommandStatusSent && command.ConfirmationLevel == domain.ConfirmationProviderAccepted
			})
			if adapter.calls.Load() != 1 {
				t.Fatalf("queued Command Adapter calls=%d", adapter.calls.Load())
			}
		})
	})

	t.Run("restart recovers expired dispatching without replay", func(t *testing.T) {
		withWorkerDatabase(t, func(_ *sql.DB, store *repository.PostgresStore) {
			seedWorkerCommand(t, store, time.Now().UTC().Add(30*time.Second), false)
			adapter := &fakeAdapter{configured: true, result: acceptedResult()}
			crashed := newTestWorkerWithConfig(t, store, adapter, Config{
				WorkerID: "run-crashed", LeaseDuration: time.Second,
			})
			registration := crashed.adapters[0]
			command, attempt, claimed, err := crashed.claimNext(context.Background(), registration)
			if err != nil || !claimed {
				t.Fatalf("claimNext=%v err=%v", claimed, err)
			}
			prepared, err := adapter.Prepare(provideradapter.DispatchRequest{
				ProviderDeviceID: "LOCK-WORKER-1", Action: command.CommandType,
				Payload: command.Payload, ProviderRequestKey: attempt.ProviderRequestKey,
			})
			if err != nil {
				t.Fatal(err)
			}
			if err := crashed.commitDispatch(context.Background(), command, attempt, domain.EventSourceSystem, time.Minute, 20*time.Millisecond, prepared.RequestSummary()); err != nil {
				t.Fatal(err)
			}
			time.Sleep(25 * time.Millisecond)

			restarted := newTestWorkerWithConfig(t, store, adapter, Config{
				WorkerID: "run-restarted", LeaseDuration: 20 * time.Millisecond, PollInterval: 5 * time.Millisecond,
			})
			runWorkerUntil(t, restarted, func() bool {
				command, err := store.Commands().Get(context.Background(), workerCommandID)
				return err == nil && command.Status == domain.CommandStatusUnknown
			})
			assertCommandStatus(t, store, domain.CommandStatusUnknown, "provider_delivery_unknown")
			if adapter.calls.Load() != 0 {
				t.Fatalf("restart replayed expired dispatching Attempt: calls=%d", adapter.calls.Load())
			}
		})
	})
}

func TestPersistentWorkerStateEventAndDeliveryRollback(t *testing.T) {
	t.Run("event conflict", func(t *testing.T) {
		withWorkerDatabase(t, func(_ *sql.DB, store *repository.PostgresStore) {
			seedWorkerCommand(t, store, time.Now().UTC().Add(30*time.Second), false)
			ctx := context.Background()
			if err := store.WithinTransaction(ctx, func(tx *repository.PostgresTx) error {
				deviceID, commandID := workerDeviceID, workerCommandID
				return tx.Events().Create(ctx, domain.Event{
					ID: "94000000-0000-4000-8000-000000000001", SchemaVersion: 1,
					EventType: domain.EventTypeCommandStatusChanged, ProjectID: workerProjectID,
					DeviceID: &deviceID, CommandID: &commandID, Source: domain.EventSourceSystem,
					Payload:          map[string]any{"from": "queued", "to": "sent", "reason_code": nil, "confirmation_level": "none", "evidence_status": "none"},
					DeduplicationKey: "command.status_changed:" + workerCommandID + ":sent",
					OccurredAt:       time.Now().UTC(), CreatedAt: time.Now().UTC(),
				})
			}); err != nil {
				t.Fatal(err)
			}
			adapter := &fakeAdapter{configured: true, result: acceptedResult()}
			worker := newTestWorker(t, store, adapter, nil)
			worked, err := worker.DispatchNext(ctx)
			if !worked || err == nil {
				t.Fatalf("DispatchNext worked=%v err=%v", worked, err)
			}
			assertQueuedClaimedWithoutHTTP(t, store, adapter)
		})
	})

	t.Run("delivery insert failure", func(t *testing.T) {
		withWorkerDatabase(t, func(db *sql.DB, store *repository.PostgresStore) {
			seedWorkerCommand(t, store, time.Now().UTC().Add(30*time.Second), true)
			if _, err := db.Exec(`
				CREATE FUNCTION fail_worker_delivery() RETURNS trigger LANGUAGE plpgsql AS $$
				BEGIN RAISE EXCEPTION 'injected delivery failure'; END $$;
				CREATE TRIGGER fail_worker_delivery BEFORE INSERT ON webhook_deliveries
				FOR EACH ROW EXECUTE FUNCTION fail_worker_delivery()
			`); err != nil {
				t.Fatal(err)
			}
			adapter := &fakeAdapter{configured: true, result: acceptedResult()}
			worker := newTestWorker(t, store, adapter, nil)
			worked, err := worker.DispatchNext(context.Background())
			if !worked || err == nil {
				t.Fatalf("DispatchNext worked=%v err=%v", worked, err)
			}
			assertQueuedClaimedWithoutHTTP(t, store, adapter)
			assertWorkerTableCount(t, db, "device_events", 0)
			assertWorkerTableCount(t, db, "webhook_deliveries", 0)
		})
	})

	t.Run("configured endpoint snapshots every status event", func(t *testing.T) {
		withWorkerDatabase(t, func(db *sql.DB, store *repository.PostgresStore) {
			seedWorkerCommand(t, store, time.Now().UTC().Add(30*time.Second), true)
			worker := newTestWorker(t, store, &fakeAdapter{configured: true, result: acceptedResult()}, nil)
			worked, err := worker.DispatchNext(context.Background())
			if err != nil || !worked {
				t.Fatalf("DispatchNext worked=%v err=%v", worked, err)
			}
			assertWorkerTableCount(t, db, "device_events", 2)
			assertWorkerTableCount(t, db, "webhook_deliveries", 2)
		})
	})

	t.Run("result Delivery failure rolls back completion", func(t *testing.T) {
		withWorkerDatabase(t, func(db *sql.DB, store *repository.PostgresStore) {
			seedWorkerCommand(t, store, time.Now().UTC().Add(30*time.Second), true)
			if _, err := db.Exec(`
				CREATE FUNCTION fail_second_worker_delivery() RETURNS trigger LANGUAGE plpgsql AS $$
				BEGIN
					IF (SELECT count(*) FROM webhook_deliveries) >= 1 THEN
						RAISE EXCEPTION 'injected result delivery failure';
					END IF;
					RETURN NEW;
				END $$;
				CREATE TRIGGER fail_second_worker_delivery BEFORE INSERT ON webhook_deliveries
				FOR EACH ROW EXECUTE FUNCTION fail_second_worker_delivery()
			`); err != nil {
				t.Fatal(err)
			}
			adapter := &fakeAdapter{configured: true, result: acceptedResult()}
			worker := newTestWorker(t, store, adapter, nil)
			worked, err := worker.DispatchNext(context.Background())
			if !worked || err == nil {
				t.Fatalf("DispatchNext worked=%v err=%v", worked, err)
			}
			command, err := store.Commands().Get(context.Background(), workerCommandID)
			if err != nil || command.Status != domain.CommandStatusSent || command.ConfirmationLevel != domain.ConfirmationNone {
				t.Fatalf("result rollback Command=%+v err=%v", command, err)
			}
			attempts, _ := store.Commands().ListAttempts(context.Background(), workerCommandID)
			if len(attempts) != 1 || attempts[0].Phase != domain.AttemptPhaseDispatching || adapter.calls.Load() != 1 {
				t.Fatalf("result rollback Attempt=%+v calls=%d", attempts, adapter.calls.Load())
			}
			assertWorkerTableCount(t, db, "device_events", 1)
			assertWorkerTableCount(t, db, "webhook_deliveries", 1)
		})
	})
}

func TestPersistentWorkerDeadlineDiscovery(t *testing.T) {
	t.Run("queued without Attempt", func(t *testing.T) {
		withWorkerDatabase(t, func(_ *sql.DB, store *repository.PostgresStore) {
			seedWorkerCommand(t, store, time.Now().UTC().Add(-time.Second), false)
			worker := newTestWorker(t, store, &fakeAdapter{configured: true}, nil)
			expired, err := worker.ExpireNextQueued(context.Background())
			if err != nil || !expired {
				t.Fatalf("ExpireNextQueued=%v err=%v", expired, err)
			}
			assertCommandStatus(t, store, domain.CommandStatusTimeout, "dispatch_deadline_exceeded")
		})
	})

	t.Run("queued with valid lease is fenced", func(t *testing.T) {
		withWorkerDatabase(t, func(db *sql.DB, store *repository.PostgresStore) {
			seedWorkerCommand(t, store, time.Now().UTC().Add(-time.Second), false)
			insertClaimedAttempt(t, db, time.Now().UTC().Add(time.Minute))
			worker := newTestWorker(t, store, &fakeAdapter{configured: true}, nil)
			expired, err := worker.ExpireNextQueued(context.Background())
			if err != nil || expired {
				t.Fatalf("valid lease crossed by scanner: expired=%v err=%v", expired, err)
			}
			assertCommandStatus(t, store, domain.CommandStatusQueued, "")
		})
	})

	t.Run("queued with expired lease completes Attempt", func(t *testing.T) {
		withWorkerDatabase(t, func(db *sql.DB, store *repository.PostgresStore) {
			seedWorkerCommand(t, store, time.Now().UTC().Add(-time.Second), false)
			insertClaimedAttempt(t, db, time.Now().UTC().Add(-time.Second))
			worker := newTestWorker(t, store, &fakeAdapter{configured: true}, nil)
			expired, err := worker.ExpireNextQueued(context.Background())
			if err != nil || !expired {
				t.Fatalf("ExpireNextQueued=%v err=%v", expired, err)
			}
			attempts, _ := store.Commands().ListAttempts(context.Background(), workerCommandID)
			if len(attempts) != 1 || attempts[0].Outcome == nil || *attempts[0].Outcome != domain.AttemptOutcomeNotDispatched {
				t.Fatalf("expired claimed Attempt=%+v", attempts)
			}
		})
	})

	for _, status := range []domain.CommandStatus{domain.CommandStatusSent, domain.CommandStatusAcked} {
		t.Run("result observation "+string(status), func(t *testing.T) {
			withWorkerDatabase(t, func(db *sql.DB, store *repository.PostgresStore) {
				seedWorkerCommand(t, store, time.Now().UTC().Add(30*time.Second), false)
				worker := newTestWorker(t, store, &fakeAdapter{configured: true, result: acceptedResult()}, nil)
				if worked, err := worker.DispatchNext(context.Background()); err != nil || !worked {
					t.Fatalf("DispatchNext worked=%v err=%v", worked, err)
				}
				confirmation, evidence := domain.ConfirmationProviderAccepted, domain.EvidenceUnverified
				if status == domain.CommandStatusAcked {
					confirmation, evidence = domain.ConfirmationDeviceAcked, domain.EvidenceVerified
				}
				if _, err := db.Exec(`
					UPDATE device_commands
					SET status = $2, confirmation_level = $3, evidence_status = $4,
						queued_at = now() - interval '3 seconds',
						sent_at = now() - interval '2 seconds',
						result_deadline_at = now() - interval '1 second'
					WHERE id = $1
				`, workerCommandID, status, confirmation, evidence); err != nil {
					t.Fatal(err)
				}
				expired, err := worker.ExpireNextResultObservation(context.Background())
				if err != nil || !expired {
					t.Fatalf("ExpireNextResultObservation=%v err=%v", expired, err)
				}
				assertCommandStatus(t, store, domain.CommandStatusTimeout, "result_observation_timeout")
				events, _ := store.Events().ListByCommand(context.Background(), workerCommandID)
				last := events[len(events)-1]
				if last.Payload["from"] != string(status) || last.Payload["to"] != "timeout" {
					t.Fatalf("result deadline Event=%+v", last)
				}
			})
		})
	}
}

type fakeAdapter struct {
	configured bool
	prepareErr error
	result     provideradapter.DispatchResult
	delay      time.Duration
	started    chan struct{}
	release    chan struct{}
	calls      atomic.Int32
}

type providerTimeoutStore struct {
	*repository.PostgresStore
	timeout time.Duration
}

type providerTimeoutDeviceTypes struct {
	repository.DeviceTypeQueries
	timeout time.Duration
}

func (s *providerTimeoutStore) DeviceTypes() repository.DeviceTypeQueries {
	return providerTimeoutDeviceTypes{DeviceTypeQueries: s.PostgresStore.DeviceTypes(), timeout: s.timeout}
}

func (q providerTimeoutDeviceTypes) GetProfile(ctx context.Context, deviceTypeID string, revision int) (domain.DeviceTypeProfile, error) {
	profile, err := q.DeviceTypeQueries.GetProfile(ctx, deviceTypeID, revision)
	if err != nil {
		return domain.DeviceTypeProfile{}, err
	}
	for index := range profile.Actions {
		profile.Actions[index].ProviderRequestTimeout = q.timeout
	}
	return profile, nil
}

type serialReader struct {
	mu      sync.Mutex
	serials []uint32
	next    byte
	index   int
}

func (r *serialReader) Read(buffer []byte) (int, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if len(buffer) == 4 {
		if r.index >= len(r.serials) {
			return 0, io.EOF
		}
		binary.BigEndian.PutUint32(buffer, r.serials[r.index])
		r.index++
		return len(buffer), nil
	}
	for index := range buffer {
		r.next++
		buffer[index] = r.next
	}
	return len(buffer), nil
}

func (a *fakeAdapter) Configured() bool { return a.configured }

func (a *fakeAdapter) Prepare(request provideradapter.DispatchRequest) (provideradapter.PreparedDispatch, error) {
	if a.prepareErr != nil {
		return nil, a.prepareErr
	}
	return &fakePrepared{adapter: a, summary: map[string]any{
		"cmd": "open", "deviceid": request.ProviderDeviceID, "serialnum": request.ProviderRequestKey,
	}}, nil
}

type fakePrepared struct {
	adapter *fakeAdapter
	summary map[string]any
}

func (p *fakePrepared) RequestSummary() map[string]any {
	result := make(map[string]any, len(p.summary))
	for key, value := range p.summary {
		result[key] = value
	}
	return result
}

func (p *fakePrepared) Dispatch(ctx context.Context) provideradapter.DispatchResult {
	p.adapter.calls.Add(1)
	if p.adapter.started != nil {
		close(p.adapter.started)
	}
	if p.adapter.release != nil {
		select {
		case <-ctx.Done():
			return provideradapter.DispatchResult{
				Outcome:           domain.AttemptOutcomeTransportErrorBeforeSend,
				ConfirmationLevel: domain.ConfirmationNone, EvidenceStatus: domain.EvidenceNone,
			}
		case <-p.adapter.release:
		}
	}
	if p.adapter.delay > 0 {
		timer := time.NewTimer(p.adapter.delay)
		defer timer.Stop()
		select {
		case <-ctx.Done():
			return provideradapter.DispatchResult{
				Outcome:           domain.AttemptOutcomeTransportErrorBeforeSend,
				ConfirmationLevel: domain.ConfirmationNone, EvidenceStatus: domain.EvidenceNone,
			}
		case <-timer.C:
		}
	}
	return p.adapter.result
}

func acceptedResult() provideradapter.DispatchResult {
	return provideradapter.DispatchResult{
		Outcome: domain.AttemptOutcomeProviderAccepted, ConfirmationLevel: domain.ConfirmationProviderAccepted,
		EvidenceStatus: domain.EvidenceUnverified, ResponseSummary: map[string]any{"result": "ok"},
	}
}

func newTestWorker(t *testing.T, store Store, adapter provideradapter.Adapter, random io.Reader) *Worker {
	t.Helper()
	return newTestWorkerWithConfig(t, store, adapter, Config{WorkerID: "test-worker", Random: random})
}

func newTestWorkerWithConfig(t *testing.T, store Store, adapter provideradapter.Adapter, config Config) *Worker {
	t.Helper()
	config.Adapters = []AdapterRegistration{{
		ProviderCode: domain.ProviderCodeWWTIOT, AdapterCode: domain.AdapterWWTIOTCloudAPI,
		Adapter: adapter, ResultSource: domain.EventSourceSystem,
	}}
	worker, err := New(store, config)
	if err != nil {
		t.Fatal(err)
	}
	return worker
}

func newSimulatorWorker(t *testing.T, store Store, config Config) *Worker {
	t.Helper()
	return newSimulatorWorkerWithSnapshot(t, store, config, simulatorruntime.ClaimSnapshot)
}

func newSimulatorWorkerWithSnapshot(t *testing.T, store Store, config Config, snapshot func(context.Context, repository.CommandTx) (map[string]any, error)) *Worker {
	t.Helper()
	config.Adapters = []AdapterRegistration{{
		ProviderCode: domain.ProviderCodeSimulator, AdapterCode: domain.AdapterSimulator,
		Adapter: simulatorruntime.NewAdapter(), ResultSource: domain.EventSourceSimulator, ClaimSnapshot: snapshot,
	}}
	worker, err := New(store, config)
	if err != nil {
		t.Fatal(err)
	}
	return worker
}

func setSimulatorConfig(t *testing.T, store *repository.PostgresStore, outcome domain.SimulatorOutcome, delay time.Duration) {
	t.Helper()
	if err := updateSimulatorConfig(store, outcome, delay); err != nil {
		t.Fatal(err)
	}
}

func updateSimulatorConfig(store *repository.PostgresStore, outcome domain.SimulatorOutcome, delay time.Duration) error {
	ctx := context.Background()
	return store.TransactSimulator(ctx, func(tx repository.SimulatorTx) error {
		current, err := tx.Simulator().GetForUpdate(ctx)
		if err != nil {
			return err
		}
		updated, err := tx.Simulator().Update(ctx, current.Version, repository.UpdateSimulatorRequest{Outcome: outcome, Delay: delay})
		if err != nil {
			return err
		}
		if !updated {
			return errors.New("Simulator config was not updated")
		}
		return nil
	})
}

func seedFairnessCommands(t *testing.T, store *repository.PostgresStore) {
	t.Helper()
	ctx := context.Background()
	first, err := store.Commands().Get(ctx, workerCommandID)
	if err != nil {
		t.Fatal(err)
	}
	secondWWTIOT := first
	secondWWTIOT.ID = "93000000-0000-4000-8000-000000000002"
	secondWWTIOT.IdempotencyKey = "worker-key-2"
	secondWWTIOT.RequestHash = bytes.Repeat([]byte{0x94}, 32)
	secondWWTIOT.QueuedAt = first.QueuedAt.Add(time.Microsecond)
	secondWWTIOT.CreatedAt = secondWWTIOT.QueuedAt
	secondWWTIOT.UpdatedAt = secondWWTIOT.QueuedAt
	simulatorCommand := first
	simulatorCommand.ID = "93000000-0000-4000-8000-000000000003"
	simulatorCommand.DeviceID = "92000000-0000-4000-8000-000000000003"
	simulatorCommand.IdempotencyKey = "worker-key-3"
	simulatorCommand.RequestHash = bytes.Repeat([]byte{0x95}, 32)
	simulatorCommand.QueuedAt = first.QueuedAt.Add(2 * time.Microsecond)
	simulatorCommand.CreatedAt = simulatorCommand.QueuedAt
	simulatorCommand.UpdatedAt = simulatorCommand.QueuedAt
	if err := store.TransactCommand(ctx, func(tx repository.CommandTx) error {
		if err := tx.Devices().Create(ctx, domain.Device{
			ID: simulatorCommand.DeviceID, ProjectID: workerProjectID, DeviceTypeID: first.DeviceTypeID,
			Name: "Fair Simulator", ProviderCode: domain.ProviderCodeSimulator, ProviderDeviceID: simulatorCommand.DeviceID,
			AccessType: domain.AccessTypeSimulator, TransportProtocol: domain.TransportProtocolInternal,
			Adapter: domain.AdapterSimulator, ConnectionStatus: domain.ConnectionStatusUnknown,
			LifecycleStatus: domain.LifecycleStatusActive, CreatedAt: first.CreatedAt, UpdatedAt: first.UpdatedAt,
		}); err != nil {
			return err
		}
		if err := tx.Commands().Create(ctx, secondWWTIOT); err != nil {
			return err
		}
		return tx.Commands().Create(ctx, simulatorCommand)
	}); err != nil {
		t.Fatal(err)
	}
}

func waitForSimulatorLockWait(t *testing.T, db *sql.DB) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		var waiting bool
		if err := db.QueryRow(`
			SELECT EXISTS (
				SELECT 1 FROM pg_stat_activity
				WHERE datname = current_database()
					AND pid <> pg_backend_pid()
					AND wait_event_type = 'Lock'
					AND query ILIKE '%simulator_config%'
			)
		`).Scan(&waiting); err != nil {
			t.Fatal(err)
		}
		if waiting {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatal("Simulator transaction did not reach the expected database row-lock wait")
}

func closeIfOpen(channel chan struct{}) {
	select {
	case <-channel:
	default:
		close(channel)
	}
}

func makeWorkerDeviceSimulator(t *testing.T, db *sql.DB) {
	t.Helper()
	if _, err := db.Exec(`
		UPDATE devices
		SET provider_code = 'simulator', provider_device_id = id::text,
			access_type = 'simulator', transport_protocol = 'internal', adapter = 'simulator'
		WHERE id = $1
	`, workerDeviceID); err != nil {
		t.Fatal(err)
	}
}

func runWorkerUntil(t *testing.T, worker *Worker, done func() bool) {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	workerDone := make(chan struct{})
	go func() {
		defer close(workerDone)
		worker.Run(ctx, func(err error) {
			if ctx.Err() == nil {
				t.Errorf("background worker: %v", err)
			}
		})
	}()
	deadline := time.NewTimer(2 * time.Second)
	ticker := time.NewTicker(5 * time.Millisecond)
	defer deadline.Stop()
	defer ticker.Stop()
	for !done() {
		select {
		case <-deadline.C:
			cancel()
			<-workerDone
			t.Fatal("background worker did not reach the expected persistent state")
		case <-ticker.C:
		}
	}
	cancel()
	<-workerDone
}

func seedWorkerCommand(t *testing.T, store *repository.PostgresStore, dispatchDeadline time.Time, withWebhook bool) {
	t.Helper()
	ctx := context.Background()
	deviceType, err := store.DeviceTypes().GetByCode(ctx, domain.DeviceTypeSmartLock)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC().Truncate(time.Microsecond)
	if !dispatchDeadline.After(now) {
		now = dispatchDeadline.Add(-30 * time.Second).UTC().Truncate(time.Microsecond)
	}
	err = store.WithinTransaction(ctx, func(tx *repository.PostgresTx) error {
		project := domain.Project{
			ID: workerProjectID, Name: "Worker Project", APIKeyDigest: bytes.Repeat([]byte{0x91}, 32),
			IPWhitelist: []string{}, CreatedAt: now, UpdatedAt: now,
		}
		if err := tx.Projects().Create(ctx, project); err != nil {
			return err
		}
		if withWebhook {
			if err := tx.Projects().CreateWebhookSecretVersion(ctx, domain.WebhookSecretVersion{
				ProjectID: workerProjectID, Version: 1, Ciphertext: bytes.Repeat([]byte{0x41}, 33),
				Nonce: bytes.Repeat([]byte{0x42}, 12), EncryptionKeyVersion: 1, CreatedAt: now,
			}); err != nil {
				return err
			}
			endpoint, version := "https://example.test/worker-events", 1
			if err := tx.Projects().SetWebhookConfiguration(ctx, workerProjectID, &endpoint, 1, &version); err != nil {
				return err
			}
		}
		if err := tx.Devices().Create(ctx, domain.Device{
			ID: workerDeviceID, ProjectID: workerProjectID, DeviceTypeID: deviceType.ID,
			Name: "Worker Lock", ProviderCode: domain.ProviderCodeWWTIOT, ProviderDeviceID: "LOCK-WORKER-1",
			AccessType: domain.AccessTypeCloudAPI, TransportProtocol: domain.TransportProtocolHTTP,
			Adapter: domain.AdapterWWTIOTCloudAPI, ConnectionStatus: domain.ConnectionStatusUnknown,
			LifecycleStatus: domain.LifecycleStatusActive, CreatedAt: now, UpdatedAt: now,
		}); err != nil {
			return err
		}
		return tx.Commands().Create(ctx, domain.Command{
			ID: workerCommandID, ProjectID: workerProjectID, DeviceID: workerDeviceID, DeviceTypeID: deviceType.ID,
			CommandType: "unlock", Payload: map[string]any{}, DeviceTypeRevision: 1,
			DeliveryPolicy: domain.DeliveryPolicyDispatchOnce, Status: domain.CommandStatusQueued,
			ConfirmationLevel: domain.ConfirmationNone, EvidenceStatus: domain.EvidenceNone,
			IdempotencyKey: "worker-key", RequestHash: bytes.Repeat([]byte{0x93}, 32),
			QueuedAt: now, DispatchDeadlineAt: dispatchDeadline, CreatedAt: now, UpdatedAt: now,
		})
	})
	if err != nil {
		t.Fatal(err)
	}
}

func insertClaimedAttempt(t *testing.T, db *sql.DB, leaseExpiresAt time.Time) {
	t.Helper()
	claimedAt := time.Now().UTC()
	if !leaseExpiresAt.After(claimedAt) {
		claimedAt = leaseExpiresAt.Add(-time.Second)
	}
	if _, err := db.Exec(`
		INSERT INTO device_command_attempts (
			id, command_id, attempt_no, phase, provider_code, adapter,
			provider_request_key, request_summary, response_summary,
			confirmation_level, evidence_status, lease_token, lease_owner,
			lease_expires_at, claimed_at
		) VALUES (
			'95000000-0000-4000-8000-000000000001', $1, 1, 'claimed', 'wwtiot',
			'wwtiot_cloud_api', '123', '{"serialnum":123}'::jsonb, '{}'::jsonb,
			'none', 'none', '96000000-0000-4000-8000-000000000001', 'fixture', $2, $3
		)
	`, workerCommandID, leaseExpiresAt, claimedAt); err != nil {
		t.Fatal(err)
	}
}

func assertQueuedClaimedWithoutHTTP(t *testing.T, store *repository.PostgresStore, adapter *fakeAdapter) {
	t.Helper()
	command, err := store.Commands().Get(context.Background(), workerCommandID)
	if err != nil || command.Status != domain.CommandStatusQueued {
		t.Fatalf("rolled-back Command=%+v err=%v", command, err)
	}
	attempts, err := store.Commands().ListAttempts(context.Background(), workerCommandID)
	if err != nil || len(attempts) != 1 || attempts[0].Phase != domain.AttemptPhaseClaimed {
		t.Fatalf("rolled-back Attempt=%+v err=%v", attempts, err)
	}
	if adapter.calls.Load() != 0 {
		t.Fatalf("external HTTP equivalent called before failed transaction committed: %d", adapter.calls.Load())
	}
}

func assertCommandStatus(t *testing.T, store *repository.PostgresStore, status domain.CommandStatus, reason string) {
	t.Helper()
	command, err := store.Commands().Get(context.Background(), workerCommandID)
	if err != nil || command.Status != status || pointerValue(command.ReasonCode) != reason {
		t.Fatalf("Command=%+v err=%v, want %s/%s", command, err, status, reason)
	}
}

func assertWorkerTableCount(t *testing.T, db *sql.DB, table string, want int) {
	t.Helper()
	var count int
	if err := db.QueryRow(`SELECT count(*) FROM ` + table).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != want {
		t.Fatalf("%s count=%d, want %d", table, count, want)
	}
}

func pointerValue(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}

func withWorkerDatabase(t *testing.T, fn func(*sql.DB, *repository.PostgresStore)) {
	t.Helper()
	baseURL := strings.TrimSpace(os.Getenv("MIGRATION_TEST_DATABASE_URL"))
	if baseURL == "" {
		t.Skip("MIGRATION_TEST_DATABASE_URL is not set")
	}
	admin, err := sql.Open("postgres", baseURL)
	if err != nil {
		t.Fatal(err)
	}
	defer admin.Close()
	schema := fmt.Sprintf("command_worker_test_%d", time.Now().UnixNano())
	if _, err := admin.Exec(`CREATE SCHEMA ` + schema); err != nil {
		t.Fatal(err)
	}
	defer func() {
		if _, err := admin.Exec(`DROP SCHEMA ` + schema + ` CASCADE`); err != nil {
			t.Errorf("drop worker schema: %v", err)
		}
	}()
	parsed, err := url.Parse(baseURL)
	if err != nil {
		t.Fatal(err)
	}
	query := parsed.Query()
	query.Set("search_path", schema)
	parsed.RawQuery = query.Encode()
	db, err := sql.Open("postgres", parsed.String())
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err := storage.ApplyMigrations(context.Background(), db); err != nil {
		t.Fatal(err)
	}
	fn(db, repository.NewPostgresStore(db))
}
