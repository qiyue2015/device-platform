//go:build integration

package main

import (
	"database/sql"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/qiyue2015/device-platform/internal/devicecore"
	"github.com/qiyue2015/device-platform/internal/deviceservice"
	"github.com/qiyue2015/device-platform/internal/gateway"
	"github.com/qiyue2015/device-platform/internal/projectservice"
	"github.com/qiyue2015/device-platform/internal/storage/repository"
	"github.com/qiyue2015/device-platform/internal/webhookaudit"
)

func TestDeviceRoutesSwitchServicesWithoutRouterRebuild(t *testing.T) {
	withAuthTestDatabase(t, func(db *sql.DB) {
		store := repository.NewPostgresStore(db)
		projects, err := projectservice.New(store, projectservice.Config{
			EncryptionKeys: map[int][]byte{1: []byte("0123456789abcdef0123456789abcdef")}, ActiveEncryptionKeyVersion: 1,
		})
		if err != nil {
			t.Fatal(err)
		}
		auth, err := newMemoryAuthenticator("admin@test.local", "Test Admin", "test-admin-password", testJWTSecret)
		if err != nil {
			t.Fatal(err)
		}
		core := devicecore.NewService()
		application := newAppWithServices(
			config{JWTSecret: testJWTSecret, Installed: true, ReadHeaderTimeout: 5 * time.Second},
			slog.New(slog.NewTextHandler(io.Discard, nil)), db, auth, core,
			gateway.NewService(gateway.NewSimulatorGateway(gateway.ModeConfig{}), gateway.ServiceConfig{}),
			webhookaudit.NewService(http.DefaultClient), newCloudProviderRegistry(config{}), projects, nil,
		)
		server := application.routes()
		admin := map[string]string{"Authorization": "Bearer " + loginProjectHTTPTestAdmin(t, server)}

		devices, err := deviceservice.New(store, deviceServiceConfig(config{}))
		if err != nil {
			t.Fatal(err)
		}
		application.setDeviceResourceService(devices)
		assertPaginatedEnvelope(t, doRequest(t, server, http.MethodGet, "/v1/device-types", "", admin), http.StatusOK, 1, 20, 1)
	})
}

func TestDeviceHTTPPostgresLifecycleIsolationAndRestart(t *testing.T) {
	withAuthTestDatabase(t, func(db *sql.DB) {
		server := newProjectHTTPTestServer(t, db)
		admin := map[string]string{"Authorization": "Bearer " + loginProjectHTTPTestAdmin(t, server)}

		deviceTypes := doRequest(t, server, http.MethodGet, "/v1/device-types?page=1&page_size=10", "", admin)
		deviceTypesBody := assertPaginatedEnvelope(t, deviceTypes, http.StatusOK, 1, 10, 1)
		deviceTypeItems := responseDataObject(t, deviceTypesBody)["items"].([]interface{})
		deviceType := deviceTypeItems[0].(map[string]interface{})
		if deviceType["code"] != "smart-lock" || deviceType["revision"] != float64(2) {
			t.Fatalf("Device Type response = %+v", deviceType)
		}
		actions, ok := deviceType["actions"].([]interface{})
		if !ok || len(actions) != 3 {
			t.Fatalf("Device Type actions = %#v", deviceType["actions"])
		}

		providers := doRequest(t, server, http.MethodGet, "/v1/cloud-providers?page=1&page_size=10", "", admin)
		providersBody := assertPaginatedEnvelope(t, providers, http.StatusOK, 1, 10, 3)
		providerItems, ok := responseDataObject(t, providersBody)["items"].([]interface{})
		if !ok || len(providerItems) != 3 {
			t.Fatalf("Provider response = %#v", providersBody.Data)
		}
		if providerItems[0].(map[string]interface{})["code"] != "simulator" ||
			providerItems[1].(map[string]interface{})["code"] != "omni" ||
			providerItems[2].(map[string]interface{})["code"] != "wwtiot" {
			t.Fatalf("Provider order = %#v", providerItems)
		}
		providerPayload := string(mustMarshalJSON(t, providerItems))
		if strings.Contains(providerPayload, "user_key") || strings.Contains(providerPayload, "endpoint") ||
			!strings.Contains(providerPayload, `"integration_status":"unconfigured"`) ||
			!strings.Contains(providerPayload, `"integration_status":"verified"`) {
			t.Fatalf("Provider response = %s", providerPayload)
		}
		assertErrorCode(t, doRequest(t, server, http.MethodGet, "/v1/cloud-providers?configured=true", "", admin), http.StatusBadRequest, "invalid_request")
		assertErrorCode(t, doRequest(t, server, http.MethodGet, "/v1/cloud-providers?page=1&page=2", "", admin), http.StatusBadRequest, "invalid_request")

		firstProject := createPersistentProjectForDeviceTest(t, server, admin, "Device Project One", "https://hooks.example.test/device-one")
		secondProject := createPersistentProjectForDeviceTest(t, server, admin, "Device Project Two", "")
		for _, test := range []struct {
			name string
			body string
			code string
		}{
			{
				name: "unknown Project",
				body: `{"project_id":"10000000-0000-0000-0000-000000000099","name":"Missing Project","device_type_code":"smart-lock","provider_code":"simulator","provider_profile":"simulator-v1"}`,
				code: "not_found",
			},
			{
				name: "unknown Device Type",
				body: `{"project_id":"` + firstProject.ID + `","name":"Missing Type","device_type_code":"missing","provider_code":"simulator","provider_profile":"simulator-v1"}`,
				code: "not_found",
			},
			{
				name: "unknown Provider",
				body: `{"project_id":"` + firstProject.ID + `","name":"Missing Provider","device_type_code":"smart-lock","provider_code":"missing"}`,
				code: "not_found",
			},
			{
				name: "forbidden simulator identity",
				body: `{"project_id":"` + firstProject.ID + `","name":"Bad Simulator","device_type_code":"smart-lock","provider_code":"simulator","provider_profile":"simulator-v1","provider_device_id":"caller-owned"}`,
				code: "invalid_request",
			},
			{
				name: "null simulator identity",
				body: `{"project_id":"` + firstProject.ID + `","name":"Null Simulator","device_type_code":"smart-lock","provider_code":"simulator","provider_profile":"simulator-v1","provider_device_id":null}`,
				code: "invalid_request",
			},
			{
				name: "forbidden metadata",
				body: `{"project_id":"` + firstProject.ID + `","name":"Metadata","device_type_code":"smart-lock","provider_code":"simulator","provider_profile":"simulator-v1","metadata":{"online":true}}`,
				code: "invalid_request",
			},
		} {
			t.Run(test.name, func(t *testing.T) {
				status := http.StatusNotFound
				if test.code == "invalid_request" {
					status = http.StatusBadRequest
				}
				assertErrorCode(t, doRequest(t, server, http.MethodPost, "/v1/devices", test.body, admin), status, test.code)
			})
		}
		assertErrorCode(t, doRequest(t, server, http.MethodGet, "/v1/device-types/missing", "", admin), http.StatusNotFound, "not_found")
		assertErrorCode(t, doRequest(t, server, http.MethodGet, "/v1/device-types?sort=code", "", admin), http.StatusBadRequest, "invalid_request")

		providerDeviceID := "LOCK-HTTP-001"
		created := doRequest(t, server, http.MethodPost, "/v1/devices", `{
			"project_id":"`+firstProject.ID+`",
			"name":"  Front Door  ",
			"device_type_code":"smart-lock",
			"provider_code":"wwtiot",
			"provider_profile":"wwtiot-cloud-api-v2",
			"provider_device_id":"`+providerDeviceID+`"
		}`, admin)
		createdBody := assertEnvelope(t, created, http.StatusCreated, true)
		createdData := responseDataObject(t, createdBody)
		deviceID := requiredStringField(t, createdData, "id")
		if createdData["name"] != "Front Door" || createdData["project_id"] != firstProject.ID ||
			createdData["device_type_code"] != "smart-lock" || createdData["provider_code"] != "wwtiot" ||
			createdData["access_type"] != "cloud_api" || createdData["transport_protocol"] != "http" ||
			createdData["adapter"] != "wwtiot_cloud_api" || createdData["connection_status"] != "unknown" ||
			createdData["lifecycle_status"] != "active" || createdData["current_state"] != nil || createdData["last_seen_at"] != nil {
			t.Fatalf("created Device = %+v", createdData)
		}

		assertDeviceAggregateCounts(t, db, deviceID, 1, 1, 1)
		insertVerifiedDeviceHTTPState(t, db, deviceID, providerDeviceID)
		stateDetail := responseDataObject(t, assertEnvelope(t,
			doRequest(t, server, http.MethodGet, "/v1/devices/"+deviceID, "", admin), http.StatusOK, true))
		currentState, ok := stateDetail["current_state"].(map[string]interface{})
		if !ok || currentState["evidence_status"] != "verified" || currentState["reported_at"] != "2026-07-31T07:59:58Z" ||
			currentState["observed_at"] != "2026-07-31T08:00:00Z" ||
			currentState["state"].(map[string]interface{})["lock_state"] != "locked" || stateDetail["last_seen_at"] != "2026-07-31T08:00:00Z" {
			t.Fatalf("trusted DeviceState response = %+v", stateDetail)
		}
		adminList := doRequest(t, server, http.MethodGet, "/v1/devices?project_id="+firstProject.ID+"&provider_code=wwtiot&page=1&page_size=10", "", admin)
		adminListBody := assertPaginatedEnvelope(t, adminList, http.StatusOK, 1, 10, 1)
		assertDeviceListContainsOnly(t, adminListBody, deviceID)

		openHeaders := map[string]string{"X-API-Key": firstProject.APIKey}
		openList := doRequest(t, server, http.MethodGet, "/v1/open/devices?device_type_code=smart-lock", "", openHeaders)
		openListBody := assertPaginatedEnvelope(t, openList, http.StatusOK, 1, 20, 1)
		assertDeviceListContainsOnly(t, openListBody, deviceID)
		assertEnvelope(t, doRequest(t, server, http.MethodGet, "/v1/open/devices/"+deviceID, "", openHeaders), http.StatusOK, true)
		assertErrorCode(t, doRequest(t, server, http.MethodGet, "/v1/open/devices/"+deviceID, "", map[string]string{"X-API-Key": secondProject.APIKey}), http.StatusNotFound, "not_found")
		assertErrorCode(t, doRequest(t, server, http.MethodGet, "/v1/open/devices?project_id="+firstProject.ID, "", openHeaders), http.StatusBadRequest, "invalid_request")
		assertErrorCode(t, doRequest(t, server, http.MethodPost, "/v1/open/devices", `{}`, openHeaders), http.StatusMethodNotAllowed, "method_not_allowed")

		restarted := newProjectHTTPTestServer(t, db)
		restartedAdmin := map[string]string{"Authorization": "Bearer " + loginProjectHTTPTestAdmin(t, restarted)}
		restartedDetail := doRequest(t, restarted, http.MethodGet, "/v1/devices/"+deviceID, "", restartedAdmin)
		restartedData := responseDataObject(t, assertEnvelope(t, restartedDetail, http.StatusOK, true))
		if restartedData["provider_device_id"] != providerDeviceID {
			t.Fatalf("restarted Device = %+v", restartedData)
		}
		assertErrorCode(t, doRequest(t, restarted, http.MethodPatch, "/v1/devices/"+deviceID, `{"name":null,"lifecycle_status":"disabled"}`, restartedAdmin), http.StatusBadRequest, "invalid_request")
		unchanged := responseDataObject(t, assertEnvelope(t, doRequest(t, restarted, http.MethodGet, "/v1/devices/"+deviceID, "", restartedAdmin), http.StatusOK, true))
		if unchanged["lifecycle_status"] != "active" {
			t.Fatalf("null name PATCH changed Device = %+v", unchanged)
		}

		conflictBody := `{
			"project_id":"` + secondProject.ID + `",
			"name":"Conflicting Lock",
			"device_type_code":"smart-lock",
			"provider_code":"wwtiot",
			"provider_profile":"wwtiot-cloud-api-v2",
			"provider_device_id":"` + providerDeviceID + `"
		}`
		assertErrorCode(t, doRequest(t, restarted, http.MethodPost, "/v1/devices", conflictBody, restartedAdmin), http.StatusConflict, "provider_device_conflict")

		deleted := doRequest(t, restarted, http.MethodPatch, "/v1/devices/"+deviceID, `{"lifecycle_status":"deleted"}`, restartedAdmin)
		deletedData := responseDataObject(t, assertEnvelope(t, deleted, http.StatusOK, true))
		if deletedData["lifecycle_status"] != "deleted" {
			t.Fatalf("deleted Device = %+v", deletedData)
		}
		assertEnvelope(t, doRequest(t, restarted, http.MethodGet, "/v1/open/devices/"+deviceID, "", openHeaders), http.StatusOK, true)

		replacement := doRequest(t, restarted, http.MethodPost, "/v1/devices", conflictBody, restartedAdmin)
		assertErrorCode(t, replacement, http.StatusConflict, "provider_device_conflict")
	})
}

func insertVerifiedDeviceHTTPState(t *testing.T, db *sql.DB, deviceID, providerDeviceID string) {
	t.Helper()
	if _, err := db.Exec(`
		INSERT INTO device_raw_messages (
			id, device_id, provider_code, provider_profile, provider_device_id, access_type, transport_protocol,
			adapter, evidence_status, direction, headers, body, deduplication_key, received_at, created_at
		) VALUES (
			'40000000-0000-0000-0000-000000000009', $1, 'wwtiot', 'wwtiot-cloud-api-v2', $2, 'cloud_api', 'http',
			'wwtiot_cloud_api', 'verified', 'inbound', '{}'::jsonb, '{"lockstatus":0}', 'device-http-state',
			'2026-07-31T08:00:00Z', '2026-07-31T08:00:00Z'
		)
	`, deviceID, providerDeviceID); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`
		INSERT INTO device_states (
			id, device_id, state, evidence_status, reported_at, observed_at, raw_message_id, created_at
		) VALUES (
			'50000000-0000-0000-0000-000000000009', $1, '{"lock_state":"locked"}', 'verified',
			'2026-07-31T07:59:58Z', '2026-07-31T08:00:00Z',
			'40000000-0000-0000-0000-000000000009', '2026-07-31T08:00:00Z'
		)
	`, deviceID); err != nil {
		t.Fatal(err)
	}
}

func TestDeviceHTTPPostgresCreateRollsBackWithDeliveryOrAuditFailure(t *testing.T) {
	withAuthTestDatabase(t, func(db *sql.DB) {
		server := newProjectHTTPTestServer(t, db)
		admin := map[string]string{"Authorization": "Bearer " + loginProjectHTTPTestAdmin(t, server)}
		project := createPersistentProjectForDeviceTest(t, server, admin, "Rollback Project", "https://hooks.example.test/rollback")

		baseline := readDeviceAggregateTotals(t, db)
		if _, err := db.Exec(`ALTER TABLE webhook_deliveries ADD CONSTRAINT reject_device_http_delivery CHECK (status <> 'pending') NOT VALID`); err != nil {
			t.Fatal(err)
		}
		failedDelivery := doRequest(t, server, http.MethodPost, "/v1/devices", `{
			"project_id":"`+project.ID+`","name":"Rollback Delivery","device_type_code":"smart-lock",
			"provider_code":"wwtiot","provider_profile":"wwtiot-cloud-api-v2","provider_device_id":"ROLLBACK-HTTP-DELIVERY"
		}`, admin)
		assertErrorCode(t, failedDelivery, http.StatusInternalServerError, "internal_error")
		assertDeviceAggregateTotals(t, db, baseline)
		if _, err := db.Exec(`ALTER TABLE webhook_deliveries DROP CONSTRAINT reject_device_http_delivery`); err != nil {
			t.Fatal(err)
		}

		if _, err := db.Exec(`ALTER TABLE audit_logs ADD CONSTRAINT reject_device_http_audit CHECK (action <> 'device.created') NOT VALID`); err != nil {
			t.Fatal(err)
		}
		failedAudit := doRequest(t, server, http.MethodPost, "/v1/devices", `{
			"project_id":"`+project.ID+`","name":"Rollback Audit","device_type_code":"smart-lock",
			"provider_code":"wwtiot","provider_profile":"wwtiot-cloud-api-v2","provider_device_id":"ROLLBACK-HTTP-AUDIT"
		}`, admin)
		assertErrorCode(t, failedAudit, http.StatusInternalServerError, "internal_error")
		assertDeviceAggregateTotals(t, db, baseline)
	})
}

type persistentProjectCredentials struct {
	ID     string
	APIKey string
}

func createPersistentProjectForDeviceTest(t *testing.T, server http.Handler, admin map[string]string, name, webhookURL string) persistentProjectCredentials {
	t.Helper()
	body := map[string]any{"name": name, "manager_user_id": authTestAdminID}
	if webhookURL != "" {
		body["webhook_url"] = webhookURL
	}
	raw, err := json.Marshal(body)
	if err != nil {
		t.Fatal(err)
	}
	response := doRequest(t, server, http.MethodPost, "/v1/projects", string(raw), admin)
	data := responseDataObject(t, assertEnvelope(t, response, http.StatusCreated, true))
	return persistentProjectCredentials{ID: requiredStringField(t, data, "id"), APIKey: requiredStringField(t, data, "api_key")}
}

func assertDeviceListContainsOnly(t *testing.T, body jsonResponse, deviceID string) {
	t.Helper()
	items, ok := responseDataObject(t, body)["items"].([]interface{})
	if !ok || len(items) != 1 {
		t.Fatalf("Device list items = %#v", responseDataObject(t, body)["items"])
	}
	item, ok := items[0].(map[string]interface{})
	if !ok || item["id"] != deviceID {
		t.Fatalf("Device list item = %#v", items[0])
	}
}

func assertDeviceAggregateCounts(t *testing.T, db *sql.DB, deviceID string, events, audits, deliveries int) {
	t.Helper()
	var gotEvents, gotAudits, gotDeliveries int
	if err := db.QueryRow(`SELECT count(*) FROM device_events WHERE device_id = $1`, deviceID).Scan(&gotEvents); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRow(`SELECT count(*) FROM audit_logs WHERE resource_type = 'device' AND resource_id = $1`, deviceID).Scan(&gotAudits); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRow(`
		SELECT count(*) FROM webhook_deliveries wd
		JOIN device_events de ON de.id = wd.event_id
		WHERE de.device_id = $1
	`, deviceID).Scan(&gotDeliveries); err != nil {
		t.Fatal(err)
	}
	if gotEvents != events || gotAudits != audits || gotDeliveries != deliveries {
		t.Fatalf("Device aggregate counts events=%d audits=%d deliveries=%d", gotEvents, gotAudits, gotDeliveries)
	}
}

type deviceAggregateTotals struct {
	Devices    int
	Events     int
	Audits     int
	Deliveries int
}

func readDeviceAggregateTotals(t *testing.T, db *sql.DB) deviceAggregateTotals {
	t.Helper()
	var totals deviceAggregateTotals
	if err := db.QueryRow(`
		SELECT
			(SELECT count(*) FROM devices),
			(SELECT count(*) FROM device_events),
			(SELECT count(*) FROM audit_logs WHERE resource_type = 'device'),
			(SELECT count(*) FROM webhook_deliveries)
	`).Scan(&totals.Devices, &totals.Events, &totals.Audits, &totals.Deliveries); err != nil {
		t.Fatal(err)
	}
	return totals
}

func assertDeviceAggregateTotals(t *testing.T, db *sql.DB, want deviceAggregateTotals) {
	t.Helper()
	if got := readDeviceAggregateTotals(t, db); got != want {
		t.Fatalf("Device aggregate totals = %+v, want %+v", got, want)
	}
}
