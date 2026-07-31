//go:build integration

package webhookworker

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"net/url"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	_ "github.com/lib/pq"
	"github.com/qiyue2015/device-platform/internal/domain"
	"github.com/qiyue2015/device-platform/internal/projectservice"
	"github.com/qiyue2015/device-platform/internal/storage"
	"github.com/qiyue2015/device-platform/internal/storage/repository"
)

const (
	webhookWorkerEventID    = "77000000-0000-4000-8000-000000000001"
	webhookWorkerDeliveryID = "78000000-0000-4000-8000-000000000001"
	webhookWorkerDeviceID   = "31000000-0000-4000-8000-000000000001"
)

type receivedWebhook struct {
	body      []byte
	timestamp string
	signature string
	eventID   string
}

func TestPersistentWebhookWorkerDeliversSignedSnapshotAndHistory(t *testing.T) {
	withWebhookWorkerDatabase(t, func(db *sql.DB, store *repository.PostgresStore) {
		var mu sync.Mutex
		var received []receivedWebhook
		endpoint := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
			body, _ := io.ReadAll(request.Body)
			mu.Lock()
			received = append(received, receivedWebhook{
				body: body, timestamp: request.Header.Get("X-Device-Platform-Timestamp"),
				signature: request.Header.Get("X-Device-Platform-Signature"), eventID: request.Header.Get("X-Device-Platform-Event-ID"),
			})
			mu.Unlock()
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(299)
		}))
		defer endpoint.Close()

		service, projectID, secret := createWebhookWorkerFixture(t, store, endpoint.URL)
		rawBody := []byte(`{"schema_version":1,"event_id":"` + webhookWorkerEventID + `","data":{"safe":true}}`)
		createWorkerDelivery(t, store, projectID, webhookWorkerEventID, webhookWorkerDeliveryID, rawBody)
		if _, err := service.RotateWebhookSecret(context.Background(), projectID, projectservice.RequestMetadata{
			ActorType: domain.ActorTypeAdmin, ActorID: "admin-test", RequestID: "79000000-0000-4000-8000-000000000002",
		}); err != nil {
			t.Fatal(err)
		}
		client, err := NewSecureHTTPClient(2*time.Second, []netip.Prefix{netip.MustParsePrefix("127.0.0.0/8")})
		if err != nil {
			t.Fatal(err)
		}
		worker, err := New(store, service, Config{WorkerID: "delivery-worker", MaxAttempts: 1, Client: client})
		if err != nil {
			t.Fatal(err)
		}
		worked, err := worker.DispatchNext(context.Background())
		if err != nil || !worked {
			t.Fatalf("DispatchNext worked=%v err=%v", worked, err)
		}

		mu.Lock()
		requests := append([]receivedWebhook(nil), received...)
		mu.Unlock()
		if len(requests) != 1 || !bytes.Equal(requests[0].body, rawBody) || requests[0].eventID != webhookWorkerEventID {
			t.Fatalf("received Webhook requests=%+v", requests)
		}
		assertWebhookSignature(t, requests[0], secret)
		delivery, err := store.Webhooks().GetDelivery(context.Background(), webhookWorkerDeliveryID)
		attempts, attemptsErr := store.Webhooks().ListAttempts(context.Background(), webhookWorkerDeliveryID)
		if err != nil || attemptsErr != nil || delivery.Status != domain.WebhookDeliveryStatusDelivered ||
			delivery.AttemptCount != 1 || delivery.DeliveredAt == nil || len(attempts) != 1 || attempts[0].HTTPStatus == nil ||
			*attempts[0].HTTPStatus != 299 || attempts[0].CompletedAt == nil {
			t.Fatalf("delivery=%+v attempts=%+v err=%v attemptsErr=%v", delivery, attempts, err, attemptsErr)
		}
		if attempts[0].ResponseSummary == nil || !strings.Contains(*attempts[0].ResponseSummary, `"captured_bytes":0`) {
			t.Fatalf("response summary=%v", attempts[0].ResponseSummary)
		}
		_ = db
	})
}

func TestPersistentWebhookWorkerRetriesExactBodyAndEndsDead(t *testing.T) {
	withWebhookWorkerDatabase(t, func(db *sql.DB, store *repository.PostgresStore) {
		var mu sync.Mutex
		var received []receivedWebhook
		endpoint := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
			body, _ := io.ReadAll(request.Body)
			mu.Lock()
			received = append(received, receivedWebhook{
				body: body, timestamp: request.Header.Get("X-Device-Platform-Timestamp"),
				signature: request.Header.Get("X-Device-Platform-Signature"), eventID: request.Header.Get("X-Device-Platform-Event-ID"),
			})
			mu.Unlock()
			w.WriteHeader(http.StatusServiceUnavailable)
			_, _ = w.Write([]byte(`{"password":"must-not-be-stored"}`))
		}))
		defer endpoint.Close()

		service, projectID, secret := createWebhookWorkerFixture(t, store, endpoint.URL)
		rawBody := []byte(`{"schema_version":1,"event_id":"` + webhookWorkerEventID + `"}`)
		createWorkerDelivery(t, store, projectID, webhookWorkerEventID, webhookWorkerDeliveryID, rawBody)
		client, _ := NewSecureHTTPClient(2*time.Second, []netip.Prefix{netip.MustParsePrefix("127.0.0.0/8")})
		worker, err := New(store, service, Config{
			WorkerID: "retry-worker", MaxAttempts: 2, RetrySchedule: []time.Duration{time.Second}, Client: client,
		})
		if err != nil {
			t.Fatal(err)
		}
		if worked, err := worker.DispatchNext(context.Background()); err != nil || !worked {
			t.Fatalf("first dispatch worked=%v err=%v", worked, err)
		}
		failed, err := store.Webhooks().GetDelivery(context.Background(), webhookWorkerDeliveryID)
		firstAttempts, attemptsErr := store.Webhooks().ListAttempts(context.Background(), webhookWorkerDeliveryID)
		if err != nil || attemptsErr != nil || failed.NextAttemptAt == nil || len(firstAttempts) != 1 || firstAttempts[0].CompletedAt == nil {
			t.Fatalf("first failure delivery=%+v attempts=%+v err=%v attemptsErr=%v", failed, firstAttempts, err, attemptsErr)
		}
		delay := failed.NextAttemptAt.Sub(*firstAttempts[0].CompletedAt)
		if delay < 750*time.Millisecond || delay > 1250*time.Millisecond {
			t.Fatalf("first retry delay=%s, want 1s", delay)
		}
		if _, err := db.Exec(`UPDATE webhook_deliveries SET next_attempt_at = now() - interval '1 second' WHERE id = $1`, webhookWorkerDeliveryID); err != nil {
			t.Fatal(err)
		}
		if worked, err := worker.DispatchNext(context.Background()); err != nil || !worked {
			t.Fatalf("second dispatch worked=%v err=%v", worked, err)
		}

		mu.Lock()
		requests := append([]receivedWebhook(nil), received...)
		mu.Unlock()
		if len(requests) != 2 {
			t.Fatalf("request count=%d", len(requests))
		}
		for _, request := range requests {
			if !bytes.Equal(request.body, rawBody) {
				t.Fatalf("retry changed raw body: %q", request.body)
			}
			assertWebhookSignature(t, request, secret)
		}
		delivery, err := store.Webhooks().GetDelivery(context.Background(), webhookWorkerDeliveryID)
		attempts, attemptsErr := store.Webhooks().ListAttempts(context.Background(), webhookWorkerDeliveryID)
		if err != nil || attemptsErr != nil || delivery.Status != domain.WebhookDeliveryStatusDead || delivery.AttemptCount != 2 || len(attempts) != 2 {
			t.Fatalf("delivery=%+v attempts=%+v err=%v attemptsErr=%v", delivery, attempts, err, attemptsErr)
		}
		for _, attempt := range attempts {
			if attempt.ResponseSummary == nil || strings.Contains(*attempt.ResponseSummary, "must-not-be-stored") || attempt.ErrorCode == nil || *attempt.ErrorCode != "http_error" {
				t.Fatalf("unsafe or incomplete Attempt=%+v", attempt)
			}
		}
	})
}

func TestPersistentWebhookWorkerRecoversLeaseAndHonorsLoweredAttemptLimit(t *testing.T) {
	t.Run("lowered maximum", func(t *testing.T) {
		withWebhookWorkerDatabase(t, func(_ *sql.DB, store *repository.PostgresStore) {
			endpoint := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(http.StatusServiceUnavailable)
			}))
			defer endpoint.Close()
			service, projectID, _ := createWebhookWorkerFixture(t, store, endpoint.URL)
			createWorkerDelivery(t, store, projectID, webhookWorkerEventID, webhookWorkerDeliveryID, []byte(`{"event_id":"`+webhookWorkerEventID+`"}`))
			client, _ := NewSecureHTTPClient(time.Second, []netip.Prefix{netip.MustParsePrefix("127.0.0.0/8")})
			worker, err := New(store, service, Config{MaxAttempts: 3, Client: client})
			if err != nil {
				t.Fatal(err)
			}
			if worked, err := worker.DispatchNext(context.Background()); err != nil || !worked {
				t.Fatalf("dispatch worked=%v err=%v", worked, err)
			}
			lowered, err := New(store, service, Config{MaxAttempts: 1, Client: client})
			if err != nil {
				t.Fatal(err)
			}
			if exhausted, err := lowered.ExhaustNext(context.Background()); err != nil || !exhausted {
				t.Fatalf("exhausted=%v err=%v", exhausted, err)
			}
			delivery, err := store.Webhooks().GetDelivery(context.Background(), webhookWorkerDeliveryID)
			if err != nil || delivery.Status != domain.WebhookDeliveryStatusDead || delivery.AttemptCount != 1 {
				t.Fatalf("lowered maximum delivery=%+v err=%v", delivery, err)
			}
		})
	})

	t.Run("expired lease restart and late fencing", func(t *testing.T) {
		withWebhookWorkerDatabase(t, func(db *sql.DB, store *repository.PostgresStore) {
			endpoint := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(http.StatusNoContent)
			}))
			defer endpoint.Close()
			service, projectID, _ := createWebhookWorkerFixture(t, store, endpoint.URL)
			createWorkerDelivery(t, store, projectID, webhookWorkerEventID, webhookWorkerDeliveryID, []byte(`{"event_id":"`+webhookWorkerEventID+`"}`))
			leaseToken := "05000000-0000-4000-8000-000000000001"
			var staleAttempt domain.WebhookDeliveryAttempt
			if err := store.TransactWebhookAudit(context.Background(), func(tx repository.WebhookAuditTx) error {
				_, attempt, claimed, err := tx.Webhooks().ClaimDue(context.Background(), repository.ClaimWebhookRequest{
					WorkerID: "crashed-worker", LeaseToken: leaseToken, LeaseDuration: 5 * time.Millisecond, MaxAttempts: 2,
				})
				staleAttempt = attempt
				if err == nil && !claimed {
					return errors.New("expected delivery claim")
				}
				return err
			}); err != nil {
				t.Fatal(err)
			}
			time.Sleep(15 * time.Millisecond)
			client, _ := NewSecureHTTPClient(time.Second, []netip.Prefix{netip.MustParsePrefix("127.0.0.0/8")})
			restarted, err := New(store, service, Config{MaxAttempts: 2, RetrySchedule: []time.Duration{time.Second}, Client: client})
			if err != nil {
				t.Fatal(err)
			}
			if recovered, err := restarted.RecoverNext(context.Background()); err != nil || !recovered {
				t.Fatalf("recovered=%v err=%v", recovered, err)
			}
			if err := store.TransactWebhookAudit(context.Background(), func(tx repository.WebhookAuditTx) error {
				updated, err := tx.Webhooks().CompleteAttempt(context.Background(), webhookWorkerDeliveryID, leaseToken, repository.CompleteWebhookAttemptRequest{
					AttemptID: staleAttempt.ID, HTTPStatus: intPointer(http.StatusNoContent), MaxAttempts: 2,
				})
				if err == nil && updated {
					return errors.New("late worker bypassed lease fencing")
				}
				return err
			}); err != nil {
				t.Fatal(err)
			}
			if _, err := db.Exec(`UPDATE webhook_deliveries SET next_attempt_at = now() - interval '1 second' WHERE id = $1`, webhookWorkerDeliveryID); err != nil {
				t.Fatal(err)
			}
			if worked, err := restarted.DispatchNext(context.Background()); err != nil || !worked {
				t.Fatalf("restart dispatch worked=%v err=%v", worked, err)
			}
			delivery, err := store.Webhooks().GetDelivery(context.Background(), webhookWorkerDeliveryID)
			attempts, attemptsErr := store.Webhooks().ListAttempts(context.Background(), webhookWorkerDeliveryID)
			if err != nil || attemptsErr != nil || delivery.Status != domain.WebhookDeliveryStatusDelivered || len(attempts) != 2 || attempts[0].ErrorCode == nil || *attempts[0].ErrorCode != "worker_lease_expired" {
				t.Fatalf("restart delivery=%+v attempts=%+v err=%v attemptsErr=%v", delivery, attempts, err, attemptsErr)
			}
		})
	})
}

func TestPersistentWebhookWorkerHandlesTransportBoundaries(t *testing.T) {
	for _, test := range []struct {
		name          string
		endpoint      func(*testing.T) (string, HTTPClient, func() int)
		wantErrorCode string
	}{
		{
			name: "network error",
			endpoint: func(*testing.T) (string, HTTPClient, func() int) {
				return "https://hooks.example.test/events", stubHTTPClient{}, func() int { return 0 }
			},
			wantErrorCode: "transport_error",
		},
		{
			name: "timeout",
			endpoint: func(t *testing.T) (string, HTTPClient, func() int) {
				server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
					time.Sleep(80 * time.Millisecond)
					w.WriteHeader(http.StatusNoContent)
				}))
				t.Cleanup(server.Close)
				client, _ := NewSecureHTTPClient(20*time.Millisecond, []netip.Prefix{netip.MustParsePrefix("127.0.0.0/8")})
				return server.URL, client, func() int { return 0 }
			},
			wantErrorCode: "transport_error",
		},
		{
			name: "redirect",
			endpoint: func(t *testing.T) (string, HTTPClient, func() int) {
				finalCalls := 0
				server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					if r.URL.Path == "/start" {
						http.Redirect(w, r, "/final", http.StatusTemporaryRedirect)
						return
					}
					finalCalls++
				}))
				t.Cleanup(server.Close)
				client, _ := NewSecureHTTPClient(time.Second, []netip.Prefix{netip.MustParsePrefix("127.0.0.0/8")})
				return server.URL + "/start", client, func() int { return finalCalls }
			},
			wantErrorCode: "http_error",
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			withWebhookWorkerDatabase(t, func(_ *sql.DB, store *repository.PostgresStore) {
				endpoint, client, finalCalls := test.endpoint(t)
				service, projectID, _ := createWebhookWorkerFixture(t, store, endpoint)
				createWorkerDelivery(t, store, projectID, webhookWorkerEventID, webhookWorkerDeliveryID, []byte(`{"event_id":"`+webhookWorkerEventID+`"}`))
				worker, err := New(store, service, Config{MaxAttempts: 1, Client: client})
				if err != nil {
					t.Fatal(err)
				}
				if worked, err := worker.DispatchNext(context.Background()); err != nil || !worked {
					t.Fatalf("dispatch worked=%v err=%v", worked, err)
				}
				delivery, err := store.Webhooks().GetDelivery(context.Background(), webhookWorkerDeliveryID)
				if err != nil || delivery.Status != domain.WebhookDeliveryStatusDead || delivery.LastErrorCode == nil || *delivery.LastErrorCode != test.wantErrorCode || finalCalls() != 0 {
					t.Fatalf("delivery=%+v finalCalls=%d err=%v", delivery, finalCalls(), err)
				}
				if delivery.LastErrorDetail != nil && strings.Contains(*delivery.LastErrorDetail, "unused") {
					t.Fatalf("transport error leaked cause: %q", *delivery.LastErrorDetail)
				}
			})
		})
	}
}

func TestPersistentWebhookWorkerAuditsSecretDecryptionFailureAtomically(t *testing.T) {
	withWebhookWorkerDatabase(t, func(_ *sql.DB, store *repository.PostgresStore) {
		endpoint := "https://hooks.example.test/events"
		_, projectID, _ := createWebhookWorkerFixture(t, store, endpoint)
		rawBody := []byte(`{"schema_version":1,"event_id":"` + webhookWorkerEventID + `"}`)
		createWorkerDelivery(t, store, projectID, webhookWorkerEventID, webhookWorkerDeliveryID, rawBody)
		wrongResolver, err := projectservice.New(store, projectservice.Config{
			EncryptionKeys: map[int][]byte{1: bytes.Repeat([]byte{0x33}, 32)}, ActiveEncryptionKeyVersion: 1,
		})
		if err != nil {
			t.Fatal(err)
		}
		worker, err := New(store, wrongResolver, Config{MaxAttempts: 1, Client: stubHTTPClient{}})
		if err != nil {
			t.Fatal(err)
		}
		if worked, err := worker.DispatchNext(context.Background()); err != nil || !worked {
			t.Fatalf("secret failure dispatch worked=%v err=%v", worked, err)
		}
		delivery, err := store.Webhooks().GetDelivery(context.Background(), webhookWorkerDeliveryID)
		if err != nil || delivery.Status != domain.WebhookDeliveryStatusDead || delivery.LastErrorCode == nil || *delivery.LastErrorCode != "secret_resolution_failed" {
			t.Fatalf("secret failure Delivery=%+v err=%v", delivery, err)
		}
		action := "project.webhook_secret_decryption_failed"
		result := domain.AuditResultFailure
		audits, total, err := store.Audits().List(context.Background(), repository.ListAuditsRequest{
			ProjectID: &projectID, Action: &action, Result: &result, Limit: 10,
		})
		if err != nil || total != 1 || len(audits) != 1 {
			t.Fatalf("secret failure Audits=%+v total=%d err=%v", audits, total, err)
		}
		audit := audits[0]
		if audit.ActorType != domain.ActorTypeSystem || audit.Metadata["error_code"] != "secret_decryption_failed" ||
			audit.Metadata["webhook_secret_version"] != float64(1) || audit.Metadata["encryption_key_version"] != float64(1) {
			t.Fatalf("secret failure Audit=%+v", audit)
		}
	})
}

func TestPersistentWebhookWorkerRollsBackCompletionWhenSecurityAuditFails(t *testing.T) {
	withWebhookWorkerDatabase(t, func(_ *sql.DB, store *repository.PostgresStore) {
		endpoint := "https://hooks.example.test/events"
		_, projectID, _ := createWebhookWorkerFixture(t, store, endpoint)
		createWorkerDelivery(t, store, projectID, webhookWorkerEventID, webhookWorkerDeliveryID, []byte(`{"event_id":"`+webhookWorkerEventID+`"}`))
		wrongResolver, err := projectservice.New(store, projectservice.Config{
			EncryptionKeys: map[int][]byte{1: bytes.Repeat([]byte{0x33}, 32)}, ActiveEncryptionKeyVersion: 1,
		})
		if err != nil {
			t.Fatal(err)
		}
		sentinel := errors.New("audit write failed")
		worker, err := New(failingAuditStore{PostgresStore: store, err: sentinel}, wrongResolver, Config{MaxAttempts: 1, Client: stubHTTPClient{}})
		if err != nil {
			t.Fatal(err)
		}
		worked, err := worker.DispatchNext(context.Background())
		if !worked || !errors.Is(err, sentinel) {
			t.Fatalf("dispatch worked=%v err=%v", worked, err)
		}
		delivery, getErr := store.Webhooks().GetDelivery(context.Background(), webhookWorkerDeliveryID)
		attempts, attemptsErr := store.Webhooks().ListAttempts(context.Background(), webhookWorkerDeliveryID)
		if getErr != nil || attemptsErr != nil || delivery.Status != domain.WebhookDeliveryStatusSending || len(attempts) != 1 || attempts[0].CompletedAt != nil {
			t.Fatalf("rolled back delivery=%+v attempts=%+v getErr=%v attemptsErr=%v", delivery, attempts, getErr, attemptsErr)
		}
		action := "project.webhook_secret_decryption_failed"
		audits, total, auditErr := store.Audits().List(context.Background(), repository.ListAuditsRequest{Action: &action, Limit: 10})
		if auditErr != nil || total != 0 || len(audits) != 0 {
			t.Fatalf("rolled back audits=%+v total=%d err=%v", audits, total, auditErr)
		}
	})
}

type failingAuditStore struct {
	*repository.PostgresStore
	err error
}

func (s failingAuditStore) TransactWebhookAudit(ctx context.Context, fn func(repository.WebhookAuditTx) error) error {
	return s.PostgresStore.TransactWebhookAudit(ctx, func(tx repository.WebhookAuditTx) error {
		return fn(failingAuditTx{WebhookAuditTx: tx, err: s.err})
	})
}

type failingAuditTx struct {
	repository.WebhookAuditTx
	err error
}

func (tx failingAuditTx) Audits() repository.AuditRepository {
	return failingAuditRepository{AuditRepository: tx.WebhookAuditTx.Audits(), err: tx.err}
}

type failingAuditRepository struct {
	repository.AuditRepository
	err error
}

func (r failingAuditRepository) Create(context.Context, domain.AuditLog) error { return r.err }

func createWebhookWorkerFixture(t *testing.T, store *repository.PostgresStore, endpoint string) (*projectservice.Service, string, string) {
	t.Helper()
	service, err := projectservice.New(store, projectservice.Config{
		EncryptionKeys: map[int][]byte{1: bytes.Repeat([]byte{0x5a}, 32)}, ActiveEncryptionKeyVersion: 1,
	})
	if err != nil {
		t.Fatal(err)
	}
	created, err := service.Create(context.Background(), projectservice.CreateRequest{Name: "Webhook Worker", WebhookURL: &endpoint}, projectservice.RequestMetadata{
		ActorType: domain.ActorTypeAdmin, ActorID: "admin-test", RequestID: "79000000-0000-4000-8000-000000000001",
	})
	if err != nil {
		t.Fatal(err)
	}
	deviceType, err := store.DeviceTypes().GetByCode(context.Background(), domain.DeviceTypeSmartLock)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC().Truncate(time.Microsecond)
	if err := store.WithinTransaction(context.Background(), func(tx *repository.PostgresTx) error {
		return tx.Devices().Create(context.Background(), domain.Device{
			ID: webhookWorkerDeviceID, ProjectID: created.Project.ID, DeviceTypeID: deviceType.ID,
			Name: "Webhook Worker Lock", ProviderCode: domain.ProviderCodeSimulator,
			ProviderDeviceID: webhookWorkerDeviceID, AccessType: domain.AccessTypeSimulator,
			TransportProtocol: domain.TransportProtocolInternal, Adapter: domain.AdapterSimulator,
			ConnectionStatus: domain.ConnectionStatusUnknown, LifecycleStatus: domain.LifecycleStatusActive,
			CreatedAt: now, UpdatedAt: now,
		})
	}); err != nil {
		t.Fatal(err)
	}
	return service, created.Project.ID, created.WebhookSecret
}

func createWorkerDelivery(t *testing.T, store *repository.PostgresStore, projectID, eventID, deliveryID string, rawBody []byte) {
	t.Helper()
	now := time.Now().UTC().Truncate(time.Microsecond)
	err := store.WithinTransaction(context.Background(), func(tx *repository.PostgresTx) error {
		if err := tx.Events().Create(context.Background(), domain.Event{
			ID: eventID, SchemaVersion: domain.EventSchemaVersion, EventType: domain.EventTypeDeviceCreated,
			ProjectID: projectID, DeviceID: stringPointer(webhookWorkerDeviceID), Source: domain.EventSourceAdmin,
			Payload:          map[string]any{"device_type_code": "smart-lock", "provider_code": "simulator", "lifecycle_status": "active"},
			DeduplicationKey: "worker-event", OccurredAt: now, CreatedAt: now,
		}); err != nil {
			return err
		}
		_, created, err := tx.Webhooks().CreateDelivery(context.Background(), repository.CreateWebhookDeliveryRequest{
			ID: deliveryID, EventID: eventID, RawBody: rawBody,
		})
		if err == nil && !created {
			return fmt.Errorf("Webhook Delivery was not created")
		}
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
}

func stringPointer(value string) *string { return &value }
func intPointer(value int) *int          { return &value }

func assertWebhookSignature(t *testing.T, request receivedWebhook, secret string) {
	t.Helper()
	mac := hmac.New(sha256.New, []byte(secret))
	_, _ = mac.Write([]byte(request.timestamp + "."))
	_, _ = mac.Write(request.body)
	want := "sha256=" + hex.EncodeToString(mac.Sum(nil))
	if request.timestamp == "" || request.signature != want {
		t.Fatalf("signature timestamp/value=%q/%q, want %q", request.timestamp, request.signature, want)
	}
}

func withWebhookWorkerDatabase(t *testing.T, fn func(*sql.DB, *repository.PostgresStore)) {
	t.Helper()
	baseURL := strings.TrimSpace(os.Getenv("MIGRATION_TEST_DATABASE_URL"))
	if baseURL == "" {
		t.Skip("MIGRATION_TEST_DATABASE_URL is not set")
	}
	admin, err := sql.Open("postgres", baseURL)
	if err != nil {
		t.Fatal(err)
	}
	defer admin.Close()
	schema := fmt.Sprintf("webhook_worker_test_%d", time.Now().UnixNano())
	if _, err := admin.Exec(`CREATE SCHEMA ` + schema); err != nil {
		t.Fatal(err)
	}
	defer func() {
		if _, err := admin.Exec(`DROP SCHEMA ` + schema + ` CASCADE`); err != nil {
			t.Errorf("drop Webhook worker schema: %v", err)
		}
	}()
	parsed, err := url.Parse(baseURL)
	if err != nil {
		t.Fatal(err)
	}
	query := parsed.Query()
	query.Set("search_path", schema)
	parsed.RawQuery = query.Encode()
	db, err := sql.Open("postgres", parsed.String())
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err := storage.ApplyMigrations(context.Background(), db); err != nil {
		t.Fatal(err)
	}
	fn(db, repository.NewPostgresStore(db))
}
