//go:build integration

package repository_test

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

	"github.com/lib/pq"
	"github.com/qiyue2015/device-platform/internal/domain"
	"github.com/qiyue2015/device-platform/internal/storage"
	"github.com/qiyue2015/device-platform/internal/storage/repository"
)

const (
	projectOneID        = "20000000-0000-0000-0000-000000000001"
	projectTwoID        = "20000000-0000-0000-0000-000000000002"
	wwtiotDeviceID      = "30000000-0000-0000-0000-000000000001"
	simulatorDeviceID   = "30000000-0000-0000-0000-000000000002"
	replacementDeviceID = "30000000-0000-0000-0000-000000000003"
)

func TestPostgresProjectDeviceRepositories(t *testing.T) {
	withRepositoryTestDatabase(t, func(db *sql.DB) {
		ctx := context.Background()
		store := repository.NewPostgresStore(db)
		now := time.Date(2026, 7, 31, 14, 0, 0, 0, time.UTC)
		apiDigest := bytes.Repeat([]byte{0x11}, 32)
		projectOne := domain.Project{
			ID:           projectOneID,
			Name:         "Project One",
			APIKeyDigest: apiDigest,
			IPWhitelist:  []string{"192.0.2.0/24", "2001:db8::1"},
			CreatedAt:    now,
			UpdatedAt:    now,
		}
		projectTwo := domain.Project{
			ID:           projectTwoID,
			Name:         "Project Two",
			APIKeyDigest: bytes.Repeat([]byte{0x22}, 32),
			IPWhitelist:  []string{},
			CreatedAt:    now,
			UpdatedAt:    now,
		}

		deviceType, err := store.DeviceTypes().GetByCode(ctx, domain.DeviceTypeSmartLock)
		if err != nil {
			t.Fatal(err)
		}
		profile, err := store.DeviceTypes().GetProfile(ctx, deviceType.ID, deviceType.CurrentRevision)
		if err != nil {
			t.Fatal(err)
		}
		if deviceType.Code != domain.DeviceTypeSmartLock || deviceType.CurrentRevision != 2 || len(profile.Actions) != 3 {
			t.Fatalf("frozen Device Type drift: type=%+v profile=%+v", deviceType, profile)
		}
		if profile.Actions[0].DispatchDeadline != 30*time.Second || profile.Actions[0].ProviderRequestTimeout != 10*time.Second || profile.Actions[0].ResultObservationTimeout != time.Minute {
			t.Fatalf("frozen action deadlines drifted: %+v", profile.Actions[0])
		}

		err = store.WithinTransaction(ctx, func(tx *repository.PostgresTx) error {
			if err := tx.Projects().Create(ctx, projectOne); err != nil {
				return err
			}
			if err := tx.Projects().Create(ctx, projectTwo); err != nil {
				return err
			}
			if err := tx.Devices().Create(ctx, domain.Device{
				ID:                wwtiotDeviceID,
				ProjectID:         projectOneID,
				DeviceTypeID:      deviceType.ID,
				Name:              "WWTIOT Lock",
				ProviderCode:      domain.ProviderCodeWWTIOT,
				ProviderDeviceID:  "LOCK-001",
				AccessType:        domain.AccessTypeCloudAPI,
				TransportProtocol: domain.TransportProtocolHTTP,
				Adapter:           domain.AdapterWWTIOTCloudAPI,
				ConnectionStatus:  domain.ConnectionStatusUnknown,
				LifecycleStatus:   domain.LifecycleStatusActive,
				CreatedAt:         now,
				UpdatedAt:         now,
			}); err != nil {
				return err
			}
			return tx.Devices().Create(ctx, domain.Device{
				ID:                simulatorDeviceID,
				ProjectID:         projectOneID,
				DeviceTypeID:      deviceType.ID,
				Name:              "Simulator Lock",
				ProviderCode:      domain.ProviderCodeSimulator,
				ProviderDeviceID:  simulatorDeviceID,
				AccessType:        domain.AccessTypeSimulator,
				TransportProtocol: domain.TransportProtocolInternal,
				Adapter:           domain.AdapterSimulator,
				ConnectionStatus:  domain.ConnectionStatusUnknown,
				LifecycleStatus:   domain.LifecycleStatusActive,
				CreatedAt:         now,
				UpdatedAt:         now,
			})
		})
		if err != nil {
			t.Fatal(err)
		}

		byKey, err := store.Projects().GetByAPIKeyDigest(ctx, apiDigest)
		if err != nil || byKey.ID != projectOneID || len(byKey.IPWhitelist) != 2 {
			t.Fatalf("project digest lookup failed: project=%+v err=%v", byKey, err)
		}
		rotatedDigest := bytes.Repeat([]byte{0x66}, 32)
		err = store.WithinTransaction(ctx, func(tx *repository.PostgresTx) error {
			locked, err := tx.Projects().GetForUpdate(ctx, projectOneID)
			if err != nil || locked.Name != projectOne.Name {
				return fmt.Errorf("lock Project: project=%+v: %w", locked, err)
			}
			if err := tx.Projects().Rename(ctx, projectOneID, "Project One Renamed"); err != nil {
				return err
			}
			if err := tx.Projects().ReplaceIPWhitelist(ctx, projectOneID, []string{"198.51.100.10"}); err != nil {
				return err
			}
			return tx.Projects().ReplaceAPIKeyDigest(ctx, projectOneID, rotatedDigest)
		})
		if err != nil {
			t.Fatal(err)
		}
		if _, err := store.Projects().GetByAPIKeyDigest(ctx, apiDigest); !errors.Is(err, sql.ErrNoRows) {
			t.Fatalf("old API key digest lookup error = %v", err)
		}
		rotatedProject, err := store.Projects().GetByAPIKeyDigest(ctx, rotatedDigest)
		if err != nil || rotatedProject.Name != "Project One Renamed" || len(rotatedProject.IPWhitelist) != 1 {
			t.Fatalf("rotated Project mismatch: project=%+v err=%v", rotatedProject, err)
		}
		items, err := store.Devices().ListByProject(ctx, projectOneID)
		if err != nil || len(items) != 2 {
			t.Fatalf("project-one devices=%+v err=%v", items, err)
		}
		otherItems, err := store.Devices().ListByProject(ctx, projectTwoID)
		if err != nil || len(otherItems) != 0 {
			t.Fatalf("project isolation failed: devices=%+v err=%v", otherItems, err)
		}

		err = store.WithinTransaction(ctx, func(tx *repository.PostgresTx) error {
			return tx.Devices().Create(ctx, domain.Device{
				ID:                replacementDeviceID,
				ProjectID:         projectTwoID,
				DeviceTypeID:      deviceType.ID,
				Name:              "Conflicting Lock",
				ProviderCode:      domain.ProviderCodeWWTIOT,
				ProviderDeviceID:  "LOCK-001",
				AccessType:        domain.AccessTypeCloudAPI,
				TransportProtocol: domain.TransportProtocolHTTP,
				Adapter:           domain.AdapterWWTIOTCloudAPI,
				ConnectionStatus:  domain.ConnectionStatusUnknown,
				LifecycleStatus:   domain.LifecycleStatusActive,
				CreatedAt:         now,
				UpdatedAt:         now,
			})
		})
		var pqErr *pq.Error
		if !errors.As(err, &pqErr) || pqErr.Code != "23505" {
			t.Fatalf("global Provider identity conflict error = %v", err)
		}

		err = store.WithinTransaction(ctx, func(tx *repository.PostgresTx) error {
			updated, err := tx.Devices().SetLifecycleStatus(ctx, wwtiotDeviceID, domain.LifecycleStatusActive, domain.LifecycleStatusDisabled)
			if err != nil || !updated {
				return fmt.Errorf("disable Device updated=%v: %w", updated, err)
			}
			updated, err = tx.Devices().SetLifecycleStatus(ctx, wwtiotDeviceID, domain.LifecycleStatusActive, domain.LifecycleStatusDeleted)
			if err != nil || updated {
				return fmt.Errorf("stale lifecycle CAS updated=%v: %w", updated, err)
			}
			updated, err = tx.Devices().SetLifecycleStatus(ctx, wwtiotDeviceID, domain.LifecycleStatusDisabled, domain.LifecycleStatusDeleted)
			if err != nil || !updated {
				return fmt.Errorf("delete Device updated=%v: %w", updated, err)
			}
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
		if _, err := store.Devices().GetByProviderIdentity(ctx, domain.ProviderCodeWWTIOT, "LOCK-001"); !errors.Is(err, sql.ErrNoRows) {
			t.Fatalf("deleted Provider identity must not resolve, got %v", err)
		}

		err = store.WithinTransaction(ctx, func(tx *repository.PostgresTx) error {
			return tx.Devices().Create(ctx, domain.Device{
				ID:                replacementDeviceID,
				ProjectID:         projectTwoID,
				DeviceTypeID:      deviceType.ID,
				Name:              "Replacement Lock",
				ProviderCode:      domain.ProviderCodeWWTIOT,
				ProviderDeviceID:  "LOCK-001",
				AccessType:        domain.AccessTypeCloudAPI,
				TransportProtocol: domain.TransportProtocolHTTP,
				Adapter:           domain.AdapterWWTIOTCloudAPI,
				ConnectionStatus:  domain.ConnectionStatusUnknown,
				LifecycleStatus:   domain.LifecycleStatusActive,
				CreatedAt:         now,
				UpdatedAt:         now,
			})
		})
		if err != nil {
			t.Fatal(err)
		}
		mapped, err := store.Devices().GetByProviderIdentity(ctx, domain.ProviderCodeWWTIOT, "LOCK-001")
		if err != nil || mapped.ID != replacementDeviceID || mapped.ProjectID != projectTwoID {
			t.Fatalf("replacement Provider identity mapping failed: device=%+v err=%v", mapped, err)
		}

		rawMessageID := insertTrustedRawMessageFixture(t, db, replacementDeviceID, now)
		err = store.WithinTransaction(ctx, func(tx *repository.PostgresTx) error {
			locked, err := tx.Devices().GetForUpdate(ctx, replacementDeviceID)
			if err != nil || locked.Name != "Replacement Lock" {
				return fmt.Errorf("lock Device: device=%+v: %w", locked, err)
			}
			if err := tx.Devices().Rename(ctx, replacementDeviceID, "Replacement Lock Renamed"); err != nil {
				return err
			}
			updated, err := tx.Devices().SetConnectionStatus(ctx, replacementDeviceID, domain.ConnectionStatusUnknown, domain.ConnectionStatusOffline)
			if err != nil || !updated {
				return fmt.Errorf("connection CAS updated=%v: %w", updated, err)
			}
			return tx.Devices().SaveState(ctx, domain.DeviceState{
				ID:             "50000000-0000-0000-0000-000000000001",
				DeviceID:       replacementDeviceID,
				State:          map[string]any{"lock_state": "locked"},
				EvidenceStatus: domain.EvidenceVerified,
				ObservedAt:     now,
				RawMessageID:   rawMessageID,
				CreatedAt:      now,
			})
		})
		if err != nil {
			t.Fatal(err)
		}
		updatedDevice, err := store.Devices().Get(ctx, replacementDeviceID)
		if err != nil || updatedDevice.Name != "Replacement Lock Renamed" || updatedDevice.ConnectionStatus != domain.ConnectionStatusOffline {
			t.Fatalf("updated Device mismatch: device=%+v err=%v", updatedDevice, err)
		}
		state, err := store.Devices().GetCurrentState(ctx, replacementDeviceID)
		if err != nil || state.EvidenceStatus != domain.EvidenceVerified || state.State["lock_state"] != "locked" || state.ReportedAt != nil {
			t.Fatalf("trusted current state mismatch: state=%+v err=%v", state, err)
		}

		webhookURL := "https://example.test/device-events"
		secretVersion := 1
		err = store.WithinTransaction(ctx, func(tx *repository.PostgresTx) error {
			if err := tx.Projects().CreateWebhookSecretVersion(ctx, domain.WebhookSecretVersion{
				ProjectID:            projectOneID,
				Version:              secretVersion,
				Ciphertext:           bytes.Repeat([]byte{0x33}, 33),
				Nonce:                bytes.Repeat([]byte{0x44}, 12),
				EncryptionKeyVersion: 1,
				CreatedAt:            now,
			}); err != nil {
				return err
			}
			return tx.Projects().SetWebhookConfiguration(ctx, projectOneID, &webhookURL, 1, &secretVersion)
		})
		if err != nil {
			t.Fatal(err)
		}
		configured, err := store.Projects().Get(ctx, projectOneID)
		if err != nil || configured.WebhookURL == nil || *configured.WebhookURL != webhookURL || configured.CurrentWebhookSecretVersion == nil || *configured.CurrentWebhookSecretVersion != 1 {
			t.Fatalf("webhook configuration mismatch: project=%+v err=%v", configured, err)
		}
		secret, err := store.Projects().GetWebhookSecretVersion(ctx, projectOneID, 1)
		if err != nil || len(secret.Nonce) != 12 || secret.EncryptionKeyVersion != 1 {
			t.Fatalf("webhook secret snapshot mismatch: secret=%+v err=%v", secret, err)
		}
	})
}

func TestPostgresStoreRollsBackProjectDeviceTransaction(t *testing.T) {
	withRepositoryTestDatabase(t, func(db *sql.DB) {
		ctx := context.Background()
		store := repository.NewPostgresStore(db)
		sentinel := errors.New("stop transaction")
		err := store.WithinTransaction(ctx, func(tx *repository.PostgresTx) error {
			if err := tx.Projects().Create(ctx, domain.Project{
				ID:           projectOneID,
				Name:         "Rolled Back",
				APIKeyDigest: bytes.Repeat([]byte{0x55}, 32),
				IPWhitelist:  []string{},
				CreatedAt:    time.Now().UTC(),
				UpdatedAt:    time.Now().UTC(),
			}); err != nil {
				return err
			}
			return sentinel
		})
		if !errors.Is(err, sentinel) {
			t.Fatalf("transaction error = %v", err)
		}
		if _, err := store.Projects().Get(ctx, projectOneID); !errors.Is(err, sql.ErrNoRows) {
			t.Fatalf("rolled-back Project lookup error = %v", err)
		}
	})
}

func insertTrustedRawMessageFixture(t *testing.T, db *sql.DB, deviceID string, now time.Time) string {
	t.Helper()
	rawMessageID := "40000000-0000-0000-0000-000000000001"
	if _, err := db.Exec(`
		INSERT INTO device_raw_messages (
			id, device_id, provider_code, provider_device_id, access_type,
			transport_protocol, adapter, direction, headers, body,
			deduplication_key, received_at, created_at
		) VALUES ($1, $2, 'wwtiot', 'LOCK-001', 'cloud_api', 'http',
			'wwtiot_cloud_api', 'inbound', '{}'::jsonb, $3, 'callback-1', $4, $4)
	`, rawMessageID, deviceID, []byte(`{"lockstatus":0}`), now); err != nil {
		t.Fatal(err)
	}
	return rawMessageID
}

func withRepositoryTestDatabase(t *testing.T, fn func(*sql.DB)) {
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
	schema := fmt.Sprintf("repository_contract_test_%d", time.Now().UnixNano())
	if _, err := admin.Exec(`CREATE SCHEMA ` + schema); err != nil {
		t.Fatal(err)
	}
	defer func() {
		if _, err := admin.Exec(`DROP SCHEMA ` + schema + ` CASCADE`); err != nil {
			t.Errorf("drop repository test schema: %v", err)
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
	fn(db)
}
