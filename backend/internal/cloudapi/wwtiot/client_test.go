package wwtiot

import (
	"context"
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/qiyue2015/device-platform/internal/domain"
)

func TestConfigValidationAndFixedTimeout(t *testing.T) {
	tests := []struct {
		name string
		cfg  Config
		want bool
	}{
		{name: "valid", cfg: Config{APIURL: "https://gps.example.test/api/", UserID: " user ", UserKey: " key "}, want: true},
		{name: "missing URL", cfg: Config{UserID: "user", UserKey: "key"}},
		{name: "URL userinfo", cfg: Config{APIURL: "https://user@gps.example.test/api/", UserID: "user", UserKey: "key"}},
		{name: "URL query", cfg: Config{APIURL: "https://gps.example.test/api/?x=1", UserID: "user", UserKey: "key"}},
		{name: "unsupported scheme", cfg: Config{APIURL: "ftp://gps.example.test/api/", UserID: "user", UserKey: "key"}},
		{name: "blank user", cfg: Config{APIURL: "https://gps.example.test/api/", UserID: "  ", UserKey: "key"}},
		{name: "blank key remains valid bytes", cfg: Config{APIURL: "https://gps.example.test/api/", UserID: "user", UserKey: " "}, want: true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			client := NewClient(test.cfg, &http.Client{})
			if client.Configured() != test.want {
				t.Fatalf("Configured() = %v, want %v", client.Configured(), test.want)
			}
			if client.httpClient.Timeout != requestTimeout {
				t.Fatalf("timeout = %s, want %s", client.httpClient.Timeout, requestTimeout)
			}
		})
	}
}

func TestDispatchBuildsExactV2Requests(t *testing.T) {
	tests := []struct {
		action domain.ActionIdentifier
		want   map[string]any
	}{
		{action: "unlock", want: map[string]any{"userid": "test-user", "cmd": "open", "deviceid": "768901037824", "serialnum": float64(123456789), "sign": "a1ab79da05120d8aa6960a20018d90ed"}},
		{action: "lock", want: map[string]any{"userid": "test-user", "cmd": "close", "deviceid": "768901037824", "serialnum": float64(123456789), "sign": "321c4575135521da8e5fdcf77f30cbf0"}},
		{action: "query_status", want: map[string]any{"userid": "test-user", "cmd": "control", "type": float64(23), "value": float64(4), "deviceid": "768901037824", "serialnum": float64(123456789), "sign": "49ba348aaa7aec30957ad1e381d54877"}},
	}
	for _, test := range tests {
		t.Run(string(test.action), func(t *testing.T) {
			var received map[string]any
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method != http.MethodPost || r.Header.Get("Content-Type") != "application/json" {
					w.WriteHeader(http.StatusBadRequest)
					return
				}
				if err := json.NewDecoder(r.Body).Decode(&received); err != nil {
					w.WriteHeader(http.StatusBadRequest)
					return
				}
				response := cloneObject(received)
				response["result"] = "ok"
				response["info"] = "cmd send ok"
				response["sign"] = "unverified-response-sign"
				_ = json.NewEncoder(w).Encode(response)
			}))
			defer server.Close()

			result := testClient(server.URL).Dispatch(context.Background(), dispatchRequest(test.action))
			assertResult(t, result, domain.AttemptOutcomeProviderAccepted, domain.ConfirmationProviderAccepted, domain.EvidenceUnverified)
			if !equalJSONObjects(received, test.want) {
				t.Fatalf("request = %#v, want %#v", received, test.want)
			}
			assertNoSecrets(t, result, "test-user", "secret-key", "unverified-response-sign")
		})
	}
}

func TestDispatchRejectsInvalidRequestsWithoutHTTP(t *testing.T) {
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { calls.Add(1) }))
	defer server.Close()
	client := testClient(server.URL)
	tests := []DispatchRequest{
		{ProviderDeviceID: "bad id", Action: "unlock", Payload: map[string]any{}, ProviderRequestKey: "1"},
		{ProviderDeviceID: "device-1", Action: "unlock", Payload: map[string]any{}, ProviderRequestKey: "0"},
		{ProviderDeviceID: "device-1", Action: "unknown", Payload: map[string]any{}, ProviderRequestKey: "1"},
		{ProviderDeviceID: "device-1", Action: "unlock", Payload: map[string]any{"value": 1}, ProviderRequestKey: "1"},
	}
	for _, request := range tests {
		result := client.Dispatch(context.Background(), request)
		assertResult(t, result, domain.AttemptOutcomeInvalidRequest, domain.ConfirmationNone, domain.EvidenceNone)
	}
	if calls.Load() != 0 {
		t.Fatalf("invalid requests made %d HTTP calls", calls.Load())
	}
}

func TestDispatchStrictResponseMatrix(t *testing.T) {
	valid := `{"result":"ok","info":"cmd send ok","userid":"test-user","cmd":"control","type":23,"value":4,"deviceid":"768901037824","serialnum":"123456789","sign":"response-sign"}`
	tests := []struct {
		name     string
		status   int
		body     string
		outcome  domain.AttemptOutcome
		confirm  domain.ConfirmationLevel
		evidence domain.EvidenceStatus
	}{
		{name: "accepted", status: 200, body: valid, outcome: domain.AttemptOutcomeProviderAccepted, confirm: domain.ConfirmationProviderAccepted, evidence: domain.EvidenceUnverified},
		{name: "rejected", status: 200, body: `{"result":"denied","info":"not allowed"}`, outcome: domain.AttemptOutcomeProviderRejected, confirm: domain.ConfirmationTransportSent, evidence: domain.EvidenceUnverified},
		{name: "uppercase result rejected", status: 200, body: `{"result":"OK"}`, outcome: domain.AttemptOutcomeProviderRejected, confirm: domain.ConfirmationTransportSent, evidence: domain.EvidenceUnverified},
		{name: "non 2xx", status: 503, body: valid, outcome: domain.AttemptOutcomeInvalidResponse, confirm: domain.ConfirmationTransportSent, evidence: domain.EvidenceVerified},
		{name: "empty", status: 200, body: "", outcome: domain.AttemptOutcomeInvalidResponse, confirm: domain.ConfirmationTransportSent, evidence: domain.EvidenceVerified},
		{name: "array", status: 200, body: `[]`, outcome: domain.AttemptOutcomeInvalidResponse, confirm: domain.ConfirmationTransportSent, evidence: domain.EvidenceVerified},
		{name: "duplicate key", status: 200, body: `{"result":"ok","result":"ok"}`, outcome: domain.AttemptOutcomeInvalidResponse, confirm: domain.ConfirmationTransportSent, evidence: domain.EvidenceVerified},
		{name: "trailing JSON", status: 200, body: valid + `{}`, outcome: domain.AttemptOutcomeInvalidResponse, confirm: domain.ConfirmationTransportSent, evidence: domain.EvidenceVerified},
		{name: "missing result", status: 200, body: `{"info":"cmd send ok"}`, outcome: domain.AttemptOutcomeInvalidResponse, confirm: domain.ConfirmationTransportSent, evidence: domain.EvidenceVerified},
		{name: "wrong result type", status: 200, body: `{"result":true}`, outcome: domain.AttemptOutcomeInvalidResponse, confirm: domain.ConfirmationTransportSent, evidence: domain.EvidenceVerified},
		{name: "missing success echo", status: 200, body: `{"result":"ok","sign":"x"}`, outcome: domain.AttemptOutcomeInvalidResponse, confirm: domain.ConfirmationTransportSent, evidence: domain.EvidenceVerified},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(test.status)
				_, _ = w.Write([]byte(test.body))
			}))
			defer server.Close()
			result := testClient(server.URL).Dispatch(context.Background(), dispatchRequest("query_status"))
			assertResult(t, result, test.outcome, test.confirm, test.evidence)
			assertNoSecrets(t, result, "test-user", "secret-key", "response-sign")
		})
	}
}

func TestDispatchResponseLimitAndSummaryLimit(t *testing.T) {
	t.Run("response body", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			_, _ = w.Write([]byte(`{"result":"` + strings.Repeat("x", maxResponseBytes) + `"}`))
		}))
		defer server.Close()
		result := testClient(server.URL).Dispatch(context.Background(), dispatchRequest("unlock"))
		assertResult(t, result, domain.AttemptOutcomeInvalidResponse, domain.ConfirmationTransportSent, domain.EvidenceVerified)
	})
	t.Run("summary text", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			_ = json.NewEncoder(w).Encode(map[string]any{"result": "denied", "info": strings.Repeat("a", maxSummaryTextBytes+100), "userid": "secret-user", "sign": "secret-sign"})
		}))
		defer server.Close()
		result := testClient(server.URL).Dispatch(context.Background(), dispatchRequest("unlock"))
		if len(result.ErrorDetail) != maxSummaryTextBytes || len(result.ResponseSummary["info"].(string)) != maxSummaryTextBytes {
			t.Fatalf("summary limits not enforced: detail=%d response=%d", len(result.ErrorDetail), len(result.ResponseSummary["info"].(string)))
		}
		assertNoSecrets(t, result, "secret-user", "secret-sign", "secret-key", "test-user")
	})
}

func TestDispatchDoesNotFollowRedirect(t *testing.T) {
	var redirected atomic.Int32
	target := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { redirected.Add(1) }))
	defer target.Close()
	redirect := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Redirect(w, &http.Request{}, target.URL, http.StatusFound)
	}))
	defer redirect.Close()
	result := testClient(redirect.URL).Dispatch(context.Background(), dispatchRequest("unlock"))
	assertResult(t, result, domain.AttemptOutcomeInvalidResponse, domain.ConfirmationTransportSent, domain.EvidenceVerified)
	if result.HTTPStatus != http.StatusFound || redirected.Load() != 0 {
		t.Fatalf("redirect status=%d followed=%d", result.HTTPStatus, redirected.Load())
	}
}

func TestDispatchClassifiesTransportBeforeAndAfterSend(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	closedURL := "http://" + listener.Addr().String()
	if err := listener.Close(); err != nil {
		t.Fatal(err)
	}
	before := testClient(closedURL).Dispatch(context.Background(), dispatchRequest("unlock"))
	assertResult(t, before, domain.AttemptOutcomeTransportErrorBeforeSend, domain.ConfirmationNone, domain.EvidenceNone)

	afterServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		hijacker, ok := w.(http.Hijacker)
		if !ok {
			return
		}
		connection, _, err := hijacker.Hijack()
		if err == nil {
			_ = connection.Close()
		}
	}))
	defer afterServer.Close()
	after := testClient(afterServer.URL).Dispatch(context.Background(), dispatchRequest("unlock"))
	assertResult(t, after, domain.AttemptOutcomeTransportErrorAfterSend, domain.ConfirmationTransportSent, domain.EvidenceVerified)
}

func testClient(apiURL string) *Client {
	return NewClient(Config{APIURL: apiURL, UserID: " test-user ", UserKey: "secret-key"}, nil)
}

func dispatchRequest(action domain.ActionIdentifier) DispatchRequest {
	return DispatchRequest{ProviderDeviceID: "768901037824", Action: action, Payload: map[string]any{}, ProviderRequestKey: "123456789"}
}

func assertResult(t *testing.T, result DispatchResult, outcome domain.AttemptOutcome, confirmation domain.ConfirmationLevel, evidence domain.EvidenceStatus) {
	t.Helper()
	if result.Outcome != outcome || result.ConfirmationLevel != confirmation || result.EvidenceStatus != evidence {
		t.Fatalf("result tuple = %s/%s/%s, want %s/%s/%s; detail=%q", result.Outcome, result.ConfirmationLevel, result.EvidenceStatus, outcome, confirmation, evidence, result.ErrorDetail)
	}
}

func assertNoSecrets(t *testing.T, result DispatchResult, secrets ...string) {
	t.Helper()
	encoded, err := json.Marshal(result)
	if err != nil {
		t.Fatal(err)
	}
	for _, secret := range secrets {
		if strings.Contains(string(encoded), secret) {
			t.Fatalf("DispatchResult leaked %q: %s", secret, encoded)
		}
	}
}

func cloneObject(value map[string]any) map[string]any {
	cloned := make(map[string]any, len(value))
	for key, item := range value {
		cloned[key] = item
	}
	return cloned
}

func equalJSONObjects(left, right map[string]any) bool {
	leftJSON, _ := json.Marshal(left)
	rightJSON, _ := json.Marshal(right)
	return string(leftJSON) == string(rightJSON)
}
