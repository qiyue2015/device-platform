package storage

import (
	"strings"
	"testing"
)

func TestFrozenContractMigrationContainsSafetyConstraints(t *testing.T) {
	content, err := embeddedMigrations.ReadFile("migrations/002_frozen_contract_alignment.up.sql")
	if err != nil {
		t.Fatal(err)
	}
	sqlText := string(content)
	required := []string{
		"requires explicit encryption of existing webhook configuration",
		"cannot infer frozen lifecycle evidence from legacy runtime rows",
		"uq_devices_active_provider_identity",
		"fk_device_commands_device_project",
		"chk_device_commands_status_timing",
		"device_type_profiles",
		"migration_002_device_type_legacy",
		"uq_command_attempts_one_incomplete",
		"^[1-9][0-9]{0,8}$",
		"lease_token UUID NOT NULL",
		"chk_device_commands_success_evidence",
		"schema_version INTEGER NOT NULL DEFAULT 1",
		"webhook_config_version BIGINT NOT NULL",
		"webhook_delivery_attempts",
		"fk_webhook_deliveries_event_project",
		"fk_webhook_deliveries_replay_project",
		"auth_login_failure_events",
		"simulator_config",
	}
	for _, marker := range required {
		if !strings.Contains(sqlText, marker) {
			t.Errorf("migration is missing %q", marker)
		}
	}
	for _, forbidden := range []string{"online_only", "queue_until_expire", "replace_latest", "timeout_then_ack", "duplicate_ack"} {
		if strings.Contains(sqlText, forbidden) {
			t.Errorf("migration contains removed contract value %q", forbidden)
		}
	}
}

func TestMigrationPairsExist(t *testing.T) {
	up, err := migrationNames(".up.sql")
	if err != nil {
		t.Fatal(err)
	}
	down, err := migrationNames(".down.sql")
	if err != nil {
		t.Fatal(err)
	}
	if len(up) != len(down) {
		t.Fatalf("up migrations = %d, down migrations = %d", len(up), len(down))
	}
	for i := range up {
		if strings.TrimSuffix(up[i], ".up.sql") != strings.TrimSuffix(down[i], ".down.sql") {
			t.Fatalf("migration pair mismatch: %s / %s", up[i], down[i])
		}
	}
}

func TestTransportFailureTimingMigrationMatchesDispatcherContract(t *testing.T) {
	content, err := embeddedMigrations.ReadFile("migrations/003_command_transport_failure_timing.up.sql")
	if err != nil {
		t.Fatal(err)
	}
	sqlText := string(content)
	for _, marker := range []string{
		"reason_code = 'provider_not_configured' AND sent_at IS NULL",
		"reason_code IN ('provider_transport_error', 'provider_rejected', 'device_reported_failure') AND sent_at IS NOT NULL",
	} {
		if !strings.Contains(sqlText, marker) {
			t.Errorf("transport failure migration is missing %q", marker)
		}
	}
	downContent, err := embeddedMigrations.ReadFile("migrations/003_command_transport_failure_timing.down.sql")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(downContent), "cannot rollback transport failure timing while dispatched provider_transport_error Commands exist") {
		t.Error("transport failure timing rollback must fail closed when the old constraint cannot represent current data")
	}
}
