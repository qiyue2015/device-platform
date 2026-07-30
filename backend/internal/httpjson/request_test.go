package httpjson

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestDecodeStrict(t *testing.T) {
	type payload struct {
		Name string `json:"name"`
		Data any    `json:"data,omitempty"`
	}
	tests := []struct {
		name string
		body string
		want error
	}{
		{name: "valid", body: `{"name":"lock","data":{"items":[{"id":1}]}}`},
		{name: "unknown field", body: `{"name":"lock","extra":true}`, want: ErrUnknownField},
		{name: "duplicate top level", body: `{"name":"lock","name":"other"}`, want: ErrInvalidJSON},
		{name: "duplicate nested", body: `{"name":"lock","data":{"id":1,"id":2}}`, want: ErrInvalidJSON},
		{name: "trailing", body: `{"name":"lock"}{}`, want: ErrInvalidJSON},
		{name: "array root", body: `[]`, want: ErrInvalidJSON},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			var value payload
			err := DecodeStrict(strings.NewReader(test.body), &value)
			if test.want == nil && err != nil {
				t.Fatalf("DecodeStrict: %v", err)
			}
			if test.want != nil && !errors.Is(err, test.want) {
				t.Fatalf("DecodeStrict error = %v, want %v", err, test.want)
			}
		})
	}
}

func TestWithRequestIDUsesServerIdentityAndKeepsValidatedClientIdentitySeparate(t *testing.T) {
	var requestID, clientID string
	handler := WithRequestID(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestID = RequestID(r.Context())
		clientID = ClientRequestID(r.Context())
		OK(w, map[string]bool{"ok": true})
	}))
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/", nil)
	request.Header.Set("X-Request-ID", "client.request-42")
	handler.ServeHTTP(recorder, request)

	serverID := recorder.Header().Get("X-Request-ID")
	if serverID == "" || serverID == "client.request-42" || requestID != serverID {
		t.Fatalf("server request ID was not generated independently: header=%q context=%q", serverID, requestID)
	}
	if clientID != "client.request-42" {
		t.Fatalf("client request ID = %q", clientID)
	}
	if !strings.Contains(recorder.Body.String(), `"request_id":"`+serverID+`"`) {
		t.Fatalf("response envelope does not contain request ID: %s", recorder.Body.String())
	}
}

func TestWithRequestIDIgnoresInvalidClientIdentity(t *testing.T) {
	var clientID string
	handler := WithRequestID(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		clientID = ClientRequestID(r.Context())
		OK(w, nil)
	}))
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/", nil)
	request.Header.Set("X-Request-ID", "invalid id with spaces")
	handler.ServeHTTP(recorder, request)
	if clientID != "" {
		t.Fatalf("invalid client request ID must not be recorded: %q", clientID)
	}
}
