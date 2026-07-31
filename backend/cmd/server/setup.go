package main

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/mail"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/lib/pq"
	"github.com/qiyue2015/device-platform/internal/storage"
	"github.com/redis/go-redis/v9"
)

const installLockFile = ".installed"

var installMu sync.Mutex

var errAdminCreationConflict = errors.New("admin creation conflict")

type setupStatus struct {
	NeedsSetup bool   `json:"needs_setup"`
	Installed  bool   `json:"installed"`
	Step       string `json:"step"`
}

type setupInstallRequest struct {
	Database databaseSetupRequest `json:"database"`
	Redis    redisSetupRequest    `json:"redis"`
	Admin    adminSetupRequest    `json:"admin"`
	Server   serverSetupRequest   `json:"server"`
}

type databaseSetupRequest struct {
	URL string `json:"url"`
}

type redisSetupRequest struct {
	URL string `json:"url"`
}

type adminSetupRequest struct {
	Email           string `json:"email"`
	DisplayName     string `json:"display_name"`
	Password        string `json:"password"`
	ConfirmPassword string `json:"confirm_password"`
}

type serverSetupRequest struct {
	Addr     string `json:"addr"`
	LogLevel string `json:"log_level"`
}

func installLockPath() string {
	if path := strings.TrimSpace(os.Getenv("INSTALL_LOCK_PATH")); path != "" {
		return absoluteInstallPath(path)
	}
	targetDir := "."
	if info, err := os.Stat("backend"); err == nil && info.IsDir() {
		targetDir = "backend"
	}
	return absoluteInstallPath(filepath.Join(targetDir, installLockFile))
}

func absoluteInstallPath(path string) string {
	absolute, err := filepath.Abs(path)
	if err != nil {
		return filepath.Clean(path)
	}
	return filepath.Clean(absolute)
}

func installLockExists() bool {
	_, err := os.Stat(installLockPath())
	return err == nil
}

func getSetupStatus() setupStatus {
	installed := installLockExists()
	return setupStatus{
		NeedsSetup: !installed,
		Installed:  installed,
		Step:       "system",
	}
}

func ensureSetupAllowed() error {
	if !getSetupStatus().NeedsSetup {
		return newAPIError(http.StatusConflict, "setup_completed", "system is already installed")
	}
	return nil
}

func validateInstallTargetWritable() error {
	if err := validateFileTargetWritable(runtimeEnvPath()); err != nil {
		return fmt.Errorf("runtime config target is not writable: %w", err)
	}
	if err := validateFileTargetWritable(installLockPath()); err != nil {
		return fmt.Errorf("install lock target is not writable: %w", err)
	}
	if err := validateFileTargetWritable(installFileLockPath()); err != nil {
		return fmt.Errorf("cross-process lock target is not writable: %w", err)
	}
	if err := validateFileTargetWritable(installJournalPath()); err != nil {
		return fmt.Errorf("recovery journal target is not writable: %w", err)
	}
	return nil
}

func validateFileTargetWritable(path string) error {
	dir := filepath.Dir(path)
	if dir == "." {
		dir = "."
	}
	info, err := os.Stat(dir)
	if err != nil {
		return fmt.Errorf("directory is not accessible: %w", err)
	}
	if !info.IsDir() {
		return fmt.Errorf("parent is not a directory")
	}
	probe, err := os.CreateTemp(dir, ".device-platform-write-test-*")
	if err != nil {
		return fmt.Errorf("directory is not writable: %w", err)
	}
	probePath := probe.Name()
	if err := probe.Close(); err != nil {
		_ = os.Remove(probePath)
		return fmt.Errorf("close write probe: %w", err)
	}
	if err := os.Remove(probePath); err != nil {
		return fmt.Errorf("remove write probe: %w", err)
	}
	return nil
}

func normalizeInstallRequest(req setupInstallRequest) setupInstallRequest {
	req.Database.URL = strings.TrimSpace(req.Database.URL)
	req.Redis.URL = strings.TrimSpace(req.Redis.URL)
	req.Admin.Email = strings.ToLower(strings.TrimSpace(req.Admin.Email))
	req.Admin.DisplayName = strings.TrimSpace(req.Admin.DisplayName)
	req.Server.Addr = strings.TrimSpace(req.Server.Addr)
	req.Server.LogLevel = strings.TrimSpace(req.Server.LogLevel)
	return req
}

func validateInstallRequest(req setupInstallRequest) error {
	if err := validateDatabaseURL(req.Database.URL); err != nil {
		return err
	}
	if _, err := redisOptionsFromURL(req.Redis.URL); err != nil {
		return fmt.Errorf("invalid redis url: %w", err)
	}
	if _, err := mail.ParseAddress(req.Admin.Email); err != nil || len(req.Admin.Email) > 254 {
		return fmt.Errorf("invalid admin email")
	}
	if len(req.Admin.DisplayName) < 2 || len(req.Admin.DisplayName) > 80 {
		return fmt.Errorf("admin display name must be 2-80 characters")
	}
	if err := validateAdminPassword(req.Admin.Password, req.Admin.ConfirmPassword); err != nil {
		return err
	}
	if err := validateServerAddr(req.Server.Addr); err != nil {
		return err
	}
	if req.Server.LogLevel != "debug" && req.Server.LogLevel != "info" && req.Server.LogLevel != "warn" && req.Server.LogLevel != "error" {
		return fmt.Errorf("invalid log level")
	}
	return nil
}

func validateDatabaseURL(raw string) error {
	parsed, err := url.Parse(raw)
	if err != nil || parsed.Scheme == "" || parsed.Host == "" {
		return fmt.Errorf("invalid database url")
	}
	if parsed.Scheme != "postgres" && parsed.Scheme != "postgresql" {
		return fmt.Errorf("database url must use postgres scheme")
	}
	return nil
}

func redisOptionsFromURL(raw string) (*redis.Options, error) {
	opts, err := redis.ParseURL(raw)
	if err != nil {
		return nil, err
	}
	if opts.Addr == "" {
		return nil, fmt.Errorf("missing redis address")
	}
	return opts, nil
}

func validateAdminPassword(password, confirm string) error {
	if password != confirm {
		return fmt.Errorf("admin password confirmation does not match")
	}
	if len(password) < 8 || len(password) > 128 {
		return fmt.Errorf("admin password must be 8-128 characters")
	}
	return nil
}

func validateServerAddr(addr string) error {
	if strings.HasPrefix(addr, ":") {
		value, err := strconv.Atoi(strings.TrimPrefix(addr, ":"))
		if err != nil || value <= 0 || value > 65535 {
			return fmt.Errorf("invalid server port")
		}
		return nil
	}
	host, port, err := net.SplitHostPort(addr)
	if err != nil || strings.TrimSpace(host) == "" {
		return fmt.Errorf("invalid server address")
	}
	value, err := strconv.Atoi(port)
	if err != nil || value <= 0 || value > 65535 {
		return fmt.Errorf("invalid server port")
	}
	return nil
}

func testDatabaseConnection(ctx context.Context, databaseURL string) error {
	if err := validateDatabaseURL(databaseURL); err != nil {
		return err
	}
	db, err := sql.Open("postgres", databaseURL)
	if err != nil {
		return fmt.Errorf("open database: %w", err)
	}
	defer db.Close()
	pingCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	if err := db.PingContext(pingCtx); err != nil {
		return fmt.Errorf("database ping failed: %w", err)
	}
	return nil
}

func testRedisConnection(ctx context.Context, redisURL string) error {
	opts, err := redisOptionsFromURL(redisURL)
	if err != nil {
		return err
	}
	client := redis.NewClient(opts)
	defer client.Close()
	pingCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	if err := client.Ping(pingCtx).Err(); err != nil {
		return fmt.Errorf("redis ping failed: %w", err)
	}
	return nil
}

type installResult struct {
	DatabaseURL                string
	RedisURL                   string
	JWTSecret                  string
	WebhookSecretEncryptionKey []byte
}

type installRuntimeFactory func(installResult, *sql.DB) (runtimeSnapshot, error)
type installSnapshotPublisher func(runtimeSnapshot) (*sql.DB, func())

type installOperations struct {
	cleanup         func(installJournal) error
	verifyFile      func(*processFileLock) error
	verifyDatabase  func(context.Context, *databaseInstallLock) error
	releaseFile     func(*processFileLock) error
	releaseDatabase func(context.Context, *databaseInstallLock) error
}

func defaultInstallOperations() installOperations {
	return installOperations{
		cleanup:    cleanupInstallArtifacts,
		verifyFile: func(lock *processFileLock) error { return lock.verify() },
		verifyDatabase: func(ctx context.Context, lock *databaseInstallLock) error {
			return lock.verify(ctx)
		},
		releaseFile: func(lock *processFileLock) error { return lock.release() },
		releaseDatabase: func(ctx context.Context, lock *databaseInstallLock) error {
			return lock.release(ctx)
		},
	}
}

var errInstallCommittedFailStop = errors.New("installation committed but process must stop for recovery")

type fileSnapshot struct {
	path   string
	exists bool
	data   []byte
}

func performInstall(ctx context.Context, req setupInstallRequest) (installResult, error) {
	return performInstallWithRuntime(ctx, req, nil, nil)
}

func performInstallWithRuntime(ctx context.Context, req setupInstallRequest, factory installRuntimeFactory, publishSnapshot installSnapshotPublisher) (result installResult, resultErr error) {
	return performInstallWithOperations(ctx, req, factory, publishSnapshot, defaultInstallOperations())
}

func performInstallWithOperations(ctx context.Context, req setupInstallRequest, factory installRuntimeFactory, publishSnapshot installSnapshotPublisher, operations installOperations) (result installResult, resultErr error) {
	installMu.Lock()
	defer installMu.Unlock()

	req = normalizeInstallRequest(req)
	if err := validateInstallRequest(req); err != nil {
		return installResult{}, newAPIError(http.StatusBadRequest, "invalid_install_request", "invalid installation request")
	}
	if err := validateInstallTargetWritable(); err != nil {
		return installResult{}, newAPIError(http.StatusInternalServerError, "install_target_not_writable", "installation target is not writable")
	}
	fileLock, err := acquireProcessFileLock(ctx)
	if err != nil {
		return installResult{}, newAPIError(http.StatusInternalServerError, "install_lock_failed", "installation lock could not be acquired")
	}
	markerCommitted := false
	fileLockHeld := true
	defer func() {
		if !fileLockHeld {
			return
		}
		if err := operations.releaseFile(fileLock); err != nil {
			if markerCommitted {
				resultErr = errors.Join(resultErr, errInstallCommittedFailStop)
			} else {
				resultErr = newAPIError(http.StatusInternalServerError, "install_lock_failed", "installation lock could not be released")
			}
		}
	}()
	if err := recoverInstallationLocked(ctx); err != nil {
		return installResult{}, newAPIError(http.StatusInternalServerError, "install_recovery_failed", "installation recovery failed")
	}
	if err := ensureSetupAllowed(); err != nil {
		return installResult{}, err
	}
	if err := testDatabaseConnection(ctx, req.Database.URL); err != nil {
		return installResult{}, newAPIError(http.StatusBadRequest, "database_unavailable", "database is unavailable")
	}
	if err := testRedisConnection(ctx, req.Redis.URL); err != nil {
		return installResult{}, newAPIError(http.StatusBadRequest, "redis_unavailable", "Redis is unavailable")
	}

	db, err := sql.Open("postgres", req.Database.URL)
	if err != nil {
		return installResult{}, newAPIError(http.StatusBadRequest, "database_unavailable", "database is unavailable")
	}
	dbOwnedByRuntime := false
	defer func() {
		if !dbOwnedByRuntime {
			_ = db.Close()
		}
	}()
	databaseLock, err := acquireDatabaseInstallLock(ctx, db)
	if err != nil {
		return installResult{}, newAPIError(http.StatusInternalServerError, "install_lock_failed", "database installation lock could not be acquired")
	}
	databaseLockHeld := true
	defer func() {
		if !databaseLockHeld {
			return
		}
		releaseCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
		defer cancel()
		if err := operations.releaseDatabase(releaseCtx, databaseLock); err != nil {
			if markerCommitted {
				resultErr = errors.Join(resultErr, errInstallCommittedFailStop)
			} else {
				resultErr = newAPIError(http.StatusInternalServerError, "install_lock_failed", "database installation lock could not be released")
			}
		}
	}()
	if err := storage.ApplyMigrations(ctx, db); err != nil {
		return installResult{}, newAPIError(http.StatusInternalServerError, "migration_failed", "database migration failed")
	}
	if err := storage.ValidateFrozenContracts(ctx, db); err != nil {
		return installResult{}, newAPIError(http.StatusInternalServerError, "migration_failed", "database contract validation failed")
	}
	if err := ensureUsersEmpty(ctx, db); err != nil {
		return installResult{}, newAPIError(http.StatusConflict, "admin_creation_failed", "administrator could not be created")
	}
	adminID, err := randomUUID()
	if err != nil {
		return installResult{}, newAPIError(http.StatusInternalServerError, "secret_generation_failed", "failed to generate administrator identifier")
	}
	installID, err := randomUUID()
	if err != nil {
		return installResult{}, newAPIError(http.StatusInternalServerError, "secret_generation_failed", "failed to generate installation identifier")
	}
	jwtSecret, err := randomHex(32)
	if err != nil {
		return installResult{}, newAPIError(http.StatusInternalServerError, "secret_generation_failed", "failed to generate JWT secret")
	}
	webhookEncryptionKey := make([]byte, 32)
	if _, err := rand.Read(webhookEncryptionKey); err != nil {
		return installResult{}, newAPIError(http.StatusInternalServerError, "secret_generation_failed", "failed to generate Webhook encryption key")
	}
	passwordHash, err := hashPassword(req.Admin.Password)
	if err != nil {
		return installResult{}, newAPIError(http.StatusInternalServerError, "secret_generation_failed", "failed to prepare administrator credentials")
	}
	result = installResult{
		DatabaseURL: req.Database.URL, RedisURL: req.Redis.URL, JWTSecret: jwtSecret,
		WebhookSecretEncryptionKey: webhookEncryptionKey,
	}
	var runtime runtimeSnapshot
	if factory != nil {
		runtime, err = factory(result, db)
		if err != nil {
			return installResult{}, newAPIError(http.StatusInternalServerError, "internal_error", "runtime services could not be prepared")
		}
	}
	envBefore, err := snapshotFile(runtimeEnvPath())
	if err != nil {
		return installResult{}, newAPIError(http.StatusInternalServerError, "config_write_failed", "runtime configuration could not be prepared")
	}
	journal, err := prepareInstallJournal(envBefore, installID, adminID)
	if err != nil {
		return installResult{}, newAPIError(http.StatusInternalServerError, "install_recovery_failed", "installation recovery state could not be prepared")
	}
	failBeforeMarker := func(cause error) (installResult, error) {
		recoveryCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 15*time.Second)
		defer cancel()
		if err := rollbackInstallBeforeMarker(recoveryCtx, db, &journal); err != nil {
			return installResult{}, newAPIError(http.StatusInternalServerError, "install_recovery_failed", "installation recovery failed")
		}
		return installResult{}, cause
	}
	if err := writeRuntimeEnvForInstall(req, jwtSecret, webhookEncryptionKey, installID); err != nil {
		return failBeforeMarker(newAPIError(http.StatusInternalServerError, "config_write_failed", "runtime configuration could not be written"))
	}
	if err := advanceInstallJournal(&journal, installPhaseAdminPending); err != nil {
		return failBeforeMarker(newAPIError(http.StatusInternalServerError, "install_recovery_failed", "installation recovery state could not be advanced"))
	}
	if err := createInitialAdmin(ctx, db, adminID, passwordHash, req.Admin); err != nil {
		if errors.Is(err, errAdminCreationConflict) {
			return failBeforeMarker(newAPIError(http.StatusConflict, "admin_creation_failed", "administrator could not be created"))
		}
		return failBeforeMarker(newAPIError(http.StatusInternalServerError, "internal_error", "internal server error"))
	}
	if err := operations.verifyFile(fileLock); err != nil {
		return failBeforeMarker(newAPIError(http.StatusInternalServerError, "install_lock_failed", "installation lock ownership was lost"))
	}
	if err := operations.verifyDatabase(ctx, databaseLock); err != nil {
		return failBeforeMarker(newAPIError(http.StatusInternalServerError, "install_lock_failed", "database installation lock ownership was lost"))
	}
	if err := createInstallLock(installID); err != nil {
		return failBeforeMarker(newAPIError(http.StatusInternalServerError, "install_lock_failed", "installation completion marker could not be written"))
	}
	markerCommitted = true
	if err := operations.cleanup(journal); err != nil {
		return installResult{}, errInstallCommittedFailStop
	}
	var previousDB *sql.DB
	var activateWorkers func()
	if publishSnapshot != nil {
		previousDB, activateWorkers = publishSnapshot(runtime)
		dbOwnedByRuntime = true
	}
	// The durable marker fixes the success response and the complete snapshot is
	// published under the file lock. Readiness and workers stay gated until both
	// external locks have been released successfully.
	releaseCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	if err := operations.releaseDatabase(releaseCtx, databaseLock); err != nil {
		cancel()
		databaseLockHeld = false
		return installResult{}, errInstallCommittedFailStop
	}
	cancel()
	databaseLockHeld = false
	if err := operations.releaseFile(fileLock); err != nil {
		fileLockHeld = false
		return installResult{}, errInstallCommittedFailStop
	}
	fileLockHeld = false
	if activateWorkers != nil {
		activateWorkers()
	}
	if previousDB != nil && previousDB != db {
		_ = previousDB.Close()
	}
	return result, nil
}

func createInitialAdmin(ctx context.Context, db *sql.DB, id, passwordHash string, req adminSetupRequest) error {
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	var totalUsers int
	if err := tx.QueryRowContext(ctx, `SELECT COUNT(1) FROM users`).Scan(&totalUsers); err != nil {
		return fmt.Errorf("count users: %w", err)
	}
	if totalUsers != 0 {
		return fmt.Errorf("%w: users table is not empty", errAdminCreationConflict)
	}
	_, err = tx.ExecContext(ctx, `
		INSERT INTO users (id, email, password_hash, display_name, is_admin)
		VALUES ($1, $2, $3, $4, true)
	`, id, req.Email, passwordHash, req.DisplayName)
	var pqErr *pq.Error
	if errors.As(err, &pqErr) && pqErr.Code == "23505" {
		return fmt.Errorf("%w: unique user conflict", errAdminCreationConflict)
	}
	if err != nil {
		return err
	}
	return tx.Commit()
}

func ensureUsersEmpty(ctx context.Context, db *sql.DB) error {
	var totalUsers int
	if err := db.QueryRowContext(ctx, `SELECT COUNT(1) FROM users`).Scan(&totalUsers); err != nil {
		return err
	}
	if totalUsers != 0 {
		return errAdminCreationConflict
	}
	return nil
}

func runtimeEnvPath() string {
	return filepath.Join(filepath.Dir(installLockPath()), ".env")
}

func writeRuntimeEnv(req setupInstallRequest, jwtSecret string, webhookEncryptionKey []byte) error {
	installID, err := randomUUID()
	if err != nil {
		return err
	}
	return writeRuntimeEnvForInstall(req, jwtSecret, webhookEncryptionKey, installID)
}

func writeRuntimeEnvForInstall(req setupInstallRequest, jwtSecret string, webhookEncryptionKey []byte, installID string) error {
	content := strings.Join([]string{
		"DATABASE_URL=" + shellQuote(req.Database.URL),
		"REDIS_URL=" + shellQuote(req.Redis.URL),
		"JWT_SECRET=" + shellQuote(jwtSecret),
		"WEBHOOK_SECRET_ENCRYPTION_KEY=" + shellQuote(base64.RawURLEncoding.EncodeToString(webhookEncryptionKey)),
		"DEVICE_PLATFORM_INSTALLED=true",
		"SERVER_ADDR=" + shellQuote(req.Server.Addr),
		"LOG_LEVEL=" + shellQuote(req.Server.LogLevel),
		"READ_HEADER_TIMEOUT=5s",
		"HEARTBEAT_TIMEOUT=90s",
		"WEBHOOK_WORKER_INTERVAL=2s",
		"WEBHOOK_REQUEST_TIMEOUT=10s",
		"WEBHOOK_LEASE_DURATION=15s",
		"WEBHOOK_MAX_ATTEMPTS=5",
		"WEBHOOK_RETRY_SCHEDULE=1s,5s,30s,2m",
		"WEBHOOK_EGRESS_ALLOWLIST=",
		"",
	}, "\n")
	return writeFileDurable(runtimeEnvPath(), []byte(content), 0o600, installID)
}

func snapshotFile(path string) (fileSnapshot, error) {
	snapshot := fileSnapshot{path: path}
	info, err := os.Stat(path)
	if err != nil {
		if os.IsNotExist(err) {
			return snapshot, nil
		}
		return snapshot, fmt.Errorf("stat %s: %w", path, err)
	}
	if info.IsDir() {
		return snapshot, fmt.Errorf("%s is a directory", path)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return snapshot, fmt.Errorf("read %s: %w", path, err)
	}
	snapshot.exists = true
	snapshot.data = data
	return snapshot, nil
}

func shellQuote(value string) string {
	if value == "" {
		return ""
	}
	if regexp.MustCompile(`^[A-Za-z0-9_./:@?=&%+\-,]+$`).MatchString(value) {
		return value
	}
	return "'" + strings.ReplaceAll(value, "'", "'\\''") + "'"
}

func createInstallLock(installID string) error {
	path := installLockPath()
	content := fmt.Sprintf("installed_at=%s\n", time.Now().UTC().Format(time.RFC3339))
	return writeFileDurable(path, []byte(content), 0o600, installID)
}

func randomHex(bytesLen int) (string, error) {
	bytes := make([]byte, bytesLen)
	if _, err := rand.Read(bytes); err != nil {
		return "", err
	}
	return hex.EncodeToString(bytes), nil
}

func randomUUID() (string, error) {
	bytes := make([]byte, 16)
	if _, err := rand.Read(bytes); err != nil {
		return "", err
	}
	bytes[6] = (bytes[6] & 0x0f) | 0x40
	bytes[8] = (bytes[8] & 0x3f) | 0x80
	return fmt.Sprintf("%08x-%04x-%04x-%04x-%012x",
		bytes[0:4],
		bytes[4:6],
		bytes[6:8],
		bytes[8:10],
		bytes[10:16],
	), nil
}

func isSetupError(err error) bool {
	var apiErr apiError
	return errors.As(err, &apiErr)
}
