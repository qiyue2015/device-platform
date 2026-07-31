package webhookworker

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"strings"
	"testing"
	"time"

	"github.com/qiyue2015/device-platform/internal/domain"
	"github.com/qiyue2015/device-platform/internal/storage/repository"
)

type stubStore struct{}

func (stubStore) Projects() repository.ProjectQueries { return nil }
func (stubStore) Events() repository.EventQueries     { return nil }
func (stubStore) Webhooks() repository.WebhookQueries { return nil }
func (stubStore) Audits() repository.AuditQueries     { return nil }
func (stubStore) TransactWebhookAudit(context.Context, func(repository.WebhookAuditTx) error) error {
	return nil
}

type stubSecretResolver struct{}

func (stubSecretResolver) ResolveWebhookSecret(context.Context, string, int) (string, error) {
	return "secret", nil
}

type stubHTTPClient struct{}

func (stubHTTPClient) Do(*http.Request) (*http.Response, error) { return nil, errors.New("unused") }

func TestNewValidatesConfigWithoutPanicking(t *testing.T) {
	validDependencies := func(config Config) error {
		config.Client = stubHTTPClient{}
		_, err := New(stubStore{}, stubSecretResolver{}, config)
		return err
	}
	for _, config := range []Config{
		{MaxAttempts: maximumAttempts + 1},
		{MaxAttempts: 2, RetrySchedule: []time.Duration{time.Nanosecond}},
		{MaxAttempts: 2, RetrySchedule: []time.Duration{500 * time.Millisecond}},
		{MaxAttempts: 2, RetrySchedule: []time.Duration{}},
	} {
		if err := validDependencies(config); !errors.Is(err, ErrInvalidConfig) {
			t.Fatalf("New(%+v) error=%v", config, err)
		}
	}
	if _, err := New(nil, stubSecretResolver{}, Config{Client: stubHTTPClient{}}); !errors.Is(err, ErrInvalidConfig) {
		t.Fatalf("nil store error=%v", err)
	}
	worker, err := New(stubStore{}, stubSecretResolver{}, Config{MaxAttempts: 1, Client: stubHTTPClient{}})
	if err != nil || len(worker.retrySchedule) != 0 || worker.maxAttempts != 1 {
		t.Fatalf("single-attempt worker=%+v err=%v", worker, err)
	}
}

func TestWebhookRequestSignsExactTimestampAndRawBody(t *testing.T) {
	delivery := domain.WebhookDelivery{
		EventID: "event-1", TargetURL: "https://hooks.example.test/events",
		RawBody: []byte("{\n  \"event_id\": \"event-1\"\n}"),
	}
	attempt := domain.WebhookDeliveryAttempt{RequestTimestamp: 1785474000}
	request, err := webhookRequest(context.Background(), delivery, attempt, "whsec_test")
	if err != nil {
		t.Fatal(err)
	}
	body, err := io.ReadAll(request.Body)
	if err != nil || !bytes.Equal(body, delivery.RawBody) {
		t.Fatalf("body=%q err=%v", body, err)
	}
	mac := hmac.New(sha256.New, []byte("whsec_test"))
	_, _ = mac.Write([]byte("1785474000."))
	_, _ = mac.Write(delivery.RawBody)
	wantSignature := "sha256=" + hex.EncodeToString(mac.Sum(nil))
	if request.Method != http.MethodPost || request.Header.Get("X-Device-Platform-Timestamp") != "1785474000" ||
		request.Header.Get("X-Device-Platform-Signature") != wantSignature ||
		request.Header.Get("X-Device-Platform-Event-ID") != delivery.EventID {
		t.Fatalf("request=%s headers=%v", request.Method, request.Header)
	}
}

func TestWebhookRequestRejectsUnsafeSnapshots(t *testing.T) {
	for _, target := range []string{
		"http://example.test/hook",
		"ftp://example.test/hook",
		"https://user:password@example.test/hook",
		"https://example.test/hook#fragment",
		"/relative",
	} {
		_, err := webhookRequest(context.Background(), domain.WebhookDelivery{
			EventID: "event-1", TargetURL: target, RawBody: []byte(`{"event_id":"event-1"}`),
		}, domain.WebhookDeliveryAttempt{RequestTimestamp: 1}, "secret")
		if err == nil {
			t.Fatalf("unsafe target accepted: %s", target)
		}
	}
	for _, target := range []string{"http://localhost/hook", "http://127.0.0.1/hook", "https://example.test/hook"} {
		if _, err := webhookRequest(context.Background(), domain.WebhookDelivery{
			EventID: "event-1", TargetURL: target, RawBody: []byte(`{"event_id":"event-1"}`),
		}, domain.WebhookDeliveryAttempt{RequestTimestamp: 1}, "secret"); err != nil {
			t.Fatalf("valid target %s rejected: %v", target, err)
		}
	}
}

func TestSummarizeResponseBoundsAndOmitsBodyValues(t *testing.T) {
	secret := "top-secret-response-value"
	response := &http.Response{
		Header: http.Header{"Content-Type": []string{"application/json; charset=utf-8"}},
		Body:   io.NopCloser(strings.NewReader(`{"password":"` + secret + `"}`)),
	}
	summary := summarizeResponse(response)
	if strings.Contains(summary, secret) || !strings.Contains(summary, `"media_type":"application/json"`) {
		t.Fatalf("summary=%s", summary)
	}
	large := &http.Response{Header: http.Header{}, Body: io.NopCloser(bytes.NewReader(bytes.Repeat([]byte("x"), responseCaptureLimit+500)))}
	summary = summarizeResponse(large)
	if !strings.Contains(summary, `"captured_bytes":4096`) || !strings.Contains(summary, `"truncated":true`) || strings.Contains(summary, strings.Repeat("x", 16)) {
		t.Fatalf("large summary=%s", summary)
	}
}

func TestEgressPolicyDefaultsAndAllowlist(t *testing.T) {
	policy := egressPolicy{}
	for _, raw := range []string{
		"10.0.0.2", "100.64.0.1", "127.0.0.1", "169.254.169.254", "192.0.2.1",
		"64:ff9b::a00:1", "2001:db8::1", "2002:a00:1::", "fec0::1",
	} {
		if policy.allows(netip.MustParseAddr(raw)) {
			t.Fatalf("default egress policy allowed %s", raw)
		}
	}
	if !policy.allows(netip.MustParseAddr("8.8.8.8")) {
		t.Fatal("default egress policy rejected a public address")
	}
	allowPrivate := egressPolicy{allowlist: []netip.Prefix{netip.MustParsePrefix("10.0.0.0/8")}}
	if !allowPrivate.allows(netip.MustParseAddr("10.0.0.2")) {
		t.Fatal("explicit private allowlist was ignored")
	}
	allowAllLinkLocal := egressPolicy{allowlist: []netip.Prefix{netip.MustParsePrefix("169.254.0.0/16")}}
	for _, raw := range []string{"169.254.0.23", "169.254.169.254", "169.254.170.2"} {
		if allowAllLinkLocal.allows(netip.MustParseAddr(raw)) {
			t.Fatalf("cloud metadata address %s was opened by a broad allowlist", raw)
		}
	}
	allowAzure := egressPolicy{allowlist: []netip.Prefix{netip.MustParsePrefix("168.63.129.0/24")}}
	if allowAzure.allows(netip.MustParseAddr("168.63.129.16")) {
		t.Fatal("Azure platform address was opened by a broad allowlist")
	}
}

func TestSecureHTTPClientDoesNotFollowRedirects(t *testing.T) {
	var finalCalls int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/start" {
			http.Redirect(w, r, "/final", http.StatusFound)
			return
		}
		finalCalls++
		w.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()
	client, err := NewSecureHTTPClient(time.Second, []netip.Prefix{netip.MustParsePrefix("127.0.0.0/8")})
	if err != nil {
		t.Fatal(err)
	}
	response, err := client.Get(server.URL + "/start")
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusFound || finalCalls != 0 {
		t.Fatalf("redirect status/final calls=%d/%d", response.StatusCode, finalCalls)
	}
}
