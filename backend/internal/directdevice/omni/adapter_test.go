package omni

import (
	"bytes"
	"context"
	"errors"
	"io"
	"testing"
	"time"

	"github.com/qiyue2015/device-platform/internal/domain"
	"github.com/qiyue2015/device-platform/internal/provideradapter"
)

type fixedClock struct{ now time.Time }

func (c fixedClock) Now() time.Time { return c.now }

func TestNormalizeDeviceIdentity(t *testing.T) {
	valid := testIMEI
	if got, err := NormalizeDeviceIdentity(&valid, "ignored"); err != nil || got != testIMEI {
		t.Fatalf("valid identity = %q, %v", got, err)
	}
	if _, err := NormalizeDeviceIdentity(nil, "ignored"); err == nil {
		t.Fatal("accepted missing IMEI")
	}
	for _, invalid := range []string{"12345678901234", "1234567890123456", "12345678901234A"} {
		if _, err := NormalizeDeviceIdentity(&invalid, "ignored"); err == nil {
			t.Fatalf("accepted invalid IMEI %q", invalid)
		}
	}
}

func TestAdapterFailsUnknownPhysicalActionsBeforeWrite(t *testing.T) {
	tests := []struct {
		name    string
		profile string
		action  domain.ActionIdentifier
		failure provideradapter.PrepareFailure
	}{
		{name: "bike unlock", profile: domain.ProviderProfileOmniBikeV207, action: "unlock", failure: provideradapter.PrepareMappingUnknown},
		{name: "bike lock", profile: domain.ProviderProfileOmniBikeV207, action: "lock", failure: provideradapter.PrepareActionUnsupported},
		{name: "IoT unlock", profile: domain.ProviderProfileOmniIoTV135, action: "unlock", failure: provideradapter.PrepareMappingUnknown},
		{name: "IoT lock", profile: domain.ProviderProfileOmniIoTV135, action: "lock", failure: provideradapter.PrepareMappingUnknown},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			registry := NewRegistry()
			writer := &scriptedWriter{steps: []writeStep{{count: 99}}}
			if _, err := registry.Register(test.profile, testIMEI, testDeviceID, testProjectID, "one", writer); err != nil {
				t.Fatal(err)
			}
			adapter := NewAdapter(registry, AdapterConfig{Configured: true})
			_, err := adapter.Prepare(dispatchRequest(test.profile, test.action))
			assertPrepareFailure(t, err, test.failure)
			if writer.writeCount() != 0 {
				t.Fatal("pre-send failure wrote to the session")
			}
		})
	}
}

func TestAdapterRequiresUniqueMatchingSession(t *testing.T) {
	registry := NewRegistry()
	adapter := NewAdapter(registry, AdapterConfig{Configured: true})
	_, err := adapter.Prepare(dispatchRequest(domain.ProviderProfileOmniIoTV135, "query_status"))
	assertPrepareFailure(t, err, provideradapter.PrepareSessionUnavailable)

	if _, err := registry.Register(domain.ProviderProfileOmniBikeV207, testIMEI, testDeviceID, testProjectID, "wrong-profile", &bytes.Buffer{}); err != nil {
		t.Fatal(err)
	}
	_, err = adapter.Prepare(dispatchRequest(domain.ProviderProfileOmniIoTV135, "query_status"))
	assertPrepareFailure(t, err, provideradapter.PrepareSessionUnavailable)
}

func TestAdapterRejectsSessionOwnedByAnotherDevice(t *testing.T) {
	registry := NewRegistry()
	writer := &scriptedWriter{steps: []writeStep{{count: 99}}}
	if _, err := registry.Register(
		domain.ProviderProfileOmniIoTV135, testIMEI, testDeviceID, testProjectID, "old-device-session", writer,
	); err != nil {
		t.Fatal(err)
	}
	request := dispatchRequest(domain.ProviderProfileOmniIoTV135, "query_status")
	request.DeviceID = "20000000-0000-0000-0000-000000000099"
	request.ProjectID = "10000000-0000-0000-0000-000000000099"
	_, err := NewAdapter(registry, AdapterConfig{Configured: true}).Prepare(request)
	assertPrepareFailure(t, err, provideradapter.PrepareSessionUnavailable)
	if writer.writeCount() != 0 {
		t.Fatal("ownership mismatch wrote to the old Device session")
	}
}

func TestAdapterFullWriteStaysDeliveryUnknown(t *testing.T) {
	registry := NewRegistry()
	writer := &limitedWriter{limit: 4}
	if _, err := registry.Register(domain.ProviderProfileOmniBikeV207, testIMEI, testDeviceID, testProjectID, "bike", writer); err != nil {
		t.Fatal(err)
	}
	adapter := NewAdapter(registry, AdapterConfig{
		Configured: true,
		Clock:      fixedClock{now: time.Date(2026, time.August, 1, 11, 22, 33, 0, time.UTC)},
	})
	prepared, err := adapter.Prepare(dispatchRequest(domain.ProviderProfileOmniBikeV207, "query_status"))
	if err != nil {
		t.Fatal(err)
	}
	result := prepared.Dispatch(context.Background())
	if result.Outcome != domain.AttemptOutcomeIndeterminate || result.ReasonCode != "provider_delivery_unknown" ||
		result.ConfirmationLevel != domain.ConfirmationTransportSent || result.EvidenceStatus != domain.EvidenceVerified {
		t.Fatalf("dispatch result = %+v", result)
	}
	want := []byte("\xff\xff*CMDS,OM," + testIMEI + ",260801112233,S5#\n")
	if !bytes.Equal(writer.Bytes(), want) {
		t.Fatalf("wire frame = %q, want %q", writer.Bytes(), want)
	}
	if _, exists := result.ResponseSummary["provider_device_id"]; exists {
		t.Fatal("response summary exposed Provider identity")
	}
}

func TestAdapterClassifiesZeroAndPartialWritesConservatively(t *testing.T) {
	tests := []struct {
		name             string
		steps            []writeStep
		wantOutcome      domain.AttemptOutcome
		wantConfirmation domain.ConfirmationLevel
		wantEvidence     domain.EvidenceStatus
		wantReason       string
		wantBytes        int
	}{
		{
			name: "zero bytes", steps: []writeStep{{count: 0, err: io.ErrClosedPipe}},
			wantOutcome: domain.AttemptOutcomeTransportErrorBeforeSend, wantConfirmation: domain.ConfirmationNone,
			wantEvidence: domain.EvidenceNone, wantReason: "provider_transport_error",
		},
		{
			name: "partial bytes", steps: []writeStep{{count: 3, err: io.ErrUnexpectedEOF}},
			wantOutcome: domain.AttemptOutcomeIndeterminate, wantConfirmation: domain.ConfirmationTransportSent,
			wantEvidence: domain.EvidenceVerified, wantReason: "provider_delivery_unknown", wantBytes: 3,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			registry := NewRegistry()
			writer := &scriptedWriter{steps: test.steps}
			if _, err := registry.Register(domain.ProviderProfileOmniIoTV135, testIMEI, testDeviceID, testProjectID, "iot", writer); err != nil {
				t.Fatal(err)
			}
			prepared, err := NewAdapter(registry, AdapterConfig{Configured: true}).Prepare(
				dispatchRequest(domain.ProviderProfileOmniIoTV135, "query_status"),
			)
			if err != nil {
				t.Fatal(err)
			}
			result := prepared.Dispatch(context.Background())
			if result.Outcome != test.wantOutcome || result.ConfirmationLevel != test.wantConfirmation ||
				result.EvidenceStatus != test.wantEvidence || result.ReasonCode != test.wantReason ||
				result.ResponseSummary["bytes_written"] != test.wantBytes {
				t.Fatalf("dispatch result = %+v", result)
			}
		})
	}
}

func TestAdapterRefusesChangedSessionGenerationAfterPrepare(t *testing.T) {
	registry := NewRegistry()
	writer := &scriptedWriter{steps: []writeStep{{count: 99}}}
	first, err := registry.Register(domain.ProviderProfileOmniIoTV135, testIMEI, testDeviceID, testProjectID, "iot-one", writer)
	if err != nil {
		t.Fatal(err)
	}
	prepared, err := NewAdapter(registry, AdapterConfig{Configured: true}).Prepare(
		dispatchRequest(domain.ProviderProfileOmniIoTV135, "query_status"),
	)
	if err != nil {
		t.Fatal(err)
	}
	registry.Unregister(first)
	if _, err := registry.Register(domain.ProviderProfileOmniIoTV135, testIMEI, testDeviceID, testProjectID, "iot-two", &bytes.Buffer{}); err != nil {
		t.Fatal(err)
	}
	result := prepared.Dispatch(context.Background())
	if result.Outcome != domain.AttemptOutcomeTransportErrorBeforeSend || result.ConfirmationLevel != domain.ConfirmationNone || writer.writeCount() != 0 {
		t.Fatalf("stale generation result = %+v, writes = %d", result, writer.writeCount())
	}
}

func TestAdapterRejectsInvalidQueryPayloadAndCancelledContext(t *testing.T) {
	registry := NewRegistry()
	writer := &scriptedWriter{steps: []writeStep{{count: 99}}}
	if _, err := registry.Register(domain.ProviderProfileOmniIoTV135, testIMEI, testDeviceID, testProjectID, "iot", writer); err != nil {
		t.Fatal(err)
	}
	adapter := NewAdapter(registry, AdapterConfig{Configured: true})
	invalid := dispatchRequest(domain.ProviderProfileOmniIoTV135, "query_status")
	invalid.Payload = map[string]any{"guess": true}
	_, err := adapter.Prepare(invalid)
	assertPrepareFailure(t, err, provideradapter.PrepareRequestInvalid)

	prepared, err := adapter.Prepare(dispatchRequest(domain.ProviderProfileOmniIoTV135, "query_status"))
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	result := prepared.Dispatch(ctx)
	if result.Outcome != domain.AttemptOutcomeTransportErrorBeforeSend || writer.writeCount() != 0 {
		t.Fatalf("cancelled dispatch result = %+v", result)
	}
}

func dispatchRequest(profile string, action domain.ActionIdentifier) provideradapter.DispatchRequest {
	return provideradapter.DispatchRequest{
		ProjectID: testProjectID, DeviceID: testDeviceID,
		ProviderDeviceID: testIMEI, ProviderProfile: profile, Action: action,
		Payload: map[string]any{}, ProviderRequestKey: "123456789",
	}
}

func assertPrepareFailure(t *testing.T, err error, want provideradapter.PrepareFailure) {
	t.Helper()
	var prepareError *provideradapter.PrepareError
	if !errors.As(err, &prepareError) || prepareError.Failure != want {
		t.Fatalf("Prepare error = %v, want %s", err, want)
	}
}
