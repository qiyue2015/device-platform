//go:build integration

package main

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/qiyue2015/device-platform/internal/storage"
)

func TestRuntimeAndInstallRejectDriftedFrozenProfile(t *testing.T) {
	baseURL := requireServerMigrationTestDatabase(t)
	admin, err := sql.Open("postgres", baseURL)
	if err != nil {
		t.Fatal(err)
	}
	defer admin.Close()
	schema := fmt.Sprintf("server_contract_test_%d", time.Now().UnixNano())
	if _, err := admin.Exec(`CREATE SCHEMA ` + schema); err != nil {
		t.Fatal(err)
	}
	defer func() {
		if _, err := admin.Exec(`DROP SCHEMA ` + schema + ` CASCADE`); err != nil {
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
	schemaURL := parsed.String()
	db, err := sql.Open("postgres", schemaURL)
	if err != nil {
		t.Fatal(err)
	}
	if err := storage.ApplyMigrations(context.Background(), db); err != nil {
		db.Close()
		t.Fatal(err)
	}
	if _, err := db.Exec(`
		ALTER TABLE device_type_profiles DISABLE TRIGGER trg_device_type_profiles_immutable;
		UPDATE device_type_profiles SET profile_hash = decode(repeat('00', 32), 'hex');
		ALTER TABLE device_type_profiles ENABLE TRIGGER trg_device_type_profiles_immutable;
	`); err != nil {
		db.Close()
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}

	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	_, err = newApp(config{
		DatabaseURL:       schemaURL,
		RedisURL:          "redis://127.0.0.1:1/0",
		JWTSecret:         testJWTSecret,
		Installed:         true,
		ReadHeaderTimeout: 5 * time.Second,
	}, logger)
	if err == nil || !strings.Contains(err.Error(), "database contract validation failed") || !strings.Contains(err.Error(), "profile hash drift") {
		t.Fatalf("newApp must fail closed on profile drift, got %v", err)
	}

	tempDir := t.TempDir()
	t.Setenv("INSTALL_LOCK_PATH", filepath.Join(tempDir, ".installed"))
	t.Setenv("DEVICE_PLATFORM_INSTALLED", "false")
	oldWorkingDirectory, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(tempDir); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(oldWorkingDirectory) })

	_, err = performInstall(context.Background(), setupInstallRequest{
		Database: databaseSetupRequest{URL: schemaURL},
		Redis:    redisSetupRequest{URL: "redis://127.0.0.1:6379/15"},
		Admin: adminSetupRequest{
			Email:           "admin@example.test",
			DisplayName:     "Test Admin",
			Password:        "StrongPass123!",
			ConfirmPassword: "StrongPass123!",
		},
		Server: serverSetupRequest{Addr: ":18080", LogLevel: "info"},
	})
	var installErr apiError
	if !errors.As(err, &installErr) || installErr.status != 500 || installErr.code != "migration_failed" || installErr.message != "database contract validation failed" {
		t.Fatalf("performInstall must fail closed on profile drift, got %v", err)
	}
	for _, path := range []string{filepath.Join(tempDir, ".env"), filepath.Join(tempDir, ".installed")} {
		if _, statErr := os.Stat(path); !os.IsNotExist(statErr) {
			t.Fatalf("failed install must not leave %s", path)
		}
	}
}

func requireServerMigrationTestDatabase(t *testing.T) string {
	t.Helper()
	raw := strings.TrimSpace(os.Getenv("MIGRATION_TEST_DATABASE_URL"))
	if raw == "" {
		t.Skip("MIGRATION_TEST_DATABASE_URL is not set")
	}
	parsed, err := url.Parse(raw)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasSuffix(strings.TrimPrefix(parsed.Path, "/"), "_test") {
		t.Fatalf("refusing integration test against database %q", parsed.Path)
	}
	return raw
}
