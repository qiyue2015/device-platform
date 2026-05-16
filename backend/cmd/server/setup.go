package main

import (
	"context"
	"crypto/rand"
	"database/sql"
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

	_ "github.com/lib/pq"
	"github.com/qiyue2015/device-platform/internal/storage"
	"github.com/redis/go-redis/v9"
)

const installLockFile = ".installed"

var installMu sync.Mutex

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
		return path
	}
	return installLockFile
}

func installLockExists() bool {
	_, err := os.Stat(installLockPath())
	return err == nil
}

func getSetupStatus() setupStatus {
	installed := installLockExists() || envBool("DEVICE_PLATFORM_INSTALLED", false)
	return setupStatus{
		NeedsSetup: !installed,
		Installed:  installed,
		Step:       "system",
	}
}

func ensureSetupAllowed() error {
	if !getSetupStatus().NeedsSetup {
		return newAPIError(http.StatusForbidden, "setup_forbidden", "system is already installed")
	}
	return nil
}

func validateInstallTargetWritable() error {
	if err := validateFileTargetWritable(runtimeEnvPath(), ".device-platform-env-write-test"); err != nil {
		return fmt.Errorf("runtime config target is not writable: %w", err)
	}
	if err := validateFileTargetWritable(installLockPath(), ".device-platform-lock-write-test"); err != nil {
		return fmt.Errorf("install lock target is not writable: %w", err)
	}
	return nil
}

func validateFileTargetWritable(path, probeName string) error {
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
	probe := filepath.Join(dir, probeName)
	if err := os.WriteFile(probe, []byte("ok"), 0o600); err != nil {
		return fmt.Errorf("directory is not writable: %w", err)
	}
	_ = os.Remove(probe)
	return nil
}

func normalizeInstallRequest(req setupInstallRequest) setupInstallRequest {
	req.Database.URL = strings.TrimSpace(req.Database.URL)
	req.Redis.URL = strings.TrimSpace(req.Redis.URL)
	req.Admin.Email = strings.ToLower(strings.TrimSpace(req.Admin.Email))
	req.Admin.DisplayName = strings.TrimSpace(req.Admin.DisplayName)
	req.Server.Addr = strings.TrimSpace(req.Server.Addr)
	req.Server.LogLevel = strings.TrimSpace(req.Server.LogLevel)
	if req.Server.Addr == "" {
		req.Server.Addr = ":8080"
	}
	if req.Server.LogLevel == "" {
		req.Server.LogLevel = "info"
	}
	if req.Admin.DisplayName == "" {
		req.Admin.DisplayName = "Administrator"
	}
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
	DatabaseURL string
	RedisURL    string
	JWTSecret   string
}

type fileSnapshot struct {
	path   string
	exists bool
	data   []byte
	mode   os.FileMode
}

func performInstall(ctx context.Context, req setupInstallRequest) (installResult, error) {
	installMu.Lock()
	defer installMu.Unlock()

	if err := ensureSetupAllowed(); err != nil {
		return installResult{}, err
	}
	req = normalizeInstallRequest(req)
	if err := validateInstallRequest(req); err != nil {
		return installResult{}, newAPIError(http.StatusBadRequest, "invalid_install_request", err.Error())
	}
	if err := validateInstallTargetWritable(); err != nil {
		return installResult{}, newAPIError(http.StatusBadRequest, "install_target_not_writable", err.Error())
	}
	if err := testDatabaseConnection(ctx, req.Database.URL); err != nil {
		return installResult{}, newAPIError(http.StatusBadRequest, "database_unavailable", err.Error())
	}
	if err := testRedisConnection(ctx, req.Redis.URL); err != nil {
		return installResult{}, newAPIError(http.StatusBadRequest, "redis_unavailable", err.Error())
	}

	db, err := sql.Open("postgres", req.Database.URL)
	if err != nil {
		return installResult{}, newAPIError(http.StatusBadRequest, "database_unavailable", err.Error())
	}
	defer db.Close()
	if err := storage.ApplyMigrations(ctx, db); err != nil {
		return installResult{}, newAPIError(http.StatusBadRequest, "migration_failed", err.Error())
	}
	jwtSecret, err := randomHex(32)
	if err != nil {
		return installResult{}, newAPIError(http.StatusInternalServerError, "secret_generation_failed", "failed to generate JWT secret")
	}
	envBefore, err := snapshotFile(runtimeEnvPath())
	if err != nil {
		return installResult{}, newAPIError(http.StatusBadRequest, "config_write_failed", err.Error())
	}
	lockBefore, err := snapshotFile(installLockPath())
	if err != nil {
		return installResult{}, newAPIError(http.StatusBadRequest, "install_lock_failed", err.Error())
	}
	adminID, err := createInitialAdmin(ctx, db, req.Admin)
	if err != nil {
		return installResult{}, newAPIError(http.StatusBadRequest, "admin_creation_failed", err.Error())
	}
	completed := false
	defer func() {
		if completed {
			return
		}
		_ = deleteInitialAdmin(context.Background(), db, adminID, req.Admin.Email)
		_ = restoreFile(envBefore)
		_ = restoreFile(lockBefore)
	}()
	if err := writeRuntimeEnv(req, jwtSecret); err != nil {
		return installResult{}, newAPIError(http.StatusBadRequest, "config_write_failed", err.Error())
	}
	if err := createInstallLock(); err != nil {
		return installResult{}, newAPIError(http.StatusBadRequest, "install_lock_failed", err.Error())
	}
	completed = true
	return installResult{DatabaseURL: req.Database.URL, RedisURL: req.Redis.URL, JWTSecret: jwtSecret}, nil
}

func createInitialAdmin(ctx context.Context, db *sql.DB, req adminSetupRequest) (string, error) {
	var totalUsers int
	if err := db.QueryRowContext(ctx, `SELECT COUNT(1) FROM users`).Scan(&totalUsers); err != nil {
		return "", fmt.Errorf("count users: %w", err)
	}
	var adminUsers int
	if err := db.QueryRowContext(ctx, `SELECT COUNT(1) FROM users WHERE is_admin = true`).Scan(&adminUsers); err != nil {
		return "", fmt.Errorf("count admin users: %w", err)
	}
	if adminUsers > 0 {
		return "", fmt.Errorf("admin user already exists")
	}
	if totalUsers > 0 {
		return "", fmt.Errorf("users table is not empty but has no admin user")
	}
	hash, err := hashPassword(req.Password)
	if err != nil {
		return "", err
	}
	id, err := randomUUID()
	if err != nil {
		return "", err
	}
	_, err = db.ExecContext(ctx, `
		INSERT INTO users (id, email, password_hash, display_name, is_admin)
		VALUES ($1, $2, $3, $4, true)
	`, id, req.Email, hash, req.DisplayName)
	if err != nil {
		return "", err
	}
	return id, nil
}

func deleteInitialAdmin(ctx context.Context, db *sql.DB, id, email string) error {
	if id == "" {
		return nil
	}
	_, err := db.ExecContext(ctx, `
		DELETE FROM users
		WHERE id = $1 AND email = $2 AND is_admin = true
	`, id, email)
	return err
}

func runtimeEnvPath() string {
	path := ".env"
	if _, err := os.Stat("backend"); err == nil {
		path = filepath.Join("backend", ".env")
	}
	return path
}

func writeRuntimeEnv(req setupInstallRequest, jwtSecret string) error {
	path := runtimeEnvPath()
	content := strings.Join([]string{
		"DATABASE_URL=" + shellQuote(req.Database.URL),
		"REDIS_URL=" + shellQuote(req.Redis.URL),
		"JWT_SECRET=" + shellQuote(jwtSecret),
		"DEVICE_PLATFORM_INSTALLED=true",
		"SERVER_ADDR=" + shellQuote(req.Server.Addr),
		"LOG_LEVEL=" + shellQuote(req.Server.LogLevel),
		"READ_HEADER_TIMEOUT=5s",
		"HEARTBEAT_TIMEOUT=90s",
		"COMMAND_WORKER_INTERVAL=1s",
		"WEBHOOK_WORKER_INTERVAL=2s",
		"EXPIRY_CHECK_INTERVAL=30s",
		"",
	}, "\n")
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, []byte(content), 0o600); err != nil {
		return fmt.Errorf("write temp env: %w", err)
	}
	if err := os.Rename(tmp, path); err != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("replace env: %w", err)
	}
	return nil
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
	snapshot.mode = info.Mode().Perm()
	return snapshot, nil
}

func restoreFile(snapshot fileSnapshot) error {
	if snapshot.path == "" {
		return nil
	}
	if !snapshot.exists {
		if err := os.Remove(snapshot.path); err != nil && !os.IsNotExist(err) {
			return err
		}
		return nil
	}
	tmp := snapshot.path + ".restore"
	mode := snapshot.mode
	if mode == 0 {
		mode = 0o600
	}
	if err := os.WriteFile(tmp, snapshot.data, mode); err != nil {
		return err
	}
	if err := os.Rename(tmp, snapshot.path); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	return nil
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

func createInstallLock() error {
	path := installLockPath()
	content := fmt.Sprintf("installed_at=%s\n", time.Now().UTC().Format(time.RFC3339))
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, []byte(content), 0o600); err != nil {
		return err
	}
	if err := os.Rename(tmp, path); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	return nil
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
