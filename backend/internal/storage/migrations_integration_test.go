//go:build integration

package storage

import (
	"context"
	"database/sql"
	"fmt"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"

	_ "github.com/lib/pq"
)

func TestFrozenContractMigrationOnPostgres(t *testing.T) {
	baseURL := requireMigrationTestDatabase(t)
	withIsolatedSchema(t, baseURL, func(db *sql.DB) {
		ctx := context.Background()
		if err := ApplyMigrations(ctx, db); err != nil {
			t.Fatalf("apply migrations: %v", err)
		}
		if err := ApplyMigrations(ctx, db); err != nil {
			t.Fatalf("repeat migrations: %v", err)
		}
		var count int
		if err := db.QueryRow(`SELECT count(*) FROM schema_migrations`).Scan(&count); err != nil || count != 7 {
			t.Fatalf("migration count = %d, err = %v", count, err)
		}
		var profileHash string
		if err := db.QueryRow(`SELECT encode(profile_hash, 'hex') FROM device_type_profiles WHERE revision = 1`).Scan(&profileHash); err != nil {
			t.Fatal(err)
		}
		if profileHash != "81f6d5efb5f627a56fc19a2e2fb7fadcccc9b6a6b53fa411d7265a15eda5b596" {
			t.Fatalf("database profile hash = %s", profileHash)
		}
		if err := ValidateFrozenContracts(ctx, db); err != nil {
			t.Fatalf("validate frozen profile snapshot: %v", err)
		}
		if _, err := db.Exec(`UPDATE device_type_profiles SET profile = '{}' WHERE revision = 1`); err == nil {
			t.Fatal("published Device Type profile must be immutable")
		}
		assertFrozenSchemaBehavior(t, db)
	})
}

func TestMigrationFailsClosedWithoutRecordingVersion(t *testing.T) {
	baseURL := requireMigrationTestDatabase(t)
	withIsolatedSchema(t, baseURL, func(db *sql.DB) {
		ctx := context.Background()
		conn, err := db.Conn(ctx)
		if err != nil {
			t.Fatal(err)
		}
		if err := ensureMigrationTable(ctx, conn); err != nil {
			conn.Close()
			t.Fatal(err)
		}
		if err := applyMigration(ctx, conn, "001_device_platform_core.up.sql", "001_device_platform_core"); err != nil {
			conn.Close()
			t.Fatal(err)
		}
		if err := conn.Close(); err != nil {
			t.Fatal(err)
		}
		_, err = db.Exec(`
			INSERT INTO projects (id, name, api_key_hash, webhook_url, webhook_secret)
			VALUES ('10000000-0000-0000-0000-000000000001', 'legacy', repeat('a', 64), 'https://example.test/hook', 'plaintext')
		`)
		if err != nil {
			t.Fatal(err)
		}
		if err := ApplyMigrations(ctx, db); err == nil || !strings.Contains(err.Error(), "explicit encryption") {
			t.Fatalf("expected fail-closed webhook migration, got %v", err)
		}
		var applied bool
		if err := db.QueryRow(`SELECT EXISTS (SELECT 1 FROM schema_migrations WHERE version = '002_frozen_contract_alignment')`).Scan(&applied); err != nil {
			t.Fatal(err)
		}
		if applied {
			t.Fatal("failed migration must not record version")
		}
		var plaintext string
		if err := db.QueryRow(`SELECT webhook_secret FROM projects LIMIT 1`).Scan(&plaintext); err != nil || plaintext != "plaintext" {
			t.Fatalf("failed migration did not roll back atomically: %q, %v", plaintext, err)
		}
	})
}

func TestMigrationRejectsUnverifiableLegacyData(t *testing.T) {
	baseURL := requireMigrationTestDatabase(t)
	tests := []struct {
		name        string
		seed        string
		wantMessage string
	}{
		{
			name: "device metadata",
			seed: legacyBaseFixture + `
				INSERT INTO devices (id, project_id, device_type_id, name, provider_code, provider_device_id, access_type, transport_protocol, adapter, metadata)
				VALUES ('20000000-0000-0000-0000-000000000001', '10000000-0000-0000-0000-000000000001', '00000000-0000-0000-0000-000000000001', 'lock', 'wwtiot', 'LOCK-1', 'cloud_api', 'http', 'wwtiot_cloud_api', '{"legacy":true}');
			`,
			wantMessage: "non-empty legacy device metadata",
		},
		{
			name: "Device Type configuration",
			seed: `
				INSERT INTO projects (id, name, api_key_hash)
				VALUES ('10000000-0000-0000-0000-000000000001', 'one', repeat('a', 64));
				INSERT INTO device_types (id, code, name, capabilities)
				VALUES ('00000000-0000-0000-0000-000000000001', 'smart_lock', 'Smart Lock', '["unlock"]');
			`,
			wantMessage: "legacy Device Type configuration",
		},
		{
			name: "simulator identity",
			seed: legacyBaseFixture + `
				INSERT INTO devices (id, project_id, device_type_id, name, provider_code, provider_device_id, access_type, transport_protocol, adapter)
				VALUES ('20000000-0000-0000-0000-000000000001', '10000000-0000-0000-0000-000000000001', '00000000-0000-0000-0000-000000000001', 'simulator', 'simulator', 'legacy-id', 'mock_gateway', 'simulator', 'mock_gateway');
			`,
			wantMessage: "reversibly normalize a legacy simulator Provider identity",
		},
		{
			name: "simulator Provider code",
			seed: legacyBaseFixture + `
				INSERT INTO devices (id, project_id, device_type_id, name, provider_code, provider_device_id, access_type, transport_protocol, adapter)
				VALUES ('20000000-0000-0000-0000-000000000001', '10000000-0000-0000-0000-000000000001', '00000000-0000-0000-0000-000000000001', 'simulator', 'legacy-mock', '20000000-0000-0000-0000-000000000001', 'mock_gateway', 'simulator', 'mock_gateway');
			`,
			wantMessage: "reversibly normalize a legacy simulator Provider identity",
		},
		{
			name: "Provider identity collision",
			seed: legacyBaseFixture + `
				INSERT INTO projects (id, name, api_key_hash) VALUES ('10000000-0000-0000-0000-000000000002', 'two', repeat('b', 64));
				INSERT INTO devices (id, project_id, device_type_id, name, provider_code, provider_device_id, access_type, transport_protocol, adapter)
				VALUES
				('20000000-0000-0000-0000-000000000001', '10000000-0000-0000-0000-000000000001', '00000000-0000-0000-0000-000000000001', 'one', 'wwtiot', 'LOCK-1', 'cloud_api', 'http', 'wwtiot_cloud_api'),
				('20000000-0000-0000-0000-000000000002', '10000000-0000-0000-0000-000000000002', '00000000-0000-0000-0000-000000000001', 'two', 'wwtiot', 'LOCK-1', 'cloud_api', 'http', 'wwtiot_cloud_api');
			`,
			wantMessage: "globally unique active Provider identities",
		},
		{
			name: "legacy command evidence",
			seed: legacyBaseFixture + `
				INSERT INTO devices (id, project_id, device_type_id, name, provider_code, provider_device_id, access_type, transport_protocol, adapter)
				VALUES ('20000000-0000-0000-0000-000000000001', '10000000-0000-0000-0000-000000000001', '00000000-0000-0000-0000-000000000001', 'lock', 'wwtiot', 'LOCK-1', 'cloud_api', 'http', 'wwtiot_cloud_api');
				INSERT INTO device_commands (id, project_id, device_id, command_type, delivery_policy, status)
				VALUES ('30000000-0000-0000-0000-000000000001', '10000000-0000-0000-0000-000000000001', '20000000-0000-0000-0000-000000000001', 'unlock', 'online_only', 'success');
			`,
			wantMessage: "cannot infer frozen lifecycle evidence",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			withIsolatedSchema(t, baseURL, func(db *sql.DB) {
				applyOnlyFirstMigration(t, db)
				if _, err := db.Exec(test.seed); err != nil {
					t.Fatal(err)
				}
				if err := ApplyMigrations(context.Background(), db); err == nil || !strings.Contains(err.Error(), test.wantMessage) {
					t.Fatalf("expected %q, got %v", test.wantMessage, err)
				}
				assertMigrationNotApplied(t, db, "002_frozen_contract_alignment")
			})
		})
	}
}

func TestMigrationRunnerRejectsGolangMigrateTable(t *testing.T) {
	baseURL := requireMigrationTestDatabase(t)
	withIsolatedSchema(t, baseURL, func(db *sql.DB) {
		if _, err := db.Exec(`CREATE TABLE schema_migrations (version BIGINT NOT NULL, dirty BOOLEAN NOT NULL)`); err != nil {
			t.Fatal(err)
		}
		err := ApplyMigrations(context.Background(), db)
		if err == nil || !strings.Contains(err.Error(), "owned by golang-migrate") {
			t.Fatalf("expected migration runner ownership error, got %v", err)
		}
	})
}

func TestMigrationRunnerHonorsAdvisoryLockContention(t *testing.T) {
	baseURL := requireMigrationTestDatabase(t)
	withIsolatedSchema(t, baseURL, func(db *sql.DB) {
		db.SetMaxOpenConns(2)
		lockConn, err := db.Conn(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		defer lockConn.Close()
		if _, err := lockConn.ExecContext(context.Background(), `SELECT pg_advisory_lock($1)`, migrationAdvisoryLock); err != nil {
			t.Fatal(err)
		}
		defer lockConn.ExecContext(context.Background(), `SELECT pg_advisory_unlock($1)`, migrationAdvisoryLock) //nolint:errcheck

		ctx, cancel := context.WithTimeout(context.Background(), 150*time.Millisecond)
		defer cancel()
		err = ApplyMigrations(ctx, db)
		if err == nil || !strings.Contains(err.Error(), "acquire migration lock") {
			t.Fatalf("expected advisory lock contention, got %v", err)
		}
	})
}

func TestMigrationRollbackAndReapply(t *testing.T) {
	baseURL := requireMigrationTestDatabase(t)
	withIsolatedSchema(t, baseURL, func(db *sql.DB) {
		ctx := context.Background()
		if err := ApplyMigrations(ctx, db); err != nil {
			t.Fatal(err)
		}
		rollbackTransportTimingMigration(t, ctx, db)
		if err := RollbackLastMigration(ctx, db); err != nil {
			t.Fatalf("rollback 002: %v", err)
		}
		var exists bool
		if err := db.QueryRow(`SELECT to_regclass('device_type_profiles') IS NOT NULL`).Scan(&exists); err != nil {
			t.Fatal(err)
		}
		if exists {
			t.Fatal("002 table still exists after rollback")
		}
		var deviceTypeCount int
		if err := db.QueryRow(`SELECT count(*) FROM device_types`).Scan(&deviceTypeCount); err != nil || deviceTypeCount != 0 {
			t.Fatalf("fresh 002 seed must be removed by rollback: count=%d err=%v", deviceTypeCount, err)
		}
		if err := ApplyMigrations(ctx, db); err != nil {
			t.Fatalf("reapply 002: %v", err)
		}
	})
}

func TestAuditLogActionsMigrationRollbackAndReapply(t *testing.T) {
	baseURL := requireMigrationTestDatabase(t)
	withIsolatedSchema(t, baseURL, func(db *sql.DB) {
		ctx := context.Background()
		if err := ApplyMigrations(ctx, db); err != nil {
			t.Fatal(err)
		}
		rollbackInstallationSingletonMigration(t, ctx, db)
		if err := RollbackLastMigration(ctx, db); err != nil {
			t.Fatalf("rollback 005: %v", err)
		}
		assertSQLFails(t, db, `
			INSERT INTO audit_logs (id, actor_type, action, resource_type, result)
			VALUES ('80000000-0000-0000-0000-000000000001', 'system', 'provider.callback_rejected', 'provider_callback', 'failure')
		`, "extended Audit action after 005 rollback")
		if err := ApplyMigrations(ctx, db); err != nil {
			t.Fatalf("reapply 005: %v", err)
		}
		if _, err := db.Exec(`
			INSERT INTO audit_logs (id, actor_type, action, resource_type, result)
			VALUES
				('80000000-0000-0000-0000-000000000001', 'system', 'project.webhook_secret_decryption_failed', 'project', 'failure'),
				('80000000-0000-0000-0000-000000000002', 'provider', 'provider.callback_rejected', 'provider_callback', 'failure')
		`); err != nil {
			t.Fatalf("insert extended Audit actions after reapply: %v", err)
		}
	})
}

func TestAuditLogActionsRollbackRejectsExtendedActions(t *testing.T) {
	baseURL := requireMigrationTestDatabase(t)
	tests := []string{
		"project.webhook_secret_decryption_failed",
		"provider.callback_rejected",
	}
	for index, action := range tests {
		t.Run(action, func(t *testing.T) {
			withIsolatedSchema(t, baseURL, func(db *sql.DB) {
				ctx := context.Background()
				if err := ApplyMigrations(ctx, db); err != nil {
					t.Fatal(err)
				}
				rollbackInstallationSingletonMigration(t, ctx, db)
				id := fmt.Sprintf("80000000-0000-0000-0000-%012d", index+1)
				if _, err := db.Exec(`
					INSERT INTO audit_logs (id, actor_type, action, resource_type, result)
					VALUES ($1, 'system', $2, 'audit_action', 'failure')
				`, id, action); err != nil {
					t.Fatalf("insert extended Audit action: %v", err)
				}

				err := RollbackLastMigration(ctx, db)
				if err == nil || !strings.Contains(err.Error(), "cannot rollback audit log actions while extended Audit actions exist") {
					t.Fatalf("expected fail-closed 005 rollback, got %v", err)
				}
				var applied bool
				if err := db.QueryRow(`SELECT EXISTS (SELECT 1 FROM schema_migrations WHERE version = '005_audit_log_actions')`).Scan(&applied); err != nil || !applied {
					t.Fatalf("005 must remain applied after refused rollback: applied=%v err=%v", applied, err)
				}
				var storedAction string
				if err := db.QueryRow(`SELECT action FROM audit_logs WHERE id = $1`, id).Scan(&storedAction); err != nil || storedAction != action {
					t.Fatalf("refused rollback must preserve Audit data: action=%q err=%v", storedAction, err)
				}
			})
		})
	}
}

func TestTransportFailureTimingRollbackRejectsIncompatibleData(t *testing.T) {
	baseURL := requireMigrationTestDatabase(t)
	withIsolatedSchema(t, baseURL, func(db *sql.DB) {
		ctx := context.Background()
		if err := ApplyMigrations(ctx, db); err != nil {
			t.Fatal(err)
		}
		rollbackWebhookAttemptLimitMigration(t, ctx, db)
		if _, err := db.Exec(`
			INSERT INTO projects (id, name, api_key_hash)
			VALUES ('10000000-0000-0000-0000-000000000001', 'rollback guard', decode(repeat('11', 32), 'hex'));
			INSERT INTO devices (
				id, project_id, device_type_id, name, provider_code, provider_device_id,
				access_type, transport_protocol, adapter
			) VALUES (
				'20000000-0000-0000-0000-000000000001', '10000000-0000-0000-0000-000000000001',
				'00000000-0000-0000-0000-000000000001', 'rollback lock', 'wwtiot', 'ROLLBACK-LOCK-1',
				'cloud_api', 'http', 'wwtiot_cloud_api'
			);
			INSERT INTO device_commands (
				id, project_id, device_id, device_type_id, command_type, payload,
				device_type_revision, delivery_policy, status, reason_code,
				confirmation_level, evidence_status, idempotency_key, request_hash,
				queued_at, dispatch_deadline_at, sent_at, result_deadline_at, finished_at
			) VALUES (
				'30000000-0000-0000-0000-000000000001', '10000000-0000-0000-0000-000000000001',
				'20000000-0000-0000-0000-000000000001', '00000000-0000-0000-0000-000000000001',
				'unlock', '{}', 1, 'dispatch_once', 'failed', 'provider_transport_error',
				'none', 'none', 'rollback-guard-1', decode(repeat('22', 32), 'hex'),
				now() - interval '2 seconds', now() + interval '28 seconds', now() - interval '1 second',
				now() + interval '59 seconds', now()
			)
		`); err != nil {
			t.Fatal(err)
		}

		err := RollbackLastMigration(ctx, db)
		if err == nil || !strings.Contains(err.Error(), "cannot rollback transport failure timing while dispatched provider_transport_error Commands exist") {
			t.Fatalf("expected fail-closed 003 rollback, got %v", err)
		}
		var applied bool
		if err := db.QueryRow(`SELECT EXISTS (SELECT 1 FROM schema_migrations WHERE version = '003_command_transport_failure_timing')`).Scan(&applied); err != nil || !applied {
			t.Fatalf("003 must remain applied after refused rollback: applied=%v err=%v", applied, err)
		}
		var sentAt time.Time
		if err := db.QueryRow(`SELECT sent_at FROM device_commands WHERE id = '30000000-0000-0000-0000-000000000001'`).Scan(&sentAt); err != nil {
			t.Fatalf("refused rollback must preserve Command data: %v", err)
		}
	})
}

func TestWebhookAttemptLimitRollbackRejectsEarlyDeadDelivery(t *testing.T) {
	baseURL := requireMigrationTestDatabase(t)
	withIsolatedSchema(t, baseURL, func(db *sql.DB) {
		ctx := context.Background()
		if err := ApplyMigrations(ctx, db); err != nil {
			t.Fatal(err)
		}
		rollbackAuditLogActionsMigration(t, ctx, db)
		if _, err := db.Exec(`
			INSERT INTO projects (id, name, api_key_hash)
			VALUES ('10000000-0000-0000-0000-000000000001', 'webhook rollback guard', decode(repeat('11', 32), 'hex'));
			INSERT INTO project_webhook_secrets (project_id, version, ciphertext, nonce, encryption_key_version)
			VALUES (
				'10000000-0000-0000-0000-000000000001', 1,
				decode(repeat('22', 17), 'hex'), decode(repeat('33', 12), 'hex'), 1
			);
			UPDATE projects
			SET webhook_url = 'https://example.test/hook', webhook_config_version = 1,
				current_webhook_secret_version = 1
			WHERE id = '10000000-0000-0000-0000-000000000001';
			INSERT INTO devices (
				id, project_id, device_type_id, name, provider_code, provider_device_id,
				access_type, transport_protocol, adapter
			) VALUES (
				'20000000-0000-0000-0000-000000000001', '10000000-0000-0000-0000-000000000001',
				'00000000-0000-0000-0000-000000000001', 'webhook lock', 'wwtiot', 'WEBHOOK-LOCK-1',
				'cloud_api', 'http', 'wwtiot_cloud_api'
			);
			INSERT INTO device_events (
				id, project_id, device_id, event_type, source, payload, occurred_at, deduplication_key
			) VALUES (
				'30000000-0000-0000-0000-000000000001', '10000000-0000-0000-0000-000000000001',
				'20000000-0000-0000-0000-000000000001', 'device.created', 'admin',
				'{"device_type_code":"smart-lock","provider_code":"wwtiot","lifecycle_status":"active"}',
				now(), 'webhook-rollback-event'
			);
			INSERT INTO webhook_deliveries (
				id, project_id, event_id, target_url, webhook_config_version,
				webhook_secret_version, raw_body, attempt_count, status,
				last_error_code, last_error_detail, next_attempt_at
			) VALUES (
				'40000000-0000-0000-0000-000000000001', '10000000-0000-0000-0000-000000000001',
				'30000000-0000-0000-0000-000000000001', 'https://example.test/hook', 1, 1,
				'body', 3, 'dead', 'attempt_limit', 'configured maximum reached', NULL
			)
		`); err != nil {
			t.Fatal(err)
		}

		err := RollbackLastMigration(ctx, db)
		if err == nil || !strings.Contains(err.Error(), "cannot rollback configurable Webhook attempt limit while early-dead Deliveries exist") {
			t.Fatalf("expected fail-closed 004 rollback, got %v", err)
		}
		var applied bool
		if err := db.QueryRow(`SELECT EXISTS (SELECT 1 FROM schema_migrations WHERE version = '004_webhook_delivery_attempt_limit')`).Scan(&applied); err != nil || !applied {
			t.Fatalf("004 must remain applied after refused rollback: applied=%v err=%v", applied, err)
		}
		var attemptCount int
		if err := db.QueryRow(`SELECT attempt_count FROM webhook_deliveries WHERE id = '40000000-0000-0000-0000-000000000001'`).Scan(&attemptCount); err != nil || attemptCount != 3 {
			t.Fatalf("refused rollback must preserve Delivery: attempts=%d err=%v", attemptCount, err)
		}
	})
}

func TestMigrationRollbackPreservesHyphenatedLegacyDeviceTypeCode(t *testing.T) {
	baseURL := requireMigrationTestDatabase(t)
	withIsolatedSchema(t, baseURL, func(db *sql.DB) {
		applyOnlyFirstMigration(t, db)
		if _, err := db.Exec(`
			INSERT INTO device_types (id, code, name)
			VALUES ('00000000-0000-0000-0000-000000000099', 'smart-lock', 'Smart Lock')
		`); err != nil {
			t.Fatal(err)
		}
		if err := ApplyMigrations(context.Background(), db); err != nil {
			t.Fatal(err)
		}
		rollbackTransportTimingMigration(t, context.Background(), db)
		if err := RollbackLastMigration(context.Background(), db); err != nil {
			t.Fatal(err)
		}
		var code string
		if err := db.QueryRow(`SELECT code FROM device_types WHERE id = '00000000-0000-0000-0000-000000000099'`).Scan(&code); err != nil {
			t.Fatal(err)
		}
		if code != "smart-lock" {
			t.Fatalf("legacy Device Type code = %q", code)
		}
	})
}

func TestMigrationRollbackUsesVersionOrderWhenTimestampsAreOutOfOrder(t *testing.T) {
	baseURL := requireMigrationTestDatabase(t)
	withIsolatedSchema(t, baseURL, func(db *sql.DB) {
		ctx := context.Background()
		if err := ApplyMigrations(ctx, db); err != nil {
			t.Fatal(err)
		}
		if _, err := db.Exec(`
			UPDATE schema_migrations
			SET applied_at = CASE
				WHEN version = '007_command_evidence_event' THEN now() - interval '1 day'
				WHEN version = '001_device_platform_core' THEN now() + interval '1 day'
				ELSE applied_at
			END
		`); err != nil {
			t.Fatal(err)
		}
		if err := RollbackLastMigration(ctx, db); err != nil {
			t.Fatal(err)
		}
		assertMigrationNotApplied(t, db, "007_command_evidence_event")
		var previousApplied bool
		if err := db.QueryRow(`SELECT EXISTS (SELECT 1 FROM schema_migrations WHERE version = '006_installation_single_admin')`).Scan(&previousApplied); err != nil {
			t.Fatal(err)
		}
		if !previousApplied {
			t.Fatal("rollback skipped migration 007 and removed an earlier version")
		}
	})
}

func TestCommandEvidenceEventMigrationDownFailsClosed(t *testing.T) {
	baseURL := requireMigrationTestDatabase(t)
	withIsolatedSchema(t, baseURL, func(db *sql.DB) {
		if err := ApplyMigrations(context.Background(), db); err != nil {
			t.Fatal(err)
		}
		assertFrozenSchemaBehavior(t, db)
		err := RollbackLastMigration(context.Background(), db)
		if err == nil || !strings.Contains(err.Error(), "cannot rollback command evidence Event contract while command.evidence_updated Events exist") {
			t.Fatalf("expected fail-closed evidence Event rollback, got %v", err)
		}
		var applied bool
		if err := db.QueryRow(`SELECT EXISTS (SELECT 1 FROM schema_migrations WHERE version = '007_command_evidence_event')`).Scan(&applied); err != nil || !applied {
			t.Fatalf("007 must remain applied after refused rollback: applied=%v err=%v", applied, err)
		}
	})
}

func TestMigrationRollbackRestoresReversibleLegacyDevice(t *testing.T) {
	baseURL := requireMigrationTestDatabase(t)
	withIsolatedSchema(t, baseURL, func(db *sql.DB) {
		applyOnlyFirstMigration(t, db)
		const deviceID = "20000000-0000-0000-0000-000000000001"
		if _, err := db.Exec(legacyBaseFixture + `
			INSERT INTO devices (id, project_id, device_type_id, name, provider_code, provider_device_id, access_type, transport_protocol, adapter)
			VALUES ('` + deviceID + `', '10000000-0000-0000-0000-000000000001', '00000000-0000-0000-0000-000000000001', 'simulator', 'simulator', '` + deviceID + `', 'mock_gateway', 'simulator', 'mock_gateway');
		`); err != nil {
			t.Fatal(err)
		}
		if err := ApplyMigrations(context.Background(), db); err != nil {
			t.Fatal(err)
		}
		rollbackTransportTimingMigration(t, context.Background(), db)
		if err := RollbackLastMigration(context.Background(), db); err != nil {
			t.Fatal(err)
		}
		var code, providerCode, providerDeviceID, accessType, transportProtocol, adapter string
		if err := db.QueryRow(`
			SELECT dt.code, d.provider_code, d.provider_device_id, d.access_type, d.transport_protocol, d.adapter
			FROM devices d JOIN device_types dt ON dt.id = d.device_type_id WHERE d.id = $1
		`, deviceID).Scan(&code, &providerCode, &providerDeviceID, &accessType, &transportProtocol, &adapter); err != nil {
			t.Fatal(err)
		}
		if code != "smart_lock" || providerCode != "simulator" || providerDeviceID != deviceID || accessType != "mock_gateway" || transportProtocol != "simulator" || adapter != "mock_gateway" {
			t.Fatalf("legacy Device not restored: %s %s %s %s %s %s", code, providerCode, providerDeviceID, accessType, transportProtocol, adapter)
		}
		var hasMetadata, hasProfileTable bool
		if err := db.QueryRow(`
			SELECT
				EXISTS (SELECT 1 FROM information_schema.columns WHERE table_schema = current_schema() AND table_name = 'devices' AND column_name = 'metadata'),
				to_regclass('device_type_profiles') IS NOT NULL
		`).Scan(&hasMetadata, &hasProfileTable); err != nil {
			t.Fatal(err)
		}
		if !hasMetadata || hasProfileTable {
			t.Fatalf("legacy catalog not restored: metadata=%v profile_table=%v", hasMetadata, hasProfileTable)
		}
	})
}

func TestMigrationDownRejectsSecurityAndRuntimeState(t *testing.T) {
	baseURL := requireMigrationTestDatabase(t)
	tests := []struct {
		name string
		seed string
	}{
		{
			name: "session generation",
			seed: `INSERT INTO users (id, email, password_hash, display_name, is_admin, session_generation)
					VALUES ('70000000-0000-0000-0000-000000000001', 'admin@example.test', 'hash', 'Admin', true, 1)`,
		},
		{
			name: "auth failures",
			seed: `INSERT INTO auth_login_failure_events (id, scope, key_digest, occurred_at, expires_at)
				VALUES ('71000000-0000-0000-0000-000000000001', 'ip', decode(repeat('11', 32), 'hex'), now(), now() + interval '1 hour')`,
		},
		{
			name: "webhook configuration",
			seed: `
				INSERT INTO projects (id, name, api_key_hash)
				VALUES ('72000000-0000-0000-0000-000000000001', 'webhook', decode(repeat('22', 32), 'hex'));
				INSERT INTO project_webhook_secrets (project_id, version, ciphertext, nonce, encryption_key_version)
				VALUES ('72000000-0000-0000-0000-000000000001', 1, decode(repeat('33', 17), 'hex'), decode(repeat('44', 12), 'hex'), 1);
				UPDATE projects SET webhook_url = 'https://example.test/hook', webhook_config_version = 1,
					current_webhook_secret_version = 1 WHERE id = '72000000-0000-0000-0000-000000000001';
			`,
		},
		{
			name: "simulator configuration",
			seed: `UPDATE simulator_config SET outcome = 'provider_rejected', version = version + 1`,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			withIsolatedSchema(t, baseURL, func(db *sql.DB) {
				if err := ApplyMigrations(context.Background(), db); err != nil {
					t.Fatal(err)
				}
				if _, err := db.Exec(test.seed); err != nil {
					t.Fatalf("seed protected state: %v", err)
				}
				rollbackTransportTimingMigration(t, context.Background(), db)
				err := RollbackLastMigration(context.Background(), db)
				if err == nil || !strings.Contains(err.Error(), "cannot discard frozen lifecycle data") {
					t.Fatalf("expected fail-closed rollback, got %v", err)
				}
				var applied bool
				if err := db.QueryRow(`SELECT EXISTS (SELECT 1 FROM schema_migrations WHERE version = '002_frozen_contract_alignment')`).Scan(&applied); err != nil || !applied {
					t.Fatalf("002 must remain applied after refused rollback: applied=%v err=%v", applied, err)
				}
			})
		})
	}
}

func rollbackTransportTimingMigration(t *testing.T, ctx context.Context, db *sql.DB) {
	t.Helper()
	rollbackWebhookAttemptLimitMigration(t, ctx, db)
	if err := RollbackLastMigration(ctx, db); err != nil {
		t.Fatalf("rollback 003: %v", err)
	}
	var applied bool
	if err := db.QueryRow(`SELECT EXISTS (SELECT 1 FROM schema_migrations WHERE version = '003_command_transport_failure_timing')`).Scan(&applied); err != nil {
		t.Fatal(err)
	}
	if applied {
		t.Fatal("003 must be removed before testing 002 rollback")
	}
}

func rollbackWebhookAttemptLimitMigration(t *testing.T, ctx context.Context, db *sql.DB) {
	t.Helper()
	rollbackAuditLogActionsMigration(t, ctx, db)
	if err := RollbackLastMigration(ctx, db); err != nil {
		t.Fatalf("rollback 004: %v", err)
	}
	var applied bool
	if err := db.QueryRow(`SELECT EXISTS (SELECT 1 FROM schema_migrations WHERE version = '004_webhook_delivery_attempt_limit')`).Scan(&applied); err != nil {
		t.Fatal(err)
	}
	if applied {
		t.Fatal("004 must be removed before testing earlier rollback")
	}
}

func rollbackAuditLogActionsMigration(t *testing.T, ctx context.Context, db *sql.DB) {
	t.Helper()
	rollbackInstallationSingletonMigration(t, ctx, db)
	if err := RollbackLastMigration(ctx, db); err != nil {
		t.Fatalf("rollback 005: %v", err)
	}
	var applied bool
	if err := db.QueryRow(`SELECT EXISTS (SELECT 1 FROM schema_migrations WHERE version = '005_audit_log_actions')`).Scan(&applied); err != nil {
		t.Fatal(err)
	}
	if applied {
		t.Fatal("005 must be removed before testing earlier rollback")
	}
}

func rollbackInstallationSingletonMigration(t *testing.T, ctx context.Context, db *sql.DB) {
	t.Helper()
	rollbackCommandEvidenceEventMigration(t, ctx, db)
	if err := RollbackLastMigration(ctx, db); err != nil {
		t.Fatalf("rollback 006: %v", err)
	}
	var applied bool
	if err := db.QueryRow(`SELECT EXISTS (SELECT 1 FROM schema_migrations WHERE version = '006_installation_single_admin')`).Scan(&applied); err != nil {
		t.Fatal(err)
	}
	if applied {
		t.Fatal("006 must be removed before testing earlier rollback")
	}
}

func rollbackCommandEvidenceEventMigration(t *testing.T, ctx context.Context, db *sql.DB) {
	t.Helper()
	if err := RollbackLastMigration(ctx, db); err != nil {
		t.Fatalf("rollback 007: %v", err)
	}
	assertMigrationNotApplied(t, db, "007_command_evidence_event")
}

func assertFrozenSchemaBehavior(t *testing.T, db *sql.DB) {
	t.Helper()
	const project1 = "10000000-0000-0000-0000-000000000001"
	const project2 = "10000000-0000-0000-0000-000000000002"
	const device1 = "20000000-0000-0000-0000-000000000001"
	const device2 = "20000000-0000-0000-0000-000000000002"
	const deviceType = "00000000-0000-0000-0000-000000000001"
	if _, err := db.Exec(`
		INSERT INTO users (id, email, password_hash, display_name, is_admin)
		VALUES ('70000000-0000-0000-0000-000000000001', 'admin@example.test', 'hash', 'Admin', true)
	`); err != nil {
		t.Fatal(err)
	}
	assertSQLFails(t, db, `
		INSERT INTO users (id, email, password_hash, display_name, is_admin)
		VALUES ('70000000-0000-0000-0000-000000000002', 'second@example.test', 'hash', 'Second', true)
	`, "second user")
	assertSQLFails(t, db, `UPDATE users SET is_admin = false WHERE id = '70000000-0000-0000-0000-000000000001'`, "non-admin singleton user")
	for index, projectID := range []string{project1, project2} {
		digest := make([]byte, 32)
		digest[len(digest)-1] = byte(index + 1)
		if _, err := db.Exec(`INSERT INTO projects (id, name, api_key_hash) VALUES ($1, $2, $3)`, projectID, "project-"+projectID[len(projectID)-1:], digest); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := db.Exec(`
		INSERT INTO devices (id, project_id, device_type_id, name, provider_code, provider_device_id, access_type, transport_protocol, adapter)
		VALUES ($1, $2, $3, 'lock one', 'wwtiot', 'LOCK-001', 'cloud_api', 'http', 'wwtiot_cloud_api')
	`, device1, project1, deviceType); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`
		INSERT INTO devices (id, project_id, device_type_id, name, provider_code, provider_device_id, access_type, transport_protocol, adapter)
		VALUES ($1, $2, $3, 'lock duplicate', 'wwtiot', 'LOCK-001', 'cloud_api', 'http', 'wwtiot_cloud_api')
	`, device2, project2, deviceType); err == nil {
		t.Fatal("active Provider identity must be globally unique")
	}
	if _, err := db.Exec(`
		INSERT INTO devices (id, project_id, device_type_id, name, provider_code, provider_device_id, access_type, transport_protocol, adapter)
		VALUES ($1, $2, $3, 'lock two', 'wwtiot', 'LOCK-002', 'cloud_api', 'http', 'wwtiot_cloud_api')
	`, device2, project2, deviceType); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`
		INSERT INTO device_commands (
			id, project_id, device_id, device_type_id, command_type, payload, status, delivery_policy,
			idempotency_key, request_hash, device_type_revision, queued_at, dispatch_deadline_at
		) VALUES (
			'30000000-0000-0000-0000-000000000001', $1, $2, $3, 'unlock', '{}', 'queued', 'dispatch_once',
			'key-1', $4, 1, now(), now() + interval '30 seconds'
		)
	`, project2, device1, deviceType, make([]byte, 32)); err == nil {
		t.Fatal("command Project must match Device Project")
	}
	if _, err := db.Exec(`
		INSERT INTO device_commands (
			id, project_id, device_id, device_type_id, command_type, payload, status, delivery_policy,
			idempotency_key, request_hash, device_type_revision, queued_at, dispatch_deadline_at
		) VALUES (
			'30000000-0000-0000-0000-000000000001', $1, $2, $3, 'unlock', '{}', 'queued', 'dispatch_once',
			'key-1', $4, 1, now(), now() + interval '30 seconds'
		)
	`, project1, device1, deviceType, make([]byte, 32)); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`
		INSERT INTO device_command_attempts (
			id, command_id, attempt_no, adapter, phase, provider_code, provider_request_key,
			lease_token, lease_owner, lease_expires_at
		) VALUES (
			'40000000-0000-0000-0000-000000000001', '30000000-0000-0000-0000-000000000001', 1,
			'wwtiot_cloud_api', 'claimed', 'wwtiot', '100000001', '50000000-0000-0000-0000-000000000001',
			'worker-1', now() + interval '30 seconds'
		)
	`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`
		INSERT INTO device_command_attempts (
			id, command_id, attempt_no, adapter, phase, provider_code, provider_request_key,
			lease_token, lease_owner, lease_expires_at
		) VALUES (
			'40000000-0000-0000-0000-000000000002', '30000000-0000-0000-0000-000000000001', 2,
			'wwtiot_cloud_api', 'claimed', 'wwtiot', '100000002', '50000000-0000-0000-0000-000000000002',
			'worker-2', now() + interval '30 seconds'
		)
	`); err == nil {
		t.Fatal("command must not have two incomplete Attempts")
	}
	assertSQLFails(t, db, `UPDATE device_commands SET sent_at = now() WHERE id = '30000000-0000-0000-0000-000000000001'`, "queued Command with sent_at")
	assertSQLFails(t, db, `UPDATE device_commands SET status = 'sent', sent_at = now() WHERE id = '30000000-0000-0000-0000-000000000001'`, "sent Command without result deadline")
	assertSQLFails(t, db, `UPDATE device_commands SET status = 'acked', sent_at = now(), result_deadline_at = now() + interval '1 minute', confirmation_level = 'device_acked', evidence_status = 'unverified' WHERE id = '30000000-0000-0000-0000-000000000001'`, "unverified device ACK")
	assertSQLFails(t, db, `UPDATE device_commands SET status = 'success', sent_at = now(), result_deadline_at = now() + interval '1 minute', confirmation_level = 'provider_accepted', evidence_status = 'verified', finished_at = now() + interval '2 seconds' WHERE id = '30000000-0000-0000-0000-000000000001'`, "success without device final evidence")

	if _, err := db.Exec(`
		UPDATE device_command_attempts SET phase = 'completed', outcome = 'not_dispatched', completed_at = now()
		WHERE id = '40000000-0000-0000-0000-000000000001'
	`); err != nil {
		t.Fatal(err)
	}
	assertSQLFails(t, db, `
		INSERT INTO device_command_attempts (id, command_id, attempt_no, adapter, phase, provider_code, provider_request_key, lease_token, lease_owner, lease_expires_at)
		VALUES ('40000000-0000-0000-0000-000000000010', '30000000-0000-0000-0000-000000000001', 0, 'wwtiot_cloud_api', 'claimed', 'wwtiot', '100000010', '50000000-0000-0000-0000-000000000010', 'worker', now() + interval '1 minute')
	`, "zero Attempt number")
	assertSQLFails(t, db, `
		INSERT INTO device_command_attempts (id, command_id, attempt_no, adapter, phase, provider_code, provider_request_key, lease_token, lease_owner, lease_expires_at)
		VALUES ('40000000-0000-0000-0000-000000000011', '30000000-0000-0000-0000-000000000001', 2, 'wwtiot_cloud_api', 'claimed', 'wwtiot', 'abc', '50000000-0000-0000-0000-000000000011', 'worker', now() + interval '1 minute')
	`, "non-decimal WWTIOT request key")
	assertSQLFails(t, db, `
		INSERT INTO device_command_attempts (id, command_id, attempt_no, adapter, phase, provider_code, provider_request_key, confirmation_level, evidence_status, lease_token, lease_owner, lease_expires_at)
		VALUES ('40000000-0000-0000-0000-000000000012', '30000000-0000-0000-0000-000000000001', 2, 'wwtiot_cloud_api', 'claimed', 'wwtiot', '100000012', 'transport_sent', 'verified', '50000000-0000-0000-0000-000000000012', 'worker', now() + interval '1 minute')
	`, "non-completed Attempt evidence")
	assertSQLFails(t, db, `
		INSERT INTO device_command_attempts (id, command_id, attempt_no, adapter, phase, provider_code, provider_request_key, outcome, confirmation_level, evidence_status, lease_token, lease_owner, lease_expires_at, claimed_at, dispatching_at, completed_at)
		VALUES ('40000000-0000-0000-0000-000000000013', '30000000-0000-0000-0000-000000000001', 2, 'wwtiot_cloud_api', 'completed', 'wwtiot', '100000013', 'provider_accepted', 'provider_accepted', 'unverified', '50000000-0000-0000-0000-000000000013', 'worker', now() + interval '1 minute', now(), now() - interval '2 seconds', now() - interval '1 second')
	`, "Attempt timestamps out of order")
	assertSQLFails(t, db, `
		INSERT INTO device_command_attempts (id, command_id, attempt_no, adapter, phase, provider_code, provider_request_key, outcome, lease_token, lease_owner, lease_expires_at, dispatching_at, completed_at)
		VALUES ('40000000-0000-0000-0000-000000000014', '30000000-0000-0000-0000-000000000001', 2, 'wwtiot_cloud_api', 'completed', 'wwtiot', '100000014', 'not_dispatched', '50000000-0000-0000-0000-000000000014', 'worker', now() + interval '1 minute', now(), now())
	`, "not-dispatched Attempt with dispatching timestamp")
	if _, err := db.Exec(`
		INSERT INTO device_events (
			id, project_id, device_id, event_type, source, payload, occurred_at, schema_version, deduplication_key
		) VALUES (
			'60000000-0000-0000-0000-000000000001', $1, $2, 'device.created', 'admin',
			'{"device_type_code":"smart-lock","provider_code":"wwtiot","lifecycle_status":"active"}', now(), 2, 'device-created'
		)
	`, project1, device1); err == nil {
		t.Fatal("event schema version other than 1 must fail")
	}
	if _, err := db.Exec(`
		INSERT INTO device_command_attempts (
			id, command_id, attempt_no, adapter, phase, provider_code, provider_request_key,
			outcome, confirmation_level, evidence_status, lease_token, lease_owner, lease_expires_at,
			claimed_at, dispatching_at, completed_at
		) VALUES (
			'40000000-0000-0000-0000-000000000020', '30000000-0000-0000-0000-000000000001', 2,
			'wwtiot_cloud_api', 'completed', 'wwtiot', '100000020',
			'provider_accepted', 'provider_accepted', 'unverified',
			'50000000-0000-0000-0000-000000000020', 'worker', now() + interval '1 minute',
			now() - interval '2 seconds', now() - interval '1 second', now()
		);
		UPDATE device_commands
		SET status = 'sent', sent_at = now() - interval '1 second', result_deadline_at = now() + interval '1 minute',
			confirmation_level = 'provider_accepted', evidence_status = 'unverified'
		WHERE id = '30000000-0000-0000-0000-000000000001';
	`); err != nil {
		t.Fatalf("seed completed evidence Attempt: %v", err)
	}
	if _, err := db.Exec(`
		INSERT INTO device_events (
			id, project_id, device_id, command_id, event_type, source, payload, occurred_at, deduplication_key
		) VALUES (
			'60000000-0000-0000-0000-000000000002', $1, $2, '30000000-0000-0000-0000-000000000001',
			'command.evidence_updated', 'system',
			'{"status":"sent","attempt_id":"40000000-0000-0000-0000-000000000020","outcome":"provider_accepted","confirmation_level":"provider_accepted","evidence_status":"unverified"}',
			now(), 'command.evidence_updated:30000000-0000-0000-0000-000000000001:attempt:40000000-0000-0000-0000-000000000020:provider_accepted:provider_accepted:unverified'
		)
	`, project1, device1); err != nil {
		t.Fatalf("valid command.evidence_updated Event: %v", err)
	}
	assertSQLFails(t, db, `
		INSERT INTO device_events (id, project_id, device_id, command_id, event_type, source, payload, occurred_at, deduplication_key)
		VALUES (
			'60000000-0000-0000-0000-000000000003', '`+project1+`', '`+device1+`', '30000000-0000-0000-0000-000000000001',
			'command.evidence_updated', 'system',
			'{"status":"sent","attempt_id":"40000000-0000-0000-0000-000000000020","outcome":"provider_accepted","confirmation_level":"provider_accepted"}',
			now(), 'command-evidence-missing-field'
		)
	`, "command.evidence_updated missing payload field")
	assertSQLFails(t, db, `
		INSERT INTO device_events (id, project_id, device_id, event_type, source, payload, occurred_at, deduplication_key)
		VALUES (
			'60000000-0000-0000-0000-000000000004', '`+project1+`', '`+device1+`',
			'command.evidence_updated', 'system',
			'{"status":"sent","attempt_id":"40000000-0000-0000-0000-000000000020","outcome":"provider_accepted","confirmation_level":"provider_accepted","evidence_status":"unverified"}',
			now(), 'command-evidence-missing-command'
		)
	`, "command.evidence_updated missing Command association")
	assertSQLFails(t, db, `
		INSERT INTO device_events (id, project_id, device_id, command_id, event_type, source, payload, occurred_at, deduplication_key)
		VALUES (
			'60000000-0000-0000-0000-000000000006', '`+project1+`', '`+device1+`', '30000000-0000-0000-0000-000000000001',
			'command.evidence_updated', 'system',
			'{"status":"sent","attempt_id":"40000000-0000-0000-0000-000000000001","outcome":"provider_accepted","confirmation_level":"provider_accepted","evidence_status":"unverified"}',
			now(), 'command.evidence_updated:30000000-0000-0000-0000-000000000001:attempt:40000000-0000-0000-0000-000000000001:provider_accepted:provider_accepted:unverified'
		)
	`, "command.evidence_updated mismatched completed Attempt")
	assertSQLFails(t, db, `
		INSERT INTO device_events (id, project_id, device_id, command_id, event_type, source, payload, occurred_at, deduplication_key)
		VALUES (
			'60000000-0000-0000-0000-000000000005', '`+project1+`', '`+device1+`', '30000000-0000-0000-0000-000000000001',
			'command.status_changed', 'system',
			'{"from":"sent","to":"sent","reason_code":null,"confirmation_level":"provider_accepted","evidence_status":"unverified"}',
			now(), 'same-state-status-event'
		)
	`, "same-state command.status_changed Event")

	if _, err := db.Exec(`
		INSERT INTO device_raw_messages (id, device_id, provider_code, provider_device_id, access_type, transport_protocol, adapter, direction, deduplication_key, body)
		VALUES ('61000000-0000-0000-0000-000000000001', $1, 'wwtiot', 'LOCK-002', 'cloud_api', 'http', 'wwtiot_cloud_api', 'inbound', 'raw-1', 'x')
	`, device2); err != nil {
		t.Fatal(err)
	}
	assertSQLFails(t, db, `
		INSERT INTO device_raw_messages (id, device_id, provider_code, provider_device_id, access_type, transport_protocol, adapter, direction, deduplication_key, body)
		VALUES ('61000000-0000-0000-0000-000000000002', '`+device2+`', 'wwtiot', 'LOCK-002', 'cloud_api', 'http', 'wwtiot_cloud_api', 'inbound', '', 'x')
	`, "empty RawMessage deduplication key")
	assertSQLFails(t, db, `
		INSERT INTO device_states (id, device_id, state, raw_message_id)
		VALUES ('62000000-0000-0000-0000-000000000001', '`+device1+`', '{}', '61000000-0000-0000-0000-000000000001')
	`, "DeviceState RawMessage ownership")

	for _, event := range []struct {
		id        string
		projectID string
		deviceID  string
		key       string
	}{
		{id: "60000000-0000-0000-0000-000000000011", projectID: project1, deviceID: device1, key: "event-1"},
		{id: "60000000-0000-0000-0000-000000000012", projectID: project2, deviceID: device2, key: "event-2"},
	} {
		if _, err := db.Exec(`
			INSERT INTO device_events (id, project_id, device_id, event_type, source, payload, occurred_at, deduplication_key)
			VALUES ($1, $2, $3, 'device.created', 'admin', '{"device_type_code":"smart-lock","provider_code":"wwtiot","lifecycle_status":"active"}', now(), $4)
		`, event.id, event.projectID, event.deviceID, event.key); err != nil {
			t.Fatal(err)
		}
	}
	assertSQLFails(t, db, `
		INSERT INTO device_events (id, project_id, device_id, event_type, source, payload, raw_message_id, occurred_at, deduplication_key)
		VALUES ('60000000-0000-0000-0000-000000000013', '`+project1+`', '`+device1+`', 'device.state_updated', 'provider_callback', '{"state":{},"observed_at":"2026-07-31T00:00:00Z","evidence_status":"verified"}', '61000000-0000-0000-0000-000000000001', now(), 'event-raw-mismatch')
	`, "Event RawMessage ownership")

	for index, projectID := range []string{project1, project2} {
		secretByte := "55"
		if index == 1 {
			secretByte = "66"
		}
		if _, err := db.Exec(`
			INSERT INTO project_webhook_secrets (project_id, version, ciphertext, nonce, encryption_key_version)
			VALUES ($1, 1, decode(repeat($2, 17), 'hex'), decode(repeat($2, 12), 'hex'), 1)
		`, projectID, secretByte); err != nil {
			t.Fatal(err)
		}
		if _, err := db.Exec(`UPDATE projects SET webhook_url = 'https://example.test/hook', webhook_config_version = 1, current_webhook_secret_version = 1 WHERE id = $1`, projectID); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := db.Exec(`
		INSERT INTO webhook_deliveries (id, project_id, event_id, target_url, webhook_config_version, webhook_secret_version, raw_body, next_attempt_at)
		VALUES ('63000000-0000-0000-0000-000000000001', $1, '60000000-0000-0000-0000-000000000011', 'https://example.test/hook', 1, 1, 'body', now())
	`, project1); err != nil {
		t.Fatal(err)
	}
	assertSQLFails(t, db, `
		INSERT INTO webhook_deliveries (id, project_id, event_id, target_url, webhook_config_version, webhook_secret_version, raw_body, next_attempt_at)
		VALUES ('63000000-0000-0000-0000-000000000002', '`+project2+`', '60000000-0000-0000-0000-000000000011', 'https://example.test/hook', 1, 1, 'body', now())
	`, "Webhook Event Project ownership")
	assertSQLFails(t, db, `
		INSERT INTO webhook_deliveries (id, project_id, event_id, target_url, webhook_config_version, webhook_secret_version, raw_body, next_attempt_at, replay_of_delivery_id)
		VALUES ('63000000-0000-0000-0000-000000000003', '`+project2+`', '60000000-0000-0000-0000-000000000012', 'https://example.test/hook', 1, 1, 'body', now(), '63000000-0000-0000-0000-000000000001')
	`, "Webhook replay Project ownership")
	assertSQLFails(t, db, `
		INSERT INTO webhook_deliveries (id, project_id, event_id, target_url, webhook_config_version, webhook_secret_version, raw_body, attempt_count, status)
		VALUES ('63000000-0000-0000-0000-000000000004', '`+project2+`', '60000000-0000-0000-0000-000000000012', 'https://example.test/hook', 1, 1, 'body', 1, 'sending')
	`, "sending Webhook without lease")
}

func assertSQLFails(t *testing.T, db *sql.DB, statement, description string) {
	t.Helper()
	if _, err := db.Exec(statement); err == nil {
		t.Fatalf("%s must fail", description)
	}
}

const legacyBaseFixture = `
	INSERT INTO projects (id, name, api_key_hash)
	VALUES ('10000000-0000-0000-0000-000000000001', 'one', repeat('a', 64));
	INSERT INTO device_types (id, code, name)
	VALUES ('00000000-0000-0000-0000-000000000001', 'smart_lock', 'Smart Lock');
`

func applyOnlyFirstMigration(t *testing.T, db *sql.DB) {
	t.Helper()
	ctx := context.Background()
	conn, err := db.Conn(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if err := ensureMigrationTable(ctx, conn); err != nil {
		conn.Close()
		t.Fatal(err)
	}
	if err := applyMigration(ctx, conn, "001_device_platform_core.up.sql", "001_device_platform_core"); err != nil {
		conn.Close()
		t.Fatal(err)
	}
	if err := conn.Close(); err != nil {
		t.Fatal(err)
	}
}

func assertMigrationNotApplied(t *testing.T, db *sql.DB, version string) {
	t.Helper()
	var applied bool
	if err := db.QueryRow(`SELECT EXISTS (SELECT 1 FROM schema_migrations WHERE version = $1)`, version).Scan(&applied); err != nil {
		t.Fatal(err)
	}
	if applied {
		t.Fatalf("failed migration %s must not record version", version)
	}
}

func requireMigrationTestDatabase(t *testing.T) string {
	t.Helper()
	raw := strings.TrimSpace(os.Getenv("MIGRATION_TEST_DATABASE_URL"))
	if raw == "" {
		t.Skip("MIGRATION_TEST_DATABASE_URL is not set")
	}
	parsed, err := url.Parse(raw)
	if err != nil {
		t.Fatal(err)
	}
	databaseName := strings.TrimPrefix(parsed.Path, "/")
	if !strings.HasSuffix(databaseName, "_test") {
		t.Fatalf("refusing migration integration test against database %q", databaseName)
	}
	return raw
}

func withIsolatedSchema(t *testing.T, baseURL string, fn func(*sql.DB)) {
	t.Helper()
	ctx := context.Background()
	admin, err := sql.Open("postgres", baseURL)
	if err != nil {
		t.Fatal(err)
	}
	defer admin.Close()
	schema := fmt.Sprintf("contract_test_%d", time.Now().UnixNano())
	if _, err := admin.ExecContext(ctx, `CREATE SCHEMA `+schema); err != nil {
		t.Fatal(err)
	}
	defer func() {
		if _, err := admin.ExecContext(context.Background(), `DROP SCHEMA `+schema+` CASCADE`); err != nil {
			t.Errorf("drop test schema: %v", err)
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
	db.SetMaxOpenConns(1)
	defer db.Close()
	if err := db.PingContext(ctx); err != nil {
		t.Fatal(err)
	}
	fn(db)
}
