//go:build integration

package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"sort"
	"strings"
	"sync"
	"testing"

	"github.com/qiyue2015/device-platform/internal/domain"
	"github.com/qiyue2015/device-platform/internal/storage/repository"
)

func TestCommandHTTPPostgresLifecycleIsolationAndValidation(t *testing.T) {
	withAuthTestDatabase(t, func(db *sql.DB) {
		server := newProjectHTTPTestServer(t, db)
		admin := map[string]string{"Authorization": "Bearer " + loginProjectHTTPTestAdmin(t, server)}
		firstProject := createPersistentProjectForDeviceTest(t, server, admin, "Command Project One", "https://hooks.example.test/commands")
		secondProject := createPersistentProjectForDeviceTest(t, server, admin, "Command Project Two", "")
		firstDevice := createPersistentDeviceForCommandTest(t, server, admin, firstProject.ID, "simulator", "")
		secondDevice := createPersistentDeviceForCommandTest(t, server, admin, secondProject.ID, "simulator", "")

		createBody := commandCreateBody(firstProject.ID, firstDevice, "query_status", "command-key-1", "")
		created := doRequest(t, server, http.MethodPost, "/v1/device-commands", createBody, admin)
		createdData := responseDataObject(t, assertEnvelope(t, created, http.StatusCreated, true))
		commandID := requiredStringField(t, createdData, "id")
		if createdData["status"] != "queued" || createdData["delivery_policy"] != "dispatch_once" ||
			createdData["confirmation_level"] != "none" || createdData["evidence_status"] != "none" ||
			createdData["device_type_revision"] != float64(2) || len(createdData["payload"].(map[string]interface{})) != 0 {
			t.Fatalf("created Command = %+v", createdData)
		}
		if _, exists := createdData["attempts"]; exists {
			t.Fatalf("Command create leaked detail-only attempts: %+v", createdData)
		}
		assertCommandAggregateCounts(t, db, commandID, 1, 1, 1)

		replay := doRequest(t, server, http.MethodPost, "/v1/device-commands",
			commandCreateBody(firstProject.ID, firstDevice, "query_status", "command-key-1", `{}`), admin)
		assertIdempotentReplay(t, replay, commandID)
		assertCommandAggregateCounts(t, db, commandID, 1, 1, 1)
		assertErrorCode(t, doRequest(t, server, http.MethodPost, "/v1/device-commands",
			commandCreateBody(firstProject.ID, firstDevice, "lock", "command-key-1", ""), admin),
			http.StatusConflict, "idempotency_key_conflict")

		openFirst := map[string]string{"X-API-Key": firstProject.APIKey}
		openSecond := map[string]string{"X-API-Key": secondProject.APIKey}
		assertErrorCode(t, doRequest(t, server, http.MethodGet, "/v1/open/device-commands/"+commandID, "", openSecond), http.StatusNotFound, "not_found")
		assertErrorCode(t, doRequest(t, server, http.MethodPost, "/v1/open/device-commands/"+commandID+"/cancel", "", openSecond), http.StatusNotFound, "not_found")
		assertErrorCode(t, doRequest(t, server, http.MethodGet, "/v1/open/device-commands?project_id="+firstProject.ID, "", openFirst), http.StatusBadRequest, "invalid_request")
		assertErrorCode(t, doRequest(t, server, http.MethodPost, "/v1/open/device-commands",
			commandCreateBody(secondProject.ID, firstDevice, "lock", "open-override", ""), openFirst), http.StatusBadRequest, "invalid_request")
		assertErrorCode(t, doRequest(t, server, http.MethodPost, "/v1/open/device-commands",
			commandCreateBody("", secondDevice, "lock", "foreign-device", ""), openFirst), http.StatusNotFound, "not_found")

		for _, action := range []string{"unlock", "lock"} {
			assertErrorCode(t, doRequest(t, server, http.MethodPost, "/v1/open/device-commands",
				commandCreateBody("", firstDevice, action, "offline-"+action, `{}`), openFirst), http.StatusConflict, "device_not_online")
		}
		secondCommand := responseDataObject(t, assertEnvelope(t, doRequest(t, server, http.MethodPost, "/v1/open/device-commands",
			commandCreateBody("", firstDevice, "query_status", "command-key-2", `{}`), openFirst), http.StatusCreated, true))
		secondCommandID := requiredStringField(t, secondCommand, "id")
		assertPaginatedEnvelope(t, doRequest(t, server, http.MethodGet,
			"/v1/device-commands?project_id="+firstProject.ID+"&device_id="+firstDevice+"&command_type=query_status&status=queued&page=1&page_size=1", "", admin),
			http.StatusOK, 1, 1, 2)
		assertPaginatedEnvelope(t, doRequest(t, server, http.MethodGet, "/v1/open/device-commands", "", openSecond), http.StatusOK, 1, 20, 0)
		for _, path := range []string{
			"/v1/device-commands?unknown=true",
			"/v1/device-commands?status=created",
			"/v1/device-commands?page_size=101",
			"/v1/device-commands?page=1&page=2",
		} {
			assertErrorCode(t, doRequest(t, server, http.MethodGet, path, "", admin), http.StatusBadRequest, "invalid_request")
		}

		detailData := responseDataObject(t, assertEnvelope(t, doRequest(t, server, http.MethodGet, "/v1/device-commands/"+secondCommandID, "", admin), http.StatusOK, true))
		if attempts, ok := detailData["attempts"].([]interface{}); !ok || len(attempts) != 0 {
			t.Fatalf("Command attempts = %#v", detailData["attempts"])
		}
		if results, ok := detailData["results"].([]interface{}); !ok || len(results) != 0 {
			t.Fatalf("Command results = %#v", detailData["results"])
		}
		if events, ok := detailData["events"].([]interface{}); !ok || len(events) != 1 || events[0].(map[string]interface{})["event_type"] != "command.created" {
			t.Fatalf("Command events = %#v", detailData["events"])
		}

		cancelled := responseDataObject(t, assertEnvelope(t, doRequest(t, server, http.MethodPost,
			"/v1/open/device-commands/"+secondCommandID+"/cancel", `{}`, openFirst), http.StatusOK, true))
		if cancelled["status"] != "cancelled" || cancelled["reason_code"] != "cancelled_by_request" {
			t.Fatalf("cancelled Command = %+v", cancelled)
		}
		assertErrorCode(t, doRequest(t, server, http.MethodPost, "/v1/device-commands/"+secondCommandID+"/cancel", "", admin), http.StatusConflict, "command_not_cancellable")
		assertCommandAggregateCounts(t, db, secondCommandID, 2, 2, 2)
		assertStableCommandEventOrdering(t, db, server, admin, secondCommandID)

		assertErrorCode(t, doRequest(t, server, http.MethodPost, "/v1/device-commands",
			commandCreateBody(firstProject.ID, firstDevice, "missing", "unsupported", ""), admin), http.StatusUnprocessableEntity, "unsupported_capability")
		assertErrorCode(t, doRequest(t, server, http.MethodPost, "/v1/device-commands",
			commandCreateBody(firstProject.ID, firstDevice, "unlock", "bad-payload", `{"force":true}`), admin), http.StatusUnprocessableEntity, "invalid_capability_payload")
		assertErrorCode(t, doRequest(t, server, http.MethodPost, "/v1/device-commands",
			commandCreateBody(firstProject.ID, firstDevice, "unlock", "null-payload", `null`), admin), http.StatusBadRequest, "invalid_request")

		disabledDevice := createPersistentDeviceForCommandTest(t, server, admin, firstProject.ID, "simulator", "")
		assertEnvelope(t, doRequest(t, server, http.MethodPatch, "/v1/devices/"+disabledDevice, `{"lifecycle_status":"disabled"}`, admin), http.StatusOK, true)
		assertErrorCode(t, doRequest(t, server, http.MethodPost, "/v1/device-commands",
			commandCreateBody(firstProject.ID, disabledDevice, "unlock", "disabled", ""), admin), http.StatusConflict, "device_disabled")

		wwtiotDevice := createPersistentDeviceForCommandTest(t, server, admin, firstProject.ID, "wwtiot", "WWTIOT-COMMAND-001")
		assertErrorCode(t, doRequest(t, server, http.MethodPost, "/v1/device-commands",
			commandCreateBody(firstProject.ID, wwtiotDevice, "unlock", "unconfigured", ""), admin), http.StatusConflict, "provider_not_configured")

		restarted := newProjectHTTPTestServer(t, db)
		restartedAdmin := map[string]string{"Authorization": "Bearer " + loginProjectHTTPTestAdmin(t, restarted)}
		restartedDetail := responseDataObject(t, assertEnvelope(t, doRequest(t, restarted, http.MethodGet, "/v1/device-commands/"+commandID, "", restartedAdmin), http.StatusOK, true))
		if restartedDetail["id"] != commandID || restartedDetail["status"] != "queued" {
			t.Fatalf("restarted Command = %+v", restartedDetail)
		}

		assertConcurrentCommandIdempotency(t, db, server, admin, firstProject.ID, firstDevice)
		assertStableCommandOrdering(t, db, server, admin, firstProject.ID)
	})
}

func TestCommandHTTPIdempotencyReplayUsesHistoricalCommandBeforeCurrentState(t *testing.T) {
	withAuthTestDatabase(t, func(db *sql.DB) {
		server := newProjectHTTPTestServer(t, db)
		admin := map[string]string{"Authorization": "Bearer " + loginProjectHTTPTestAdmin(t, server)}
		project := createPersistentProjectForDeviceTest(t, server, admin, "Historical Replay Project", "")
		deviceID := createPersistentDeviceForCommandTest(t, server, admin, project.ID, "simulator", "")
		body := commandCreateBody(project.ID, deviceID, "query_status", "historical-replay-key", "")
		created := responseDataObject(t, assertEnvelope(
			t, doRequest(t, server, http.MethodPost, "/v1/device-commands", body, admin), http.StatusCreated, true,
		))
		commandID := requiredStringField(t, created, "id")
		beforeReplay := readCommandAggregateTotals(t, db)

		assertEnvelope(t, doRequest(
			t, server, http.MethodPatch, "/v1/devices/"+deviceID, `{"lifecycle_status":"disabled"}`, admin,
		), http.StatusOK, true)
		assertIdempotentReplay(t, doRequest(
			t, server, http.MethodPost, "/v1/device-commands",
			commandCreateBody(project.ID, deviceID, "query_status", "historical-replay-key", `{}`), admin,
		), commandID)
		assertErrorCode(t, doRequest(
			t, server, http.MethodPost, "/v1/device-commands",
			commandCreateBody(project.ID, deviceID, "lock", "historical-replay-key", ""), admin,
		), http.StatusConflict, "idempotency_key_conflict")

		assertCommandAggregateTotals(t, db, beforeReplay)
		var attempts, commands int
		if err := db.QueryRow(`SELECT count(*) FROM device_command_attempts WHERE command_id = $1`, commandID).Scan(&attempts); err != nil {
			t.Fatal(err)
		}
		if err := db.QueryRow(`SELECT count(*) FROM device_commands WHERE project_id = $1 AND idempotency_key = 'historical-replay-key'`, project.ID).Scan(&commands); err != nil {
			t.Fatal(err)
		}
		if attempts != 0 || commands != 1 {
			t.Fatalf("historical replay effects: attempts=%d commands=%d", attempts, commands)
		}
	})
}

func TestCommandHTTPPostgresCreationRollsBackAtomically(t *testing.T) {
	withAuthTestDatabase(t, func(db *sql.DB) {
		server := newProjectHTTPTestServer(t, db)
		admin := map[string]string{"Authorization": "Bearer " + loginProjectHTTPTestAdmin(t, server)}
		project := createPersistentProjectForDeviceTest(t, server, admin, "Command Rollback", "https://hooks.example.test/command-rollback")
		deviceID := createPersistentDeviceForCommandTest(t, server, admin, project.ID, "simulator", "")
		baseline := readCommandAggregateTotals(t, db)

		failures := []struct {
			name string
			add  string
			drop string
			key  string
		}{
			{name: "Event", add: `ALTER TABLE device_events ADD CONSTRAINT reject_command_http_event CHECK (event_type <> 'command.created') NOT VALID`, drop: `ALTER TABLE device_events DROP CONSTRAINT reject_command_http_event`, key: "rollback-event"},
			{name: "Delivery", add: `ALTER TABLE webhook_deliveries ADD CONSTRAINT reject_command_http_delivery CHECK (status <> 'pending') NOT VALID`, drop: `ALTER TABLE webhook_deliveries DROP CONSTRAINT reject_command_http_delivery`, key: "rollback-delivery"},
			{name: "Audit", add: `ALTER TABLE audit_logs ADD CONSTRAINT reject_command_http_audit CHECK (action <> 'command.created') NOT VALID`, drop: `ALTER TABLE audit_logs DROP CONSTRAINT reject_command_http_audit`, key: "rollback-audit"},
		}
		for _, failure := range failures {
			t.Run(failure.name, func(t *testing.T) {
				if _, err := db.Exec(failure.add); err != nil {
					t.Fatal(err)
				}
				response := doRequest(t, server, http.MethodPost, "/v1/device-commands",
					commandCreateBody(project.ID, deviceID, "query_status", failure.key, ""), admin)
				assertErrorCode(t, response, http.StatusInternalServerError, "internal_error")
				assertCommandAggregateTotals(t, db, baseline)
				if _, err := db.Exec(failure.drop); err != nil {
					t.Fatal(err)
				}
			})
		}

		created := responseDataObject(t, assertEnvelope(t, doRequest(t, server, http.MethodPost, "/v1/device-commands",
			commandCreateBody(project.ID, deviceID, "query_status", "rollback-cancel", ""), admin), http.StatusCreated, true))
		commandID := requiredStringField(t, created, "id")
		baseline = readCommandAggregateTotals(t, db)
		if _, err := db.Exec(`ALTER TABLE audit_logs ADD CONSTRAINT reject_command_http_cancel_audit CHECK (action <> 'command.cancelled') NOT VALID`); err != nil {
			t.Fatal(err)
		}
		assertErrorCode(t, doRequest(t, server, http.MethodPost, "/v1/device-commands/"+commandID+"/cancel", "", admin), http.StatusInternalServerError, "internal_error")
		assertCommandAggregateTotals(t, db, baseline)
		var status string
		if err := db.QueryRow(`SELECT status FROM device_commands WHERE id = $1`, commandID).Scan(&status); err != nil || status != "queued" {
			t.Fatalf("rolled-back cancellation status = %q, err=%v", status, err)
		}
		if _, err := db.Exec(`ALTER TABLE audit_logs DROP CONSTRAINT reject_command_http_cancel_audit`); err != nil {
			t.Fatal(err)
		}
		assertEnvelope(t, doRequest(t, server, http.MethodPost, "/v1/device-commands/"+commandID+"/cancel", "", admin), http.StatusOK, true)
	})
}

func createPersistentDeviceForCommandTest(t *testing.T, server http.Handler, admin map[string]string, projectID, providerCode, providerDeviceID string) string {
	t.Helper()
	body := map[string]any{
		"project_id": projectID, "name": "Command Lock", "device_type_code": "smart-lock", "provider_code": providerCode,
	}
	switch providerCode {
	case domain.ProviderCodeSimulator:
		body["provider_profile"] = domain.ProviderProfileSimulatorV1
	case domain.ProviderCodeWWTIOT:
		body["provider_profile"] = domain.ProviderProfileWWTIOTV2
	default:
		t.Fatalf("unsupported fixture Provider %q", providerCode)
	}
	if providerDeviceID != "" {
		body["provider_device_id"] = providerDeviceID
	}
	raw, err := json.Marshal(body)
	if err != nil {
		t.Fatal(err)
	}
	data := responseDataObject(t, assertEnvelope(t, doRequest(t, server, http.MethodPost, "/v1/devices", string(raw), admin), http.StatusCreated, true))
	return requiredStringField(t, data, "id")
}

func commandCreateBody(projectID, deviceID, commandType, idempotencyKey, payload string) string {
	fields := []string{`"device_id":"` + deviceID + `"`, `"command_type":"` + commandType + `"`, `"idempotency_key":"` + idempotencyKey + `"`}
	if projectID != "" {
		fields = append(fields, `"project_id":"`+projectID+`"`)
	}
	if payload != "" {
		fields = append(fields, `"payload":`+payload)
	}
	return "{" + strings.Join(fields, ",") + "}"
}

func assertIdempotentReplay(t *testing.T, response *httptest.ResponseRecorder, commandID string) {
	t.Helper()
	if response.Code != http.StatusOK {
		t.Fatalf("idempotent replay status = %d: %s", response.Code, response.Body.String())
	}
	var body struct {
		Success bool `json:"success"`
		Data    struct {
			ID string `json:"id"`
		} `json:"data"`
		Meta struct {
			IdempotentReplay bool `json:"idempotent_replay"`
		} `json:"meta"`
	}
	decodeBody(t, strings.NewReader(response.Body.String()), &body)
	if !body.Success || body.Data.ID != commandID || !body.Meta.IdempotentReplay {
		t.Fatalf("idempotent replay body = %s", response.Body.String())
	}
}

type commandAggregateTotals struct {
	Commands   int
	Events     int
	Audits     int
	Deliveries int
}

func readCommandAggregateTotals(t *testing.T, db *sql.DB) commandAggregateTotals {
	t.Helper()
	var totals commandAggregateTotals
	if err := db.QueryRow(`
		SELECT
			(SELECT count(*) FROM device_commands),
			(SELECT count(*) FROM device_events WHERE command_id IS NOT NULL),
			(SELECT count(*) FROM audit_logs WHERE resource_type = 'command'),
			(SELECT count(*) FROM webhook_deliveries wd JOIN device_events de ON de.id = wd.event_id WHERE de.command_id IS NOT NULL)
	`).Scan(&totals.Commands, &totals.Events, &totals.Audits, &totals.Deliveries); err != nil {
		t.Fatal(err)
	}
	return totals
}

func assertCommandAggregateTotals(t *testing.T, db *sql.DB, want commandAggregateTotals) {
	t.Helper()
	if got := readCommandAggregateTotals(t, db); got != want {
		t.Fatalf("Command aggregate totals = %+v, want %+v", got, want)
	}
}

func assertCommandAggregateCounts(t *testing.T, db *sql.DB, commandID string, events, audits, deliveries int) {
	t.Helper()
	var gotEvents, gotAudits, gotDeliveries int
	if err := db.QueryRow(`SELECT count(*) FROM device_events WHERE command_id = $1`, commandID).Scan(&gotEvents); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRow(`SELECT count(*) FROM audit_logs WHERE resource_type = 'command' AND resource_id = $1`, commandID).Scan(&gotAudits); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRow(`SELECT count(*) FROM webhook_deliveries wd JOIN device_events de ON de.id = wd.event_id WHERE de.command_id = $1`, commandID).Scan(&gotDeliveries); err != nil {
		t.Fatal(err)
	}
	if gotEvents != events || gotAudits != audits || gotDeliveries != deliveries {
		t.Fatalf("Command aggregate counts events=%d audits=%d deliveries=%d", gotEvents, gotAudits, gotDeliveries)
	}
}

func assertConcurrentCommandIdempotency(t *testing.T, db *sql.DB, server http.Handler, admin map[string]string, projectID, deviceID string) {
	t.Helper()
	body := commandCreateBody(projectID, deviceID, "query_status", "concurrent-key", "")
	responses := make([]*httptest.ResponseRecorder, 2)
	var wait sync.WaitGroup
	for index := range responses {
		wait.Add(1)
		go func(index int) {
			defer wait.Done()
			responses[index] = doRequest(t, server, http.MethodPost, "/v1/device-commands", body, admin)
		}(index)
	}
	wait.Wait()
	statuses := []int{responses[0].Code, responses[1].Code}
	sort.Ints(statuses)
	if statuses[0] != http.StatusOK || statuses[1] != http.StatusCreated {
		t.Fatalf("concurrent idempotency statuses = %v", statuses)
	}
	first := responseDataObject(t, decodeSuccessEnvelopeWithMeta(t, responses[0]))
	second := responseDataObject(t, decodeSuccessEnvelopeWithMeta(t, responses[1]))
	if first["id"] != second["id"] {
		t.Fatalf("concurrent idempotency IDs = %v and %v", first["id"], second["id"])
	}
	assertCommandAggregateCounts(t, db, first["id"].(string), 1, 1, 1)
}

func decodeSuccessEnvelopeWithMeta(t *testing.T, response *httptest.ResponseRecorder) jsonResponse {
	t.Helper()
	var body jsonResponse
	decodeResponse(t, response, &body)
	if !body.Success || body.Code != 0 || body.ErrorCode != "" {
		t.Fatalf("success envelope = %+v", body)
	}
	return body
}

func assertStableCommandOrdering(t *testing.T, db *sql.DB, server http.Handler, admin map[string]string, projectID string) {
	t.Helper()
	var ids []string
	rows, err := db.Query(`SELECT id::text FROM device_commands WHERE project_id = $1 ORDER BY id DESC`, projectID)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			t.Fatal(err)
		}
		ids = append(ids, id)
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	if len(ids) != 3 {
		t.Fatalf("ordering fixture IDs = %v", ids)
	}
	if _, err := db.Exec(`UPDATE device_commands SET created_at = '2026-07-31T10:00:00Z' WHERE project_id = $1`, projectID); err != nil {
		t.Fatal(err)
	}
	body := assertPaginatedEnvelope(t, doRequest(t, server, http.MethodGet, "/v1/device-commands?project_id="+projectID+"&page=1&page_size=3", "", admin), http.StatusOK, 1, 3, 3)
	items := responseDataObject(t, body)["items"].([]interface{})
	for index, id := range ids {
		if items[index].(map[string]interface{})["id"] != id {
			t.Fatalf("stable Command order = %#v, want %v", items, ids)
		}
	}
	if _, _, err := repository.NewPostgresStore(db).Commands().List(context.Background(), repository.ListCommandsRequest{Limit: 101}); !errors.Is(err, repository.ErrInvalidRepositoryRequest) {
		t.Fatalf("repository page-size error = %v", err)
	}
}

func assertStableCommandEventOrdering(t *testing.T, db *sql.DB, server http.Handler, admin map[string]string, commandID string) {
	t.Helper()
	var ids []string
	rows, err := db.Query(`SELECT id::text FROM device_events WHERE command_id = $1 ORDER BY id`, commandID)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			t.Fatal(err)
		}
		ids = append(ids, id)
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	if len(ids) != 2 {
		t.Fatalf("Command Event ordering fixture IDs = %v", ids)
	}
	if _, err := db.Exec(`UPDATE device_events SET occurred_at = '2026-07-31T10:00:00Z' WHERE command_id = $1`, commandID); err != nil {
		t.Fatal(err)
	}
	detail := responseDataObject(t, assertEnvelope(t, doRequest(t, server, http.MethodGet, "/v1/device-commands/"+commandID, "", admin), http.StatusOK, true))
	events := detail["events"].([]interface{})
	for index, id := range ids {
		if events[index].(map[string]interface{})["event_id"] != id {
			t.Fatalf("stable Command Event order = %#v, want %v", events, ids)
		}
	}
}
