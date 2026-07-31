package httpjson

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestWriteWithMetaPreservesPagination(t *testing.T) {
	recorder := httptest.NewRecorder()
	recorder.Header().Set("X-Request-ID", "request-pagination")
	WriteWithMeta(recorder, http.StatusOK, "ok", map[string]any{"items": []any{}}, map[string]any{
		"page": 2, "page_size": 20, "total": 21,
	})
	var response Response
	if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
		t.Fatal(err)
	}
	meta, ok := response.Meta.(map[string]any)
	if recorder.Code != http.StatusOK || !ok || meta["page"] != float64(2) || meta["page_size"] != float64(20) || meta["total"] != float64(21) {
		t.Fatalf("pagination envelope mismatch: code=%d response=%+v", recorder.Code, response)
	}
}

func TestEnvelopeAlwaysIncludesFrozenFields(t *testing.T) {
	tests := []struct {
		name  string
		write func(http.ResponseWriter)
	}{
		{name: "success", write: func(w http.ResponseWriter) { OK(w, map[string]bool{"ok": true}) }},
		{name: "error", write: func(w http.ResponseWriter) { Error(w, http.StatusBadRequest, "invalid_request", "invalid request") }},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			recorder := httptest.NewRecorder()
			test.write(recorder)
			var raw map[string]json.RawMessage
			if err := json.Unmarshal(recorder.Body.Bytes(), &raw); err != nil {
				t.Fatal(err)
			}
			for _, field := range []string{"success", "status", "code", "message", "error_code", "data", "meta", "request_id"} {
				if _, exists := raw[field]; !exists {
					t.Fatalf("response omitted frozen field %q: %s", field, recorder.Body.String())
				}
			}
			if string(raw["meta"]) != "null" {
				t.Fatalf("ordinary envelope meta = %s, want null", raw["meta"])
			}
		})
	}
}
