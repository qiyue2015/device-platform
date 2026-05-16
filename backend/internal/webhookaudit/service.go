package webhookaudit

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strings"
	"sync"
	"time"
)

var (
	ErrInvalidArgument = errors.New("invalid argument")
	ErrNotFound        = errors.New("not found")
	ErrNotDeadDelivery = errors.New("only dead webhook deliveries can be resent")
	ErrDeliveryBusy    = errors.New("webhook delivery is already being processed")
)

type HTTPClient interface {
	Do(req *http.Request) (*http.Response, error)
}

type Service struct {
	mu         sync.Mutex
	nextID     int64
	projects   map[string]ProjectEndpoint
	events     map[string]*Event
	deliveries map[string]*WebhookDelivery
	audits     map[string]*AuditLog
	client     HTTPClient
	now        func() time.Time
	retryBase  time.Duration
}

func NewService(client HTTPClient) *Service {
	if client == nil {
		client = http.DefaultClient
	}
	return &Service{
		nextID:     1000,
		projects:   map[string]ProjectEndpoint{},
		events:     map[string]*Event{},
		deliveries: map[string]*WebhookDelivery{},
		audits:     map[string]*AuditLog{},
		client:     client,
		now:        func() time.Time { return time.Now().UTC() },
		retryBase:  time.Second,
	}
}

func (s *Service) SetRetryBase(delay time.Duration) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.retryBase = delay
}

func (s *Service) UpsertProject(project ProjectEndpoint) error {
	project.ProjectID = strings.TrimSpace(project.ProjectID)
	project.WebhookURL = strings.TrimSpace(project.WebhookURL)
	if project.ProjectID == "" {
		return fmt.Errorf("%w: project_id is required", ErrInvalidArgument)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.projects[project.ProjectID] = project
	return nil
}

func (s *Service) CreateEvent(ctx context.Context, req CreateEventRequest) (Event, *WebhookDelivery, error) {
	req.ProjectID = strings.TrimSpace(req.ProjectID)
	req.EventType = strings.TrimSpace(req.EventType)
	req.Source = strings.TrimSpace(req.Source)
	if req.ProjectID == "" || req.EventType == "" {
		return Event{}, nil, fmt.Errorf("%w: project_id and event_type are required", ErrInvalidArgument)
	}
	if req.Source == "" {
		req.Source = "system"
	}
	if req.Payload == nil {
		req.Payload = map[string]any{}
	}

	s.mu.Lock()
	now := s.now()
	event := &Event{
		ID:         s.newIDLocked("evt"),
		ProjectID:  req.ProjectID,
		DeviceID:   strings.TrimSpace(req.DeviceID),
		CommandID:  strings.TrimSpace(req.CommandID),
		EventType:  req.EventType,
		Source:     req.Source,
		Payload:    cloneMap(req.Payload),
		OccurredAt: now,
		CreatedAt:  now,
	}
	s.events[event.ID] = event

	var delivery *WebhookDelivery
	project, ok := s.projects[event.ProjectID]
	if ok && project.WebhookURL != "" {
		body := webhookBody(event)
		bodyBytes, err := json.Marshal(body)
		if err != nil {
			s.mu.Unlock()
			return Event{}, nil, err
		}
		signature := SignPayload(project.WebhookSecret, bodyBytes)
		delivery = &WebhookDelivery{
			ID:           s.newIDLocked("wh"),
			EventID:      event.ID,
			ProjectID:    event.ProjectID,
			DeviceID:     event.DeviceID,
			WebhookURL:   project.WebhookURL,
			Status:       StatusPending,
			MaxAttempts:  MaxDeliveryAttempts,
			RequestBody:  body,
			Signature:    signature,
			CreatedAt:    now,
			UpdatedAt:    now,
			AttemptCount: 0,
		}
		s.deliveries[delivery.ID] = delivery
	}
	eventCopy := cloneEvent(*event)
	deliveryCopy := cloneDeliveryPtr(delivery)
	s.mu.Unlock()

	if deliveryCopy != nil {
		_, _ = s.Deliver(ctx, deliveryCopy.ID)
	}
	return eventCopy, deliveryCopy, nil
}

func (s *Service) Deliver(ctx context.Context, deliveryID string) (WebhookDelivery, error) {
	attempt, err := s.beginAttempt(deliveryID)
	if err != nil {
		return WebhookDelivery{}, err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, attempt.WebhookURL, bytes.NewReader(attempt.body))
	if err != nil {
		return s.finishAttempt(deliveryID, 0, "", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Device-Platform-Signature", attempt.Signature)
	req.Header.Set("X-Device-Platform-Event", attempt.EventType)

	resp, err := s.client.Do(req)
	if err != nil {
		return s.finishAttempt(deliveryID, 0, "", err)
	}
	defer resp.Body.Close()
	respBody, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
	return s.finishAttempt(deliveryID, resp.StatusCode, string(respBody), nil)
}

func (s *Service) ResendDead(ctx context.Context, deliveryID string) (WebhookDelivery, error) {
	s.mu.Lock()
	delivery := s.deliveries[deliveryID]
	if delivery == nil {
		s.mu.Unlock()
		return WebhookDelivery{}, ErrNotFound
	}
	if delivery.Status != StatusDead {
		s.mu.Unlock()
		return WebhookDelivery{}, ErrNotDeadDelivery
	}
	delivery.Status = StatusPending
	delivery.AttemptCount = 0
	delivery.LastResponseCode = 0
	delivery.LastResponseBody = ""
	delivery.LastError = ""
	delivery.NextRetryAt = nil
	delivery.DeliveredAt = nil
	delivery.UpdatedAt = s.now()
	s.mu.Unlock()
	return s.Deliver(ctx, deliveryID)
}

func (s *Service) ListDeliveries() []WebhookDelivery {
	s.mu.Lock()
	defer s.mu.Unlock()
	items := make([]WebhookDelivery, 0, len(s.deliveries))
	for _, delivery := range s.deliveries {
		items = append(items, cloneDelivery(*delivery))
	}
	sort.Slice(items, func(i, j int) bool {
		return items[i].CreatedAt.After(items[j].CreatedAt)
	})
	return items
}

func (s *Service) ListEvents() []Event {
	s.mu.Lock()
	defer s.mu.Unlock()
	items := make([]Event, 0, len(s.events))
	for _, event := range s.events {
		items = append(items, cloneEvent(*event))
	}
	sort.Slice(items, func(i, j int) bool {
		return items[i].CreatedAt.After(items[j].CreatedAt)
	})
	return items
}

func (s *Service) RecordAudit(req AuditRequest) (AuditLog, error) {
	req.Action = strings.TrimSpace(req.Action)
	req.ActorType = strings.TrimSpace(req.ActorType)
	req.ResourceType = strings.TrimSpace(req.ResourceType)
	if req.Action == "" || req.ActorType == "" || req.ResourceType == "" {
		return AuditLog{}, fmt.Errorf("%w: action, actor_type, and resource_type are required", ErrInvalidArgument)
	}
	if req.Metadata == nil {
		req.Metadata = map[string]any{}
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	now := s.now()
	audit := &AuditLog{
		ID:           s.newIDLocked("aud"),
		Action:       req.Action,
		ActorType:    req.ActorType,
		ProjectID:    strings.TrimSpace(req.ProjectID),
		ResourceType: req.ResourceType,
		ResourceID:   strings.TrimSpace(req.ResourceID),
		RequestID:    strings.TrimSpace(req.RequestID),
		IPAddress:    strings.TrimSpace(req.IPAddress),
		UserAgent:    strings.TrimSpace(req.UserAgent),
		Metadata:     cloneMap(req.Metadata),
		CreatedAt:    now,
	}
	s.audits[audit.ID] = audit
	return cloneAudit(*audit), nil
}

func (s *Service) ListAudits() []AuditLog {
	s.mu.Lock()
	defer s.mu.Unlock()
	items := make([]AuditLog, 0, len(s.audits))
	for _, audit := range s.audits {
		items = append(items, cloneAudit(*audit))
	}
	sort.Slice(items, func(i, j int) bool {
		return items[i].CreatedAt.After(items[j].CreatedAt)
	})
	return items
}

type attempt struct {
	WebhookURL string
	Signature  string
	EventType  string
	body       []byte
}

func (s *Service) beginAttempt(deliveryID string) (attempt, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delivery := s.deliveries[deliveryID]
	if delivery == nil {
		return attempt{}, ErrNotFound
	}
	if delivery.Status != StatusPending && delivery.Status != StatusFailed {
		return attempt{}, ErrDeliveryBusy
	}
	event := s.events[delivery.EventID]
	if event == nil {
		return attempt{}, ErrNotFound
	}
	body, err := json.Marshal(delivery.RequestBody)
	if err != nil {
		return attempt{}, err
	}
	delivery.AttemptCount++
	delivery.Status = StatusSending
	delivery.UpdatedAt = s.now()
	return attempt{
		WebhookURL: delivery.WebhookURL,
		Signature:  delivery.Signature,
		EventType:  event.EventType,
		body:       body,
	}, nil
}

func (s *Service) finishAttempt(deliveryID string, code int, body string, deliveryErr error) (WebhookDelivery, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delivery := s.deliveries[deliveryID]
	if delivery == nil {
		return WebhookDelivery{}, ErrNotFound
	}
	now := s.now()
	delivery.LastResponseCode = code
	delivery.LastResponseBody = body
	delivery.UpdatedAt = now
	if deliveryErr == nil && code >= 200 && code < 300 {
		delivery.Status = StatusDelivered
		delivery.LastError = ""
		delivery.NextRetryAt = nil
		delivery.DeliveredAt = &now
		return cloneDelivery(*delivery), nil
	}

	if deliveryErr != nil {
		delivery.LastError = deliveryErr.Error()
	} else {
		delivery.LastError = fmt.Sprintf("webhook responded with status %d", code)
	}
	if delivery.AttemptCount >= MaxDeliveryAttempts {
		delivery.Status = StatusDead
		delivery.NextRetryAt = nil
		return cloneDelivery(*delivery), nil
	}
	next := now.Add(time.Duration(delivery.AttemptCount) * s.retryBase)
	delivery.Status = StatusFailed
	delivery.NextRetryAt = &next
	return cloneDelivery(*delivery), nil
}

func (s *Service) RetryDue(ctx context.Context) int {
	now := s.now()
	var ids []string
	s.mu.Lock()
	for _, delivery := range s.deliveries {
		if delivery.Status == StatusFailed && delivery.NextRetryAt != nil && !delivery.NextRetryAt.After(now) {
			ids = append(ids, delivery.ID)
		}
	}
	s.mu.Unlock()
	for _, id := range ids {
		_, _ = s.Deliver(ctx, id)
	}
	return len(ids)
}

func (s *Service) newIDLocked(prefix string) string {
	s.nextID++
	return fmt.Sprintf("%s_%d", prefix, s.nextID)
}

func webhookBody(event *Event) map[string]any {
	return map[string]any{
		"event_id":    event.ID,
		"event_type":  event.EventType,
		"project_id":  event.ProjectID,
		"device_id":   event.DeviceID,
		"command_id":  event.CommandID,
		"payload":     cloneMap(event.Payload),
		"occurred_at": event.OccurredAt,
	}
}

func SignPayload(secret string, body []byte) string {
	if secret == "" {
		secret = "local-dev-webhook-secret"
	}
	mac := hmac.New(sha256.New, []byte(secret))
	_, _ = mac.Write(body)
	return "sha256=" + hex.EncodeToString(mac.Sum(nil))
}

func cloneMap(in map[string]any) map[string]any {
	out := make(map[string]any, len(in))
	for key, value := range in {
		out[key] = value
	}
	return out
}

func cloneEvent(in Event) Event {
	in.Payload = cloneMap(in.Payload)
	return in
}

func cloneDeliveryPtr(in *WebhookDelivery) *WebhookDelivery {
	if in == nil {
		return nil
	}
	out := cloneDelivery(*in)
	return &out
}

func cloneDelivery(in WebhookDelivery) WebhookDelivery {
	in.RequestBody = cloneMap(in.RequestBody)
	if in.NextRetryAt != nil {
		next := *in.NextRetryAt
		in.NextRetryAt = &next
	}
	if in.DeliveredAt != nil {
		delivered := *in.DeliveredAt
		in.DeliveredAt = &delivered
	}
	return in
}

func cloneAudit(in AuditLog) AuditLog {
	in.Metadata = cloneMap(in.Metadata)
	return in
}
