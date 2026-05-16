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
	server := newTestServer()

	blocked := httptest.NewRecorder()
	server.ServeHTTP(blocked, httptest.NewRequest(http.MethodGet, "/v1/open/projects/local-project", nil))
	if blocked.Code != http.StatusUnauthorized {
		t.Fatalf("expected missing api key 401, got %d", blocked.Code)
	}

	allowed := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/v1/open/projects/local-project", nil)
	req.Header.Set("X-API-Key", "test-open-key")
	server.ServeHTTP(allowed, req)
	if allowed.Code != http.StatusOK {
		t.Fatalf("expected api key 200, got %d", allowed.Code)
	}
	var body jsonResponse
	decodeResponse(t, allowed, &body)
	data, ok := body.Data.(map[string]interface{})
	if !ok || data["project_id"] != "local-project" {
		t.Fatalf("expected project id in open api context, got %+v", body.Data)
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
	return newApp(cfg, logger).routes()
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
