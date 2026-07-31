package main

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/qiyue2015/device-platform/internal/commandworker"
	"github.com/qiyue2015/device-platform/internal/devicecore"
	"github.com/qiyue2015/device-platform/internal/httpjson"
	"github.com/qiyue2015/device-platform/internal/webhookaudit"
	"github.com/qiyue2015/device-platform/internal/webhookworker"
)

const testJWTSecret = defaultMemoryJWTSecret

type errorAuthenticator struct {
	loginErr error
	parseErr error
}

type readTrap struct {
	reads int
}

type lifecycleCommandWorker struct {
	started chan struct{}
	stopped chan struct{}
}

type lifecycleWebhookWorker struct {
	started chan struct{}
	stopped chan struct{}
}

func (w *lifecycleCommandWorker) Run(ctx context.Context, _ commandworker.ErrorReporter) {
	close(w.started)
	<-ctx.Done()
	close(w.stopped)
}

func (w *lifecycleWebhookWorker) Run(ctx context.Context, _ webhookworker.ErrorReporter) {
	close(w.started)
	<-ctx.Done()
	close(w.stopped)
}

func (r *readTrap) Read([]byte) (int, error) {
	r.reads++
	return 0, io.ErrUnexpectedEOF
}

func (a errorAuthenticator) Login(context.Context, string, string, authRequestMetadata) (currentUser, error) {
	return currentUser{}, a.loginErr
}

func (a errorAuthenticator) IssueToken(currentUser) (string, error) { return "", nil }

func (a errorAuthenticator) ParseToken(context.Context, string) (currentUser, error) {
	return currentUser{}, a.parseErr
}

func (a errorAuthenticator) RecordRefresh(context.Context, currentUser, authRequestMetadata) error {
	return nil
}

func (a errorAuthenticator) Logout(context.Context, currentUser, authRequestMetadata) error {
	return nil
}

func TestAppRuntimeStateCanSwitchConcurrently(t *testing.T) {
	application := &app{}
	auth := errorAuthenticator{}
	start := make(chan struct{})
	var readers sync.WaitGroup
	for range 8 {
		readers.Add(1)
		go func() {
			defer readers.Done()
			<-start
			for range 1000 {
				_ = application.runtimeConfig()
				_ = application.authenticationService()
			}
		}()
	}
	close(start)
	for i := range 1000 {
		application.replaceRuntime(config{Installed: i%2 == 0}, nil, auth, nil, nil, nil)
	}
	readers.Wait()
	if application.authenticationService() == nil {
		t.Fatal("authentication service was not installed")
	}
}

func TestAppCommandWorkerReplacementStopsPreviousWorker(t *testing.T) {
	application := &app{logger: slog.New(slog.NewTextHandler(io.Discard, nil))}
	first := &lifecycleCommandWorker{started: make(chan struct{}), stopped: make(chan struct{})}
	second := &lifecycleCommandWorker{started: make(chan struct{}), stopped: make(chan struct{})}

	application.replaceCommandWorker(first)
	<-first.started
	application.replaceCommandWorker(second)
	select {
	case <-first.stopped:
	default:
		t.Fatal("replacement returned before the previous Command worker stopped")
	}
	<-second.started

	application.replaceCommandWorker(nil)
	select {
	case <-second.stopped:
	default:
		t.Fatal("shutdown returned before the current Command worker stopped")
	}
}

func TestAppWebhookWorkerReplacementStopsPreviousWorker(t *testing.T) {
	application := &app{logger: slog.New(slog.NewTextHandler(io.Discard, nil))}
	first := &lifecycleWebhookWorker{started: make(chan struct{}), stopped: make(chan struct{})}
	second := &lifecycleWebhookWorker{started: make(chan struct{}), stopped: make(chan struct{})}

	application.replaceWebhookWorker(first)
	<-first.started
	application.replaceWebhookWorker(second)
	select {
	case <-first.stopped:
	default:
		t.Fatal("replacement returned before the previous Webhook worker stopped")
	}
	<-second.started

	application.replaceWebhookWorker(nil)
	select {
	case <-second.stopped:
	default:
		t.Fatal("shutdown returned before the current Webhook worker stopped")
	}
}

func TestAppCloseWaitsForCommandAndWebhookWorkers(t *testing.T) {
	application := &app{logger: slog.New(slog.NewTextHandler(io.Discard, nil))}
	command := &lifecycleCommandWorker{started: make(chan struct{}), stopped: make(chan struct{})}
	webhook := &lifecycleWebhookWorker{started: make(chan struct{}), stopped: make(chan struct{})}
	application.replaceCommandWorker(command)
	application.replaceWebhookWorker(webhook)
	<-command.started
	<-webhook.started
	if err := application.close(); err != nil {
		t.Fatal(err)
	}
	for name, stopped := range map[string]<-chan struct{}{"Command": command.stopped, "Webhook": webhook.stopped} {
		select {
		case <-stopped:
		default:
			t.Fatalf("close returned before the %s worker stopped", name)
		}
	}
}

func TestSetupStatusUsesPublishedRuntimeState(t *testing.T) {
	lockPath := filepath.Join(t.TempDir(), ".installed")
	t.Setenv("INSTALL_LOCK_PATH", lockPath)
	application := newAppWithDeviceService(
		config{JWTSecret: testJWTSecret},
		slog.New(slog.NewTextHandler(io.Discard, nil)),
		devicecore.NewService(),
	)
	server := application.routes()

	if err := os.WriteFile(lockPath, []byte("installed\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	var before setupStatus
	decodeResponseData(t, doRequest(t, server, http.MethodGet, "/setup/status", "", nil), &before)
	if before.Installed || !before.NeedsSetup {
		t.Fatalf("setup status followed unpublished lock state: %+v", before)
	}

	cfg := application.runtimeConfig()
	cfg.Installed = true
	application.replaceRuntime(cfg, nil, application.authenticationService(), application.projectService(), application.deviceResourceService(), application.commandResourceService())
	var after setupStatus
	decodeResponseData(t, doRequest(t, server, http.MethodGet, "/setup/status", "", nil), &after)
	if !after.Installed || after.NeedsSetup {
		t.Fatalf("setup status did not publish installed runtime: %+v", after)
	}
}

func TestUninstalledAppDoesNotPublishLegacyRuntimeServices(t *testing.T) {
	t.Setenv("INSTALL_LOCK_PATH", filepath.Join(t.TempDir(), ".installed"))
	application, err := newApp(config{}, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatal(err)
	}
	defer application.close()

	if application.deviceService != nil || application.commandRouter != nil || application.projectService() != nil ||
		application.gateway != nil || application.webhooks != nil {
		t.Fatal("uninstalled production app published legacy in-memory services")
	}

	response := doRequest(t, application.routes(), http.MethodGet, "/v1/open/projects", "", nil)
	envelope := assertEnvelope(t, response, http.StatusServiceUnavailable, false)
	if envelope.ErrorCode != "setup_required" {
		t.Fatalf("error_code = %q, want setup_required", envelope.ErrorCode)
	}
}

func TestLoadConfigLoadsEnvFilesWithoutOverridingProcessEnv(t *testing.T) {
	unsetEnvForTest(t, "DATABASE_URL", "REDIS_URL", "JWT_SECRET", "WEBHOOK_SECRET_ENCRYPTION_KEY", "LOG_LEVEL", "READ_HEADER_TIMEOUT", "DEVICE_PLATFORM_INSTALLED")
	t.Setenv("SERVER_ADDR", ":9090")
	t.Setenv("INSTALL_LOCK_PATH", filepath.Join(t.TempDir(), ".installed"))

	dir := t.TempDir()
	envPath := filepath.Join(dir, ".env")
	if err := os.WriteFile(envPath, []byte(`
SERVER_ADDR=:8081
DATABASE_URL=postgres://postgres:postgres@localhost:5432/device_platform?sslmode=disable
REDIS_URL=redis://localhost:6379/0
JWT_SECRET=0123456789abcdef0123456789abcdef
WEBHOOK_SECRET_ENCRYPTION_KEY=AAECAwQFBgcICQoLDA0ODxAREhMUFRYXGBkaGxwdHh8
LOG_LEVEL=debug
READ_HEADER_TIMEOUT=3s
WWTIOT_USER_ID=env-user
WWTIOT_USER_KEY=env-key
`), 0o600); err != nil {
		t.Fatal(err)
	}

	cfg, err := loadConfig(envPath)
	if err != nil {
		t.Fatal(err)
	}

	if cfg.ServerAddr != ":9090" {
		t.Fatalf("expected process env to win, got %q", cfg.ServerAddr)
	}
	if cfg.DatabaseURL == "" || cfg.RedisURL == "" || cfg.JWTSecret == "" || len(cfg.WebhookSecretEncryptionKey) != 32 {
		t.Fatalf("expected runtime connection fields from env file, got %+v", cfg)
	}
	if cfg.ReadHeaderTimeout != 3*time.Second {
		t.Fatalf("expected parsed read header timeout, got %s", cfg.ReadHeaderTimeout)
	}
	if cfg.WWTIOTUserID != "env-user" || cfg.WWTIOTUserKey != "env-key" {
		t.Fatal("expected WWTIOT credentials from env file")
	}
}

func TestLoadConfigPreservesWWTIOTUserKeyBytes(t *testing.T) {
	t.Setenv("INSTALL_LOCK_PATH", filepath.Join(t.TempDir(), ".installed"))
	t.Setenv("DEVICE_PLATFORM_INSTALLED", "false")
	t.Setenv("WWTIOT_USER_KEY", " key-with-significant-spaces ")

	cfg, err := loadConfig()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.WWTIOTUserKey != " key-with-significant-spaces " {
		t.Fatalf("WWTIOT UserKey bytes changed: %q", cfg.WWTIOTUserKey)
	}
}

func TestSetupErrorsFollowFrozenContract(t *testing.T) {
	t.Setenv("INSTALL_LOCK_PATH", filepath.Join(t.TempDir(), ".installed"))
	t.Setenv("DEVICE_PLATFORM_INSTALLED", "true")
	completed := doRequest(t, newTestServer(), http.MethodPost, "/setup/test-db", `{}`, nil)
	completedEnvelope := assertEnvelope(t, completed, http.StatusConflict, false)
	if completedEnvelope.ErrorCode != "setup_completed" {
		t.Fatalf("setup error_code = %q", completedEnvelope.ErrorCode)
	}

	t.Setenv("DEVICE_PLATFORM_INSTALLED", "false")
	cfg := config{ServerAddr: ":0", LogLevel: "error", JWTSecret: testJWTSecret, ReadHeaderTimeout: 5 * time.Second}
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	server := newAppWithDeviceService(cfg, logger, devicecore.NewService()).routes()
	invalid := doRequest(t, server, http.MethodPost, "/setup/test-db", `{"url":"not-a-database-url","extra":"postgres://user:secret@example.test/db"}`, nil)
	invalidEnvelope := assertEnvelope(t, invalid, http.StatusBadRequest, false)
	if invalidEnvelope.ErrorCode != "invalid_install_request" || strings.Contains(invalid.Body.String(), "secret") {
		t.Fatalf("invalid setup response is not stable and redacted: %s", invalid.Body.String())
	}
	invalidURL := doRequest(t, server, http.MethodPost, "/setup/test-db", `{"url":"not-a-database-url"}`, nil)
	invalidURLEnvelope := assertEnvelope(t, invalidURL, http.StatusBadRequest, false)
	if invalidURLEnvelope.ErrorCode != "invalid_install_request" || invalidURLEnvelope.Message != "invalid setup request" {
		t.Fatalf("database setup response = %+v", invalidURLEnvelope)
	}
	invalidRedis := doRequest(t, server, http.MethodPost, "/setup/test-redis", `{"url":"not-a-redis-url"}`, nil)
	if envelope := assertEnvelope(t, invalidRedis, http.StatusBadRequest, false); envelope.ErrorCode != "invalid_install_request" {
		t.Fatalf("Redis setup response = %+v", envelope)
	}
}

func TestInstallRequestDoesNotDefaultRequiredFields(t *testing.T) {
	valid := setupInstallRequest{
		Database: databaseSetupRequest{URL: "postgres://local/device_platform_test"},
		Redis:    redisSetupRequest{URL: "redis://local/0"},
		Admin: adminSetupRequest{
			Email: "admin@example.test", DisplayName: "Administrator",
			Password: "correct-horse", ConfirmPassword: "correct-horse",
		},
		Server: serverSetupRequest{Addr: ":8080", LogLevel: "info"},
	}
	for _, mutate := range []func(*setupInstallRequest){
		func(request *setupInstallRequest) { request.Server.Addr = "" },
		func(request *setupInstallRequest) { request.Server.LogLevel = "" },
		func(request *setupInstallRequest) { request.Admin.DisplayName = "" },
	} {
		request := valid
		mutate(&request)
		if err := validateInstallRequest(normalizeInstallRequest(request)); err == nil {
			t.Fatalf("missing required install field was defaulted: %+v", request)
		}
	}
}

func TestInstallTargetProbeDoesNotTouchFixedNames(t *testing.T) {
	dir := t.TempDir()
	legacyProbe := filepath.Join(dir, ".device-platform-env-write-test")
	if err := os.WriteFile(legacyProbe, []byte("owned-by-user"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := validateFileTargetWritable(filepath.Join(dir, ".env")); err != nil {
		t.Fatal(err)
	}
	contents, err := os.ReadFile(legacyProbe)
	if err != nil {
		t.Fatal(err)
	}
	if string(contents) != "owned-by-user" {
		t.Fatalf("writability probe changed unrelated file: %q", contents)
	}
}

func unsetEnvForTest(t *testing.T, keys ...string) {
	t.Helper()
	for _, key := range keys {
		value, existed := os.LookupEnv(key)
		if err := os.Unsetenv(key); err != nil {
			t.Fatalf("unset env %s: %v", key, err)
		}
		t.Cleanup(func() {
			if existed {
				_ = os.Setenv(key, value)
			} else {
				_ = os.Unsetenv(key)
			}
		})
	}
}

func useFreshSetupConfigForTest(t *testing.T) {
	t.Helper()
	unsetEnvForTest(t, "DATABASE_URL", "REDIS_URL", "JWT_SECRET", "WEBHOOK_SECRET_ENCRYPTION_KEY", "DEVICE_PLATFORM_INSTALLED")
	t.Setenv("INSTALL_LOCK_PATH", filepath.Join(t.TempDir(), ".installed"))
}

func TestLoadConfigRequiresRuntimeFieldsAfterInstall(t *testing.T) {
	t.Setenv("INSTALL_LOCK_PATH", filepath.Join(t.TempDir(), ".installed"))
	t.Setenv("DEVICE_PLATFORM_INSTALLED", "true")
	t.Setenv("DATABASE_URL", "")
	t.Setenv("REDIS_URL", "")
	t.Setenv("JWT_SECRET", "")
	t.Setenv("WEBHOOK_SECRET_ENCRYPTION_KEY", "")

	_, err := loadConfig()
	if err == nil || !strings.Contains(err.Error(), "DATABASE_URL") {
		t.Fatalf("expected DATABASE_URL error after install, got %v", err)
	}
}

func TestLoadConfigRejectsMalformedWebhookRuntimeSettings(t *testing.T) {
	keys := []string{
		"WEBHOOK_WORKER_INTERVAL", "WEBHOOK_REQUEST_TIMEOUT", "WEBHOOK_LEASE_DURATION",
		"WEBHOOK_MAX_ATTEMPTS", "WEBHOOK_RETRY_SCHEDULE", "WEBHOOK_EGRESS_ALLOWLIST",
	}
	for _, test := range []struct {
		name  string
		key   string
		value string
	}{
		{name: "worker interval", key: "WEBHOOK_WORKER_INTERVAL", value: "invalid"},
		{name: "request timeout", key: "WEBHOOK_REQUEST_TIMEOUT", value: "0s"},
		{name: "lease duration", key: "WEBHOOK_LEASE_DURATION", value: "10s"},
		{name: "maximum attempts", key: "WEBHOOK_MAX_ATTEMPTS", value: "6"},
		{name: "retry count", key: "WEBHOOK_RETRY_SCHEDULE", value: "1s,5s"},
		{name: "retry shortening", key: "WEBHOOK_RETRY_SCHEDULE", value: "500ms,5s,30s,2m"},
		{name: "egress allowlist", key: "WEBHOOK_EGRESS_ALLOWLIST", value: "not-a-prefix"},
	} {
		t.Run(test.name, func(t *testing.T) {
			useFreshSetupConfigForTest(t)
			unsetEnvForTest(t, keys...)
			t.Setenv(test.key, test.value)
			if _, err := loadConfig(); err == nil || !strings.Contains(err.Error(), test.key) {
				t.Fatalf("loadConfig error=%v, want %s", err, test.key)
			}
		})
	}
}

func TestLoadConfigAcceptsLowerWebhookAttemptLimitAndExtendedSchedule(t *testing.T) {
	useFreshSetupConfigForTest(t)
	unsetEnvForTest(t,
		"WEBHOOK_WORKER_INTERVAL", "WEBHOOK_REQUEST_TIMEOUT", "WEBHOOK_LEASE_DURATION",
		"WEBHOOK_MAX_ATTEMPTS", "WEBHOOK_RETRY_SCHEDULE", "WEBHOOK_EGRESS_ALLOWLIST",
	)
	t.Setenv("WEBHOOK_MAX_ATTEMPTS", "3")
	t.Setenv("WEBHOOK_RETRY_SCHEDULE", "2s,10s")
	t.Setenv("WEBHOOK_EGRESS_ALLOWLIST", "10.20.0.0/16,2001:db8::1")
	cfg, err := loadConfig()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.WebhookMaxAttempts != 3 || len(cfg.WebhookRetrySchedule) != 2 || cfg.WebhookRetrySchedule[0] != 2*time.Second || cfg.WebhookRetrySchedule[1] != 10*time.Second || cfg.WebhookLeaseDuration != 15*time.Second {
		t.Fatalf("Webhook runtime config=%+v", cfg)
	}
}

func TestExampleConfigAllowsFreshSetupWithoutWebhookEncryptionKey(t *testing.T) {
	unsetEnvForTest(t, "DATABASE_URL", "REDIS_URL", "JWT_SECRET", "WEBHOOK_SECRET_ENCRYPTION_KEY", "DEVICE_PLATFORM_INSTALLED")
	t.Setenv("INSTALL_LOCK_PATH", filepath.Join(t.TempDir(), ".installed"))

	cfg, err := loadConfig(filepath.Join("..", "..", ".env.example"))
	if err != nil {
		t.Fatalf("load fresh example config: %v", err)
	}
	if cfg.isInstalled() || len(cfg.WebhookSecretEncryptionKey) != 0 {
		t.Fatalf("fresh example config must remain setup-ready: installed=%v key_len=%d", cfg.isInstalled(), len(cfg.WebhookSecretEncryptionKey))
	}
}

func TestDecodeWebhookEncryptionKey(t *testing.T) {
	valid, err := decodeWebhookEncryptionKey("AAECAwQFBgcICQoLDA0ODxAREhMUFRYXGBkaGxwdHh8")
	if err != nil || len(valid) != 32 {
		t.Fatalf("valid Webhook encryption key: len=%d err=%v", len(valid), err)
	}
	for _, raw := range []string{"not-base64!", "c2hvcnQ=", "c2hvcnQ"} {
		if _, err := decodeWebhookEncryptionKey(raw); err == nil {
			t.Fatalf("invalid Webhook encryption key accepted: %q", raw)
		}
	}
}

func TestWriteRuntimeEnvPersistsIndependentWebhookEncryptionKey(t *testing.T) {
	dir := t.TempDir()
	previous, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(previous) })

	key := make([]byte, 32)
	for i := range key {
		key[i] = byte(i)
	}
	request := setupInstallRequest{
		Database: databaseSetupRequest{URL: "postgres://local/device_platform_test"},
		Redis:    redisSetupRequest{URL: "redis://local/0"},
		Server:   serverSetupRequest{Addr: ":8080", LogLevel: "info"},
	}
	if err := os.WriteFile(filepath.Join(dir, ".env.tmp"), []byte("stale"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(filepath.Join(dir, ".env.tmp"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := writeRuntimeEnv(request, "jwt-secret-that-is-separate-from-webhook", key); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(filepath.Join(dir, ".env"))
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("runtime env mode=%o, want 600", info.Mode().Perm())
	}
	raw, err := os.ReadFile(filepath.Join(dir, ".env"))
	if err != nil {
		t.Fatal(err)
	}
	contents := string(raw)
	if !strings.Contains(contents, "WEBHOOK_SECRET_ENCRYPTION_KEY=AAECAwQFBgcICQoLDA0ODxAREhMUFRYXGBkaGxwdHh8") ||
		strings.Contains(contents, "WEBHOOK_SECRET_ENCRYPTION_KEY=jwt-secret") {
		t.Fatal("runtime env did not persist an independent Webhook encryption key")
	}
}

func TestNewAppRequiresRuntimeDependenciesAfterInstall(t *testing.T) {
	t.Setenv("INSTALL_LOCK_PATH", filepath.Join(t.TempDir(), ".installed"))
	cfg := config{
		DatabaseURL:       "postgres://postgres:postgres@127.0.0.1:1/device_platform?sslmode=disable",
		RedisURL:          "redis://127.0.0.1:1/0",
		JWTSecret:         testJWTSecret,
		Installed:         true,
		ReadHeaderTimeout: 5 * time.Second,
	}
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))

	if _, err := newApp(cfg, logger); err == nil || !strings.Contains(err.Error(), "database unavailable after installation") {
		t.Fatalf("expected installed app startup to fail on unavailable database, got %v", err)
	}
}

func TestAuthenticationHTTPErrorClassification(t *testing.T) {
	tests := []struct {
		name       string
		auth       authenticator
		method     string
		path       string
		body       string
		bearer     bool
		wantStatus int
		wantCode   string
		wantRetry  string
	}{
		{
			name:       "login rate limited",
			auth:       errorAuthenticator{loginErr: authRateLimitError{RetryAfter: 321}},
			method:     http.MethodPost,
			path:       "/v1/auth/login",
			body:       `{"email":"admin@example.test","password":"wrong"}`,
			wantStatus: http.StatusTooManyRequests,
			wantCode:   "rate_limited",
			wantRetry:  "321",
		},
		{
			name:       "login dependency unavailable",
			auth:       errorAuthenticator{loginErr: errAuthDependencyUnavailable},
			method:     http.MethodPost,
			path:       "/v1/auth/login",
			body:       `{"email":"admin@example.test","password":"wrong"}`,
			wantStatus: http.StatusServiceUnavailable,
			wantCode:   "auth_dependency_unavailable",
		},
		{
			name:       "session dependency unavailable",
			auth:       errorAuthenticator{parseErr: errAuthDependencyUnavailable},
			method:     http.MethodGet,
			path:       "/v1/auth/me",
			bearer:     true,
			wantStatus: http.StatusServiceUnavailable,
			wantCode:   "auth_dependency_unavailable",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			application := newAppWithDeviceService(config{JWTSecret: testJWTSecret, Installed: true}, slog.New(slog.NewTextHandler(io.Discard, nil)), devicecore.NewService())
			application.auth = tt.auth
			recorder := httptest.NewRecorder()
			request := httptest.NewRequest(tt.method, tt.path, strings.NewReader(tt.body))
			if tt.bearer {
				request.Header.Set("Authorization", "Bearer test-token")
			}
			application.routes().ServeHTTP(recorder, request)
			var response jsonResponse
			decodeResponse(t, recorder, &response)
			if recorder.Code != tt.wantStatus || response.ErrorCode != tt.wantCode {
				t.Fatalf("status=%d code=%q, want status=%d code=%q", recorder.Code, response.ErrorCode, tt.wantStatus, tt.wantCode)
			}
			if recorder.Header().Get("Retry-After") != tt.wantRetry {
				t.Fatalf("Retry-After=%q, want %q", recorder.Header().Get("Retry-After"), tt.wantRetry)
			}
		})
	}
}

func TestValidateServerAddrRejectsInvalidPort(t *testing.T) {
	for _, addr := range []string{":0", ":99999", "127.0.0.1:0", "127.0.0.1:99999"} {
		if err := validateServerAddr(addr); err == nil {
			t.Fatalf("expected invalid server address %q to fail", addr)
		}
	}
}

func TestHealthAndReadyUseUnifiedJSON(t *testing.T) {
	server := newTestServer()

	for _, path := range []string{"/healthz", "/readyz"} {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, path, nil)

		server.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("%s expected 200, got %d", path, rec.Code)
		}
		var body jsonResponse
		decodeResponse(t, rec, &body)
		if !body.Success || body.Code != 0 {
			t.Fatalf("%s expected success envelope, got %+v", path, body)
		}
		if body.RequestID == "" || rec.Header().Get("X-Request-ID") != body.RequestID {
			t.Fatalf("%s request ID header/envelope mismatch: header=%q body=%q", path, rec.Header().Get("X-Request-ID"), body.RequestID)
		}
		if rec.Header().Get("Access-Control-Expose-Headers") != "X-Request-ID" {
			t.Fatalf("%s must expose X-Request-ID to browser clients", path)
		}
	}
}

func TestHTTPAuditFieldsUseServerRequestIdentityAndDirectPeer(t *testing.T) {
	var audit webhookaudit.AuditRequest
	handler := httpjson.WithRequestID(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		audit = withHTTPAuditFields(webhookaudit.AuditRequest{}, r)
	}))
	request := httptest.NewRequest(http.MethodPost, "/v1/projects", nil)
	request.RemoteAddr = "[2001:db8::1]:4321"
	request.Header.Set("X-Request-ID", "client-request")
	request.Header.Set("X-Forwarded-For", "203.0.113.9")
	handler.ServeHTTP(httptest.NewRecorder(), request.WithContext(context.Background()))

	if audit.RequestID == "" || audit.RequestID == "client-request" {
		t.Fatalf("audit must use generated server request ID, got %q", audit.RequestID)
	}
	if audit.IPAddress != "2001:db8::1" {
		t.Fatalf("audit IP must use the direct peer, got %q", audit.IPAddress)
	}
}

func TestAuthCompatibilityLoginMeAndBearerGate(t *testing.T) {
	server := newTestServer()

	missingCredentials := httptest.NewRecorder()
	server.ServeHTTP(missingCredentials, httptest.NewRequest(http.MethodPost, "/v1/auth/login", nil))
	if missingCredentials.Code != http.StatusUnauthorized {
		t.Fatalf("expected missing credentials 401, got %d", missingCredentials.Code)
	}

	legacyLogin := httptest.NewRecorder()
	server.ServeHTTP(legacyLogin, httptest.NewRequest(http.MethodPost, "/api/auth/login", nil))
	if legacyLogin.Code != http.StatusNotFound {
		t.Fatalf("expected legacy login 404, got %d", legacyLogin.Code)
	}

	invalidCredentials := httptest.NewRecorder()
	server.ServeHTTP(invalidCredentials, httptest.NewRequest(http.MethodPost, "/v1/auth/login", strings.NewReader(`{"email":"admin@test.local","password":"wrong-password"}`)))
	if invalidCredentials.Code != http.StatusUnauthorized {
		t.Fatalf("expected invalid credentials 401, got %d", invalidCredentials.Code)
	}

	login := httptest.NewRecorder()
	server.ServeHTTP(login, httptest.NewRequest(http.MethodPost, "/v1/auth/login", strings.NewReader(`{"email":"admin@test.local","password":"test-admin-password"}`)))
	if login.Code != http.StatusOK {
		t.Fatalf("expected login 200, got %d", login.Code)
	}
	var loginBody jsonResponse
	decodeResponse(t, login, &loginBody)
	data, ok := loginBody.Data.(map[string]interface{})
	token, _ := data["access_token"].(string)
	if !ok || token == "" || data["token_type"] != "Bearer" {
		t.Fatalf("unexpected login response: %+v", loginBody.Data)
	}

	legacyMe := httptest.NewRecorder()
	legacyReq := httptest.NewRequest(http.MethodGet, "/api/me", nil)
	legacyReq.Header.Set("Authorization", "Bearer "+token)
	server.ServeHTTP(legacyMe, legacyReq)
	if legacyMe.Code != http.StatusNotFound {
		t.Fatalf("expected legacy me 404, got %d", legacyMe.Code)
	}

	blocked := httptest.NewRecorder()
	server.ServeHTTP(blocked, httptest.NewRequest(http.MethodGet, "/v1/auth/me", nil))
	if blocked.Code != http.StatusUnauthorized {
		t.Fatalf("expected v1 me without bearer 401, got %d", blocked.Code)
	}

	allowed := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/v1/auth/me", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	server.ServeHTTP(allowed, req)
	if allowed.Code != http.StatusOK {
		t.Fatalf("expected v1 me with bearer 200, got %d", allowed.Code)
	}

	refreshBlocked := httptest.NewRecorder()
	server.ServeHTTP(refreshBlocked, httptest.NewRequest(http.MethodPost, "/v1/auth/refresh", nil))
	if refreshBlocked.Code != http.StatusUnauthorized {
		t.Fatalf("expected refresh without bearer 401, got %d", refreshBlocked.Code)
	}

	refreshAllowed := httptest.NewRecorder()
	refreshReq := httptest.NewRequest(http.MethodPost, "/v1/auth/refresh", nil)
	refreshReq.Header.Set("Authorization", "Bearer "+token)
	server.ServeHTTP(refreshAllowed, refreshReq)
	if refreshAllowed.Code != http.StatusOK {
		t.Fatalf("expected refresh with bearer 200, got %d", refreshAllowed.Code)
	}
	var refreshBody jsonResponse
	decodeResponse(t, refreshAllowed, &refreshBody)
	refreshData, ok := refreshBody.Data.(map[string]interface{})
	if !ok || refreshData["expires_in"] != float64(86400) {
		t.Fatalf("refresh expires_in must be an integer JSON number, got %+v", refreshBody.Data)
	}

	logout := httptest.NewRecorder()
	logoutReq := httptest.NewRequest(http.MethodPost, "/v1/auth/logout", nil)
	logoutReq.Header.Set("Authorization", "Bearer "+token)
	server.ServeHTTP(logout, logoutReq)
	if logout.Code != http.StatusOK {
		t.Fatalf("expected logout 200, got %d: %s", logout.Code, logout.Body.String())
	}

	invalidated := httptest.NewRecorder()
	invalidatedReq := httptest.NewRequest(http.MethodGet, "/v1/auth/me", nil)
	invalidatedReq.Header.Set("Authorization", "Bearer "+token)
	server.ServeHTTP(invalidated, invalidatedReq)
	if invalidated.Code != http.StatusUnauthorized {
		t.Fatalf("expected logged-out token to be rejected, got %d", invalidated.Code)
	}
}

func TestOpenAPIKeyGate(t *testing.T) {
	server := newTestServer()
	projectID, apiKey := createProjectForOpenAPITest(t, server)

	blocked := httptest.NewRecorder()
	server.ServeHTTP(blocked, httptest.NewRequest(http.MethodGet, "/v1/open/projects/"+projectID, nil))
	if blocked.Code != http.StatusUnauthorized {
		t.Fatalf("expected missing api key 401, got %d", blocked.Code)
	}

	allowed := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/v1/open/projects/"+projectID, nil)
	req.Header.Set("X-API-Key", apiKey)
	server.ServeHTTP(allowed, req)
	if allowed.Code != http.StatusOK {
		t.Fatalf("expected api key 200, got %d", allowed.Code)
	}
	var body devicecore.Project
	decodeResponseData(t, allowed, &body)
	if body.ID != projectID {
		t.Fatalf("expected open api project id %q, got %+v", projectID, body)
	}
}

func TestSmokeRoutesReturnUnifiedEnvelope(t *testing.T) {
	server := newTestServer()

	setupStatus := doRequest(t, server, http.MethodGet, "/setup/status", "", nil)
	assertEnvelope(t, setupStatus, http.StatusOK, true)

	login := doRequest(t, server, http.MethodPost, "/v1/auth/login", `{"email":"admin@test.local","password":"test-admin-password"}`, nil)
	loginBody := assertEnvelope(t, login, http.StatusOK, true)
	token := dataFieldString(t, loginBody, "access_token")

	authHeaders := map[string]string{"Authorization": "Bearer " + token}
	project := doRequest(t, server, http.MethodPost, "/v1/projects", `{"name":"smoke project"}`, authHeaders)
	projectBody := assertEnvelope(t, project, http.StatusCreated, true)
	projectID := dataFieldString(t, projectBody, "id")
	apiKey := dataFieldString(t, projectBody, "api_key")

	projects := doRequest(t, server, http.MethodGet, "/v1/projects", "", authHeaders)
	assertPaginatedEnvelope(t, projects, http.StatusOK, 1, 20, 1)

	device := doRequest(t, server, http.MethodPost, "/v1/devices", `{"project_id":"`+projectID+`","name":"smoke lock","device_type":"smart_lock","online":true}`, authHeaders)
	deviceBody := assertEnvelope(t, device, http.StatusCreated, true)
	deviceID := dataFieldString(t, deviceBody, "id")

	devices := doRequest(t, server, http.MethodGet, "/v1/devices?project_id="+projectID, "", authHeaders)
	assertEnvelope(t, devices, http.StatusOK, true)

	openProject := doRequest(t, server, http.MethodGet, "/v1/open/projects/"+projectID, "", map[string]string{"X-API-Key": apiKey})
	assertEnvelope(t, openProject, http.StatusOK, true)

	command := doRequest(t, server, http.MethodPost, "/v1/device-commands", `{"project_id":"`+projectID+`","device_id":"`+deviceID+`","command_type":"query_status"}`, authHeaders)
	commandBody := assertEnvelope(t, command, http.StatusCreated, true)
	commandID := dataFieldString(t, commandBody, "id")

	commandDetail := doRequest(t, server, http.MethodGet, "/v1/device-commands/"+commandID+"?project_id="+projectID, "", authHeaders)
	assertEnvelope(t, commandDetail, http.StatusOK, true)

	webhooks := doRequest(t, server, http.MethodGet, "/v1/webhook-deliveries", "", authHeaders)
	assertEnvelope(t, webhooks, http.StatusOK, true)

	resendMissing := doRequest(t, server, http.MethodPost, "/v1/webhook-deliveries/missing/resend", "", authHeaders)
	assertEnvelope(t, resendMissing, http.StatusNotFound, false)

	simulator := doRequest(t, server, http.MethodGet, "/v1/simulator", "", authHeaders)
	assertEnvelope(t, simulator, http.StatusOK, true)

	simulatorUpdate := doRequest(t, server, http.MethodPatch, "/v1/simulator", `{"mode":"normal","delay_ms":100}`, authHeaders)
	assertEnvelope(t, simulatorUpdate, http.StatusOK, true)
}

func TestDeviceRoutesPreserveAppFoundation(t *testing.T) {
	server := newTestServer()
	projectID, apiKey := createProjectForOpenAPITest(t, server)
	deviceID := createDeviceForTest(t, server, projectID)

	ready := httptest.NewRecorder()
	server.ServeHTTP(ready, httptest.NewRequest(http.MethodGet, "/readyz", nil))
	if ready.Code != http.StatusOK {
		t.Fatalf("expected readyz 200, got %d", ready.Code)
	}

	login := httptest.NewRecorder()
	server.ServeHTTP(login, httptest.NewRequest(http.MethodPost, "/v1/auth/login", strings.NewReader(`{"email":"admin@test.local","password":"test-admin-password"}`)))
	if login.Code != http.StatusOK {
		t.Fatalf("expected login 200, got %d", login.Code)
	}

	adminBlocked := httptest.NewRecorder()
	server.ServeHTTP(adminBlocked, httptest.NewRequest(http.MethodGet, "/v1/projects", nil))
	if adminBlocked.Code != http.StatusUnauthorized {
		t.Fatalf("expected admin projects without bearer 401, got %d", adminBlocked.Code)
	}

	adminAllowed := httptest.NewRecorder()
	adminReq := httptest.NewRequest(http.MethodGet, "/v1/projects", nil)
	setAdminBearer(adminReq)
	server.ServeHTTP(adminAllowed, adminReq)
	if adminAllowed.Code != http.StatusOK {
		t.Fatalf("expected admin projects with bearer 200, got %d", adminAllowed.Code)
	}

	openBlocked := httptest.NewRecorder()
	server.ServeHTTP(openBlocked, httptest.NewRequest(http.MethodGet, "/v1/open/device-commands", nil))
	if openBlocked.Code != http.StatusUnauthorized {
		t.Fatalf("expected open commands without api key 401, got %d", openBlocked.Code)
	}

	openCreate := httptest.NewRecorder()
	openReq := httptest.NewRequest(http.MethodPost, "/v1/open/device-commands", strings.NewReader(`{"device_id":"`+deviceID+`","command_type":"query_status"}`))
	openReq.Header.Set("X-API-Key", apiKey)
	server.ServeHTTP(openCreate, openReq)
	if openCreate.Code != http.StatusCreated {
		t.Fatalf("expected open command create 201, got %d body=%s", openCreate.Code, openCreate.Body.String())
	}
}

func TestCreateDeviceAcceptsSimulatorAccessFields(t *testing.T) {
	server := newTestServer()
	projectID := createProjectForTest(t, server)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/v1/devices", strings.NewReader(`{
		"project_id":"`+projectID+`",
		"name":"Simulator Lock",
		"device_type":"smart_lock",
		"access_type":"mock_gateway",
		"provider_code":"simulator",
		"provider_device_id":"sim-001",
		"transport_protocol":"simulator",
		"adapter":"mock_gateway",
		"online":true
	}`))
	req.Header.Set("Content-Type", "application/json")
	setAdminBearer(req)
	server.ServeHTTP(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("expected device 201, got %d: %s", rec.Code, rec.Body.String())
	}
	var device map[string]interface{}
	decodeResponseData(t, rec, &device)
	if device["access_type"] != "mock_gateway" ||
		device["transport_protocol"] != "simulator" ||
		device["adapter"] != "mock_gateway" ||
		device["provider_code"] != "simulator" ||
		device["provider_device_id"] != "sim-001" ||
		device["connection_status"] != "online" ||
		device["lifecycle_status"] != "active" {
		t.Fatalf("unexpected device response: %+v", device)
	}
}

func TestCreateDeviceAcceptsCloudAPIAccessFields(t *testing.T) {
	server := newTestServer()
	projectID := createProjectForTest(t, server)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/v1/devices", strings.NewReader(`{
		"project_id":"`+projectID+`",
		"name":"WWTIOT Lock",
		"device_type":"smart_lock",
		"access_type":"cloud_api",
		"provider_code":"wwtiot",
		"provider_device_id":"768901037824",
		"transport_protocol":"http",
		"adapter":"wwtiot_cloud_api"
	}`))
	req.Header.Set("Content-Type", "application/json")
	setAdminBearer(req)
	server.ServeHTTP(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("expected device 201, got %d: %s", rec.Code, rec.Body.String())
	}
	var device map[string]interface{}
	decodeResponseData(t, rec, &device)
	if device["access_type"] != "cloud_api" ||
		device["transport_protocol"] != "http" ||
		device["adapter"] != "wwtiot_cloud_api" ||
		device["provider_code"] != "wwtiot" ||
		device["provider_device_id"] != "768901037824" ||
		device["connection_status"] != "unknown" ||
		device["lifecycle_status"] != "active" {
		t.Fatalf("unexpected cloud api device response: %+v", device)
	}
}

func TestCreateDeviceRejectsDuplicateProviderIdentity(t *testing.T) {
	server := newTestServer()
	projectID := createProjectForTest(t, server)
	token := testAdminToken(t)
	body := `{
		"project_id":"` + projectID + `",
		"name":"WWTIOT Lock",
		"device_type":"smart_lock",
		"access_type":"cloud_api",
		"provider_code":"wwtiot",
		"provider_device_id":"768901037824",
		"transport_protocol":"http",
		"adapter":"wwtiot_cloud_api"
	}`

	first := doRequest(t, server, http.MethodPost, "/v1/devices", body, map[string]string{"Authorization": "Bearer " + token})
	assertEnvelope(t, first, http.StatusCreated, true)
	second := doRequest(t, server, http.MethodPost, "/v1/devices", body, map[string]string{"Authorization": "Bearer " + token})
	envelope := assertEnvelope(t, second, http.StatusConflict, false)
	if envelope.ErrorCode != "duplicate_device" {
		t.Fatalf("error_code = %q, want duplicate_device", envelope.ErrorCode)
	}
}

func TestCloudProviderEndpointExposesConfigMetadataOnly(t *testing.T) {
	cfg := config{
		ServerAddr:        ":0",
		LogLevel:          "error",
		JWTSecret:         testJWTSecret,
		Installed:         true,
		ReadHeaderTimeout: 5 * time.Second,
		WWTIOTAPIURL:      "https://example.invalid/api",
		WWTIOTUserID:      "test-user",
		WWTIOTUserKey:     "secret-key",
	}
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	server := newAppWithDeviceService(cfg, logger, devicecore.NewService()).routes()

	rec := doRequest(t, server, http.MethodGet, "/v1/cloud-providers?page=1&page_size=1", "", map[string]string{"Authorization": "Bearer " + testAdminToken(t)})
	envelope := assertPaginatedEnvelope(t, rec, http.StatusOK, 1, 1, 2)
	data, ok := envelope.Data.(map[string]interface{})
	if !ok {
		t.Fatalf("Provider data = %T, want object", envelope.Data)
	}
	payload, err := json.Marshal(data["items"])
	if err != nil {
		t.Fatalf("marshal providers: %v", err)
	}
	if strings.Contains(string(payload), "secret-key") || strings.Contains(string(payload), "test-user") {
		t.Fatalf("provider endpoint leaked credentials: %s", payload)
	}
	var providers []map[string]interface{}
	if err := json.Unmarshal(payload, &providers); err != nil {
		t.Fatalf("decode providers: %v", err)
	}
	if len(providers) != 1 || providers[0]["code"] != "simulator" ||
		providers[0]["integration_status"] != "verified" {
		t.Fatalf("unexpected providers: %+v", providers)
	}
	second := doRequest(t, server, http.MethodGet, "/v1/cloud-providers?page=2&page_size=1", "", map[string]string{"Authorization": "Bearer " + testAdminToken(t)})
	secondEnvelope := assertPaginatedEnvelope(t, second, http.StatusOK, 2, 1, 2)
	secondData, ok := secondEnvelope.Data.(map[string]interface{})
	if !ok {
		t.Fatalf("Provider data = %T, want object", secondEnvelope.Data)
	}
	secondItems := secondData["items"].([]interface{})
	wwtiot := secondItems[0].(map[string]interface{})
	if wwtiot["code"] != "wwtiot" || wwtiot["adapter"] != devicecore.AdapterWWTIOTCloudAPI ||
		wwtiot["integration_status"] != "configured_unverified" {
		t.Fatalf("unexpected WWTIOT Provider: %+v", wwtiot)
	}
	for _, query := range []string{"?configured=true", "?page=", "?page=1&page=2", "?page_size=101"} {
		response := doRequest(t, server, http.MethodGet, "/v1/cloud-providers"+query, "", map[string]string{"Authorization": "Bearer " + testAdminToken(t)})
		if invalid := assertEnvelope(t, response, http.StatusBadRequest, false); invalid.ErrorCode != "invalid_request" {
			t.Fatalf("query %q error = %+v", query, invalid)
		}
	}
}

func TestWWTIOTCallbackFailsClosedWithoutReadingBody(t *testing.T) {
	server := newTestServer()
	body := &readTrap{}
	request := httptest.NewRequest(http.MethodPost, "/v1/provider-callbacks/wwtiot", body)
	request.Header.Set("Content-Type", "application/json")
	recorder := httptest.NewRecorder()
	server.ServeHTTP(recorder, request)
	envelope := assertEnvelope(t, recorder, http.StatusServiceUnavailable, false)
	if envelope.ErrorCode != "provider_callback_unverified" || body.reads != 0 {
		t.Fatalf("callback response=%+v body_reads=%d", envelope, body.reads)
	}

	unknown := doRequest(t, server, http.MethodPost, "/v1/provider-callbacks/missing", `{"ignored":true}`, nil)
	if envelope := assertEnvelope(t, unknown, http.StatusNotFound, false); envelope.ErrorCode != "not_found" {
		t.Fatalf("unknown callback response = %+v", envelope)
	}
	wrongMethod := doRequest(t, server, http.MethodGet, "/v1/provider-callbacks/wwtiot", "", nil)
	if envelope := assertEnvelope(t, wrongMethod, http.StatusMethodNotAllowed, false); envelope.ErrorCode != "method_not_allowed" {
		t.Fatalf("callback method response = %+v", envelope)
	}
}

func TestCreateDeviceRejectsUnknownCloudProvider(t *testing.T) {
	server := newTestServer()
	projectID := createProjectForTest(t, server)

	rec := doRequest(t, server, http.MethodPost, "/v1/devices", `{
		"project_id":"`+projectID+`",
		"name":"Unknown Provider Lock",
		"device_type":"smart_lock",
		"access_type":"cloud_api",
		"provider_code":"missing-provider",
		"provider_device_id":"768901037824",
		"transport_protocol":"http",
		"adapter":"wwtiot_cloud_api"
	}`, map[string]string{"Authorization": "Bearer " + testAdminToken(t)})
	envelope := assertEnvelope(t, rec, http.StatusBadRequest, false)
	if envelope.ErrorCode != "invalid_argument" || !strings.Contains(envelope.Message, "unknown cloud provider") {
		t.Fatalf("unexpected envelope: %+v", envelope)
	}
}

func TestCreateDeviceReportsContractErrorsWithoutInvalidJSON(t *testing.T) {
	server := newTestServer()
	projectID := createProjectForTest(t, server)

	cases := []struct {
		name      string
		body      string
		wantCode  string
		wantError string
	}{
		{
			name: "unknown field",
			body: `{
				"project_id":"` + projectID + `",
				"name":"Unknown Field Lock",
				"device_type":"smart_lock",
				"access_type":"mock_gateway",
				"surprise":"not in contract"
			}`,
			wantCode:  "invalid_request",
			wantError: "invalid request",
		},
		{
			name: "cloud api transport mismatch",
			body: `{
				"project_id":"` + projectID + `",
				"name":"Mismatched Transport Lock",
				"device_type":"smart_lock",
				"access_type":"cloud_api",
				"provider_device_id":"111",
				"transport_protocol":"simulator",
				"adapter":"wwtiot_cloud_api"
			}`,
			wantCode:  "invalid_argument",
			wantError: "invalid_argument: transport_protocol does not match access_type",
		},
		{
			name: "cloud adapter mismatch",
			body: `{
				"project_id":"` + projectID + `",
				"name":"Mismatched Adapter Lock",
				"device_type":"smart_lock",
				"access_type":"cloud_api",
				"provider_device_id":"111",
				"transport_protocol":"http",
				"adapter":"mock_gateway"
			}`,
			wantCode:  "invalid_argument",
			wantError: "invalid_argument: adapter does not match access_type",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rec := httptest.NewRecorder()
			req := httptest.NewRequest(http.MethodPost, "/v1/devices", strings.NewReader(tc.body))
			req.Header.Set("Content-Type", "application/json")
			setAdminBearer(req)
			server.ServeHTTP(rec, req)
			if rec.Code != http.StatusBadRequest {
				t.Fatalf("expected device 400, got %d: %s", rec.Code, rec.Body.String())
			}
			var body jsonResponse
			decodeResponse(t, rec, &body)
			if body.ErrorCode != tc.wantCode {
				t.Fatalf("error_code = %q, want %q", body.ErrorCode, tc.wantCode)
			}
			if body.Message != tc.wantError {
				t.Fatalf("message = %q, want %q", body.Message, tc.wantError)
			}
			if body.ErrorCode == "invalid_json" {
				t.Fatalf("contract error must not be reported as invalid_json")
			}
		})
	}
}

func TestCORSPreflight(t *testing.T) {
	server := newTestServer()

	rec := httptest.NewRecorder()
	server.ServeHTTP(rec, httptest.NewRequest(http.MethodOptions, "/v1/auth/me", nil))

	if rec.Code != http.StatusNoContent {
		t.Fatalf("expected preflight 204, got %d", rec.Code)
	}
	if rec.Header().Get("Access-Control-Allow-Headers") == "" {
		t.Fatal("expected CORS headers")
	}
}

func TestControlRoutesRequireAdminBearer(t *testing.T) {
	server := newTestServer()

	cases := []struct {
		method string
		path   string
		body   string
	}{
		{method: http.MethodGet, path: "/v1/simulator"},
		{method: http.MethodGet, path: "/v1/webhook-deliveries"},
		{method: http.MethodGet, path: "/v1/audit-logs"},
		{method: http.MethodPost, path: "/v1/events", body: `{"project_id":"proj_1","event_type":"state_changed"}`},
	}

	for _, tc := range cases {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(tc.method, tc.path, strings.NewReader(tc.body))
		req.Header.Set("Content-Type", "application/json")
		server.ServeHTTP(rec, req)
		if rec.Code != http.StatusUnauthorized {
			t.Fatalf("%s %s without bearer = %d, want 401", tc.method, tc.path, rec.Code)
		}
	}
}

func TestUnknownAPIRoutesUseFrozenEnvelope(t *testing.T) {
	server := newTestServer()
	_, apiKey := createProjectForOpenAPITest(t, server)
	tests := []struct {
		path    string
		headers map[string]string
	}{
		{path: "/v1/admin/diagnostics", headers: map[string]string{"Authorization": "Bearer " + testAdminToken(t)}},
		{path: "/v1/open/diagnostics", headers: map[string]string{"X-API-Key": apiKey}},
	}
	for _, test := range tests {
		response := doRequest(t, server, http.MethodGet, test.path, "", test.headers)
		envelope := assertEnvelope(t, response, http.StatusNotFound, false)
		if envelope.ErrorCode != "not_found" {
			t.Fatalf("GET %s error_code = %q, want not_found", test.path, envelope.ErrorCode)
		}
	}
}

func TestMemoryCompatibilityEventAndAuditRoutesAreReadOnly(t *testing.T) {
	server := newTestServer()

	for _, path := range []string{"/v1/events", "/v1/audit-logs"} {
		recorder := httptest.NewRecorder()
		request := httptest.NewRequest(http.MethodPost, path, strings.NewReader(`{}`))
		request.Header.Set("Content-Type", "application/json")
		setAdminBearer(request)
		server.ServeHTTP(recorder, request)
		if recorder.Code != http.StatusMethodNotAllowed {
			t.Fatalf("POST %s = %d, want 405: %s", path, recorder.Code, recorder.Body.String())
		}
		var body jsonResponse
		decodeResponse(t, recorder, &body)
		if body.ErrorCode != "method_not_allowed" {
			t.Fatalf("POST %s error_code = %q, want method_not_allowed", path, body.ErrorCode)
		}
	}
}

func TestCommandCreationRecordsAuditWithoutLegacyProjectWebhookAuthority(t *testing.T) {
	server := newTestServer()

	projectID := createProjectForTest(t, server)
	legacyWebhook := httptest.NewRecorder()
	legacyWebhookReq := httptest.NewRequest(http.MethodPost, "/v1/projects/webhook-endpoints", strings.NewReader(`{
		"project_id":"`+projectID+`",
		"webhook_url":"https://example.invalid/device-webhook",
		"webhook_secret":"test-secret"
	}`))
	legacyWebhookReq.Header.Set("Content-Type", "application/json")
	setAdminBearer(legacyWebhookReq)
	server.ServeHTTP(legacyWebhook, legacyWebhookReq)
	if legacyWebhook.Code != http.StatusMethodNotAllowed {
		t.Fatalf("legacy Project Webhook authority status = %d, want 405: %s", legacyWebhook.Code, legacyWebhook.Body.String())
	}
	deviceID := createDeviceForTest(t, server, projectID)

	command := httptest.NewRecorder()
	commandReq := httptest.NewRequest(http.MethodPost, "/v1/device-commands", strings.NewReader(`{
		"project_id":"`+projectID+`",
		"device_id":"`+deviceID+`",
		"command_type":"query_status"
	}`))
	commandReq.Header.Set("Content-Type", "application/json")
	setAdminBearer(commandReq)
	server.ServeHTTP(command, commandReq)
	if command.Code != http.StatusCreated {
		t.Fatalf("expected command 201, got %d: %s", command.Code, command.Body.String())
	}
	var commandBody map[string]interface{}
	decodeResponseData(t, command, &commandBody)
	commandID, _ := commandBody["id"].(string)
	if commandID == "" {
		t.Fatalf("command id missing: %+v", commandBody)
	}
	audits := httptest.NewRecorder()
	auditReq := httptest.NewRequest(http.MethodGet, "/v1/audit-logs", nil)
	setAdminBearer(auditReq)
	server.ServeHTTP(audits, auditReq)
	var auditBody struct {
		Items []map[string]interface{} `json:"items"`
	}
	decodeResponseData(t, audits, &auditBody)
	found := false
	for _, item := range auditBody.Items {
		if item["action"] == "command.created" {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("expected command.created audit, got %+v", auditBody.Items)
	}
}

func TestCloudAPICommandCreationDoesNotSynchronouslyDispatch(t *testing.T) {
	requestCount := 0
	vendor := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestCount++
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer vendor.Close()

	service := devicecore.NewService()
	cfg := config{
		ServerAddr:        ":0",
		LogLevel:          "error",
		JWTSecret:         testJWTSecret,
		Installed:         true,
		ReadHeaderTimeout: 5 * time.Second,
		WWTIOTAPIURL:      vendor.URL,
		WWTIOTUserID:      "test-user",
		WWTIOTUserKey:     "test-key",
	}
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	server := newAppWithDeviceService(cfg, logger, service).routes()
	projectID := createProjectForTest(t, server)
	deviceID := createCloudAPIDeviceForTest(t, server, projectID)
	token := testAdminToken(t)

	command := doRequest(t, server, http.MethodPost, "/v1/device-commands", `{
		"project_id":"`+projectID+`",
		"device_id":"`+deviceID+`",
		"command_type":"query_status"
	}`, map[string]string{"Authorization": "Bearer " + token})
	commandBody := assertEnvelope(t, command, http.StatusCreated, true)
	if dataFieldString(t, commandBody, "status") != string(devicecore.CommandStatusQueued) {
		t.Fatalf("command status = %+v, want queued", commandBody.Data)
	}
	commandID := dataFieldString(t, commandBody, "id")

	detail := doRequest(t, server, http.MethodGet, "/v1/device-commands/"+commandID+"?project_id="+projectID, "", map[string]string{"Authorization": "Bearer " + token})
	var detailBody struct {
		Command  map[string]interface{}   `json:"command"`
		Attempts []map[string]interface{} `json:"attempts"`
	}
	decodeResponseData(t, detail, &detailBody)
	if detailBody.Command["status"] != string(devicecore.CommandStatusQueued) {
		t.Fatalf("detail status = %+v, want queued", detailBody.Command)
	}
	if len(detailBody.Attempts) != 0 {
		t.Fatalf("attempts = %+v, want no synchronous attempt", detailBody.Attempts)
	}
	if requestCount != 0 {
		t.Fatalf("Command creation made %d synchronous Provider requests", requestCount)
	}
}

func newTestServer() http.Handler {
	return newTestServerWithDeviceService(devicecore.NewService())
}

func newTestServerWithDeviceService(service *devicecore.Service) http.Handler {
	cfg := config{
		ServerAddr:        ":0",
		LogLevel:          "error",
		JWTSecret:         testJWTSecret,
		Installed:         true,
		ReadHeaderTimeout: 5 * time.Second,
	}
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	return newAppWithDeviceService(cfg, logger, service).routes()
}

type webhookDeliveryListForTest struct {
	Items []map[string]interface{} `json:"items"`
}

func waitForWebhookDelivery(t *testing.T, server http.Handler) webhookDeliveryListForTest {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	var last webhookDeliveryListForTest
	for {
		deliveries := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, "/v1/webhook-deliveries", nil)
		setAdminBearer(req)
		server.ServeHTTP(deliveries, req)
		if deliveries.Code != http.StatusOK {
			t.Fatalf("expected deliveries 200, got %d", deliveries.Code)
		}
		decodeResponseData(t, deliveries, &last)
		if len(last.Items) > 0 {
			return last
		}
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for webhook delivery, got %+v", last.Items)
		}
		time.Sleep(50 * time.Millisecond)
	}
}

func createProjectForTest(t *testing.T, server http.Handler) string {
	t.Helper()
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/v1/projects", strings.NewReader(`{"name":"hook project"}`))
	req.Header.Set("Content-Type", "application/json")
	setAdminBearer(req)
	server.ServeHTTP(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("expected project 201, got %d: %s", rec.Code, rec.Body.String())
	}
	var project map[string]interface{}
	decodeResponseData(t, rec, &project)
	id, _ := project["id"].(string)
	if id == "" {
		t.Fatalf("project id missing: %+v", project)
	}
	return id
}

func createProjectForOpenAPITest(t *testing.T, server http.Handler) (string, string) {
	t.Helper()
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/v1/projects", strings.NewReader(`{"name":"open api project"}`))
	req.Header.Set("Content-Type", "application/json")
	setAdminBearer(req)
	server.ServeHTTP(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("expected project 201, got %d: %s", rec.Code, rec.Body.String())
	}
	var project map[string]interface{}
	decodeResponseData(t, rec, &project)
	id, _ := project["id"].(string)
	apiKey, _ := project["api_key"].(string)
	if id == "" || apiKey == "" {
		t.Fatalf("project open api fields missing: %+v", project)
	}
	return id, apiKey
}

func createDeviceForTest(t *testing.T, server http.Handler, projectID string) string {
	t.Helper()
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/v1/devices", strings.NewReader(`{
		"project_id":"`+projectID+`",
		"name":"hook lock",
		"device_type":"smart_lock",
		"online":true
	}`))
	req.Header.Set("Content-Type", "application/json")
	setAdminBearer(req)
	server.ServeHTTP(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("expected device 201, got %d: %s", rec.Code, rec.Body.String())
	}
	var device map[string]interface{}
	decodeResponseData(t, rec, &device)
	id, _ := device["id"].(string)
	if id == "" {
		t.Fatalf("device id missing: %+v", device)
	}
	return id
}

func createCloudAPIDeviceForTest(t *testing.T, server http.Handler, projectID string) string {
	t.Helper()
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/v1/devices", strings.NewReader(`{
		"project_id":"`+projectID+`",
		"name":"WWTIOT Lock",
		"device_type":"smart_lock",
		"access_type":"cloud_api",
		"provider_code":"wwtiot",
		"provider_device_id":"768901037824",
		"transport_protocol":"http",
		"adapter":"wwtiot_cloud_api"
	}`))
	req.Header.Set("Content-Type", "application/json")
	setAdminBearer(req)
	server.ServeHTTP(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("expected cloud api device 201, got %d: %s", rec.Code, rec.Body.String())
	}
	var device map[string]interface{}
	decodeResponseData(t, rec, &device)
	id, _ := device["id"].(string)
	if id == "" {
		t.Fatalf("device id missing: %+v", device)
	}
	return id
}

func decodeResponse(t *testing.T, body *httptest.ResponseRecorder, dest interface{}) {
	t.Helper()
	decodeBody(t, strings.NewReader(body.Body.String()), dest)
}

func decodeResponseData(t *testing.T, body *httptest.ResponseRecorder, dest interface{}) {
	t.Helper()
	var envelope jsonResponse
	decodeResponse(t, body, &envelope)
	assertEnvelopeFields(t, body, envelope, true)
	payload, err := json.Marshal(envelope.Data)
	if err != nil {
		t.Fatalf("marshal envelope data: %v", err)
	}
	if err := json.Unmarshal(payload, dest); err != nil {
		t.Fatalf("decode envelope data: %v", err)
	}
}

func doRequest(t *testing.T, server http.Handler, method, path, body string, headers map[string]string) *httptest.ResponseRecorder {
	t.Helper()
	rec := httptest.NewRecorder()
	var reader io.Reader
	if body != "" {
		reader = strings.NewReader(body)
	}
	req := httptest.NewRequest(method, path, reader)
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	for key, value := range headers {
		req.Header.Set(key, value)
	}
	server.ServeHTTP(rec, req)
	return rec
}

func assertEnvelope(t *testing.T, rec *httptest.ResponseRecorder, wantStatus int, wantSuccess bool) jsonResponse {
	t.Helper()
	if rec.Code != wantStatus {
		t.Fatalf("HTTP status = %d, want %d: %s", rec.Code, wantStatus, rec.Body.String())
	}
	var envelope jsonResponse
	decodeResponse(t, rec, &envelope)
	assertEnvelopeFields(t, rec, envelope, wantSuccess)
	return envelope
}

func assertPaginatedEnvelope(t *testing.T, rec *httptest.ResponseRecorder, wantStatus, wantPage, wantPageSize, wantTotal int) jsonResponse {
	t.Helper()
	if rec.Code != wantStatus {
		t.Fatalf("HTTP status = %d, want %d: %s", rec.Code, wantStatus, rec.Body.String())
	}
	var envelope jsonResponse
	decodeResponse(t, rec, &envelope)
	if !envelope.Success || envelope.Status != rec.Code || envelope.Code != 0 || envelope.Message == "" || envelope.ErrorCode != "" {
		t.Fatalf("expected paginated success envelope, got %+v", envelope)
	}
	var raw struct {
		Meta struct {
			Page     int `json:"page"`
			PageSize int `json:"page_size"`
			Total    int `json:"total"`
		} `json:"meta"`
	}
	decodeBody(t, strings.NewReader(rec.Body.String()), &raw)
	if raw.Meta.Page != wantPage || raw.Meta.PageSize != wantPageSize || raw.Meta.Total != wantTotal {
		t.Fatalf("pagination meta = %+v, want page=%d page_size=%d total=%d", raw.Meta, wantPage, wantPageSize, wantTotal)
	}
	return envelope
}

func assertEnvelopeFields(t *testing.T, rec *httptest.ResponseRecorder, envelope jsonResponse, wantSuccess bool) {
	t.Helper()
	if envelope.Success != wantSuccess {
		t.Fatalf("success = %v, want %v: %+v", envelope.Success, wantSuccess, envelope)
	}
	if envelope.Status != rec.Code {
		t.Fatalf("envelope status = %d, want HTTP status %d", envelope.Status, rec.Code)
	}
	if wantSuccess {
		if envelope.Code != 0 || envelope.Message == "" || envelope.ErrorCode != "" {
			t.Fatalf("expected success envelope, got %+v", envelope)
		}
		var raw map[string]json.RawMessage
		decodeBody(t, strings.NewReader(rec.Body.String()), &raw)
		if string(raw["meta"]) != "null" {
			t.Fatalf("success envelope meta = %s, want null", raw["meta"])
		}
		return
	}
	if envelope.Code == 0 || envelope.ErrorCode == "" || envelope.Data != nil {
		t.Fatalf("expected error envelope, got %+v", envelope)
	}
	var raw map[string]json.RawMessage
	decodeBody(t, strings.NewReader(rec.Body.String()), &raw)
	if string(raw["meta"]) != "null" {
		t.Fatalf("error envelope meta = %s, want null", raw["meta"])
	}
}

func dataFieldString(t *testing.T, envelope jsonResponse, key string) string {
	t.Helper()
	data, ok := envelope.Data.(map[string]interface{})
	if !ok {
		t.Fatalf("data is %T, want object: %+v", envelope.Data, envelope.Data)
	}
	value, _ := data[key].(string)
	if value == "" {
		t.Fatalf("data.%s missing: %+v", key, data)
	}
	return value
}

func setAdminBearer(req *http.Request) {
	token, err := createJWT(currentUser{
		ID:          "test-admin",
		Name:        "Test Admin",
		Nickname:    "Test Admin",
		Email:       "admin@test.local",
		DisplayName: "Test Admin",
		IsAdmin:     true,
	}, testJWTSecret, time.Now().UTC())
	if err != nil {
		panic(err)
	}
	req.Header.Set("Authorization", "Bearer "+token)
}

func testAdminToken(t testing.TB) string {
	t.Helper()
	token, err := createJWT(currentUser{
		ID:          "test-admin",
		Name:        "Test Admin",
		Nickname:    "Test Admin",
		Email:       "admin@test.local",
		DisplayName: "Test Admin",
		IsAdmin:     true,
	}, testJWTSecret, time.Now().UTC())
	if err != nil {
		t.Fatalf("create test admin token: %v", err)
	}
	return token
}

func decodeBody(t *testing.T, body io.Reader, dest interface{}) {
	t.Helper()
	if err := json.NewDecoder(body).Decode(dest); err != nil {
		t.Fatalf("decode response: %v", err)
	}
}
