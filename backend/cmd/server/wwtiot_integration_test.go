//go:build integration

package main

import (
	"context"
	"crypto/md5"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/qiyue2015/device-platform/internal/domain"
	"github.com/qiyue2015/device-platform/internal/storage/repository"
)

const (
	wwtiotTestUserID  = "contract-user"
	wwtiotTestUserKey = "contract-secret-key"
)

func TestInstalledAppWWTIOTClientPersistentWorkerAcceptanceAndTimeout(t *testing.T) {
	requestBody := make(chan map[string]any, 1)
	vendor := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.Header.Get("Content-Type") != "application/json" {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		requestBody <- body
		response := make(map[string]any, len(body)+2)
		for key, value := range body {
			response[key] = value
		}
		response["result"] = "ok"
		response["info"] = "cmd send ok"
		response["sign"] = "unverified-response-sign"
		_ = json.NewEncoder(w).Encode(response)
	}))
	defer vendor.Close()

	withAuthTestDatabase(t, func(db *sql.DB) {
		application, server, token := newInstalledWWTIOTTestApp(t, db, vendor.URL)
		defer application.close()
		defer server.Close()
		serverURL := server.URL
		projectID := processCreateProject(t, serverURL, token, "")
		deviceID := createWWTIOTProcessDevice(t, serverURL, token, projectID, "768901037824")
		commandID := createWWTIOTProcessCommand(t, serverURL, token, projectID, deviceID, "query_status", "wwtiot-acceptance")

		store := repository.NewPostgresStore(db)
		waitForProcessState(t, 15*time.Second, func() (bool, error) {
			command, err := store.Commands().Get(context.Background(), commandID)
			return err == nil && command.Status == domain.CommandStatusSent &&
				command.ConfirmationLevel == domain.ConfirmationProviderAccepted &&
				command.EvidenceStatus == domain.EvidenceUnverified, err
		})

		var sent map[string]any
		select {
		case sent = <-requestBody:
		case <-time.After(time.Second):
			t.Fatal("local WWTIOT endpoint did not receive a request")
		}
		assertExactWWTIOTStatusRequest(t, sent, "768901037824")

		attempts, err := store.Commands().ListAttempts(context.Background(), commandID)
		if err != nil || len(attempts) != 1 {
			t.Fatalf("WWTIOT Attempts=%+v err=%v", attempts, err)
		}
		attempt := attempts[0]
		if attempt.Phase != domain.AttemptPhaseCompleted || attempt.Outcome == nil ||
			*attempt.Outcome != domain.AttemptOutcomeProviderAccepted ||
			attempt.ConfirmationLevel != domain.ConfirmationProviderAccepted ||
			attempt.EvidenceStatus != domain.EvidenceUnverified ||
			attempt.RequestSummary["cmd"] != "control" || attempt.RequestSummary["type"] != float64(23) ||
			attempt.RequestSummary["value"] != float64(4) || attempt.ResponseSummary["result"] != "ok" {
			t.Fatalf("WWTIOT persisted Attempt=%+v", attempt)
		}
		assertJSONOmits(t, attempt, vendor.URL, wwtiotTestUserID, wwtiotTestUserKey, "sign", "unverified-response-sign")

		events, err := store.Events().ListByCommand(context.Background(), commandID)
		if err != nil || len(events) != 3 ||
			events[0].EventType != domain.EventTypeCommandCreated ||
			events[1].EventType != domain.EventTypeCommandStatusChanged ||
			events[2].EventType != domain.EventTypeCommandEvidenceUpdated {
			t.Fatalf("WWTIOT acceptance Events=%+v err=%v", events, err)
		}

		if _, err := db.Exec(`
			UPDATE device_commands
			SET queued_at = now() - interval '3 seconds',
				dispatch_deadline_at = now() - interval '3 seconds' + dispatch_deadline_ms * interval '1 millisecond',
				sent_at = now() - interval '2 seconds',
				result_deadline_at = now() - interval '1 second'
			WHERE id = $1
		`, commandID); err != nil {
			t.Fatal(err)
		}
		waitForProcessState(t, 15*time.Second, func() (bool, error) {
			command, err := store.Commands().Get(context.Background(), commandID)
			return err == nil && command.Status == domain.CommandStatusTimeout &&
				wwtiotStringValue(command.ReasonCode) == "result_observation_timeout", err
		})
		command, err := store.Commands().Get(context.Background(), commandID)
		if err != nil || command.Status == domain.CommandStatusAcked || command.Status == domain.CommandStatusSuccess ||
			command.ConfirmationLevel != domain.ConfirmationProviderAccepted || command.EvidenceStatus != domain.EvidenceUnverified {
			t.Fatalf("WWTIOT timeout Command=%+v err=%v", command, err)
		}
		events, err = store.Events().ListByCommand(context.Background(), commandID)
		if err != nil || len(events) != 4 || events[3].EventType != domain.EventTypeCommandStatusChanged ||
			events[3].Payload["from"] != "sent" || events[3].Payload["to"] != "timeout" {
			t.Fatalf("WWTIOT timeout Events=%+v err=%v", events, err)
		}

		status, detail, err := processJSONRequest(serverURL, token, http.MethodGet, "/v1/device-commands/"+commandID, "")
		if err != nil || status != http.StatusOK {
			t.Fatalf("WWTIOT Command detail status=%d body=%+v err=%v", status, detail, err)
		}
		assertJSONOmits(t, detail, vendor.URL, wwtiotTestUserID, wwtiotTestUserKey, "unverified-response-sign")
	})
}

func TestInstalledAppWWTIOTTransportFailureIsRedactedFromPersistentAPI(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	closedURL := "http://" + listener.Addr().String()
	if err := listener.Close(); err != nil {
		t.Fatal(err)
	}

	withAuthTestDatabase(t, func(db *sql.DB) {
		application, server, token := newInstalledWWTIOTTestApp(t, db, closedURL)
		defer application.close()
		defer server.Close()
		serverURL := server.URL
		projectID := processCreateProject(t, serverURL, token, "")
		deviceID := createWWTIOTProcessDevice(t, serverURL, token, projectID, "768901037825")
		commandID := createWWTIOTProcessCommand(t, serverURL, token, projectID, deviceID, "query_status", "wwtiot-redaction")
		store := repository.NewPostgresStore(db)
		waitForProcessState(t, 15*time.Second, func() (bool, error) {
			command, err := store.Commands().Get(context.Background(), commandID)
			return err == nil && command.Status == domain.CommandStatusFailed, err
		})

		command, err := store.Commands().Get(context.Background(), commandID)
		if err != nil || wwtiotStringValue(command.ReasonCode) != "provider_transport_error" ||
			wwtiotStringValue(command.ReasonDetail) != "WWTIOT transport failed before request write" {
			t.Fatalf("redacted transport Command=%+v err=%v", command, err)
		}
		attempts, err := store.Commands().ListAttempts(context.Background(), commandID)
		if err != nil || len(attempts) != 1 || attempts[0].Outcome == nil ||
			*attempts[0].Outcome != domain.AttemptOutcomeTransportErrorBeforeSend ||
			wwtiotStringValue(attempts[0].ErrorDetail) != "WWTIOT transport failed before request write" {
			t.Fatalf("redacted transport Attempts=%+v err=%v", attempts, err)
		}

		status, detail, err := processJSONRequest(serverURL, token, http.MethodGet, "/v1/device-commands/"+commandID, "")
		if err != nil || status != http.StatusOK {
			t.Fatalf("transport Command detail status=%d body=%+v err=%v", status, detail, err)
		}
		assertJSONOmits(t, detail, closedURL, wwtiotTestUserID, wwtiotTestUserKey)
	})
}

func newInstalledWWTIOTTestApp(t *testing.T, db *sql.DB, endpoint string) (*app, *httptest.Server, string) {
	t.Helper()
	seedAuthTestAdmin(t, db)
	application, err := newApp(config{
		DatabaseURL:                processTestDatabaseURL(t, db),
		RedisURL:                   requireProcessRedisTestURL(t),
		JWTSecret:                  testJWTSecret,
		WebhookSecretEncryptionKey: []byte("0123456789abcdef0123456789abcdef"),
		Installed:                  true,
		ReadHeaderTimeout:          5 * time.Second,
		WWTIOTAPIURL:               endpoint,
		WWTIOTUserID:               wwtiotTestUserID,
		WWTIOTUserKey:              wwtiotTestUserKey,
	}, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(application.routes())
	return application, server, processLogin(t, server.URL)
}

func createWWTIOTProcessDevice(t *testing.T, baseURL, token, projectID, providerDeviceID string) string {
	t.Helper()
	body, err := json.Marshal(map[string]any{
		"project_id": projectID, "name": "WWTIOT Contract Lock", "device_type_code": "smart-lock",
		"provider_code": "wwtiot", "provider_profile": "wwtiot-cloud-api-v2", "provider_device_id": providerDeviceID,
	})
	if err != nil {
		t.Fatal(err)
	}
	status, envelope, err := processJSONRequest(baseURL, token, http.MethodPost, "/v1/devices", string(body))
	if err != nil || status != http.StatusCreated {
		t.Fatalf("create WWTIOT Device status=%d body=%+v err=%v", status, envelope, err)
	}
	var device struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(envelope.Data, &device); err != nil || device.ID == "" {
		t.Fatalf("decode WWTIOT Device=%+v err=%v", device, err)
	}
	return device.ID
}

func createWWTIOTProcessCommand(t *testing.T, baseURL, token, projectID, deviceID, action, idempotencyKey string) string {
	t.Helper()
	body := commandCreateBody(projectID, deviceID, action, idempotencyKey, `{}`)
	status, envelope, err := processJSONRequest(baseURL, token, http.MethodPost, "/v1/device-commands", body)
	if err != nil || status != http.StatusCreated {
		t.Fatalf("create WWTIOT Command status=%d body=%+v err=%v", status, envelope, err)
	}
	var command struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(envelope.Data, &command); err != nil || command.ID == "" {
		t.Fatalf("decode WWTIOT Command=%+v err=%v", command, err)
	}
	return command.ID
}

func assertExactWWTIOTStatusRequest(t *testing.T, request map[string]any, providerDeviceID string) {
	t.Helper()
	if len(request) != 7 || request["userid"] != wwtiotTestUserID || request["cmd"] != "control" ||
		request["type"] != float64(23) || request["value"] != float64(4) || request["deviceid"] != providerDeviceID {
		t.Fatalf("WWTIOT request=%+v", request)
	}
	serialFloat, ok := request["serialnum"].(float64)
	if !ok || serialFloat != float64(int64(serialFloat)) {
		t.Fatalf("WWTIOT serialnum=%#v", request["serialnum"])
	}
	serial := strconv.FormatInt(int64(serialFloat), 10)
	digest := md5.Sum([]byte(wwtiotTestUserID + "control" + "23" + "4" + providerDeviceID + serial + wwtiotTestUserKey))
	if request["sign"] != hex.EncodeToString(digest[:]) {
		t.Fatalf("WWTIOT sign=%v, want exact V2 digest", request["sign"])
	}
}

func assertJSONOmits(t *testing.T, value any, forbidden ...string) {
	t.Helper()
	encoded, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	for _, item := range forbidden {
		if item != "" && strings.Contains(string(encoded), item) {
			t.Fatalf("JSON leaked %q: %s", item, encoded)
		}
	}
}

func wwtiotStringValue(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}
