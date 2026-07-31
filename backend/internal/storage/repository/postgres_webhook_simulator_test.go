package repository

import (
	"testing"
	"time"

	"github.com/qiyue2015/device-platform/internal/domain"
)

func TestWebhookCompletionDerivesStatus(t *testing.T) {
	ok := 204
	rejected := 503
	errorCode := "http_status"
	errorDetail := "upstream unavailable"
	tests := []struct {
		name        string
		attempts    int
		maximum     int
		httpStatus  *int
		errorCode   *string
		errorDetail *string
		retryDelay  time.Duration
		wantStatus  domain.WebhookDeliveryStatus
		wantRetry   time.Duration
		wantValid   bool
	}{
		{name: "2xx delivered", attempts: 1, maximum: 5, httpStatus: &ok, wantStatus: domain.WebhookDeliveryStatusDelivered, wantValid: true},
		{name: "2xx cannot carry error", attempts: 1, maximum: 5, httpStatus: &ok, errorCode: &errorCode},
		{name: "non-2xx schedules retry", attempts: 1, maximum: 5, httpStatus: &rejected, errorCode: &errorCode, errorDetail: &errorDetail, retryDelay: time.Second, wantStatus: domain.WebhookDeliveryStatusFailed, wantRetry: time.Second, wantValid: true},
		{name: "configured maximum becomes dead", attempts: 3, maximum: 3, httpStatus: &rejected, errorCode: &errorCode, errorDetail: &errorDetail, wantStatus: domain.WebhookDeliveryStatusDead, wantValid: true},
		{name: "terminal failure cannot schedule retry", attempts: 3, maximum: 3, httpStatus: &rejected, errorCode: &errorCode, retryDelay: time.Second},
		{name: "failure requires error code", attempts: 1, maximum: 5, httpStatus: &rejected, retryDelay: time.Second},
		{name: "sub-microsecond retry rejected", attempts: 1, maximum: 5, httpStatus: &rejected, errorCode: &errorCode, retryDelay: time.Nanosecond},
		{name: "invalid HTTP status rejected", attempts: 1, maximum: 5, httpStatus: intRef(600), errorCode: &errorCode, retryDelay: time.Second},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			status, retry, valid := webhookCompletion(test.attempts, test.maximum, test.httpStatus, test.errorCode, test.errorDetail, test.retryDelay)
			if status != test.wantStatus || retry != test.wantRetry || valid != test.wantValid {
				t.Fatalf("webhookCompletion() = %q, %s, %v; want %q, %s, %v", status, retry, valid, test.wantStatus, test.wantRetry, test.wantValid)
			}
		})
	}
}

func TestValidSimulatorUpdate(t *testing.T) {
	for _, outcome := range []domain.SimulatorOutcome{
		domain.SimulatorOutcomeProviderAccepted,
		domain.SimulatorOutcomeProviderRejected,
		domain.SimulatorOutcomeTransportErrorBeforeSend,
		domain.SimulatorOutcomeTransportErrorAfterSend,
		domain.SimulatorOutcomeInvalidResponse,
	} {
		if !validSimulatorUpdate(UpdateSimulatorRequest{Outcome: outcome, Delay: 60 * time.Second}) {
			t.Fatalf("valid Simulator outcome rejected: %s", outcome)
		}
	}
	for _, request := range []UpdateSimulatorRequest{
		{Outcome: domain.SimulatorOutcome("future"), Delay: time.Second},
		{Outcome: domain.SimulatorOutcomeProviderAccepted, Delay: -time.Millisecond},
		{Outcome: domain.SimulatorOutcomeProviderAccepted, Delay: 60*time.Second + time.Millisecond},
		{Outcome: domain.SimulatorOutcomeProviderAccepted, Delay: time.Nanosecond},
	} {
		if validSimulatorUpdate(request) {
			t.Fatalf("invalid Simulator update accepted: %+v", request)
		}
	}
}

func TestValidWebhookRetrySchedule(t *testing.T) {
	if !validWebhookRetrySchedule(3, []time.Duration{time.Second, 5 * time.Second}) {
		t.Fatal("valid Webhook retry schedule rejected")
	}
	for _, test := range []struct {
		maximum int
		delays  []time.Duration
	}{
		{maximum: 0},
		{maximum: 6, delays: []time.Duration{time.Second, time.Second, time.Second, time.Second, time.Second}},
		{maximum: 3, delays: []time.Duration{time.Second}},
		{maximum: 2, delays: []time.Duration{time.Nanosecond}},
	} {
		if validWebhookRetrySchedule(test.maximum, test.delays) {
			t.Fatalf("invalid Webhook retry schedule accepted: maximum=%d delays=%v", test.maximum, test.delays)
		}
	}
}

func intRef(value int) *int {
	return &value
}
