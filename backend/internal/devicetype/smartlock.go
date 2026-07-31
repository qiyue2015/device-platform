package devicetype

import (
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"time"

	"github.com/qiyue2015/device-platform/internal/domain"
)

const SmartLockProfileJSON = `{"actions":[{"identifier":"unlock","risk_level":"high","payload_schema":{"type":"object","maxProperties":0,"additionalProperties":false},"delivery_policy":"online_only","dispatch_deadline_ms":30000,"provider_request_timeout_ms":10000,"result_observation_timeout_ms":60000,"retry_allowed":false,"delivery_policy_override_allowed":false},{"identifier":"lock","risk_level":"high","payload_schema":{"type":"object","maxProperties":0,"additionalProperties":false},"delivery_policy":"online_only","dispatch_deadline_ms":30000,"provider_request_timeout_ms":10000,"result_observation_timeout_ms":60000,"retry_allowed":false,"delivery_policy_override_allowed":false},{"identifier":"query_status","risk_level":"low","payload_schema":{"type":"object","maxProperties":0,"additionalProperties":false},"delivery_policy":"dispatch_once","dispatch_deadline_ms":30000,"provider_request_timeout_ms":10000,"result_observation_timeout_ms":60000,"retry_allowed":false,"delivery_policy_override_allowed":false}]}`

type encodedProfile struct {
	Actions []encodedAction `json:"actions"`
}

type encodedAction struct {
	Identifier                    domain.ActionIdentifier `json:"identifier"`
	RiskLevel                     string                  `json:"risk_level"`
	PayloadSchema                 map[string]any          `json:"payload_schema"`
	DeliveryPolicy                domain.DeliveryPolicy   `json:"delivery_policy"`
	DispatchDeadlineMS            int64                   `json:"dispatch_deadline_ms"`
	ProviderRequestTimeoutMS      int64                   `json:"provider_request_timeout_ms"`
	ResultObservationTimeoutMS    int64                   `json:"result_observation_timeout_ms"`
	RetryAllowed                  bool                    `json:"retry_allowed"`
	DeliveryPolicyOverrideAllowed bool                    `json:"delivery_policy_override_allowed"`
}

func SmartLockProfile() domain.DeviceTypeProfile {
	var encoded encodedProfile
	if err := json.Unmarshal([]byte(SmartLockProfileJSON), &encoded); err != nil {
		panic("invalid embedded smart-lock profile: " + err.Error())
	}
	actions := make([]domain.CapabilityAction, 0, len(encoded.Actions))
	for _, action := range encoded.Actions {
		actions = append(actions, domain.CapabilityAction{
			Identifier:                    action.Identifier,
			PayloadSchema:                 action.PayloadSchema,
			RiskLevel:                     action.RiskLevel,
			DeliveryPolicy:                action.DeliveryPolicy,
			DispatchDeadline:              time.Duration(action.DispatchDeadlineMS) * time.Millisecond,
			ProviderRequestTimeout:        time.Duration(action.ProviderRequestTimeoutMS) * time.Millisecond,
			ResultObservationTimeout:      time.Duration(action.ResultObservationTimeoutMS) * time.Millisecond,
			RetryAllowed:                  action.RetryAllowed,
			DeliveryPolicyOverrideAllowed: action.DeliveryPolicyOverrideAllowed,
		})
	}
	hash := sha256.Sum256([]byte(SmartLockProfileJSON))
	return domain.DeviceTypeProfile{
		Revision:    domain.DeviceTypeSmartLockRevision,
		Actions:     actions,
		ProfileHash: hash[:],
	}
}

// ValidateSmartLockSnapshot fails closed when the database snapshot differs
// from the profile published with this binary. The Device Type ID is resolved
// by code by the caller and is intentionally not part of the release profile.
func ValidateSmartLockSnapshot(currentRevision int, profileJSON, profileHash []byte) error {
	if currentRevision != domain.DeviceTypeSmartLockRevision {
		return fmt.Errorf("smart-lock revision drift: database=%d binary=%d", currentRevision, domain.DeviceTypeSmartLockRevision)
	}
	want := SmartLockProfile()
	if !bytes.Equal(profileHash, want.ProfileHash) {
		return fmt.Errorf("smart-lock profile hash drift")
	}
	var databaseProfile any
	if err := json.Unmarshal(profileJSON, &databaseProfile); err != nil {
		return fmt.Errorf("decode smart-lock database profile: %w", err)
	}
	var publishedProfile any
	if err := json.Unmarshal([]byte(SmartLockProfileJSON), &publishedProfile); err != nil {
		return fmt.Errorf("decode embedded smart-lock profile: %w", err)
	}
	if !deepJSONEqual(databaseProfile, publishedProfile) {
		return fmt.Errorf("smart-lock profile JSON drift")
	}
	return nil
}

func deepJSONEqual(left, right any) bool {
	leftJSON, leftErr := json.Marshal(left)
	rightJSON, rightErr := json.Marshal(right)
	return leftErr == nil && rightErr == nil && bytes.Equal(leftJSON, rightJSON)
}
