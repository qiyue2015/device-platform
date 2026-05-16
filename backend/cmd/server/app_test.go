package main

import (
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/qiyue2015/device-platform/internal/devicecore"
)

const testJWTSecret = defaultMemoryJWTSecret

func TestLoadConfigLoadsEnvFilesWithoutOverridingProcessEnv(t *testing.T) {
	t.Setenv("SERVER_ADDR", ":9090")
	t.Setenv("INSTALL_LOCK_PATH", filepath.Join(t.TempDir(), ".installed"))

	dir := t.TempDir()
	envPath := filepath.Join(dir, ".env")
	if err := os.WriteFile(envPath, []byte(`
SERVER_ADDR=:8081
DATABASE_URL=postgres://postgres:postgres@localhost:5432/device_platform?sslmode=disable
REDIS_URL=redis://localhost:6379/0
JWT_SECRET=0123456789abcdef0123456789abcdef
LOG_LEVEL=debug
READ_HEADER_TIMEOUT=3s
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
	if cfg.DatabaseURL == "" || cfg.RedisURL == "" || cfg.JWTSecret == "" {
		t.Fatalf("expected runtime connection fields from env file, got %+v", cfg)
	}
	if cfg.ReadHeaderTimeout != 3*time.Second {
		t.Fatalf("expected parsed read header timeout, got %s", cfg.ReadHeaderTimeout)
	}
}

func TestLoadConfigRequiresRuntimeFieldsAfterInstall(t *testing.T) {
	t.Setenv("INSTALL_LOCK_PATH", filepath.Join(t.TempDir(), ".installed"))
	t.Setenv("DEVICE_PLATFORM_INSTALLED", "true")
	t.Setenv("DATABASE_URL", "")
	t.Setenv("REDIS_URL", "")
	t.Setenv("JWT_SECRET", "")

	_, err := loadConfig()
	if err == nil || !strings.Contains(err.Error(), "DATABASE_URL") {
		t.Fatalf("expected DATABASE_URL error after install, got %v", err)
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
	decodeResponse(t, allowed, &body)
	if body.ID != projectID {
		t.Fatalf("expected open api project id %q, got %+v", projectID, body)
	}
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

func TestCreateDeviceAcceptsMVP15AccessFields(t *testing.T) {
	server := newTestServer()
	projectID := createProjectForTest(t, server)

	cases := []struct {
		name              string
		body              string
		wantAccessType    string
		wantProtocol      string
		wantAdapter       string
		wantProviderCode  string
		wantProviderID    string
		wantConnection    string
		wantLifecycle     string
		wantMetadataModel string
	}{
		{
			name: "cloud api wwtiot",
			body: `{
				"project_id":"` + projectID + `",
				"name":"Smoke Test Lock",
				"device_type":"smart_lock",
				"access_type":"cloud_api",
				"provider_code":"wwtiot",
				"provider_device_id":"111",
				"transport_protocol":"http",
				"adapter":"wwtiot_cloud_api",
				"metadata":{"model":"wwtiot-lock"}
			}`,
			wantAccessType:    "cloud_api",
			wantProtocol:      "http",
			wantAdapter:       "wwtiot_cloud_api",
			wantProviderCode:  "wwtiot",
			wantProviderID:    "111",
			wantConnection:    "unknown",
			wantLifecycle:     "active",
			wantMetadataModel: "wwtiot-lock",
		},
		{
			name: "mock gateway simulator",
			body: `{
				"project_id":"` + projectID + `",
				"name":"Simulator Lock",
				"device_type":"smart_lock",
				"access_type":"mock_gateway",
				"provider_code":"simulator",
				"provider_device_id":"sim-001",
				"transport_protocol":"simulator",
				"adapter":"mock_gateway",
				"online":true
			}`,
			wantAccessType:   "mock_gateway",
			wantProtocol:     "simulator",
			wantAdapter:      "mock_gateway",
			wantProviderCode: "simulator",
			wantProviderID:   "sim-001",
			wantConnection:   "online",
			wantLifecycle:    "active",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rec := httptest.NewRecorder()
			req := httptest.NewRequest(http.MethodPost, "/v1/devices", strings.NewReader(tc.body))
			req.Header.Set("Content-Type", "application/json")
			setAdminBearer(req)
			server.ServeHTTP(rec, req)
			if rec.Code != http.StatusCreated {
				t.Fatalf("expected device 201, got %d: %s", rec.Code, rec.Body.String())
			}
			var device map[string]interface{}
			decodeResponse(t, rec, &device)
			if device["access_type"] != tc.wantAccessType ||
				device["transport_protocol"] != tc.wantProtocol ||
				device["adapter"] != tc.wantAdapter ||
				device["provider_code"] != tc.wantProviderCode ||
				device["provider_device_id"] != tc.wantProviderID ||
				device["connection_status"] != tc.wantConnection ||
				device["lifecycle_status"] != tc.wantLifecycle {
				t.Fatalf("unexpected device response: %+v", device)
			}
			if tc.wantMetadataModel != "" {
				metadata, ok := device["metadata"].(map[string]interface{})
				if !ok || metadata["model"] != tc.wantMetadataModel {
					t.Fatalf("metadata = %+v, want model %q", device["metadata"], tc.wantMetadataModel)
				}
			}
		})
	}
}

func TestCreateDeviceReportsContractErrorsWithoutInvalidJSON(t *testing.T) {
	server := newTestServer()
	projectID := createProjectForTest(t, server)

	cases := []struct {
		name      string
		body      string
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
			wantError: "unknown_field",
		},
		{
			name: "invalid enum",
			body: `{
				"project_id":"` + projectID + `",
				"name":"Bad Adapter Lock",
				"device_type":"smart_lock",
				"access_type":"cloud_api",
				"provider_code":"wwtiot",
				"provider_device_id":"111",
				"transport_protocol":"http",
				"adapter":"cloud_api"
			}`,
			wantError: "invalid_argument: unsupported adapter",
		},
		{
			name: "mismatched access adapter",
			body: `{
				"project_id":"` + projectID + `",
				"name":"Mismatched Adapter Lock",
				"device_type":"smart_lock",
				"access_type":"cloud_api",
				"provider_code":"wwtiot",
				"provider_device_id":"111",
				"transport_protocol":"http",
				"adapter":"mock_gateway"
			}`,
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
			var body map[string]string
			decodeResponse(t, rec, &body)
			if body["error"] != tc.wantError {
				t.Fatalf("error = %q, want %q", body["error"], tc.wantError)
			}
			if body["error"] == "invalid_json" {
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
		{method: http.MethodPost, path: "/v1/projects/webhook-endpoints", body: `{"project_id":"proj_1","webhook_url":"https://example.invalid/hook"}`},
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

func TestCommandCreationRecordsWebhookDeliveryAndAudit(t *testing.T) {
	server := newTestServer()

	projectID := createProjectForTest(t, server)
	configureWebhookForTest(t, server, projectID)
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
	decodeResponse(t, command, &commandBody)
	commandID, _ := commandBody["id"].(string)
	if commandID == "" {
		t.Fatalf("command id missing: %+v", commandBody)
	}
	deliveryBody := waitForWebhookDelivery(t, server)
	if len(deliveryBody.Items) == 0 {
		t.Fatal("expected command event to create a webhook delivery")
	}

	audits := httptest.NewRecorder()
	auditReq := httptest.NewRequest(http.MethodGet, "/v1/audit-logs", nil)
	setAdminBearer(auditReq)
	server.ServeHTTP(audits, auditReq)
	var auditBody struct {
		Items []map[string]interface{} `json:"items"`
	}
	decodeResponse(t, audits, &auditBody)
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
		decodeResponse(t, deliveries, &last)
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
	decodeResponse(t, rec, &project)
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
	decodeResponse(t, rec, &project)
	id, _ := project["id"].(string)
	apiKey, _ := project["api_key"].(string)
	if id == "" || apiKey == "" {
		t.Fatalf("project open api fields missing: %+v", project)
	}
	return id, apiKey
}

func configureWebhookForTest(t *testing.T, server http.Handler, projectID string) {
	t.Helper()
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/v1/projects/webhook-endpoints", strings.NewReader(`{
		"project_id":"`+projectID+`",
		"webhook_url":"https://example.invalid/device-webhook",
		"webhook_secret":"test-secret"
	}`))
	req.Header.Set("Content-Type", "application/json")
	setAdminBearer(req)
	server.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected webhook config 200, got %d: %s", rec.Code, rec.Body.String())
	}
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
	decodeResponse(t, rec, &device)
	id, _ := device["id"].(string)
	if id == "" {
		t.Fatalf("device id missing: %+v", device)
	}
	return id
}

func decodeResponse(t *testing.T, body *httptest.ResponseRecorder, dest interface{}) {
	t.Helper()
	decodeBody(t, body.Body, dest)
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

func decodeBody(t *testing.T, body io.Reader, dest interface{}) {
	t.Helper()
	if err := json.NewDecoder(body).Decode(dest); err != nil {
		t.Fatalf("decode response: %v", err)
	}
}
