package main

import (
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/qiyue2015/device-platform/internal/devicecore"
)

const testJWTSecret = defaultMemoryJWTSecret

func TestLoadConfigLoadsEnvFilesWithoutOverridingProcessEnv(t *testing.T) {
	unsetEnvForTest(t, "DATABASE_URL", "REDIS_URL", "JWT_SECRET", "LOG_LEVEL", "READ_HEADER_TIMEOUT", "DEVICE_PLATFORM_INSTALLED")
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
WWTIOT_PROVIDER_CODE=hotel-wwtiot
WWTIOT_PROVIDER_NAME=Hotel WWTIOT
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
	if cfg.DatabaseURL == "" || cfg.RedisURL == "" || cfg.JWTSecret == "" {
		t.Fatalf("expected runtime connection fields from env file, got %+v", cfg)
	}
	if cfg.ReadHeaderTimeout != 3*time.Second {
		t.Fatalf("expected parsed read header timeout, got %s", cfg.ReadHeaderTimeout)
	}
	if cfg.WWTIOTProviderCode != "hotel-wwtiot" || cfg.WWTIOTProviderName != "Hotel WWTIOT" {
		t.Fatalf("unexpected wwtiot provider config: %+v", cfg)
	}
	if cfg.WWTIOTUserID != "env-user" || cfg.WWTIOTUserKey != "env-key" {
		t.Fatal("expected WWTIOT credentials from env file")
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
	assertEnvelope(t, projects, http.StatusOK, true)

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
		ServerAddr:         ":0",
		LogLevel:           "error",
		JWTSecret:          testJWTSecret,
		Installed:          true,
		ReadHeaderTimeout:  5 * time.Second,
		WWTIOTProviderCode: "hotel-wwtiot",
		WWTIOTProviderName: "Hotel WWTIOT",
		WWTIOTAPIURL:       "https://example.invalid/api",
		WWTIOTUserID:       "test-user",
		WWTIOTUserKey:      "secret-key",
		WWTIOTTimeout:      2 * time.Second,
	}
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	server := newAppWithDeviceService(cfg, logger, devicecore.NewService()).routes()

	rec := doRequest(t, server, http.MethodGet, "/v1/cloud-providers", "", map[string]string{"Authorization": "Bearer " + testAdminToken(t)})
	envelope := assertEnvelope(t, rec, http.StatusOK, true)
	payload, err := json.Marshal(envelope.Data)
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
	if len(providers) != 1 ||
		providers[0]["code"] != "hotel-wwtiot" ||
		providers[0]["adapter"] != devicecore.AdapterWWTIOTCloudAPI ||
		providers[0]["configured"] != true {
		t.Fatalf("unexpected providers: %+v", providers)
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
			wantError: `json: unknown field "surprise"`,
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
			if body.ErrorCode != "invalid_argument" && body.ErrorCode != "unknown_field" {
				t.Fatalf("error_code = %q, want stable error code", body.ErrorCode)
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
	decodeResponseData(t, command, &commandBody)
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

func TestCloudAPICommandDispatchesToWWTIOT(t *testing.T) {
	var requestBody map[string]interface{}
	vendor := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Fatalf("vendor method = %s, want POST", r.Method)
		}
		if r.Header.Get("Content-Type") != "application/json" {
			t.Fatalf("content-type = %q, want application/json", r.Header.Get("Content-Type"))
		}
		decodeBody(t, r.Body, &requestBody)
		if requestBody["userid"] != "test-user" ||
			requestBody["cmd"] != "control" ||
			requestBody["deviceid"] != "768901037824" ||
			requestBody["sign"] == "" {
			t.Fatalf("unexpected vendor request: %+v", requestBody)
		}
		serial, _ := requestBody["serialnum"].(float64)
		_, _ = w.Write([]byte(`{"result":"ok","info":"cmd send ok","deviceid":"768901037824","cmd":"control","serialnum":` + strconv.FormatInt(int64(serial), 10) + `,"userid":"test-user","userkey":"vendor-key","sign":"vendor-sign"}`))
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
		WWTIOTTimeout:     2 * time.Second,
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
	if dataFieldString(t, commandBody, "status") != string(devicecore.CommandStatusSuccess) {
		t.Fatalf("command status = %+v, want success", commandBody.Data)
	}
	commandID := dataFieldString(t, commandBody, "id")

	detail := doRequest(t, server, http.MethodGet, "/v1/device-commands/"+commandID+"?project_id="+projectID, "", map[string]string{"Authorization": "Bearer " + token})
	var detailBody struct {
		Command  map[string]interface{}   `json:"command"`
		Attempts []map[string]interface{} `json:"attempts"`
	}
	decodeResponseData(t, detail, &detailBody)
	if detailBody.Command["status"] != string(devicecore.CommandStatusSuccess) {
		t.Fatalf("detail status = %+v, want success", detailBody.Command)
	}
	if len(detailBody.Attempts) != 1 {
		t.Fatalf("attempts = %+v, want one attempt", detailBody.Attempts)
	}
	attempt := detailBody.Attempts[0]
	if attempt["adapter"] != devicecore.AdapterWWTIOTCloudAPI || attempt["status"] != "acked" {
		t.Fatalf("unexpected attempt: %+v", attempt)
	}
	request, ok := attempt["request_body"].(map[string]interface{})
	if !ok {
		t.Fatalf("request_body = %T, want object", attempt["request_body"])
	}
	body, ok := request["body"].(map[string]interface{})
	if !ok || body["sign"] != "[redacted]" || body["userid"] != "[redacted]" {
		t.Fatalf("request body must redact credentials, got %+v", request["body"])
	}
	response, ok := attempt["response_body"].(map[string]interface{})
	if !ok || response["sign"] != "[redacted]" || response["userid"] != "[redacted]" || response["userkey"] != "[redacted]" || response["result"] != "ok" {
		t.Fatalf("response body must include ok result and redacted credentials, got %+v", attempt["response_body"])
	}
	attemptJSON, err := json.Marshal(attempt)
	if err != nil {
		t.Fatalf("marshal attempt: %v", err)
	}
	for _, secret := range []string{"test-user", "test-key", "vendor-sign", "vendor-key"} {
		if strings.Contains(string(attemptJSON), secret) {
			t.Fatalf("attempt detail leaked %q: %s", secret, attemptJSON)
		}
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
