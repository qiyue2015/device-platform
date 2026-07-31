package webhookaudit

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/qiyue2015/device-platform/internal/domain"
	"github.com/qiyue2015/device-platform/internal/storage/repository"
)

type unusedPersistentStore struct {
	transactions int
}

func (*unusedPersistentStore) Events() repository.EventQueries     { return nil }
func (*unusedPersistentStore) Webhooks() repository.WebhookQueries { return nil }
func (*unusedPersistentStore) Audits() repository.AuditQueries     { return nil }
func (s *unusedPersistentStore) TransactWebhookAudit(context.Context, func(repository.WebhookAuditTx) error) error {
	s.transactions++
	return errors.New("unexpected transaction")
}

func TestNewPersistentServiceConfiguration(t *testing.T) {
	if _, err := newPersistentService(nil, PersistentConfig{}); !errors.Is(err, ErrInvalidRequest) {
		t.Fatalf("nil store error = %v", err)
	}
	store := &unusedPersistentStore{}
	service := NewPersistentService(store)
	if service.store != store || service.random == nil || service.clock == nil {
		t.Fatalf("Persistent service defaults = %+v", service)
	}
}

func TestPersistentServiceRejectsInvalidRequestsBeforePersistence(t *testing.T) {
	store := &unusedPersistentStore{}
	service := NewPersistentService(store)
	ctx := context.Background()
	invalidUUID := "invalid"
	invalidEventType := "future"
	invalidStatus := "future"
	invalidActorType := "open_api"
	invalidAction := "future"
	invalidResult := "pending"
	empty := ""

	checks := []func() error{
		func() error { _, err := service.ListEvents(ctx, EventListRequest{Page: -1}); return err },
		func() error {
			_, err := service.ListEvents(ctx, EventListRequest{ProjectID: &invalidUUID})
			return err
		},
		func() error {
			_, err := service.ListEvents(ctx, EventListRequest{EventType: &invalidEventType})
			return err
		},
		func() error { _, err := service.GetEvent(ctx, invalidUUID); return err },
		func() error {
			_, err := service.ListDeliveries(ctx, DeliveryListRequest{Status: &invalidStatus})
			return err
		},
		func() error { _, err := service.GetDelivery(ctx, invalidUUID); return err },
		func() error {
			_, err := service.ListAudits(ctx, AuditListRequest{ActorType: &invalidActorType})
			return err
		},
		func() error {
			_, err := service.ListAudits(ctx, AuditListRequest{Action: &invalidAction})
			return err
		},
		func() error {
			_, err := service.ListAudits(ctx, AuditListRequest{Result: &invalidResult})
			return err
		},
		func() error {
			_, err := service.ListAudits(ctx, AuditListRequest{ResourceType: &empty})
			return err
		},
		func() error { _, err := service.GetAudit(ctx, invalidUUID); return err },
		func() error {
			_, err := service.ReplayDead(ctx, "65000000-0000-0000-0000-000000000001", ReplayRequest{RequestID: "request"})
			return err
		},
	}
	for index, check := range checks {
		if err := check(); !errors.Is(err, ErrInvalidRequest) {
			t.Fatalf("invalid check %d error = %v", index, err)
		}
	}
	if store.transactions != 0 {
		t.Fatalf("invalid requests opened %d transactions", store.transactions)
	}
}

func TestPersistentDeliveryDTOIsSafeAndDistinguishesDetailAttempts(t *testing.T) {
	now := time.Date(2026, 7, 31, 12, 0, 0, 0, time.UTC)
	secretVersion := 7
	lease := "05000000-0000-4000-8000-000000000001"
	delivery := domain.WebhookDelivery{
		ID: "65000000-0000-0000-0000-000000000001", ProjectID: "10000000-0000-0000-0000-000000000001",
		EventID: "75000000-0000-0000-0000-000000000001", TargetURL: "https://hooks.example.test/events",
		WebhookConfigVersion: 3, WebhookSecretVersion: secretVersion, RawBody: []byte("RAW_PRIVATE_MARKER"),
		Status: domain.WebhookDeliveryStatusSending, AttemptCount: 1, LeaseToken: &lease, LeaseOwner: &lease,
		LeaseExpiresAt: &now, CreatedAt: now, UpdatedAt: now,
	}
	listJSON := mustPersistentJSON(t, safeDelivery(delivery, nil))
	if strings.Contains(listJSON, "attempts") {
		t.Fatalf("Delivery list DTO embedded Attempts: %s", listJSON)
	}
	detailJSON := mustPersistentJSON(t, safeDelivery(delivery, []domain.WebhookDeliveryAttempt{}))
	if !strings.Contains(detailJSON, `"attempts":[]`) {
		t.Fatalf("Delivery detail empty Attempts = %s", detailJSON)
	}
	for _, forbidden := range []string{"RAW_PRIVATE_MARKER", "raw_body", "webhook_secret", "lease_token", "lease_owner", "lease_expires_at", "signature"} {
		if strings.Contains(listJSON, forbidden) || strings.Contains(detailJSON, forbidden) {
			t.Fatalf("Delivery DTO exposed %q: list=%s detail=%s", forbidden, listJSON, detailJSON)
		}
	}
}

func TestPersistentServiceIdentifierFailureDoesNotStartReplayTransaction(t *testing.T) {
	store := &unusedPersistentStore{}
	service, err := newPersistentService(store, PersistentConfig{Random: bytes.NewReader(nil)})
	if err != nil {
		t.Fatal(err)
	}
	_, err = service.ReplayDead(context.Background(), "65000000-0000-0000-0000-000000000001", ReplayRequest{
		ActorID: "admin-id", IPAddress: "192.0.2.1", RequestID: "request-id",
	})
	if !errors.Is(err, ErrIdentifierGeneration) || store.transactions != 0 {
		t.Fatalf("identifier failure err=%v transactions=%d", err, store.transactions)
	}
}

func mustPersistentJSON(t *testing.T, value any) string {
	t.Helper()
	encoded, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	return string(encoded)
}
