package devicetype

import (
	"encoding/hex"
	"strings"
	"testing"
	"time"

	"github.com/qiyue2015/device-platform/internal/domain"
)

func TestSmartLockProfileMatchesFrozenRevision(t *testing.T) {
	profile := SmartLockProfile()
	if profile.DeviceTypeID != "" {
		t.Fatalf("published profile must not fix a database UUID: %q", profile.DeviceTypeID)
	}
	if profile.Revision != 1 {
		t.Fatalf("revision = %d", profile.Revision)
	}
	if got := hex.EncodeToString(profile.ProfileHash); got != "81f6d5efb5f627a56fc19a2e2fb7fadcccc9b6a6b53fa411d7265a15eda5b596" {
		t.Fatalf("profile hash = %s", got)
	}
	if len(profile.Actions) != 3 {
		t.Fatalf("actions = %d", len(profile.Actions))
	}
	want := []domain.ActionIdentifier{"unlock", "lock", "query_status"}
	for index, action := range profile.Actions {
		if action.Identifier != want[index] {
			t.Fatalf("action %d = %s", index, action.Identifier)
		}
		if action.DeliveryPolicy != domain.DeliveryPolicyDispatchOnce || action.DispatchDeadline != 30*time.Second ||
			action.ProviderRequestTimeout != 10*time.Second || action.ResultObservationTimeout != 60*time.Second ||
			action.RetryAllowed || action.DeliveryPolicyOverrideAllowed {
			t.Fatalf("action %s has drifted safety metadata", action.Identifier)
		}
		if action.PayloadSchema["type"] != "object" || action.PayloadSchema["additionalProperties"] != false {
			t.Fatalf("action %s has drifted payload schema", action.Identifier)
		}
	}
}

func TestValidateSmartLockSnapshot(t *testing.T) {
	profile := SmartLockProfile()
	if err := ValidateSmartLockSnapshot(profile.Revision, []byte(SmartLockProfileJSON), profile.ProfileHash); err != nil {
		t.Fatalf("validate published snapshot: %v", err)
	}
	semanticEquivalent := []byte(`{
		"actions": [
			{"delivery_policy_override_allowed":false,"retry_allowed":false,"result_observation_timeout_ms":60000,"provider_request_timeout_ms":10000,"dispatch_deadline_ms":30000,"delivery_policy":"dispatch_once","payload_schema":{"additionalProperties":false,"maxProperties":0,"type":"object"},"risk_level":"high","identifier":"unlock"},
			{"identifier":"lock","risk_level":"high","payload_schema":{"type":"object","additionalProperties":false,"maxProperties":0},"delivery_policy":"dispatch_once","dispatch_deadline_ms":30000,"provider_request_timeout_ms":10000,"result_observation_timeout_ms":60000,"retry_allowed":false,"delivery_policy_override_allowed":false},
			{"identifier":"query_status","risk_level":"low","payload_schema":{"type":"object","maxProperties":0,"additionalProperties":false},"delivery_policy":"dispatch_once","dispatch_deadline_ms":30000,"provider_request_timeout_ms":10000,"result_observation_timeout_ms":60000,"retry_allowed":false,"delivery_policy_override_allowed":false}
		]
	}`)
	if err := ValidateSmartLockSnapshot(profile.Revision, semanticEquivalent, profile.ProfileHash); err != nil {
		t.Fatalf("semantic JSON equality: %v", err)
	}
	tests := []struct {
		name     string
		revision int
		json     []byte
		hash     []byte
		want     string
	}{
		{name: "revision", revision: 2, json: []byte(SmartLockProfileJSON), hash: profile.ProfileHash, want: "revision drift"},
		{name: "hash", revision: 1, json: []byte(SmartLockProfileJSON), hash: make([]byte, 32), want: "hash drift"},
		{name: "JSON", revision: 1, json: []byte(`{"actions":[]}`), hash: profile.ProfileHash, want: "JSON drift"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			err := ValidateSmartLockSnapshot(test.revision, test.json, test.hash)
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("expected %q, got %v", test.want, err)
			}
		})
	}
}
