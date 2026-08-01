//go:build integration

package main

import (
	"database/sql"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/qiyue2015/device-platform/internal/commandservice"
	"github.com/qiyue2015/device-platform/internal/devicecore"
	"github.com/qiyue2015/device-platform/internal/deviceservice"
	"github.com/qiyue2015/device-platform/internal/gateway"
	"github.com/qiyue2015/device-platform/internal/projectservice"
	"github.com/qiyue2015/device-platform/internal/storage/repository"
	"github.com/qiyue2015/device-platform/internal/webhookaudit"
)

func TestProjectHTTPPostgresLifecycleAndOpenAuthentication(t *testing.T) {
	withAuthTestDatabase(t, func(db *sql.DB) {
		server := newProjectHTTPTestServer(t, db)
		token := loginProjectHTTPTestAdmin(t, server)
		admin := map[string]string{"Authorization": "Bearer " + token}
		webhookURL := "https://hooks.example.test/device-events"

		created := doRequest(t, server, http.MethodPost, "/v1/projects", `{
			"name":"  Project Alpha  ",
			"webhook_url":"`+webhookURL+`",
			"ip_whitelist":["192.0.2.42/24","192.0.2.0/24","2001:db8::/64"]
		}`, admin)
		createdBody := assertEnvelope(t, created, http.StatusCreated, true)
		createdData := responseDataObject(t, createdBody)
		projectID := requiredStringField(t, createdData, "id")
		apiKey := requiredStringField(t, createdData, "api_key")
		webhookSecret := requiredStringField(t, createdData, "webhook_secret")
		if !strings.HasPrefix(apiKey, "dp_") || !strings.HasPrefix(webhookSecret, "whsec_") {
			t.Fatalf("credential prefixes api_key=%q webhook_secret=%q", apiKey, webhookSecret)
		}
		if createdData["name"] != "Project Alpha" || createdData["webhook_configured"] != true {
			t.Fatalf("created Project was not normalized: %+v", createdData)
		}
		whitelist, ok := createdData["ip_whitelist"].([]interface{})
		if !ok || len(whitelist) != 2 || whitelist[0] != "192.0.2.0/24" || whitelist[1] != "2001:db8::/64" {
			t.Fatalf("canonical IP whitelist = %#v", createdData["ip_whitelist"])
		}

		detail := doRequest(t, server, http.MethodGet, "/v1/projects/"+projectID, "", admin)
		detailBody := assertEnvelope(t, detail, http.StatusOK, true)
		assertNoCredentialDisclosure(t, responseDataObject(t, detailBody), apiKey, webhookSecret)

		list := doRequest(t, server, http.MethodGet, "/v1/projects?name=Project%20Alpha&page=1&page_size=10", "", admin)
		listBody := assertPaginatedEnvelope(t, list, http.StatusOK, 1, 10, 1)
		listData := responseDataObject(t, listBody)
		items, ok := listData["items"].([]interface{})
		if !ok || len(items) != 1 {
			t.Fatalf("Project list items = %#v", listData["items"])
		}
		item, ok := items[0].(map[string]interface{})
		if !ok {
			t.Fatalf("Project list item = %T", items[0])
		}
		assertNoCredentialDisclosure(t, item, apiKey, webhookSecret)

		for _, test := range []struct {
			name   string
			method string
			path   string
			body   string
			status int
			code   string
		}{
			{name: "unknown list query", method: http.MethodGet, path: "/v1/projects?sort=name", status: http.StatusBadRequest, code: "invalid_request"},
			{name: "malformed list query", method: http.MethodGet, path: "/v1/projects?page=1;page_size=2", status: http.StatusBadRequest, code: "invalid_request"},
			{name: "duplicate list query", method: http.MethodGet, path: "/v1/projects?page=1&page=2", status: http.StatusBadRequest, code: "invalid_request"},
			{name: "invalid page", method: http.MethodGet, path: "/v1/projects?page=0", status: http.StatusBadRequest, code: "invalid_request"},
			{name: "empty page", method: http.MethodGet, path: "/v1/projects?page=", status: http.StatusBadRequest, code: "invalid_request"},
			{name: "empty page size", method: http.MethodGet, path: "/v1/projects?page_size=", status: http.StatusBadRequest, code: "invalid_request"},
			{name: "empty patch", method: http.MethodPatch, path: "/v1/projects/" + projectID, body: `{}`, status: http.StatusBadRequest, code: "invalid_request"},
			{name: "null name", method: http.MethodPatch, path: "/v1/projects/" + projectID, body: `{"name":null}`, status: http.StatusBadRequest, code: "invalid_request"},
			{name: "null whitelist", method: http.MethodPatch, path: "/v1/projects/" + projectID, body: `{"ip_whitelist":null}`, status: http.StatusBadRequest, code: "invalid_request"},
			{name: "unknown patch field", method: http.MethodPatch, path: "/v1/projects/" + projectID, body: `{"api_key":"forbidden"}`, status: http.StatusBadRequest, code: "invalid_request"},
			{name: "rotation method", method: http.MethodGet, path: "/v1/projects/" + projectID + "/api-key/rotate", status: http.StatusMethodNotAllowed, code: "method_not_allowed"},
			{name: "rotation unknown body", method: http.MethodPost, path: "/v1/projects/" + projectID + "/api-key/rotate", body: `{"reason":"ignored"}`, status: http.StatusBadRequest, code: "invalid_request"},
		} {
			t.Run(test.name, func(t *testing.T) {
				response := doRequest(t, server, test.method, test.path, test.body, admin)
				assertErrorCode(t, response, test.status, test.code)
			})
		}

		spoofed := httptest.NewRecorder()
		spoofedRequest := httptest.NewRequest(http.MethodGet, "/v1/open/projects/"+projectID, nil)
		spoofedRequest.RemoteAddr = "198.51.100.9:4321"
		spoofedRequest.Header.Set("X-API-Key", apiKey)
		spoofedRequest.Header.Set("X-Forwarded-For", "192.0.2.9")
		spoofedRequest.Header.Set("Forwarded", "for=192.0.2.9")
		server.ServeHTTP(spoofed, spoofedRequest)
		assertErrorCode(t, spoofed, http.StatusForbidden, "forbidden")

		spacedKey := doRequest(t, server, http.MethodGet, "/v1/open/projects/"+projectID, "", map[string]string{"X-API-Key": " " + apiKey})
		assertErrorCode(t, spacedKey, http.StatusUnauthorized, "unauthorized")

		restarted := newProjectHTTPTestServer(t, db)
		allowedIPv6 := httptest.NewRecorder()
		allowedIPv6Request := httptest.NewRequest(http.MethodGet, "/v1/open/projects/"+projectID, nil)
		allowedIPv6Request.RemoteAddr = "[2001:db8::7]:4321"
		allowedIPv6Request.Header.Set("X-API-Key", apiKey)
		restarted.ServeHTTP(allowedIPv6, allowedIPv6Request)
		assertEnvelope(t, allowedIPv6, http.StatusOK, true)

		restartedToken := loginProjectHTTPTestAdmin(t, restarted)
		restartedAdmin := map[string]string{"Authorization": "Bearer " + restartedToken}
		rotatedKeyResponse := doRequest(t, restarted, http.MethodPost, "/v1/projects/"+projectID+"/api-key/rotate", `{}`, restartedAdmin)
		rotatedKeyBody := assertEnvelope(t, rotatedKeyResponse, http.StatusOK, true)
		rotatedKeyData := responseDataObject(t, rotatedKeyBody)
		rotatedAPIKey := requiredStringField(t, rotatedKeyData, "api_key")
		delete(rotatedKeyData, "api_key")
		assertNoCredentialDisclosure(t, rotatedKeyData, webhookSecret)

		oldKey := doRequest(t, restarted, http.MethodGet, "/v1/open/projects/"+projectID, "", map[string]string{"X-API-Key": apiKey})
		assertErrorCode(t, oldKey, http.StatusUnauthorized, "unauthorized")
		newKey := doRequest(t, restarted, http.MethodGet, "/v1/open/projects/"+projectID, "", map[string]string{"X-API-Key": rotatedAPIKey})
		assertEnvelope(t, newKey, http.StatusOK, true)

		rotatedWebhookResponse := doRequest(t, restarted, http.MethodPost, "/v1/projects/"+projectID+"/webhook-secret/rotate", `{}`, restartedAdmin)
		rotatedWebhookBody := assertEnvelope(t, rotatedWebhookResponse, http.StatusOK, true)
		rotatedWebhookData := responseDataObject(t, rotatedWebhookBody)
		rotatedWebhookSecret := requiredStringField(t, rotatedWebhookData, "webhook_secret")
		if rotatedWebhookSecret == webhookSecret {
			t.Fatal("Webhook secret rotation returned the old secret")
		}
		delete(rotatedWebhookData, "webhook_secret")
		assertNoCredentialDisclosure(t, rotatedWebhookData, rotatedAPIKey)

		disabled := doRequest(t, restarted, http.MethodPatch, "/v1/projects/"+projectID, `{"webhook_url":null}`, restartedAdmin)
		disabledBody := assertEnvelope(t, disabled, http.StatusOK, true)
		disabledData := responseDataObject(t, disabledBody)
		if disabledData["webhook_configured"] != false {
			t.Fatalf("disabled webhook response = %+v", disabledData)
		}
		assertNoCredentialDisclosure(t, disabledData, rotatedAPIKey, rotatedWebhookSecret)
		rotateDisabled := doRequest(t, restarted, http.MethodPost, "/v1/projects/"+projectID+"/webhook-secret/rotate", `{}`, restartedAdmin)
		assertErrorCode(t, rotateDisabled, http.StatusConflict, "webhook_not_configured")

		reenabled := doRequest(t, restarted, http.MethodPatch, "/v1/projects/"+projectID, `{"webhook_url":"`+webhookURL+`"}`, restartedAdmin)
		reenabledBody := assertEnvelope(t, reenabled, http.StatusOK, true)
		reenabledData := responseDataObject(t, reenabledBody)
		if reenabledData["webhook_configured"] != true {
			t.Fatalf("re-enabled webhook response = %+v", reenabledData)
		}
		assertNoCredentialDisclosure(t, reenabledData, rotatedAPIKey, rotatedWebhookSecret)

		withoutWebhook := doRequest(t, restarted, http.MethodPost, "/v1/projects", `{"name":"No Webhook"}`, restartedAdmin)
		withoutWebhookBody := assertEnvelope(t, withoutWebhook, http.StatusCreated, true)
		withoutWebhookData := responseDataObject(t, withoutWebhookBody)
		withoutWebhookID := requiredStringField(t, withoutWebhookData, "id")
		if _, exists := withoutWebhookData["webhook_secret"]; exists {
			t.Fatalf("Project without endpoint disclosed webhook_secret: %+v", withoutWebhookData)
		}
		firstEndpoint := doRequest(t, restarted, http.MethodPatch, "/v1/projects/"+withoutWebhookID, `{"webhook_url":"https://hooks.example.test/first"}`, restartedAdmin)
		firstEndpointBody := assertEnvelope(t, firstEndpoint, http.StatusOK, true)
		firstEndpointSecret := requiredStringField(t, responseDataObject(t, firstEndpointBody), "webhook_secret")
		if !strings.HasPrefix(firstEndpointSecret, "whsec_") {
			t.Fatalf("first endpoint secret = %q", firstEndpointSecret)
		}

		var projectAudits, leakedAudits int
		if err := db.QueryRow(`SELECT count(*) FROM audit_logs WHERE resource_type = 'project'`).Scan(&projectAudits); err != nil {
			t.Fatal(err)
		}
		if projectAudits != 7 {
			t.Fatalf("Project audit count = %d, want 7", projectAudits)
		}
		if err := db.QueryRow(`
			SELECT count(*) FROM audit_logs
			WHERE metadata::text LIKE '%' || $1 || '%'
			   OR metadata::text LIKE '%' || $2 || '%'
		`, apiKey, webhookSecret).Scan(&leakedAudits); err != nil {
			t.Fatal(err)
		}
		if leakedAudits != 0 {
			t.Fatalf("credential plaintext leaked into %d Audit rows", leakedAudits)
		}
	})
}

func newProjectHTTPTestServer(t *testing.T, db *sql.DB) http.Handler {
	t.Helper()
	projects, err := projectservice.New(repository.NewPostgresStore(db), projectservice.Config{
		EncryptionKeys: map[int][]byte{1: []byte("0123456789abcdef0123456789abcdef")}, ActiveEncryptionKeyVersion: 1,
	})
	if err != nil {
		t.Fatal(err)
	}
	devices, err := deviceservice.New(repository.NewPostgresStore(db), deviceServiceConfig(config{}))
	if err != nil {
		t.Fatal(err)
	}
	commands, err := commandservice.New(repository.NewPostgresStore(db), commandServiceConfig(devices))
	if err != nil {
		t.Fatal(err)
	}
	auth, err := newMemoryAuthenticator("admin@test.local", "Test Admin", "test-admin-password", testJWTSecret)
	if err != nil {
		t.Fatal(err)
	}
	deviceService := devicecore.NewService()
	providerRegistry := newCloudProviderRegistry(config{})
	application := newAppWithServices(
		config{JWTSecret: testJWTSecret, Installed: true, ReadHeaderTimeout: 5 * time.Second},
		slog.New(slog.NewTextHandler(io.Discard, nil)), db, auth, deviceService,
		gateway.NewService(gateway.NewSimulatorGateway(gateway.ModeConfig{}), gateway.ServiceConfig{}),
		webhookaudit.NewService(http.DefaultClient), providerRegistry, projects, devices,
	)
	application.setCommandResourceService(commands)
	return application.routes()
}

func loginProjectHTTPTestAdmin(t *testing.T, server http.Handler) string {
	t.Helper()
	response := doRequest(t, server, http.MethodPost, "/v1/auth/login", `{"email":"admin@test.local","password":"test-admin-password"}`, nil)
	body := assertEnvelope(t, response, http.StatusOK, true)
	return dataFieldString(t, body, "access_token")
}

func responseDataObject(t *testing.T, response jsonResponse) map[string]interface{} {
	t.Helper()
	data, ok := response.Data.(map[string]interface{})
	if !ok {
		t.Fatalf("response data = %T, want object", response.Data)
	}
	return data
}

func requiredStringField(t *testing.T, data map[string]interface{}, field string) string {
	t.Helper()
	value, ok := data[field].(string)
	if !ok || value == "" {
		t.Fatalf("%s missing from response: %+v", field, data)
	}
	return value
}

func assertNoCredentialDisclosure(t *testing.T, data map[string]interface{}, secrets ...string) {
	t.Helper()
	for _, field := range []string{"api_key", "api_key_hash", "api_key_digest", "webhook_secret", "webhook_secret_hash", "ciphertext", "nonce"} {
		if _, exists := data[field]; exists {
			t.Fatalf("ordinary Project response exposed %q: %+v", field, data)
		}
	}
	raw := string(mustMarshalJSON(t, data))
	for _, secret := range secrets {
		if secret != "" && strings.Contains(raw, secret) {
			t.Fatalf("ordinary Project response leaked credential plaintext")
		}
	}
}

func assertErrorCode(t *testing.T, response *httptest.ResponseRecorder, status int, code string) {
	t.Helper()
	body := assertEnvelope(t, response, status, false)
	if body.ErrorCode != code {
		t.Fatalf("error_code = %q, want %q: %s", body.ErrorCode, code, response.Body.String())
	}
}

func mustMarshalJSON(t *testing.T, value interface{}) []byte {
	t.Helper()
	data, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	return data
}
