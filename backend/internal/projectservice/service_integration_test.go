//go:build integration

package projectservice_test

import (
	"bytes"
	"context"
	"crypto/sha256"
	"database/sql"
	"errors"
	"fmt"
	"net/url"
	"os"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	_ "github.com/lib/pq"
	"github.com/qiyue2015/device-platform/internal/access"
	"github.com/qiyue2015/device-platform/internal/projectservice"
	"github.com/qiyue2015/device-platform/internal/storage"
	"github.com/qiyue2015/device-platform/internal/storage/repository"
)

type fixedClock struct{ now time.Time }

func (clock fixedClock) Now() time.Time { return clock.now }

func TestProjectServicePostgresLifecycle(t *testing.T) {
	withProjectServiceDatabase(t, func(db *sql.DB) {
		ctx := context.Background()
		store := repository.NewPostgresStore(db)
		service, err := projectservice.New(store, projectservice.Config{
			EncryptionKeys:             map[int][]byte{7: bytes.Repeat([]byte{0x5a}, 32)},
			ActiveEncryptionKeyVersion: 7,
			Clock:                      fixedClock{now: time.Date(2026, 7, 31, 16, 0, 0, 0, time.UTC)},
		})
		if err != nil {
			t.Fatal(err)
		}
		scope := access.SuperAdmin(projectServiceAdminID)
		metadata := projectservice.RequestMetadata{
			ActorUserID: projectServiceAdminID,
			IPAddress:   "2001:db8::10",
			RequestID:   "10000000-0000-0000-0000-000000000001",
		}
		webhookURL := "https://hooks.example.test/device-events"
		created, err := service.Create(ctx, scope, projectservice.CreateRequest{
			Name: "  Project Alpha  ", ManagerUserID: projectServiceAdminID, WebhookURL: &webhookURL,
			IPWhitelist: []string{"192.0.2.42/24", "192.0.2.0/24", "2001:0db8::1"},
		}, metadata)
		if err != nil {
			t.Fatal(err)
		}
		if created.Project.Name != "Project Alpha" || created.APIKey == "" || created.WebhookSecret == "" || !created.Project.WebhookConfigured {
			t.Fatalf("created result = %+v", created)
		}
		if got := created.Project.IPWhitelist; len(got) != 2 || got[0] != "192.0.2.0/24" || got[1] != "2001:db8::1" {
			t.Fatalf("canonical whitelist = %#v", got)
		}
		assertSafeProjectShape(t)

		digest := sha256.Sum256([]byte(created.APIKey))
		var storedDigest, ciphertext, nonce []byte
		var keyVersion int
		if err := db.QueryRow(`
			SELECT p.api_key_hash, s.ciphertext, s.nonce, s.encryption_key_version
			FROM projects p
			JOIN project_webhook_secrets s ON s.project_id = p.id AND s.version = 1
			WHERE p.id = $1
		`, created.Project.ID).Scan(&storedDigest, &ciphertext, &nonce, &keyVersion); err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(storedDigest, digest[:]) || bytes.Contains(ciphertext, []byte(created.WebhookSecret)) || len(nonce) != 12 || keyVersion != 7 {
			t.Fatalf("credential persistence violated: digest=%x ciphertext=%x nonce=%d key_version=%d", storedDigest, ciphertext, len(nonce), keyVersion)
		}
		resolved, err := service.ResolveWebhookSecret(ctx, created.Project.ID, 1)
		if err != nil || resolved != created.WebhookSecret {
			t.Fatalf("resolved secret mismatch: secret=%q err=%v", resolved, err)
		}

		if _, err := service.AuthenticateAPIKey(ctx, created.APIKey, "192.0.2.9:443"); err != nil {
			t.Fatalf("allowed IPv4 authentication: %v", err)
		}
		if _, err := service.AuthenticateAPIKey(ctx, created.APIKey, "[2001:db8::1]:443"); err != nil {
			t.Fatalf("allowed IPv6 authentication: %v", err)
		}
		if _, err := service.AuthenticateAPIKey(ctx, created.APIKey, "198.51.100.9:443"); !errors.Is(err, projectservice.ErrSourceIPNotAllowed) {
			t.Fatalf("outside source error = %v", err)
		}

		second, err := service.Create(ctx, scope, projectservice.CreateRequest{Name: "Project Beta", ManagerUserID: projectServiceAdminID}, metadata)
		if err != nil {
			t.Fatal(err)
		}
		third, err := service.Create(ctx, scope, projectservice.CreateRequest{Name: "Project Alpha", ManagerUserID: projectServiceAdminID}, metadata)
		if err != nil {
			t.Fatal(err)
		}
		list, err := service.List(ctx, scope, projectservice.ListRequest{Name: stringPointer("Project Alpha"), Page: 1, PageSize: 1})
		if err != nil || list.Total != 2 || len(list.Items) != 1 || list.Items[0].Name != "Project Alpha" {
			t.Fatalf("exact filtered page = %+v err=%v", list, err)
		}
		emptyPage, err := service.List(ctx, scope, projectservice.ListRequest{Name: stringPointer("Project Alpha"), Page: 3, PageSize: 1})
		if err != nil || emptyPage.Total != 2 || len(emptyPage.Items) != 0 {
			t.Fatalf("empty filtered page = %+v err=%v", emptyPage, err)
		}
		_ = second
		_ = third

		disabled, err := service.Update(ctx, scope, created.Project.ID, projectservice.UpdateRequest{WebhookURLSet: true}, metadata)
		if err != nil || disabled.Project.WebhookConfigured || disabled.WebhookSecret != "" {
			t.Fatalf("disable result = %+v err=%v", disabled, err)
		}
		if _, err := service.RotateWebhookSecret(ctx, scope, created.Project.ID, metadata); !errors.Is(err, projectservice.ErrWebhookNotConfigured) {
			t.Fatalf("rotate disabled endpoint error = %v", err)
		}
		reenabled, err := service.Update(ctx, scope, created.Project.ID, projectservice.UpdateRequest{WebhookURLSet: true, WebhookURL: &webhookURL}, metadata)
		if err != nil || !reenabled.Project.WebhookConfigured || reenabled.WebhookSecret != "" {
			t.Fatalf("reenable result = %+v err=%v", reenabled, err)
		}
		if resolved, err := service.ResolveWebhookSecret(ctx, created.Project.ID, 1); err != nil || resolved != created.WebhookSecret {
			t.Fatalf("reenable did not retain secret: secret=%q err=%v", resolved, err)
		}

		rotatedAPI, err := service.RotateAPIKey(ctx, scope, created.Project.ID, metadata)
		if err != nil || rotatedAPI.APIKey == "" || rotatedAPI.APIKey == created.APIKey {
			t.Fatalf("rotated API key = %+v err=%v", rotatedAPI, err)
		}
		if _, err := service.AuthenticateAPIKey(ctx, created.APIKey, "192.0.2.9:443"); !errors.Is(err, projectservice.ErrAuthenticationFailed) {
			t.Fatalf("old API key remains valid: %v", err)
		}
		if _, err := service.AuthenticateAPIKey(ctx, rotatedAPI.APIKey, "192.0.2.9:443"); err != nil {
			t.Fatalf("new API key authentication: %v", err)
		}

		rotatedWebhook, err := service.RotateWebhookSecret(ctx, scope, created.Project.ID, metadata)
		if err != nil || rotatedWebhook.WebhookSecret == "" || rotatedWebhook.WebhookSecret == created.WebhookSecret {
			t.Fatalf("rotated Webhook secret = %+v err=%v", rotatedWebhook, err)
		}
		oldSecret, oldErr := service.ResolveWebhookSecret(ctx, created.Project.ID, 1)
		newSecret, newErr := service.ResolveWebhookSecret(ctx, created.Project.ID, 2)
		if oldErr != nil || newErr != nil || oldSecret != created.WebhookSecret || newSecret != rotatedWebhook.WebhookSecret {
			t.Fatalf("versioned secrets old=%q/%v new=%q/%v", oldSecret, oldErr, newSecret, newErr)
		}
		var retiredAt sql.NullTime
		if err := db.QueryRow(`SELECT retired_at FROM project_webhook_secrets WHERE project_id = $1 AND version = 1`, created.Project.ID).Scan(&retiredAt); err != nil || !retiredAt.Valid {
			t.Fatalf("old secret not retired: retired=%v err=%v", retiredAt, err)
		}

		var auditCount int
		if err := db.QueryRow(`SELECT count(*) FROM audit_logs WHERE project_id = $1`, created.Project.ID).Scan(&auditCount); err != nil || auditCount != 5 {
			t.Fatalf("Project audit count=%d err=%v", auditCount, err)
		}
		var leaked int
		if err := db.QueryRow(`
			SELECT count(*) FROM audit_logs
			WHERE project_id = $1 AND metadata::text LIKE '%' || $2 || '%'
		`, created.Project.ID, created.WebhookSecret).Scan(&leaked); err != nil || leaked != 0 {
			t.Fatalf("secret leaked into Audit metadata: count=%d err=%v", leaked, err)
		}

		if _, err := db.Exec(`ALTER TABLE audit_logs ADD CONSTRAINT reject_project_update_for_rollback_test CHECK (action <> 'project.updated') NOT VALID`); err != nil {
			t.Fatal(err)
		}
		changedName := "Must Roll Back"
		if _, err := service.Update(ctx, scope, created.Project.ID, projectservice.UpdateRequest{Name: &changedName}, metadata); err == nil {
			t.Fatal("update unexpectedly committed without its Audit")
		}
		afterRollback, err := service.Get(ctx, scope, created.Project.ID)
		if err != nil || afterRollback.Name != "Project Alpha" {
			t.Fatalf("Project write did not roll back with Audit: project=%+v err=%v", afterRollback, err)
		}
	})
}

func TestProjectServiceSerializesConcurrentWebhookRotations(t *testing.T) {
	withProjectServiceDatabase(t, func(db *sql.DB) {
		ctx := context.Background()
		service, err := projectservice.New(repository.NewPostgresStore(db), projectservice.Config{
			EncryptionKeys:             map[int][]byte{1: bytes.Repeat([]byte{0x2a}, 32)},
			ActiveEncryptionKeyVersion: 1,
		})
		if err != nil {
			t.Fatal(err)
		}
		scope := access.SuperAdmin(projectServiceAdminID)
		metadata := projectservice.RequestMetadata{
			ActorUserID: projectServiceAdminID,
			RequestID:   "10000000-0000-0000-0000-000000000002",
		}
		webhookURL := "https://hooks.example.test/concurrent"
		created, err := service.Create(ctx, scope, projectservice.CreateRequest{Name: "Concurrent", ManagerUserID: projectServiceAdminID, WebhookURL: &webhookURL}, metadata)
		if err != nil {
			t.Fatal(err)
		}

		const rotations = 4
		type rotationResult struct {
			secret string
			err    error
		}
		results := make(chan rotationResult, rotations)
		var wait sync.WaitGroup
		for index := 0; index < rotations; index++ {
			wait.Add(1)
			go func() {
				defer wait.Done()
				result, rotateErr := service.RotateWebhookSecret(ctx, scope, created.Project.ID, metadata)
				results <- rotationResult{secret: result.WebhookSecret, err: rotateErr}
			}()
		}
		wait.Wait()
		close(results)
		secrets := map[string]struct{}{created.WebhookSecret: {}}
		for result := range results {
			if result.err != nil || result.secret == "" {
				t.Fatalf("concurrent rotation secret=%q err=%v", result.secret, result.err)
			}
			secrets[result.secret] = struct{}{}
		}
		if len(secrets) != rotations+1 {
			t.Fatalf("concurrent rotations produced %d unique secrets, want %d", len(secrets), rotations+1)
		}
		var configVersion int64
		var secretVersion, storedSecrets, audits int
		if err := db.QueryRow(`
			SELECT webhook_config_version, current_webhook_secret_version,
				(SELECT count(*) FROM project_webhook_secrets WHERE project_id = projects.id),
				(SELECT count(*) FROM audit_logs WHERE project_id = projects.id)
			FROM projects WHERE id = $1
		`, created.Project.ID).Scan(&configVersion, &secretVersion, &storedSecrets, &audits); err != nil {
			t.Fatal(err)
		}
		if configVersion != rotations+1 || secretVersion != rotations+1 || storedSecrets != rotations+1 || audits != rotations+1 {
			t.Fatalf("serialized state config=%d secret=%d rows=%d audits=%d", configVersion, secretVersion, storedSecrets, audits)
		}
		for version := 1; version <= rotations+1; version++ {
			if _, err := service.ResolveWebhookSecret(ctx, created.Project.ID, version); err != nil {
				t.Fatalf("resolve retained version %d: %v", version, err)
			}
		}
	})
}

func TestProjectServiceRollsBackEveryWriteWhenAuditFails(t *testing.T) {
	withProjectServiceDatabase(t, func(db *sql.DB) {
		ctx := context.Background()
		service, err := projectservice.New(repository.NewPostgresStore(db), projectservice.Config{
			EncryptionKeys:             map[int][]byte{1: bytes.Repeat([]byte{0x3c}, 32)},
			ActiveEncryptionKeyVersion: 1,
		})
		if err != nil {
			t.Fatal(err)
		}
		scope := access.SuperAdmin(projectServiceAdminID)
		metadata := projectservice.RequestMetadata{
			ActorUserID: projectServiceAdminID,
			RequestID:   "10000000-0000-0000-0000-000000000003",
		}
		webhookURL := "https://hooks.example.test/rollback"

		addRejectedAuditAction(t, db, "project.created")
		failedCreate, err := service.Create(ctx, scope, projectservice.CreateRequest{Name: "Must Not Exist", ManagerUserID: projectServiceAdminID, WebhookURL: &webhookURL}, metadata)
		if err == nil {
			t.Fatal("Create unexpectedly committed without its Audit")
		}
		if failedCreate.APIKey != "" || failedCreate.WebhookSecret != "" || failedCreate.Project.ID != "" {
			t.Fatalf("failed Create exposed one-time credentials: %+v", failedCreate)
		}
		assertTableCount(t, db, "projects", 0)
		assertTableCount(t, db, "project_webhook_secrets", 0)
		assertTableCount(t, db, "audit_logs", 0)
		dropRejectedAuditAction(t, db)

		created, err := service.Create(ctx, scope, projectservice.CreateRequest{Name: "Rollback Baseline", ManagerUserID: projectServiceAdminID, WebhookURL: &webhookURL}, metadata)
		if err != nil {
			t.Fatal(err)
		}
		var originalDigest []byte
		if err := db.QueryRow(`SELECT api_key_hash FROM projects WHERE id = $1`, created.Project.ID).Scan(&originalDigest); err != nil {
			t.Fatal(err)
		}

		addRejectedAuditAction(t, db, "project.api_key_rotated")
		failedAPIRotation, err := service.RotateAPIKey(ctx, scope, created.Project.ID, metadata)
		if err == nil {
			t.Fatal("API key rotation unexpectedly committed without its Audit")
		}
		if failedAPIRotation.APIKey != "" || failedAPIRotation.Project.ID != "" {
			t.Fatalf("failed API key rotation exposed one-time credential: %+v", failedAPIRotation)
		}
		var digestAfterFailure []byte
		if err := db.QueryRow(`SELECT api_key_hash FROM projects WHERE id = $1`, created.Project.ID).Scan(&digestAfterFailure); err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(digestAfterFailure, originalDigest) {
			t.Fatal("API key digest changed despite Audit failure")
		}
		if _, err := service.AuthenticateAPIKey(ctx, created.APIKey, "127.0.0.1:443"); err != nil {
			t.Fatalf("old API key stopped working after rolled-back rotation: %v", err)
		}
		assertTableCount(t, db, "audit_logs", 1)
		dropRejectedAuditAction(t, db)

		addRejectedAuditAction(t, db, "project.webhook_secret_rotated")
		failedWebhookRotation, err := service.RotateWebhookSecret(ctx, scope, created.Project.ID, metadata)
		if err == nil {
			t.Fatal("Webhook secret rotation unexpectedly committed without its Audit")
		}
		if failedWebhookRotation.WebhookSecret != "" || failedWebhookRotation.Project.ID != "" {
			t.Fatalf("failed Webhook rotation exposed one-time credential: %+v", failedWebhookRotation)
		}
		var configVersion int64
		var currentSecretVersion int
		if err := db.QueryRow(`
			SELECT webhook_config_version, current_webhook_secret_version
			FROM projects WHERE id = $1
		`, created.Project.ID).Scan(&configVersion, &currentSecretVersion); err != nil {
			t.Fatal(err)
		}
		if configVersion != 1 || currentSecretVersion != 1 {
			t.Fatalf("Webhook configuration changed despite Audit failure: config=%d secret=%d", configVersion, currentSecretVersion)
		}
		var retiredAt sql.NullTime
		if err := db.QueryRow(`
			SELECT retired_at FROM project_webhook_secrets
			WHERE project_id = $1 AND version = 1
		`, created.Project.ID).Scan(&retiredAt); err != nil {
			t.Fatal(err)
		}
		if retiredAt.Valid {
			t.Fatal("current Webhook secret was retired despite Audit failure")
		}
		assertTableCount(t, db, "project_webhook_secrets", 1)
		assertTableCount(t, db, "audit_logs", 1)
		resolved, err := service.ResolveWebhookSecret(ctx, created.Project.ID, 1)
		if err != nil || resolved != created.WebhookSecret {
			t.Fatalf("original Webhook secret changed after rolled-back rotation: secret=%q err=%v", resolved, err)
		}
	})
}

func addRejectedAuditAction(t *testing.T, db *sql.DB, action string) {
	t.Helper()
	if _, err := db.Exec(`
		ALTER TABLE audit_logs
		ADD CONSTRAINT reject_project_action_for_rollback_test
		CHECK (action <> '` + action + `') NOT VALID
	`); err != nil {
		t.Fatal(err)
	}
}

func dropRejectedAuditAction(t *testing.T, db *sql.DB) {
	t.Helper()
	if _, err := db.Exec(`ALTER TABLE audit_logs DROP CONSTRAINT reject_project_action_for_rollback_test`); err != nil {
		t.Fatal(err)
	}
}

func assertTableCount(t *testing.T, db *sql.DB, table string, want int) {
	t.Helper()
	allowed := map[string]bool{"projects": true, "project_webhook_secrets": true, "audit_logs": true}
	if !allowed[table] {
		t.Fatalf("test attempted to count unexpected table %q", table)
	}
	var got int
	if err := db.QueryRow(`SELECT count(*) FROM ` + table).Scan(&got); err != nil {
		t.Fatal(err)
	}
	if got != want {
		t.Fatalf("%s count=%d, want %d", table, got, want)
	}
}

func assertSafeProjectShape(t *testing.T) {
	t.Helper()
	typeOfProject := reflect.TypeOf(projectservice.Project{})
	for index := 0; index < typeOfProject.NumField(); index++ {
		name := strings.ToLower(typeOfProject.Field(index).Name)
		if strings.Contains(name, "digest") || strings.Contains(name, "hash") || strings.Contains(name, "secret") || strings.Contains(name, "cipher") || strings.Contains(name, "nonce") {
			t.Fatalf("ordinary Project exposes credential field %q", typeOfProject.Field(index).Name)
		}
	}
}

func stringPointer(value string) *string { return &value }

const projectServiceAdminID = "70000000-0000-0000-0000-000000000001"

func withProjectServiceDatabase(t *testing.T, fn func(*sql.DB)) {
	t.Helper()
	baseURL := strings.TrimSpace(os.Getenv("MIGRATION_TEST_DATABASE_URL"))
	if baseURL == "" {
		t.Skip("MIGRATION_TEST_DATABASE_URL is not set")
	}
	parsedBase, err := url.Parse(baseURL)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasSuffix(strings.TrimPrefix(parsedBase.Path, "/"), "_test") {
		t.Fatalf("refusing Project service integration test against database %q", strings.TrimPrefix(parsedBase.Path, "/"))
	}
	admin, err := sql.Open("postgres", baseURL)
	if err != nil {
		t.Fatal(err)
	}
	defer admin.Close()
	schema := fmt.Sprintf("project_service_test_%d", time.Now().UnixNano())
	if _, err := admin.Exec(`CREATE SCHEMA ` + schema); err != nil {
		t.Fatal(err)
	}
	defer func() {
		if _, err := admin.Exec(`DROP SCHEMA ` + schema + ` CASCADE`); err != nil {
			t.Errorf("drop Project service test schema: %v", err)
		}
	}()
	query := parsedBase.Query()
	query.Set("search_path", schema)
	parsedBase.RawQuery = query.Encode()
	db, err := sql.Open("postgres", parsedBase.String())
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err := storage.ApplyMigrations(context.Background(), db); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`
		INSERT INTO users (id, email, password_hash, display_name, is_super_admin, status)
		VALUES ($1, 'admin@example.test', 'hash', 'Test Admin', true, 'active')
	`, projectServiceAdminID); err != nil {
		t.Fatal(err)
	}
	fn(db)
}
