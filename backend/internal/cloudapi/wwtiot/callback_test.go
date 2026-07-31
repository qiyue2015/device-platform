package wwtiot

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/qiyue2015/device-platform/internal/domain"
)

type callbackLookup struct {
	ids []string
	err error
}

func (lookup callbackLookup) LookupWWTIOTDeviceIDs(context.Context, string) ([]string, error) {
	return lookup.ids, lookup.err
}

func TestDecodeCallbackAcceptsDocumentedIntegerForms(t *testing.T) {
	candidate, err := DecodeCallback(strings.NewReader(`{
		"userid":"test-user","cmd":"deviceinfo","deviceid":"device-1",
		"battery":"88","lockstatus":0,"time":"2026-07-31T09:10:11+08:00",
		"serialnum":"0","sign":"secret-sign","lng":"102.1","extra":{"x":1}
	}`))
	if err != nil {
		t.Fatal(err)
	}
	if candidate.Battery != 88 || candidate.LockStatus != 0 || candidate.SerialNumber != 0 {
		t.Fatalf("decoded integers = battery:%d lock:%d serial:%d", candidate.Battery, candidate.LockStatus, candidate.SerialNumber)
	}
	diagnostic := candidate.DiagnosticFields()
	if _, exists := diagnostic["userid"]; exists {
		t.Fatal("diagnostic fields contain userid")
	}
	if _, exists := diagnostic["sign"]; exists {
		t.Fatal("diagnostic fields contain sign")
	}
	if _, exists := diagnostic["extra"]; !exists {
		t.Fatalf("diagnostic fields did not preserve extension: %+v", diagnostic)
	}
}

func TestDecodeCallbackPreservesWWTIOTV2DocumentSampleAsUnverified(t *testing.T) {
	candidate, err := DecodeCallback(strings.NewReader(`{
		"userid":"shrtwater","cmd":"close","deviceid":"768901019955",
		"battery":100,"bike":0,"lockstatus":0,"lng":113.17122,"lat":22.575148,
		"gx":435,"gy":435,"gz":10000,"time":"20210201193822","serialnum":0,
		"sign":"de41d6168a839d648234736e94db6682"
	}`))
	if err != nil {
		t.Fatal(err)
	}
	validator, err := NewCallbackValidator("shrtwater", callbackLookup{ids: []string{"device-uuid"}})
	if err != nil {
		t.Fatal(err)
	}
	mapped, err := validator.Validate(context.Background(), candidate)
	if err != nil {
		t.Fatal(err)
	}
	if candidate.Command != "close" || candidate.ProviderDeviceID != "768901019955" ||
		candidate.Battery != 100 || candidate.LockStatus != 0 || candidate.SerialNumber != 0 {
		t.Fatalf("decoded V2 sample = %+v", candidate)
	}
	if mapped.ReportedAt != nil || mapped.NormalizedState["reported_at"] != nil ||
		mapped.EvidenceStatus != domain.EvidenceUnverified || mapped.SignatureVerified {
		t.Fatalf("V2 sample trust/time = %+v", mapped)
	}
	diagnostic := candidate.DiagnosticFields()
	for _, field := range []string{"bike", "lng", "lat", "gx", "gy", "gz"} {
		if _, ok := diagnostic[field]; !ok {
			t.Fatalf("V2 sample diagnostic field %q missing: %+v", field, diagnostic)
		}
	}
	for _, secret := range []string{"userid", "sign"} {
		if _, ok := diagnostic[secret]; ok {
			t.Fatalf("V2 sample diagnostic fields retained %q", secret)
		}
	}
}

func TestDecodeCallbackStableErrors(t *testing.T) {
	valid := `{"userid":"u","cmd":"c","deviceid":"d","battery":1,"lockstatus":0,"time":"t","serialnum":0,"sign":"s"}`
	tests := []struct {
		name string
		body string
		code string
	}{
		{name: "too large", body: strings.Repeat("x", maxCallbackBytes+1), code: CallbackPayloadTooLarge},
		{name: "array", body: `[]`, code: CallbackInvalidJSON},
		{name: "duplicate", body: `{"userid":"u","userid":"u"}`, code: CallbackInvalidJSON},
		{name: "trailing", body: valid + `{}`, code: CallbackInvalidJSON},
		{name: "missing", body: `{"userid":"u"}`, code: CallbackMissingField},
		{name: "blank string", body: strings.Replace(valid, `"cmd":"c"`, `"cmd":" "`, 1), code: CallbackInvalidField},
		{name: "fraction", body: strings.Replace(valid, `"battery":1`, `"battery":1.5`, 1), code: CallbackInvalidField},
		{name: "boolean integer", body: strings.Replace(valid, `"serialnum":0`, `"serialnum":true`, 1), code: CallbackInvalidField},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, err := DecodeCallback(strings.NewReader(test.body))
			assertCallbackError(t, err, test.code)
		})
	}
}

func TestCallbackValidatorMapsOnlyUnverifiedCandidate(t *testing.T) {
	validator, err := NewCallbackValidator(" test-user ", callbackLookup{ids: []string{"device-uuid"}})
	if err != nil {
		t.Fatal(err)
	}
	candidate, err := DecodeCallback(strings.NewReader(`{
		"userid":"test-user","cmd":"deviceinfo","deviceid":"device-1",
		"battery":88,"lockstatus":0,"time":"2026-07-31T09:10:11+08:00",
		"serialnum":0,"sign":"secret-sign"
	}`))
	if err != nil {
		t.Fatal(err)
	}
	mapped, err := validator.Validate(context.Background(), candidate)
	if err != nil {
		t.Fatal(err)
	}
	if mapped.DeviceID != "device-uuid" || mapped.NormalizedState["lock_state"] != "locked" || mapped.NormalizedState["battery_percent"] != int64(88) {
		t.Fatalf("mapped candidate = %+v", mapped)
	}
	wantReportedAt := time.Date(2026, 7, 31, 1, 10, 11, 0, time.UTC)
	if mapped.ReportedAt == nil || !mapped.ReportedAt.Equal(wantReportedAt) || mapped.EvidenceStatus != domain.EvidenceUnverified || mapped.SignatureVerified {
		t.Fatalf("candidate trust/time = %+v", mapped)
	}
}

func TestCallbackValidatorPreservesUnknownNormalization(t *testing.T) {
	validator, err := NewCallbackValidator("user", callbackLookup{ids: []string{"device-uuid"}})
	if err != nil {
		t.Fatal(err)
	}
	candidate, err := DecodeCallback(strings.NewReader(`{
		"userid":"user","cmd":"deviceinfo","deviceid":"device-1",
		"battery":101,"lockstatus":9,"time":"2026-07-31 09:10:11",
		"serialnum":0,"sign":"secret-sign"
	}`))
	if err != nil {
		t.Fatal(err)
	}
	mapped, err := validator.Validate(context.Background(), candidate)
	if err != nil {
		t.Fatal(err)
	}
	if mapped.NormalizedState["lock_state"] != "unknown" || mapped.ReportedAt != nil || mapped.NormalizedState["reported_at"] != nil {
		t.Fatalf("unknown normalization = %+v", mapped)
	}
	if _, exists := mapped.NormalizedState["battery_percent"]; exists {
		t.Fatalf("out-of-range battery was normalized: %+v", mapped.NormalizedState)
	}
}

func TestCallbackValidatorIdentityFailures(t *testing.T) {
	candidate := CallbackCandidate{UserID: "user", ProviderDeviceID: "device-1"}
	tests := []struct {
		name   string
		userID string
		lookup callbackLookup
		code   string
	}{
		{name: "user mismatch", userID: "other", lookup: callbackLookup{ids: []string{"one"}}, code: CallbackUserMismatch},
		{name: "not found", userID: "user", lookup: callbackLookup{}, code: CallbackDeviceNotFound},
		{name: "ambiguous", userID: "user", lookup: callbackLookup{ids: []string{"one", "two"}}, code: CallbackDeviceAmbiguous},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			validator, err := NewCallbackValidator(test.userID, test.lookup)
			if err != nil {
				t.Fatal(err)
			}
			_, err = validator.Validate(context.Background(), candidate)
			assertCallbackError(t, err, test.code)
		})
	}

	dependencyErr := errors.New("database unavailable")
	validator, err := NewCallbackValidator("user", callbackLookup{err: dependencyErr})
	if err != nil {
		t.Fatal(err)
	}
	_, err = validator.Validate(context.Background(), candidate)
	if !errors.Is(err, dependencyErr) {
		t.Fatalf("dependency error = %v", err)
	}
}

func assertCallbackError(t *testing.T, err error, wantCode string) {
	t.Helper()
	var callbackErr CallbackError
	if !errors.As(err, &callbackErr) || callbackErr.Code != wantCode {
		t.Fatalf("callback error = %v, want code %s", err, wantCode)
	}
}
