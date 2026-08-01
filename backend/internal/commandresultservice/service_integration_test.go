//go:build integration

package commandresultservice

import (
	"bytes"
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"

	_ "github.com/lib/pq"
	"github.com/qiyue2015/device-platform/internal/domain"
	"github.com/qiyue2015/device-platform/internal/storage"
	"github.com/qiyue2015/device-platform/internal/storage/repository"
)

const (
	resultProjectID = "11000000-0000-4000-8000-000000000001"
	resultDeviceID  = "12000000-0000-4000-8000-000000000001"
	resultCommandID = "13000000-0000-4000-8000-000000000001"
)

type fixedClock struct{ now time.Time }

func (clock fixedClock) Now() time.Time { return clock.now }

func TestRecordResultLifecycleDeduplicationAtomicityAndImmutability(t *testing.T) {
	withResultDatabase(t, func(db *sql.DB, store *repository.PostgresStore) {
		ctx := context.Background()
		now := time.Date(2026, 7, 31, 15, 0, 0, 0, time.UTC)
		seedResultCommand(t, ctx, store, now)
		service, err := New(store, Config{Clock: fixedClock{now: now}})
		if err != nil {
			t.Fatal(err)
		}

		ackRequest := RecordRequest{
			CommandID: resultCommandID, Source: domain.EventSourceSimulator,
			Outcome: domain.ResultOutcomeDeviceAcked, DeduplicationKey: "sim-result-ack-1",
			ObservedAt: now, Payload: map[string]any{"message_id": "ack-1"},
		}
		ack, err := service.Record(ctx, ackRequest)
		if err != nil || ack.Command.Status != domain.CommandStatusAcked || ack.Result.Late ||
			ack.Result.ConfirmationLevel != domain.ConfirmationDeviceAcked || ack.Result.EvidenceStatus != domain.EvidenceVerified {
			t.Fatalf("ACK result=%+v err=%v", ack, err)
		}
		assertResultAggregateCounts(t, db, 1, 1, 1)

		replay, err := service.Record(ctx, ackRequest)
		if err != nil || !replay.IdempotentReplay || replay.Result.ID != ack.Result.ID || replay.Command.Status != domain.CommandStatusAcked {
			t.Fatalf("ACK replay=%+v err=%v", replay, err)
		}
		assertResultAggregateCounts(t, db, 1, 1, 1)

		conflict := ackRequest
		conflict.Outcome = domain.ResultOutcomeDeviceFailed
		if _, err := service.Record(ctx, conflict); !errors.Is(err, ErrResultConflict) {
			t.Fatalf("conflicting duplicate error=%v", err)
		}
		assertResultAggregateCounts(t, db, 1, 1, 1)

		final, err := service.Record(ctx, RecordRequest{
			CommandID: resultCommandID, Source: domain.EventSourceSimulator,
			Outcome: domain.ResultOutcomeDeviceSucceeded, DeduplicationKey: "sim-result-final-1",
			ObservedAt: now.Add(time.Second), Payload: map[string]any{"message_id": "final-1"},
		})
		if err != nil || final.Command.Status != domain.CommandStatusSuccess || final.Command.FinishedAt == nil ||
			!final.Command.FinishedAt.Equal(now.Add(time.Second)) || final.Result.Late {
			t.Fatalf("final result=%+v err=%v", final, err)
		}
		finishedAt := *final.Command.FinishedAt

		late, err := service.Record(ctx, RecordRequest{
			CommandID: resultCommandID, Source: domain.EventSourceSystem,
			Outcome: domain.ResultOutcomeDeviceFailed, DeduplicationKey: "system-late-final-1",
			ObservedAt: now.Add(2 * time.Second), Payload: map[string]any{"fixture": "late"},
		})
		if err != nil || !late.Result.Late || late.Command.Status != domain.CommandStatusSuccess ||
			late.Command.ReasonCode != nil || late.Command.FinishedAt == nil || !late.Command.FinishedAt.Equal(finishedAt) {
			t.Fatalf("late result=%+v err=%v", late, err)
		}
		results, err := store.Commands().ListResults(ctx, resultCommandID)
		if err != nil || len(results) != 3 || results[0].ID != ack.Result.ID || results[1].ID != final.Result.ID || results[2].ID != late.Result.ID {
			t.Fatalf("ordered Results=%+v err=%v", results, err)
		}
		assertResultAggregateCounts(t, db, 3, 3, 3)

		if _, err := db.Exec(`UPDATE device_command_results SET payload = '{}'::jsonb WHERE id = $1`, ack.Result.ID); err == nil {
			t.Fatal("CommandResult update must be rejected")
		}
		if _, err := db.Exec(`DELETE FROM device_command_results WHERE id = $1`, ack.Result.ID); err == nil {
			t.Fatal("CommandResult delete must be rejected")
		}

		if _, err := db.Exec(`ALTER TABLE webhook_deliveries ADD CONSTRAINT reject_result_delivery CHECK (status <> 'pending') NOT VALID`); err != nil {
			t.Fatal(err)
		}
		_, err = service.Record(ctx, RecordRequest{
			CommandID: resultCommandID, Source: domain.EventSourceSystem,
			Outcome: domain.ResultOutcomeDeviceSucceeded, DeduplicationKey: "system-rollback-1",
			ObservedAt: now.Add(3 * time.Second), Payload: map[string]any{"fixture": "rollback"},
		})
		if err == nil {
			t.Fatal("Delivery failure must roll back the Result aggregate")
		}
		assertResultAggregateCounts(t, db, 3, 3, 3)
	})
}

func seedResultCommand(t *testing.T, ctx context.Context, store *repository.PostgresStore, now time.Time) {
	t.Helper()
	deviceType, err := store.DeviceTypes().GetByCode(ctx, domain.DeviceTypeSmartLock)
	if err != nil {
		t.Fatal(err)
	}
	secretVersion := 1
	sentAt := now.Add(-time.Minute)
	resultDeadline := now.Add(time.Minute)
	queuedAt := sentAt.Add(-time.Second).Truncate(time.Millisecond)
	err = store.WithinTransaction(ctx, func(tx *repository.PostgresTx) error {
		if err := tx.Projects().Create(ctx, domain.Project{
			ID: resultProjectID, Name: "Result Project", APIKeyDigest: bytes.Repeat([]byte{0x31}, 32),
			IPWhitelist: []string{}, CreatedAt: queuedAt, UpdatedAt: queuedAt,
		}); err != nil {
			return err
		}
		if err := tx.Projects().CreateWebhookSecretVersion(ctx, domain.WebhookSecretVersion{
			ProjectID: resultProjectID, Version: secretVersion, Ciphertext: bytes.Repeat([]byte{0x41}, 17),
			Nonce: bytes.Repeat([]byte{0x42}, 12), EncryptionKeyVersion: 1, CreatedAt: queuedAt,
		}); err != nil {
			return err
		}
		endpoint := "https://hooks.example.test/results"
		if err := tx.Projects().SetWebhookConfiguration(ctx, resultProjectID, &endpoint, 1, &secretVersion); err != nil {
			return err
		}
		if err := tx.Devices().Create(ctx, domain.Device{
			ID: resultDeviceID, ProjectID: resultProjectID, DeviceTypeID: deviceType.ID, Name: "Result Simulator",
			ProviderCode: domain.ProviderCodeSimulator, ProviderProfile: domain.ProviderProfileSimulatorV1,
			ProviderDeviceID: resultDeviceID,
			AccessType:       domain.AccessTypeSimulator, TransportProtocol: domain.TransportProtocolInternal,
			Adapter: domain.AdapterSimulator, ConnectionStatus: domain.ConnectionStatusUnknown,
			LifecycleStatus: domain.LifecycleStatusActive, CreatedAt: queuedAt, UpdatedAt: queuedAt,
		}); err != nil {
			return err
		}
		return tx.Commands().Create(ctx, domain.Command{
			ID: resultCommandID, ProjectID: resultProjectID, DeviceID: resultDeviceID, DeviceTypeID: deviceType.ID,
			DeviceTypeCode: domain.DeviceTypeSmartLock, ProviderCode: domain.ProviderCodeSimulator,
			ProviderProfile: domain.ProviderProfileSimulatorV1, ProviderDeviceID: resultDeviceID, Adapter: domain.AdapterSimulator,
			CommandType: "query_status", Payload: map[string]any{}, DeviceTypeRevision: domain.DeviceTypeSmartLockRevision,
			DeliveryPolicy: domain.DeliveryPolicyDispatchOnce, DispatchDeadline: 30 * time.Second,
			ProviderTimeout: 10 * time.Second, ResultTimeout: time.Minute, RetryAllowed: false,
			Status: domain.CommandStatusSent, ConfirmationLevel: domain.ConfirmationTransportSent,
			EvidenceStatus: domain.EvidenceVerified, IdempotencyKey: "result-command-1",
			RequestHash: bytes.Repeat([]byte{0x32}, 32), QueuedAt: queuedAt,
			DispatchDeadlineAt: queuedAt.Add(30 * time.Second), SentAt: &sentAt, ResultDeadlineAt: &resultDeadline,
			CreatedAt: queuedAt, UpdatedAt: sentAt,
		})
	})
	if err != nil {
		t.Fatal(err)
	}
}

func assertResultAggregateCounts(t *testing.T, db *sql.DB, results, events, deliveries int) {
	t.Helper()
	for table, want := range map[string]int{
		"device_command_results": results,
		"device_events":          events,
		"webhook_deliveries":     deliveries,
	} {
		var count int
		if err := db.QueryRow(`SELECT count(*) FROM ` + table).Scan(&count); err != nil || count != want {
			t.Fatalf("%s count=%d want=%d err=%v", table, count, want, err)
		}
	}
}

func withResultDatabase(t *testing.T, fn func(*sql.DB, *repository.PostgresStore)) {
	t.Helper()
	baseURL := strings.TrimSpace(os.Getenv("MIGRATION_TEST_DATABASE_URL"))
	if baseURL == "" {
		t.Skip("MIGRATION_TEST_DATABASE_URL is not set")
	}
	parsed, err := url.Parse(baseURL)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasSuffix(strings.TrimPrefix(parsed.Path, "/"), "_test") {
		t.Fatalf("refusing CommandResult integration test against database %q", parsed.Path)
	}
	admin, err := sql.Open("postgres", baseURL)
	if err != nil {
		t.Fatal(err)
	}
	defer admin.Close()
	schema := fmt.Sprintf("command_result_test_%d", time.Now().UnixNano())
	if _, err := admin.Exec(`CREATE SCHEMA ` + schema); err != nil {
		t.Fatal(err)
	}
	defer func() {
		if _, err := admin.Exec(`DROP SCHEMA ` + schema + ` CASCADE`); err != nil {
			t.Errorf("drop CommandResult test schema: %v", err)
		}
	}()
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
