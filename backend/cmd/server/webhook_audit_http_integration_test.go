//go:build integration

package main

import (
	"context"
	"database/sql"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/qiyue2015/device-platform/internal/domain"
	"github.com/qiyue2015/device-platform/internal/storage/repository"
)

func TestWebhookAuditHTTPPostgresReadReplayAndRestart(t *testing.T) {
	withAuthTestDatabase(t, func(db *sql.DB) {
		server := newProjectHTTPTestServer(t, db)
		admin := map[string]string{"Authorization": "Bearer " + loginProjectHTTPTestAdmin(t, server)}
		fixture := createWebhookAuditHTTPFixture(t, db, server, admin, "Read Replay")

		events := assertPaginatedEnvelope(t, doRequest(t, server, http.MethodGet,
			"/v1/events?project_id="+fixture.projectID+"&device_id="+fixture.deviceID+"&event_type=device.created&page=1&page_size=10", "", admin),
			http.StatusOK, 1, 10, 2)
		eventItems := responseDataObject(t, events)["items"].([]interface{})
		assertDescendingIDs(t, eventItems, "event_id")
		if eventItems[0].(map[string]interface{})["id"] != nil || eventItems[0].(map[string]interface{})["schema_version"] != float64(1) {
			t.Fatalf("Event DTO = %+v", eventItems[0])
		}
		assertEnvelope(t, doRequest(t, server, http.MethodGet, "/v1/events/"+fixture.eventID, "", admin), http.StatusOK, true)

		allDeliveries := assertPaginatedEnvelope(t, doRequest(t, server, http.MethodGet,
			"/v1/webhook-deliveries?project_id="+fixture.projectID+"&page=1&page_size=10", "", admin),
			http.StatusOK, 1, 10, 2)
		assertDescendingIDs(t, responseDataObject(t, allDeliveries)["items"].([]interface{}), "id")
		deliveries := assertPaginatedEnvelope(t, doRequest(t, server, http.MethodGet,
			"/v1/webhook-deliveries?project_id="+fixture.projectID+"&status=dead&page=1&page_size=10", "", admin),
			http.StatusOK, 1, 10, 1)
		deliveryItems := responseDataObject(t, deliveries)["items"].([]interface{})
		if _, exists := deliveryItems[0].(map[string]interface{})["attempts"]; exists {
			t.Fatalf("Delivery list embedded Attempts: %+v", deliveryItems[0])
		}
		detailResponse := doRequest(t, server, http.MethodGet, "/v1/webhook-deliveries/"+fixture.deliveryID, "", admin)
		detail := responseDataObject(t, assertEnvelope(t, detailResponse, http.StatusOK, true))
		attempts, ok := detail["attempts"].([]interface{})
		if !ok || len(attempts) != 2 || attempts[0].(map[string]interface{})["attempt_no"] != float64(1) || attempts[1].(map[string]interface{})["attempt_no"] != float64(2) {
			t.Fatalf("Delivery Attempts = %#v", detail["attempts"])
		}
		assertWebhookDeliverySafe(t, detailResponse.Body.String())

		audits := assertPaginatedEnvelope(t, doRequest(t, server, http.MethodGet,
			"/v1/audit-logs?project_id="+fixture.projectID+"&actor_type=system&action=simulator.updated&result=success&resource_type=simulator&resource_id=simulator&page=1&page_size=10", "", admin),
			http.StatusOK, 1, 10, 2)
		auditItems := responseDataObject(t, audits)["items"].([]interface{})
		assertDescendingIDs(t, auditItems, "id")
		auditID := auditItems[0].(map[string]interface{})["id"].(string)
		auditDetail := responseDataObject(t, assertEnvelope(t, doRequest(t, server, http.MethodGet, "/v1/audit-logs/"+auditID, "", admin), http.StatusOK, true))
		if auditDetail["result"] != "success" || auditDetail["occurred_at"] == nil {
			t.Fatalf("Audit DTO = %+v", auditDetail)
		}
		providerAudits := assertPaginatedEnvelope(t, doRequest(t, server, http.MethodGet,
			"/v1/audit-logs?project_id="+fixture.projectID+"&actor_type=provider&action=provider.message_received&result=success&page=1&page_size=10", "", admin),
			http.StatusOK, 1, 10, 1)
		providerAuditItems := responseDataObject(t, providerAudits)["items"].([]interface{})
		if providerAuditItems[0].(map[string]interface{})["actor_id"] != "omni" {
			t.Fatalf("Provider message Audit DTO = %+v", providerAuditItems[0])
		}

		assertWebhookAuditStrictHTTP(t, server, admin, fixture)

		updated := doRequest(t, server, http.MethodPatch, "/v1/projects/"+fixture.projectID,
			`{"webhook_url":"https://hooks.example.test/current-v2"}`, admin)
		assertEnvelope(t, updated, http.StatusOK, true)
		replayResponse := doRequest(t, server, http.MethodPost, "/v1/webhook-deliveries/"+fixture.deliveryID+"/resend", "", admin)
		replay := responseDataObject(t, assertEnvelope(t, replayResponse, http.StatusCreated, true))
		replayID := requiredStringField(t, replay, "id")
		if replay["target_url"] != "https://hooks.example.test/current-v2" || replay["webhook_config_version"] != float64(2) || replay["replay_of_delivery_id"] != fixture.deliveryID || replay["status"] != "pending" {
			t.Fatalf("replayed Delivery = %+v", replay)
		}
		assertWebhookDeliverySafe(t, replayResponse.Body.String())
		assertErrorCode(t, doRequest(t, server, http.MethodPost, "/v1/webhook-deliveries/"+replayID+"/resend", "", admin),
			http.StatusConflict, "webhook_delivery_not_dead")

		var replayAudits int
		var originalStatus, originalRawBody string
		var actorUserID string
		if err := db.QueryRow(`SELECT count(*) FROM audit_logs WHERE action = 'webhook.delivery_replayed' AND resource_id = $1`, replayID).Scan(&replayAudits); err != nil {
			t.Fatal(err)
		}
		if err := db.QueryRow(`SELECT actor_user_id::text FROM audit_logs WHERE action = 'webhook.delivery_replayed' AND resource_id = $1`, replayID).Scan(&actorUserID); err != nil {
			t.Fatal(err)
		}
		if err := db.QueryRow(`SELECT status, convert_from(raw_body, 'UTF8') FROM webhook_deliveries WHERE id = $1`, fixture.deliveryID).Scan(&originalStatus, &originalRawBody); err != nil {
			t.Fatal(err)
		}
		if replayAudits != 1 || actorUserID != authTestAdminID || originalStatus != "dead" || originalRawBody != "RAW_PRIVATE_MARKER" {
			t.Fatalf("replay atomic facts audits=%d actor=%q original=%s raw=%q", replayAudits, actorUserID, originalStatus, originalRawBody)
		}

		restarted := newProjectHTTPTestServer(t, db)
		restartedAdmin := map[string]string{"Authorization": "Bearer " + loginProjectHTTPTestAdmin(t, restarted)}
		restartedDetail := responseDataObject(t, assertEnvelope(t,
			doRequest(t, restarted, http.MethodGet, "/v1/webhook-deliveries/"+replayID, "", restartedAdmin), http.StatusOK, true))
		restartedAttempts, ok := restartedDetail["attempts"].([]interface{})
		if !ok || len(restartedAttempts) != 0 {
			t.Fatalf("restarted replay Attempts = %#v", restartedDetail["attempts"])
		}
		assertEnvelope(t, doRequest(t, restarted, http.MethodGet, "/v1/events/"+fixture.eventID, "", restartedAdmin), http.StatusOK, true)
	})
}

func TestWebhookAuditHTTPPostgresReplayConcurrencyConfigurationAndRollback(t *testing.T) {
	withAuthTestDatabase(t, func(db *sql.DB) {
		server := newProjectHTTPTestServer(t, db)
		token := loginProjectHTTPTestAdmin(t, server)
		admin := map[string]string{"Authorization": "Bearer " + token}
		fixture := createWebhookAuditHTTPFixture(t, db, server, admin, "Concurrency Rollback")

		if _, err := db.Exec(`ALTER TABLE audit_logs ADD CONSTRAINT reject_replay_http_audit CHECK (action <> 'webhook.delivery_replayed') NOT VALID`); err != nil {
			t.Fatal(err)
		}
		beforeDeliveries, beforeAudits := replayCounts(t, db, fixture.deliveryID)
		failed := doRequest(t, server, http.MethodPost, "/v1/webhook-deliveries/"+fixture.deliveryID+"/resend", "", admin)
		assertErrorCode(t, failed, http.StatusInternalServerError, "internal_error")
		afterDeliveries, afterAudits := replayCounts(t, db, fixture.deliveryID)
		if beforeDeliveries != afterDeliveries || beforeAudits != afterAudits {
			t.Fatalf("failed replay was not rolled back: before=(%d,%d) after=(%d,%d)", beforeDeliveries, beforeAudits, afterDeliveries, afterAudits)
		}
		if _, err := db.Exec(`ALTER TABLE audit_logs DROP CONSTRAINT reject_replay_http_audit`); err != nil {
			t.Fatal(err)
		}

		responses := make(chan *httptest.ResponseRecorder, 2)
		start := make(chan struct{})
		var workers sync.WaitGroup
		for range 2 {
			workers.Add(1)
			go func() {
				defer workers.Done()
				<-start
				recorder := httptest.NewRecorder()
				request := httptest.NewRequest(http.MethodPost, "/v1/webhook-deliveries/"+fixture.deliveryID+"/resend", nil)
				request.Header.Set("Authorization", "Bearer "+token)
				server.ServeHTTP(recorder, request)
				responses <- recorder
			}()
		}
		close(start)
		workers.Wait()
		close(responses)
		ids := map[string]struct{}{}
		for response := range responses {
			data := responseDataObject(t, assertEnvelope(t, response, http.StatusCreated, true))
			ids[requiredStringField(t, data, "id")] = struct{}{}
		}
		deliveries, audits := replayCounts(t, db, fixture.deliveryID)
		if len(ids) != 2 || deliveries != 2 || audits != 2 {
			t.Fatalf("concurrent replay ids=%d deliveries=%d audits=%d", len(ids), deliveries, audits)
		}

		assertEnvelope(t, doRequest(t, server, http.MethodPatch, "/v1/projects/"+fixture.projectID, `{"webhook_url":null}`, admin), http.StatusOK, true)
		assertErrorCode(t, doRequest(t, server, http.MethodPost, "/v1/webhook-deliveries/"+fixture.deliveryID+"/resend", "", admin),
			http.StatusConflict, "webhook_not_configured")
		deliveriesAfter, auditsAfter := replayCounts(t, db, fixture.deliveryID)
		if deliveriesAfter != deliveries || auditsAfter != audits {
			t.Fatalf("unconfigured replay wrote facts: before=(%d,%d) after=(%d,%d)", deliveries, audits, deliveriesAfter, auditsAfter)
		}
	})
}

type webhookAuditHTTPFixture struct {
	projectID  string
	deviceID   string
	eventID    string
	deliveryID string
}

func createWebhookAuditHTTPFixture(t *testing.T, db *sql.DB, server http.Handler, admin map[string]string, suffix string) webhookAuditHTTPFixture {
	t.Helper()
	project := createPersistentProjectForDeviceTest(t, server, admin, suffix+" Project", "https://hooks.example.test/current-v1")
	created := responseDataObject(t, assertEnvelope(t, doRequest(t, server, http.MethodPost, "/v1/devices", `{
		"project_id":"`+project.ID+`","name":"`+suffix+` Device","device_type_code":"smart-lock","provider_code":"simulator","provider_profile":"simulator-v1"
	}`, admin), http.StatusCreated, true))
	deviceID := requiredStringField(t, created, "id")
	var eventID, deliveryID string
	if err := db.QueryRow(`SELECT id::text FROM device_events WHERE device_id = $1 AND event_type = 'device.created'`, deviceID).Scan(&eventID); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRow(`SELECT id::text FROM webhook_deliveries WHERE event_id = $1`, eventID).Scan(&deliveryID); err != nil {
		t.Fatal(err)
	}
	fixedTime := time.Date(2026, 7, 31, 12, 0, 0, 0, time.UTC)
	secondEventID := "75000000-0000-0000-0000-000000000099"
	secondDeliveryID := "65000000-0000-0000-0000-000000000099"
	store := repository.NewPostgresStore(db)
	if err := store.WithinTransaction(context.Background(), func(tx *repository.PostgresTx) error {
		if err := tx.Events().Create(context.Background(), domain.Event{
			ID: secondEventID, SchemaVersion: domain.EventSchemaVersion, EventType: domain.EventTypeDeviceCreated,
			ProjectID: project.ID, DeviceID: &deviceID, Source: domain.EventSourceAdmin,
			Payload:          map[string]any{"device_type_code": "smart-lock", "provider_code": "simulator", "lifecycle_status": "active"},
			DeduplicationKey: "http-observability-second", OccurredAt: fixedTime, CreatedAt: fixedTime,
		}); err != nil {
			return err
		}
		_, created, err := tx.Webhooks().CreateDelivery(context.Background(), repository.CreateWebhookDeliveryRequest{
			ID: secondDeliveryID, EventID: secondEventID, RawBody: []byte(`{"schema_version":1}`),
		})
		if err != nil || !created {
			return fmt.Errorf("create second Delivery=%v: %w", created, err)
		}
		resourceID := "simulator"
		for _, id := range []string{"85000000-0000-0000-0000-000000000001", "85000000-0000-0000-0000-000000000002"} {
			projectID := project.ID
			if err := tx.Audits().Create(context.Background(), domain.AuditLog{
				ID: id, ActorType: domain.ActorTypeSystem, ProjectID: &projectID, Action: "simulator.updated",
				Result: domain.AuditResultSuccess, ResourceType: "simulator", ResourceID: &resourceID,
				Metadata: map[string]any{"source": "fixture"}, OccurredAt: fixedTime,
			}); err != nil {
				return err
			}
		}
		providerActorID := domain.ProviderCodeOmni
		providerResourceID := "86000000-0000-0000-0000-000000000001"
		projectID := project.ID
		if err := tx.Audits().Create(context.Background(), domain.AuditLog{
			ID: "85000000-0000-0000-0000-000000000003", ActorType: domain.ActorTypeProvider,
			ActorID: &providerActorID, ProjectID: &projectID, Action: "provider.message_received",
			Result: domain.AuditResultSuccess, ResourceType: "device_raw_message", ResourceID: &providerResourceID,
			Metadata: map[string]any{"provider_code": "omni", "evidence_status": "unverified"}, OccurredAt: fixedTime,
		}); err != nil {
			return err
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`UPDATE device_events SET occurred_at = $2, created_at = $2 WHERE id = $1`, eventID, fixedTime); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`
		UPDATE webhook_deliveries
		SET status = 'dead', attempt_count = 2, next_attempt_at = NULL,
			last_error_code = 'http_status', last_error_detail = 'upstream rejected', raw_body = convert_to('RAW_PRIVATE_MARKER', 'UTF8'),
			created_at = $2, updated_at = $2
		WHERE id = $1
	`, deliveryID, fixedTime); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`UPDATE webhook_deliveries SET created_at = $2, updated_at = $2 WHERE id = $1`, secondDeliveryID, fixedTime); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`
		INSERT INTO webhook_delivery_attempts (
			id, delivery_id, attempt_no, request_timestamp, http_status, response_summary, error_code, error_detail, started_at, completed_at
		) VALUES
			('95000000-0000-0000-0000-000000000002', $1, 2, 2, 503, 'second', 'http_status', 'rejected', $2::timestamptz + interval '2 seconds', $2::timestamptz + interval '3 seconds'),
			('95000000-0000-0000-0000-000000000001', $1, 1, 1, 500, 'first', 'http_status', 'failed', $2::timestamptz, $2::timestamptz + interval '1 second')
	`, deliveryID, fixedTime); err != nil {
		t.Fatal(err)
	}
	return webhookAuditHTTPFixture{projectID: project.ID, deviceID: deviceID, eventID: eventID, deliveryID: deliveryID}
}

func assertWebhookAuditStrictHTTP(t *testing.T, server http.Handler, admin map[string]string, fixture webhookAuditHTTPFixture) {
	t.Helper()
	for _, test := range []struct {
		path string
		code string
	}{
		{path: "/v1/events?sort=occurred_at", code: "invalid_request"},
		{path: "/v1/events?page=1&page=2", code: "invalid_request"},
		{path: "/v1/events?page=1;page_size=2", code: "invalid_request"},
		{path: "/v1/events?project_id=invalid", code: "invalid_request"},
		{path: "/v1/events?event_type=future", code: "invalid_request"},
		{path: "/v1/webhook-deliveries?status=future", code: "invalid_request"},
		{path: "/v1/webhook-deliveries?event_id=invalid", code: "invalid_request"},
		{path: "/v1/audit-logs?actor_type=open_api", code: "invalid_request"},
		{path: "/v1/audit-logs?action=future", code: "invalid_request"},
		{path: "/v1/audit-logs?result=pending", code: "invalid_request"},
		{path: "/v1/audit-logs?resource_type=", code: "invalid_request"},
		{path: "/v1/audit-logs?page_size=101", code: "invalid_request"},
		{path: "/v1/events/" + fixture.eventID + "?expand=true", code: "invalid_request"},
	} {
		assertErrorCode(t, doRequest(t, server, http.MethodGet, test.path, "", admin), http.StatusBadRequest, test.code)
	}
	missingID := "75000000-0000-0000-0000-000000000404"
	for _, path := range []string{"/v1/events/" + missingID, "/v1/webhook-deliveries/" + missingID, "/v1/audit-logs/" + missingID} {
		assertErrorCode(t, doRequest(t, server, http.MethodGet, path, "", admin), http.StatusNotFound, "not_found")
	}
	assertErrorCode(t, doRequest(t, server, http.MethodGet, "/v1/events/"+fixture.eventID+"/extra", "", admin), http.StatusNotFound, "not_found")
	assertErrorCode(t, doRequest(t, server, http.MethodPost, "/v1/events", `{}`, admin), http.StatusMethodNotAllowed, "method_not_allowed")
	assertErrorCode(t, doRequest(t, server, http.MethodPost, "/v1/audit-logs", `{}`, admin), http.StatusMethodNotAllowed, "method_not_allowed")
	assertErrorCode(t, doRequest(t, server, http.MethodPost, "/v1/webhook-deliveries/"+fixture.deliveryID+"/resend", `{"force":true}`, admin), http.StatusBadRequest, "invalid_request")
	assertErrorCode(t, doRequest(t, server, http.MethodPost, "/v1/webhook-deliveries/"+fixture.deliveryID+"/resend", `null`, admin), http.StatusBadRequest, "invalid_request")
}

func assertDescendingIDs(t *testing.T, items []interface{}, field string) {
	t.Helper()
	if len(items) != 2 {
		t.Fatalf("items = %d, want 2", len(items))
	}
	first := items[0].(map[string]interface{})[field].(string)
	second := items[1].(map[string]interface{})[field].(string)
	if first <= second {
		t.Fatalf("stable order %s: %q before %q", field, first, second)
	}
}

func assertWebhookDeliverySafe(t *testing.T, raw string) {
	t.Helper()
	for _, forbidden := range []string{
		"RAW_PRIVATE_MARKER", "raw_body", "webhook_secret", "ciphertext", "nonce", "signature",
		"lease_token", "lease_owner", "lease_expires_at", "request_timestamp",
		"last_error_code", "last_error_detail",
	} {
		if strings.Contains(raw, forbidden) {
			t.Fatalf("Webhook Delivery response exposed %q: %s", forbidden, raw)
		}
	}
}

func replayCounts(t *testing.T, db *sql.DB, originalID string) (int, int) {
	t.Helper()
	var deliveries, audits int
	if err := db.QueryRow(`SELECT count(*) FROM webhook_deliveries WHERE replay_of_delivery_id = $1`, originalID).Scan(&deliveries); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRow(`
		SELECT count(*) FROM audit_logs
		WHERE action = 'webhook.delivery_replayed' AND metadata->>'original_delivery_id' = $1
	`, originalID).Scan(&audits); err != nil {
		t.Fatal(err)
	}
	return deliveries, audits
}
