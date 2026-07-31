package main

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

const (
	testInstallID = "10000000-0000-4000-8000-000000000001"
	testAdminID   = "20000000-0000-4000-8000-000000000001"
)

func TestPreparedRecoveryRestoresPriorConfigAndRemovesArtifacts(t *testing.T) {
	dir := useSetupDirectory(t)
	prior := []byte("LOG_LEVEL=warn\nSAFE_VALUE=prior\n")
	if err := os.WriteFile(filepath.Join(dir, ".env"), prior, 0o600); err != nil {
		t.Fatal(err)
	}
	snapshot, err := snapshotFile(runtimeEnvPath())
	if err != nil {
		t.Fatal(err)
	}
	journal, err := prepareInstallJournal(snapshot, testInstallID, testAdminID)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(runtimeEnvPath(), []byte("DATABASE_URL=postgres://user:secret@db/device_platform_test\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	rawJournal, err := os.ReadFile(installJournalPath())
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(rawJournal), "SAFE_VALUE") || strings.Contains(string(rawJournal), "postgres://") || strings.Contains(string(rawJournal), "secret") {
		t.Fatalf("recovery journal contains configuration material: %s", rawJournal)
	}
	for _, path := range []string{installJournalPath(), journal.BackupPath} {
		info, err := os.Stat(path)
		if err != nil {
			t.Fatal(err)
		}
		if info.Mode().Perm() != 0o600 {
			t.Fatalf("%s mode=%o, want 600", path, info.Mode().Perm())
		}
	}

	if err := recoverInstallation(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := recoverInstallation(context.Background()); err != nil {
		t.Fatalf("repeated prepared recovery failed: %v", err)
	}
	restored, err := os.ReadFile(runtimeEnvPath())
	if err != nil {
		t.Fatal(err)
	}
	if string(restored) != string(prior) {
		t.Fatalf("restored runtime config=%q, want %q", restored, prior)
	}
	for _, path := range []string{installJournalPath(), journal.BackupPath} {
		if _, err := os.Stat(path); !os.IsNotExist(err) {
			t.Fatalf("recovery artifact remains at %s: %v", path, err)
		}
	}
}

func TestMarkerPresentRecoveryOnlyCleansArtifacts(t *testing.T) {
	useSetupDirectory(t)
	if err := os.WriteFile(runtimeEnvPath(), []byte("SAFE_VALUE=prior\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	snapshot, err := snapshotFile(runtimeEnvPath())
	if err != nil {
		t.Fatal(err)
	}
	journal, err := prepareInstallJournal(snapshot, testInstallID, testAdminID)
	if err != nil {
		t.Fatal(err)
	}
	current := []byte("SAFE_VALUE=installed\n")
	if err := os.WriteFile(runtimeEnvPath(), current, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := createInstallLock(testInstallID); err != nil {
		t.Fatal(err)
	}

	if err := recoverInstallation(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := recoverInstallation(context.Background()); err != nil {
		t.Fatalf("repeated marker-present recovery failed: %v", err)
	}
	contents, err := os.ReadFile(runtimeEnvPath())
	if err != nil {
		t.Fatal(err)
	}
	if string(contents) != string(current) {
		t.Fatalf("marker-present recovery restored prior config: %q", contents)
	}
	for _, path := range []string{installJournalPath(), journal.BackupPath} {
		if _, err := os.Stat(path); !os.IsNotExist(err) {
			t.Fatalf("marker-present recovery artifact remains at %s: %v", path, err)
		}
	}
}

func TestAdminRevertedRecoveryRestoresConfigWithoutDatabaseTarget(t *testing.T) {
	useSetupDirectory(t)
	prior := []byte("LOG_LEVEL=warn\n")
	if err := os.WriteFile(runtimeEnvPath(), prior, 0o600); err != nil {
		t.Fatal(err)
	}
	snapshot, err := snapshotFile(runtimeEnvPath())
	if err != nil {
		t.Fatal(err)
	}
	journal, err := prepareInstallJournal(snapshot, testInstallID, testAdminID)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(runtimeEnvPath(), []byte("DATABASE_URL=invalid\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := advanceInstallJournal(&journal, installPhaseAdminReverted); err != nil {
		t.Fatal(err)
	}

	if err := recoverInstallation(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := recoverInstallation(context.Background()); err != nil {
		t.Fatalf("repeated admin_reverted recovery failed: %v", err)
	}
	restored, err := os.ReadFile(runtimeEnvPath())
	if err != nil || string(restored) != string(prior) {
		t.Fatalf("admin_reverted recovery config=%q err=%v", restored, err)
	}
}

func TestConfigRevertedRecoveryOnlyCleansArtifacts(t *testing.T) {
	useSetupDirectory(t)
	if err := os.WriteFile(runtimeEnvPath(), []byte("LOG_LEVEL=prior\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	snapshot, err := snapshotFile(runtimeEnvPath())
	if err != nil {
		t.Fatal(err)
	}
	journal, err := prepareInstallJournal(snapshot, testInstallID, testAdminID)
	if err != nil {
		t.Fatal(err)
	}
	current := []byte("LOG_LEVEL=current\n")
	if err := os.WriteFile(runtimeEnvPath(), current, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := advanceInstallJournal(&journal, installPhaseConfigReverted); err != nil {
		t.Fatal(err)
	}

	if err := recoverInstallation(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := recoverInstallation(context.Background()); err != nil {
		t.Fatalf("repeated config_reverted recovery failed: %v", err)
	}
	contents, err := os.ReadFile(runtimeEnvPath())
	if err != nil || string(contents) != string(current) {
		t.Fatalf("config_reverted recovery changed config=%q err=%v", contents, err)
	}
	for _, path := range []string{installJournalPath(), journal.BackupPath} {
		if _, err := os.Stat(path); !os.IsNotExist(err) {
			t.Fatalf("config_reverted artifact remains at %s: %v", path, err)
		}
	}
}

func TestProcessFileLockWaitRespectsContext(t *testing.T) {
	useSetupDirectory(t)
	first, err := acquireProcessFileLock(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	defer first.release() //nolint:errcheck
	ctx, cancel := context.WithTimeout(context.Background(), 75*time.Millisecond)
	defer cancel()
	if _, err := acquireProcessFileLock(ctx); err == nil || !strings.Contains(err.Error(), "deadline exceeded") {
		t.Fatalf("contended file lock error=%v, want context deadline", err)
	}
}

func TestInstallationArtifactsShareStableAbsoluteTarget(t *testing.T) {
	targetDir := filepath.Join(t.TempDir(), "persistent-installation")
	if err := os.MkdirAll(targetDir, 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("INSTALL_LOCK_PATH", filepath.Join(targetDir, ".installed"))

	paths := []string{runtimeEnvPath(), installLockPath(), installFileLockPath(), installJournalPath()}
	for _, path := range paths {
		if !filepath.IsAbs(path) {
			t.Fatalf("installation path is not absolute: %s", path)
		}
		if filepath.Dir(path) != targetDir {
			t.Fatalf("installation path %s is outside target %s", path, targetDir)
		}
	}
	before := append([]string(nil), paths...)
	otherDir := t.TempDir()
	previous, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(otherDir); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(previous) })
	after := []string{runtimeEnvPath(), installLockPath(), installFileLockPath(), installJournalPath()}
	for index := range before {
		if before[index] != after[index] {
			t.Fatalf("installation path changed with cwd: %s -> %s", before[index], after[index])
		}
	}
}

func TestCommittedInstallFailStopRespondsInstalledAndSignalsFatal(t *testing.T) {
	useSetupDirectory(t)
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	application := newAppWithServices(config{}, logger, nil, nil, nil, nil, nil, newCloudProviderRegistry(config{}), nil, nil)
	if err := createInstallLock(testInstallID); err != nil {
		t.Fatal(err)
	}
	recorder := httptest.NewRecorder()
	application.respondCommittedInstallFailStop(recorder, errInstallCommittedFailStop)

	if recorder.Code != http.StatusOK {
		t.Fatalf("committed fail-stop status=%d, want 200", recorder.Code)
	}
	var envelope struct {
		Data struct {
			Installed bool `json:"installed"`
		} `json:"data"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &envelope); err != nil {
		t.Fatal(err)
	}
	if !envelope.Data.Installed {
		t.Fatal("committed fail-stop did not acknowledge durable installation")
	}
	if !application.recoveryNeeded {
		t.Fatal("committed fail-stop left process ready")
	}
	select {
	case err := <-application.fatalErrors():
		if !errors.Is(err, errInstallCommittedFailStop) {
			t.Fatalf("fatal error=%v", err)
		}
	default:
		t.Fatal("committed fail-stop did not request process exit")
	}
}

func TestReadinessDistinguishesSetupRecoveryAndRestart(t *testing.T) {
	markerPath := filepath.Join(t.TempDir(), ".installed")
	t.Setenv("INSTALL_LOCK_PATH", markerPath)
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	application, err := newApp(config{}, logger)
	if err != nil {
		t.Fatal(err)
	}
	defer application.close()
	server := application.routes()

	assertSetupReadinessError(t, server, "setup_required")
	if err := os.WriteFile(markerPath, []byte("installed\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	assertSetupReadinessError(t, server, "setup_restart_required")
	application.setRecoveryNeeded(true)
	assertSetupReadinessError(t, server, "setup_recovery_required")

	blocked := doRequest(t, server, http.MethodGet, "/v1/projects", "", nil)
	if envelope := assertEnvelope(t, blocked, http.StatusServiceUnavailable, false); envelope.ErrorCode != "setup_recovery_required" {
		t.Fatalf("business API recovery error=%q", envelope.ErrorCode)
	}
}

func assertSetupReadinessError(t *testing.T, handler http.Handler, code string) {
	t.Helper()
	response := doRequest(t, handler, http.MethodGet, "/readyz", "", nil)
	envelope := assertEnvelope(t, response, http.StatusServiceUnavailable, false)
	if envelope.ErrorCode != code {
		t.Fatalf("ready error_code=%q, want %q", envelope.ErrorCode, code)
	}
}

func useSetupDirectory(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	previous, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(previous) })
	t.Setenv("INSTALL_LOCK_PATH", filepath.Join(dir, ".installed"))
	return dir
}
