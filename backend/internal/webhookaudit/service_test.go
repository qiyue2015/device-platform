package webhookaudit

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) Do(req *http.Request) (*http.Response, error) {
	return f(req)
}

func TestCreateEventCreatesSignedDelivery(t *testing.T) {
	var gotSignature string
	client := roundTripFunc(func(req *http.Request) (*http.Response, error) {
		gotSignature = req.Header.Get("X-Device-Platform-Signature")
		if gotSignature == "" {
			t.Fatal("missing signature header")
		}
		if req.Header.Get("X-Device-Platform-Event") != "command_finished" {
			t.Fatalf("unexpected event header: %s", req.Header.Get("X-Device-Platform-Event"))
		}
		return response(202, "accepted"), nil
	})
	service := NewService(client)
	must(t, service.UpsertProject(ProjectEndpoint{
		ProjectID:     "proj_1",
		WebhookURL:    "https://example.test/webhook",
		WebhookSecret: "secret",
	}))

	event, delivery, err := service.CreateEvent(context.Background(), CreateEventRequest{
		ProjectID: "proj_1",
		DeviceID:  "dev_1",
		EventType: "command_finished",
		Payload:   map[string]any{"status": "success"},
	})
	must(t, err)
	if event.ID == "" || delivery == nil {
		t.Fatalf("expected event and delivery, got event=%+v delivery=%+v", event, delivery)
	}

	deliveries := service.ListDeliveries()
	if len(deliveries) != 1 {
		t.Fatalf("expected one delivery, got %d", len(deliveries))
	}
	got := deliveries[0]
	if got.Status != StatusDelivered {
		t.Fatalf("expected delivered, got %s", got.Status)
	}
	if got.AttemptCount != 1 {
		t.Fatalf("expected one attempt, got %d", got.AttemptCount)
	}
	if got.Signature == "" || got.Signature != gotSignature {
		t.Fatalf("signature not persisted/sent: persisted=%q sent=%q", got.Signature, gotSignature)
	}
	if !strings.HasPrefix(got.Signature, "sha256=") {
		t.Fatalf("unexpected signature format: %s", got.Signature)
	}
}

func TestDeliveryRetriesThreeTimesThenDead(t *testing.T) {
	calls := 0
	service := NewService(roundTripFunc(func(req *http.Request) (*http.Response, error) {
		calls++
		return response(500, "nope"), nil
	}))
	service.SetRetryBase(0)
	must(t, service.UpsertProject(ProjectEndpoint{
		ProjectID:     "proj_1",
		WebhookURL:    "https://example.test/fail",
		WebhookSecret: "secret",
	}))

	_, first, err := service.CreateEvent(context.Background(), CreateEventRequest{
		ProjectID: "proj_1",
		EventType: "state_changed",
		Payload:   map[string]any{"online": true},
	})
	must(t, err)
	if first == nil {
		t.Fatal("expected delivery")
	}

	service.RetryDue(context.Background())
	service.RetryDue(context.Background())
	service.RetryDue(context.Background())

	got := service.ListDeliveries()[0]
	if got.Status != StatusDead {
		t.Fatalf("expected dead, got %+v", got)
	}
	if got.AttemptCount != MaxDeliveryAttempts {
		t.Fatalf("expected %d attempts, got %d", MaxDeliveryAttempts, got.AttemptCount)
	}
	if calls != MaxDeliveryAttempts {
		t.Fatalf("expected %d HTTP calls, got %d", MaxDeliveryAttempts, calls)
	}
}

func TestOnlyDeadDeliveryCanBeResent(t *testing.T) {
	service := NewService(roundTripFunc(func(req *http.Request) (*http.Response, error) {
		return response(202, "ok"), nil
	}))
	must(t, service.UpsertProject(ProjectEndpoint{
		ProjectID:  "proj_1",
		WebhookURL: "https://example.test/ok",
	}))

	_, delivery, err := service.CreateEvent(context.Background(), CreateEventRequest{
		ProjectID: "proj_1",
		EventType: "command_finished",
		Payload:   map[string]any{"status": "success"},
	})
	must(t, err)
	if delivery == nil {
		t.Fatal("expected delivery")
	}
	if _, err := service.ResendDead(context.Background(), delivery.ID); err == nil {
		t.Fatal("expected non-dead resend to fail")
	}
}

func TestDeadDeliveryManualResendResetsAttempts(t *testing.T) {
	calls := 0
	service := NewService(roundTripFunc(func(req *http.Request) (*http.Response, error) {
		calls++
		if calls <= MaxDeliveryAttempts {
			return response(500, "fail"), nil
		}
		return response(204, ""), nil
	}))
	service.SetRetryBase(0)
	must(t, service.UpsertProject(ProjectEndpoint{
		ProjectID:  "proj_1",
		WebhookURL: "https://example.test/recover",
	}))

	_, delivery, err := service.CreateEvent(context.Background(), CreateEventRequest{
		ProjectID: "proj_1",
		EventType: "state_changed",
	})
	must(t, err)
	for i := 0; i < MaxDeliveryAttempts; i++ {
		service.RetryDue(context.Background())
	}
	dead := service.ListDeliveries()[0]
	if dead.Status != StatusDead {
		t.Fatalf("expected dead before resend, got %s", dead.Status)
	}

	resent, err := service.ResendDead(context.Background(), delivery.ID)
	must(t, err)
	if resent.Status != StatusDelivered {
		t.Fatalf("expected delivered after resend, got %+v", resent)
	}
	if resent.AttemptCount != 1 {
		t.Fatalf("expected resend attempt count reset to 1, got %d", resent.AttemptCount)
	}
}

func TestRecordAudit(t *testing.T) {
	service := NewService(nil)
	audit, err := service.RecordAudit(AuditRequest{
		Action:       "command.created",
		ActorType:    "open-api",
		ProjectID:    "proj_1",
		ResourceType: "device_command",
		ResourceID:   "cmd_1",
		IPAddress:    "127.0.0.1",
		Metadata:     map[string]any{"status": "queued"},
	})
	must(t, err)
	if audit.ID == "" || audit.CreatedAt.IsZero() {
		t.Fatalf("audit was not populated: %+v", audit)
	}
	items := service.ListAudits()
	if len(items) != 1 || items[0].Action != "command.created" {
		t.Fatalf("unexpected audits: %+v", items)
	}
}

func response(status int, body string) *http.Response {
	return &http.Response{
		StatusCode: status,
		Body:       io.NopCloser(strings.NewReader(body)),
		Header:     http.Header{},
	}
}

func must(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}
