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

func TestLoadConfigLoadsEnvFilesWithoutOverridingProcessEnv(t *testing.T) {
	t.Setenv("SERVER_ADDR", ":9090")

	dir := t.TempDir()
	envPath := filepath.Join(dir, ".env")
	if err := os.WriteFile(envPath, []byte(`
SERVER_ADDR=:8081
ADMIN_EMAIL=file-admin@example.com
ADMIN_PASSWORD=file-admin-password
ADMIN_ACCESS_TOKEN=file-token
OPEN_API_KEYS=project-a:key-a,project-b:key-b
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
	if cfg.AdminAccessToken != "file-token" {
		t.Fatalf("expected admin token from env file, got %q", cfg.AdminAccessToken)
	}
	if cfg.AdminEmail != "file-admin@example.com" || cfg.AdminPassword != "file-admin-password" {
		t.Fatalf("expected admin credentials from env file, got %q/%q", cfg.AdminEmail, cfg.AdminPassword)
	}
	if cfg.OpenAPIKeys["key-b"] != "project-b" {
		t.Fatalf("expected parsed open api project id, got %q", cfg.OpenAPIKeys["key-b"])
	}
	if cfg.ReadHeaderTimeout != 3*time.Second {
		t.Fatalf("expected parsed read header timeout, got %s", cfg.ReadHeaderTimeout)
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
	server.ServeHTTP(missingCredentials, httptest.NewRequest(http.MethodPost, "/api/auth/login", nil))
	if missingCredentials.Code != http.StatusUnauthorized {
		t.Fatalf("expected missing credentials 401, got %d", missingCredentials.Code)
	}

	invalidCredentials := httptest.NewRecorder()
	server.ServeHTTP(invalidCredentials, httptest.NewRequest(http.MethodPost, "/api/auth/login", strings.NewReader(`{"email":"admin@example.com","password":"wrong-password"}`)))
	if invalidCredentials.Code != http.StatusUnauthorized {
		t.Fatalf("expected invalid credentials 401, got %d", invalidCredentials.Code)
	}

	login := httptest.NewRecorder()
	server.ServeHTTP(login, httptest.NewRequest(http.MethodPost, "/api/auth/login", strings.NewReader(`{"email":"admin@example.com","password":"test-admin-password"}`)))
	if login.Code != http.StatusOK {
		t.Fatalf("expected login 200, got %d", login.Code)
	}
	var loginBody jsonResponse
	decodeResponse(t, login, &loginBody)
	data, ok := loginBody.Data.(map[string]interface{})
	if !ok || data["access_token"] != "test-admin-token" || data["token_type"] != "Bearer" {
		t.Fatalf("unexpected login response: %+v", loginBody.Data)
	}

	legacyMe := httptest.NewRecorder()
	legacyReq := httptest.NewRequest(http.MethodGet, "/api/me", nil)
	legacyReq.Header.Set("Authorization", "Bearer test-admin-token")
	server.ServeHTTP(legacyMe, legacyReq)
	if legacyMe.Code != http.StatusOK {
		t.Fatalf("expected legacy me 200, got %d", legacyMe.Code)
	}

	blocked := httptest.NewRecorder()
	server.ServeHTTP(blocked, httptest.NewRequest(http.MethodGet, "/v1/auth/me", nil))
	if blocked.Code != http.StatusUnauthorized {
		t.Fatalf("expected v1 me without bearer 401, got %d", blocked.Code)
	}

	allowed := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/v1/auth/me", nil)
	req.Header.Set("Authorization", "Bearer test-admin-token")
	server.ServeHTTP(allowed, req)
	if allowed.Code != http.StatusOK {
		t.Fatalf("expected v1 me with bearer 200, got %d", allowed.Code)
	}

	refreshBlocked := httptest.NewRecorder()
	server.ServeHTTP(refreshBlocked, httptest.NewRequest(http.MethodPost, "/api/auth/refresh", nil))
	if refreshBlocked.Code != http.StatusUnauthorized {
		t.Fatalf("expected refresh without bearer 401, got %d", refreshBlocked.Code)
	}

	refreshAllowed := httptest.NewRecorder()
	refreshReq := httptest.NewRequest(http.MethodPost, "/api/auth/refresh", nil)
	refreshReq.Header.Set("Authorization", "Bearer test-admin-token")
	server.ServeHTTP(refreshAllowed, refreshReq)
	if refreshAllowed.Code != http.StatusOK {
		t.Fatalf("expected refresh with bearer 200, got %d", refreshAllowed.Code)
	}
}

func TestOpenAPIKeyGate(t *testing.T) {
	service := devicecore.NewService()
	project, err := service.CreateProject(devicecore.CreateProjectRequest{Name: "Hotel A"})
	if err != nil {
		t.Fatalf("create project: %v", err)
	}
	server := newTestServerWithDeviceService(service)

	blocked := httptest.NewRecorder()
	server.ServeHTTP(blocked, httptest.NewRequest(http.MethodGet, "/v1/open/projects/"+project.ID, nil))
	if blocked.Code != http.StatusUnauthorized {
		t.Fatalf("expected missing api key 401, got %d", blocked.Code)
	}

	allowed := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/v1/open/projects/"+project.ID, nil)
	req.Header.Set("X-API-Key", project.APIKey)
	server.ServeHTTP(allowed, req)
	if allowed.Code != http.StatusOK {
		t.Fatalf("expected api key 200, got %d", allowed.Code)
	}
	var body devicecore.Project
	decodeResponse(t, allowed, &body)
	if body.ID != project.ID {
		t.Fatalf("expected project id %q, got %+v", project.ID, body)
	}
}

func TestDeviceRoutesPreserveAppFoundation(t *testing.T) {
	service := devicecore.NewService()
	project, err := service.CreateProject(devicecore.CreateProjectRequest{Name: "Hotel A"})
	if err != nil {
		t.Fatalf("create project: %v", err)
	}
	device, err := service.CreateDevice(devicecore.CreateDeviceRequest{
		ProjectID:  project.ID,
		Name:       "Front Door",
		DeviceType: "smart_lock",
		Online:     true,
	})
	if err != nil {
		t.Fatalf("create device: %v", err)
	}

	server := newTestServerWithDeviceService(service)

	ready := httptest.NewRecorder()
	server.ServeHTTP(ready, httptest.NewRequest(http.MethodGet, "/readyz", nil))
	if ready.Code != http.StatusOK {
		t.Fatalf("expected readyz 200, got %d", ready.Code)
	}

	login := httptest.NewRecorder()
	server.ServeHTTP(login, httptest.NewRequest(http.MethodPost, "/api/auth/login", strings.NewReader(`{"email":"admin@example.com","password":"test-admin-password"}`)))
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
	adminReq.Header.Set("Authorization", "Bearer test-admin-token")
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
	openReq := httptest.NewRequest(http.MethodPost, "/v1/open/device-commands", strings.NewReader(`{"device_id":"`+device.ID+`","command_type":"query_status"}`))
	openReq.Header.Set("X-API-Key", project.APIKey)
	server.ServeHTTP(openCreate, openReq)
	if openCreate.Code != http.StatusCreated {
		t.Fatalf("expected open command create 201, got %d body=%s", openCreate.Code, openCreate.Body.String())
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

func newTestServer() http.Handler {
	return newTestServerWithDeviceService(devicecore.NewService())
}

func newTestServerWithDeviceService(service *devicecore.Service) http.Handler {
	cfg := config{
		ServerAddr:        ":0",
		LogLevel:          "error",
		AdminEmail:        "admin@example.com",
		AdminPassword:     "test-admin-password",
		AdminAccessToken:  "test-admin-token",
		OpenAPIKeys:       map[string]string{"test-open-key": "local-project"},
		ReadHeaderTimeout: 5 * time.Second,
	}
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	return newAppWithDeviceService(cfg, logger, service).routes()
}

func decodeResponse(t *testing.T, body *httptest.ResponseRecorder, dest interface{}) {
	t.Helper()
	decodeBody(t, body.Body, dest)
}

func decodeBody(t *testing.T, body io.Reader, dest interface{}) {
	t.Helper()
	if err := json.NewDecoder(body).Decode(dest); err != nil {
		t.Fatalf("decode response: %v", err)
	}
}
