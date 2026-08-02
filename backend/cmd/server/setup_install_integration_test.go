//go:build integration

package main

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

const setupInstallProcessHelperEnv = "DEVICE_PLATFORM_SETUP_INSTALL_PROCESS_HELPER"

func TestSetupInstallProcessHelper(t *testing.T) {
	if os.Getenv(setupInstallProcessHelperEnv) != "1" {
		return
	}
	gate := os.Getenv("DEVICE_PLATFORM_SETUP_INSTALL_GATE")
	deadline := time.Now().Add(15 * time.Second)
	for {
		if _, err := os.Stat(gate); err == nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("setup install process gate did not open")
		}
		time.Sleep(10 * time.Millisecond)
	}
	_, err := performInstall(context.Background(), setupInstallRequest{
		Database: databaseSetupRequest{URL: os.Getenv("DEVICE_PLATFORM_SETUP_INSTALL_DATABASE_URL")},
		Redis:    redisSetupRequest{URL: os.Getenv("DEVICE_PLATFORM_SETUP_INSTALL_REDIS_URL")},
		Admin: adminSetupRequest{
			Email: "admin@example.test", DisplayName: "Test Admin",
			Password: "StrongPass123!", ConfirmPassword: "StrongPass123!",
		},
		Server: serverSetupRequest{Addr: ":18080", LogLevel: "info"},
	})
	if err == nil {
		fmt.Println("SETUP_RESULT success")
		return
	}
	var apiErr apiError
	if errors.As(err, &apiErr) {
		fmt.Printf("SETUP_RESULT %d:%s\n", apiErr.status, apiErr.code)
		return
	}
	t.Fatalf("unexpected setup result: %v", err)
}

func TestConcurrentInstallProcessesHaveSingleWinner(t *testing.T) {
	withAuthTestDatabase(t, func(db *sql.DB) {
		dir := t.TempDir()
		markerPath := filepath.Join(dir, ".installed")
		outcomes := runSetupInstallProcesses(t, db, []string{markerPath, markerPath})
		if outcomes["success"] != 1 || outcomes["409:setup_completed"] != 1 {
			t.Fatalf("cross-process setup outcomes=%v", outcomes)
		}
		assertSingleSetupAdmin(t, db)
		marker, err := os.Stat(markerPath)
		if err != nil {
			t.Fatal(err)
		}
		lock, err := os.Stat(markerPath + ".lock")
		if err != nil {
			t.Fatal(err)
		}
		if os.SameFile(marker, lock) {
			t.Fatal("completion marker and cross-process lock share an inode")
		}
	})
}

func TestConcurrentInstallProcessesWithDifferentFilesShareDatabaseOwnership(t *testing.T) {
	withAuthTestDatabase(t, func(db *sql.DB) {
		root := t.TempDir()
		firstDir := filepath.Join(root, "first")
		secondDir := filepath.Join(root, "second")
		if err := os.MkdirAll(firstDir, 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.MkdirAll(secondDir, 0o700); err != nil {
			t.Fatal(err)
		}
		markers := []string{filepath.Join(firstDir, ".installed"), filepath.Join(secondDir, ".installed")}
		outcomes := runSetupInstallProcesses(t, db, markers)
		if outcomes["success"] != 1 || outcomes["409:admin_creation_failed"] != 1 {
			t.Fatalf("database-owned cross-process setup outcomes=%v", outcomes)
		}
		assertSingleSetupAdmin(t, db)
		markerCount := 0
		for _, markerPath := range markers {
			if _, err := os.Stat(markerPath); err == nil {
				markerCount++
			} else if !os.IsNotExist(err) {
				t.Fatal(err)
			}
		}
		if markerCount != 1 {
			t.Fatalf("database ownership wrote %d completion markers", markerCount)
		}
	})
}

type setupProcessResult struct {
	output string
	err    error
}

func runSetupInstallProcesses(t *testing.T, db *sql.DB, markerPaths []string) map[string]int {
	t.Helper()
	gatePath := filepath.Join(t.TempDir(), ".start")
	databaseURL := processTestDatabaseURL(t, db)
	redisURL := requireProcessRedisTestURL(t)
	results := make(chan setupProcessResult, len(markerPaths))
	var wait sync.WaitGroup
	for _, markerPath := range markerPaths {
		command := exec.Command(os.Args[0], "-test.run=^TestSetupInstallProcessHelper$", "-test.v=false")
		command.Dir = filepath.Dir(markerPath)
		command.Env = append(os.Environ(),
			setupInstallProcessHelperEnv+"=1",
			"DEVICE_PLATFORM_SETUP_INSTALL_GATE="+gatePath,
			"DEVICE_PLATFORM_SETUP_INSTALL_DATABASE_URL="+databaseURL,
			"DEVICE_PLATFORM_SETUP_INSTALL_REDIS_URL="+redisURL,
			"INSTALL_LOCK_PATH="+markerPath,
		)
		wait.Add(1)
		go func() {
			defer wait.Done()
			output, err := command.CombinedOutput()
			results <- setupProcessResult{output: string(output), err: err}
		}()
	}
	if err := os.WriteFile(gatePath, []byte("start\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	wait.Wait()
	close(results)
	outcomes := map[string]int{}
	for result := range results {
		if result.err != nil {
			t.Fatalf("setup process failed: %v\n%s", result.err, result.output)
		}
		outcomes[setupProcessOutcome(result.output)]++
	}
	return outcomes
}

func assertSingleSetupAdmin(t *testing.T, db *sql.DB) {
	t.Helper()
	var users, admins int
	if err := db.QueryRow(`SELECT count(*), count(*) FILTER (WHERE is_super_admin = true) FROM users`).Scan(&users, &admins); err != nil {
		t.Fatal(err)
	}
	if users != 1 || admins != 1 {
		t.Fatalf("cross-process setup users=%d admins=%d", users, admins)
	}
}

func TestAdminPendingCrashRecoveryDeletesOnlyJournalAdminAndRestoresConfig(t *testing.T) {
	withAuthTestDatabase(t, func(db *sql.DB) {
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
		current := []byte("DATABASE_URL=" + shellQuote(processTestDatabaseURL(t, db)) + "\n")
		if err := writeFileDurable(runtimeEnvPath(), current, 0o600, testInstallID); err != nil {
			t.Fatal(err)
		}
		if err := advanceInstallJournal(&journal, installPhaseAdminPending); err != nil {
			t.Fatal(err)
		}
		if _, err := db.Exec(`
			INSERT INTO users (id, email, password_hash, display_name, is_super_admin, status)
			VALUES ($1, 'admin@example.test', 'hash', 'Test Admin', true, 'active')
		`, testAdminID); err != nil {
			t.Fatal(err)
		}

		if err := recoverInstallation(context.Background()); err != nil {
			t.Fatal(err)
		}
		if err := recoverInstallation(context.Background()); err != nil {
			t.Fatalf("repeated admin_pending recovery failed: %v", err)
		}
		var users int
		if err := db.QueryRow(`SELECT count(*) FROM users`).Scan(&users); err != nil {
			t.Fatal(err)
		}
		if users != 0 {
			t.Fatalf("crash recovery left %d users", users)
		}
		restored, err := os.ReadFile(runtimeEnvPath())
		if err != nil || string(restored) != string(prior) {
			t.Fatalf("crash recovery config=%q err=%v", restored, err)
		}
	})
}

func TestAdminPendingRecoveryBeforeAdminInsertRestoresConfig(t *testing.T) {
	withAuthTestDatabase(t, func(db *sql.DB) {
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
		current := []byte("DATABASE_URL=" + shellQuote(processTestDatabaseURL(t, db)) + "\n")
		if err := writeFileDurable(runtimeEnvPath(), current, 0o600, testInstallID); err != nil {
			t.Fatal(err)
		}
		if err := advanceInstallJournal(&journal, installPhaseAdminPending); err != nil {
			t.Fatal(err)
		}

		if err := recoverInstallation(context.Background()); err != nil {
			t.Fatal(err)
		}
		var users int
		if err := db.QueryRow(`SELECT count(*) FROM users`).Scan(&users); err != nil {
			t.Fatal(err)
		}
		if users != 0 {
			t.Fatalf("pre-insert recovery left %d users", users)
		}
		restored, err := os.ReadFile(runtimeEnvPath())
		if err != nil || string(restored) != string(prior) {
			t.Fatalf("pre-insert recovery config=%q err=%v", restored, err)
		}
		if err := recoverInstallation(context.Background()); err != nil {
			t.Fatalf("repeated pre-insert recovery failed: %v", err)
		}
	})
}

func TestMarkerPresentCrashRecoveryNeverDeletesAdminOrRestoresConfig(t *testing.T) {
	withAuthTestDatabase(t, func(db *sql.DB) {
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
		current := []byte("DATABASE_URL=" + shellQuote(processTestDatabaseURL(t, db)) + "\n")
		if err := writeFileDurable(runtimeEnvPath(), current, 0o600, testInstallID); err != nil {
			t.Fatal(err)
		}
		if err := advanceInstallJournal(&journal, installPhaseAdminPending); err != nil {
			t.Fatal(err)
		}
		if _, err := db.Exec(`
			INSERT INTO users (id, email, password_hash, display_name, is_super_admin, status)
			VALUES ($1, 'admin@example.test', 'hash', 'Test Admin', true, 'active')
		`, testAdminID); err != nil {
			t.Fatal(err)
		}
		if err := createInstallLock(testInstallID); err != nil {
			t.Fatal(err)
		}

		if err := recoverInstallation(context.Background()); err != nil {
			t.Fatal(err)
		}
		var users int
		if err := db.QueryRow(`SELECT count(*) FROM users WHERE id = $1`, testAdminID).Scan(&users); err != nil {
			t.Fatal(err)
		}
		if users != 1 {
			t.Fatal("marker-present recovery deleted the administrator")
		}
		contents, err := os.ReadFile(runtimeEnvPath())
		if err != nil || string(contents) != string(current) {
			t.Fatalf("marker-present recovery restored config=%q err=%v", contents, err)
		}
	})
}

func TestInstallLockVerificationFailureCompensatesBeforeMarker(t *testing.T) {
	withAuthTestDatabase(t, func(db *sql.DB) {
		dir := useSetupDirectory(t)
		prior := []byte("LOG_LEVEL=warn\n")
		if err := os.WriteFile(runtimeEnvPath(), prior, 0o600); err != nil {
			t.Fatal(err)
		}
		operations := defaultInstallOperations()
		operations.verifyFile = func(*processFileLock) error {
			return errors.New("test file lock ownership loss")
		}
		_, err := performInstallWithOperations(context.Background(), validSetupInstallRequest(
			processTestDatabaseURL(t, db), requireProcessRedisTestURL(t),
		), nil, nil, operations)
		var apiErr apiError
		if !errors.As(err, &apiErr) || apiErr.status != 500 || apiErr.code != "install_lock_failed" {
			t.Fatalf("verification failure=%v, want 500 install_lock_failed", err)
		}
		assertSetupArtifactsRolledBack(t, db, dir, prior)
	})
}

func TestDatabaseInstallLockVerificationDoesNotReenterAndDetectsLoss(t *testing.T) {
	withAuthTestDatabase(t, func(db *sql.DB) {
		lock, err := acquireDatabaseInstallLock(context.Background(), db)
		if err != nil {
			t.Fatal(err)
		}
		defer func() {
			if lock.conn != nil {
				_ = lock.conn.Close()
			}
		}()

		if err := lock.verify(context.Background()); err != nil {
			t.Fatalf("first advisory lock verification failed: %v", err)
		}
		if err := lock.verify(context.Background()); err != nil {
			t.Fatalf("second advisory lock verification failed: %v", err)
		}
		var released bool
		if err := lock.conn.QueryRowContext(context.Background(), `SELECT pg_advisory_unlock($1)`, installAdvisoryLock).Scan(&released); err != nil {
			t.Fatal(err)
		}
		if !released {
			t.Fatal("original advisory lock was not held")
		}
		if err := lock.verify(context.Background()); err == nil {
			t.Fatal("advisory verification did not detect ownership loss")
		}
		if err := lock.conn.QueryRowContext(context.Background(), `SELECT pg_advisory_unlock($1)`, installAdvisoryLock).Scan(&released); err != nil {
			t.Fatal(err)
		}
		if released {
			t.Fatal("advisory verification changed the session lock count")
		}
	})
}

func TestPostMarkerLockReleaseFailuresKeepPublishedRuntimeUnready(t *testing.T) {
	for _, test := range []struct {
		name   string
		inject func(*installOperations)
	}{
		{
			name: "database",
			inject: func(operations *installOperations) {
				operations.releaseDatabase = func(ctx context.Context, lock *databaseInstallLock) error {
					return errors.Join(lock.release(ctx), errors.New("test database lock release failure"))
				}
			},
		},
		{
			name: "file",
			inject: func(operations *installOperations) {
				operations.releaseFile = func(lock *processFileLock) error {
					return errors.Join(lock.release(), errors.New("test file lock release failure"))
				}
			},
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			withAuthTestDatabase(t, func(db *sql.DB) {
				useSetupDirectory(t)
				logger := slog.New(slog.NewTextHandler(io.Discard, nil))
				application := newAppWithServices(config{}, logger, nil, nil, nil, nil, nil, newCloudProviderRegistry(config{}), nil, nil)
				defer application.close() //nolint:errcheck
				operations := defaultInstallOperations()
				test.inject(&operations)
				factory := func(_ installResult, runtimeDB *sql.DB) (runtimeSnapshot, error) {
					return runtimeSnapshot{cfg: config{Installed: true}, db: runtimeDB}, nil
				}

				_, err := performInstallWithOperations(context.Background(), validSetupInstallRequest(
					processTestDatabaseURL(t, db), requireProcessRedisTestURL(t),
				), factory, application.publishRuntimeSnapshot, operations)
				if !errors.Is(err, errInstallCommittedFailStop) {
					t.Fatalf("post-marker %s lock release failure=%v, want committed fail-stop", test.name, err)
				}
				if !installLockExists() || installRecoveryExists() {
					t.Fatalf("post-marker %s failure marker=%v recovery=%v", test.name, installLockExists(), installRecoveryExists())
				}
				if _, err := os.Stat(runtimeEnvPath()); err != nil {
					t.Fatalf("post-marker %s failure removed runtime config: %v", test.name, err)
				}
				assertSingleSetupAdmin(t, db)
				if application.commandCancel != nil || application.webhookCancel != nil {
					t.Fatalf("post-marker %s failure activated workers", test.name)
				}
				if err := application.runtimeUnavailableError(); err == nil {
					t.Fatalf("post-marker %s failure left runtime ready", test.name)
				}

				recorder := httptest.NewRecorder()
				application.respondCommittedInstallFailStop(recorder, err)
				if recorder.Code != http.StatusOK {
					t.Fatalf("post-marker %s failure response=%d, want 200", test.name, recorder.Code)
				}
				select {
				case <-application.fatalErrors():
				default:
					t.Fatalf("post-marker %s failure did not signal fatal shutdown", test.name)
				}
			})
		})
	}
}

func TestPostMarkerCleanupFailureFailStopsWithoutRollback(t *testing.T) {
	withAuthTestDatabase(t, func(db *sql.DB) {
		useSetupDirectory(t)
		operations := defaultInstallOperations()
		operations.cleanup = func(installJournal) error {
			return errors.New("test cleanup failure")
		}
		_, err := performInstallWithOperations(context.Background(), validSetupInstallRequest(
			processTestDatabaseURL(t, db), requireProcessRedisTestURL(t),
		), nil, nil, operations)
		if !errors.Is(err, errInstallCommittedFailStop) {
			t.Fatalf("post-marker cleanup failure=%v, want committed fail-stop", err)
		}
		if !installLockExists() {
			t.Fatal("post-marker failure removed completion marker")
		}
		if !installRecoveryExists() {
			t.Fatal("post-marker cleanup failure removed recovery journal")
		}
		assertSingleSetupAdmin(t, db)
	})
}

func validSetupInstallRequest(databaseURL, redisURL string) setupInstallRequest {
	return setupInstallRequest{
		Database: databaseSetupRequest{URL: databaseURL},
		Redis:    redisSetupRequest{URL: redisURL},
		Admin: adminSetupRequest{
			Email: "admin@example.test", DisplayName: "Test Admin",
			Password: "StrongPass123!", ConfirmPassword: "StrongPass123!",
		},
		Server: serverSetupRequest{Addr: ":18080", LogLevel: "info"},
	}
}

func assertSetupArtifactsRolledBack(t *testing.T, db *sql.DB, dir string, prior []byte) {
	t.Helper()
	var users int
	if err := db.QueryRow(`SELECT count(*) FROM users`).Scan(&users); err != nil {
		t.Fatal(err)
	}
	if users != 0 {
		t.Fatalf("pre-marker compensation left %d users", users)
	}
	restored, err := os.ReadFile(filepath.Join(dir, ".env"))
	if err != nil || string(restored) != string(prior) {
		t.Fatalf("pre-marker compensation config=%q err=%v", restored, err)
	}
	for _, path := range []string{installLockPath(), installJournalPath()} {
		if _, err := os.Stat(path); !os.IsNotExist(err) {
			t.Fatalf("pre-marker compensation left %s: %v", path, err)
		}
	}
}

func setupProcessOutcome(output string) string {
	for _, line := range strings.Split(output, "\n") {
		if value, ok := strings.CutPrefix(line, "SETUP_RESULT "); ok {
			return strings.TrimSpace(value)
		}
	}
	return "missing"
}
