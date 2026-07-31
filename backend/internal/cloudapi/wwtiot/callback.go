package wwtiot

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"strconv"
	"strings"
	"time"

	"github.com/qiyue2015/device-platform/internal/domain"
	"github.com/qiyue2015/device-platform/internal/httpjson"
)

const maxCallbackBytes = 64 << 10

const (
	CallbackPayloadTooLarge = "callback_payload_too_large"
	CallbackInvalidJSON     = "callback_invalid_json"
	CallbackMissingField    = "callback_missing_field"
	CallbackInvalidField    = "callback_invalid_field"
	CallbackUserMismatch    = "callback_user_mismatch"
	CallbackDeviceNotFound  = "callback_device_not_found"
	CallbackDeviceAmbiguous = "callback_device_ambiguous"
)

type CallbackError struct {
	Code   string
	Detail string
}

func (e CallbackError) Error() string {
	if e.Detail == "" {
		return e.Code
	}
	return e.Code + ": " + e.Detail
}

type CallbackCandidate struct {
	UserID           string
	Command          string
	ProviderDeviceID string
	Battery          int64
	LockStatus       int64
	ReportedTime     string
	SerialNumber     int64
	Sign             string
	raw              map[string]json.RawMessage
}

type CallbackDeviceLookup interface {
	LookupWWTIOTDeviceIDs(ctx context.Context, providerDeviceID string) ([]string, error)
}

type CallbackValidator struct {
	userID  string
	devices CallbackDeviceLookup
}

type MappedCallbackCandidate struct {
	DeviceID          string
	ProviderDeviceID  string
	Command           string
	SerialNumber      int64
	NormalizedState   map[string]any
	ReportedAt        *time.Time
	EvidenceStatus    domain.EvidenceStatus
	SignatureVerified bool
}

func NewCallbackValidator(userID string, devices CallbackDeviceLookup) (*CallbackValidator, error) {
	normalizedUserID := strings.TrimSpace(userID)
	if normalizedUserID == "" || len(normalizedUserID) > 128 || devices == nil {
		return nil, CallbackError{Code: CallbackInvalidField, Detail: "validator configuration is invalid"}
	}
	return &CallbackValidator{userID: normalizedUserID, devices: devices}, nil
}

func DecodeCallback(reader io.Reader) (CallbackCandidate, error) {
	data, err := io.ReadAll(io.LimitReader(reader, maxCallbackBytes+1))
	if err != nil {
		return CallbackCandidate{}, CallbackError{Code: CallbackInvalidJSON, Detail: "read callback body"}
	}
	if len(data) > maxCallbackBytes {
		return CallbackCandidate{}, CallbackError{Code: CallbackPayloadTooLarge, Detail: "callback body exceeds 64 KiB"}
	}
	var raw map[string]json.RawMessage
	if err := httpjson.DecodeStrict(bytes.NewReader(data), &raw); err != nil {
		return CallbackCandidate{}, CallbackError{Code: CallbackInvalidJSON, Detail: "callback body must be one strict JSON object"}
	}

	required := []string{"userid", "cmd", "deviceid", "battery", "lockstatus", "time", "serialnum", "sign"}
	for _, field := range required {
		if _, exists := raw[field]; !exists {
			return CallbackCandidate{}, CallbackError{Code: CallbackMissingField, Detail: field}
		}
	}
	userID, err := callbackString(raw, "userid")
	if err != nil {
		return CallbackCandidate{}, err
	}
	command, err := callbackString(raw, "cmd")
	if err != nil {
		return CallbackCandidate{}, err
	}
	deviceID, err := callbackString(raw, "deviceid")
	if err != nil {
		return CallbackCandidate{}, err
	}
	reportedTime, err := callbackString(raw, "time")
	if err != nil {
		return CallbackCandidate{}, err
	}
	sign, err := callbackString(raw, "sign")
	if err != nil {
		return CallbackCandidate{}, err
	}
	battery, err := callbackInteger(raw, "battery")
	if err != nil {
		return CallbackCandidate{}, err
	}
	lockStatus, err := callbackInteger(raw, "lockstatus")
	if err != nil {
		return CallbackCandidate{}, err
	}
	serialNumber, err := callbackInteger(raw, "serialnum")
	if err != nil {
		return CallbackCandidate{}, err
	}
	return CallbackCandidate{
		UserID: userID, Command: command, ProviderDeviceID: deviceID,
		Battery: battery, LockStatus: lockStatus, ReportedTime: reportedTime,
		SerialNumber: serialNumber, Sign: sign, raw: raw,
	}, nil
}

func (v *CallbackValidator) Validate(ctx context.Context, candidate CallbackCandidate) (MappedCallbackCandidate, error) {
	if v == nil || v.devices == nil {
		return MappedCallbackCandidate{}, CallbackError{Code: CallbackInvalidField, Detail: "validator is not configured"}
	}
	if candidate.UserID != v.userID {
		return MappedCallbackCandidate{}, CallbackError{Code: CallbackUserMismatch, Detail: "callback userid does not match Provider configuration"}
	}
	deviceIDs, err := v.devices.LookupWWTIOTDeviceIDs(ctx, candidate.ProviderDeviceID)
	if err != nil {
		return MappedCallbackCandidate{}, fmt.Errorf("lookup WWTIOT callback device: %w", err)
	}
	if len(deviceIDs) == 0 {
		return MappedCallbackCandidate{}, CallbackError{Code: CallbackDeviceNotFound, Detail: "Provider device identity was not found"}
	}
	if len(deviceIDs) != 1 || strings.TrimSpace(deviceIDs[0]) == "" {
		return MappedCallbackCandidate{}, CallbackError{Code: CallbackDeviceAmbiguous, Detail: "Provider device identity is not unique"}
	}

	state := map[string]any{"lock_state": "unknown"}
	switch candidate.LockStatus {
	case 0:
		state["lock_state"] = "locked"
	case 1:
		state["lock_state"] = "unlocked"
	}
	if candidate.Battery >= 0 && candidate.Battery <= 100 {
		state["battery_percent"] = candidate.Battery
	}
	var reportedAt *time.Time
	state["reported_at"] = nil
	if parsed, parseErr := time.Parse(time.RFC3339, candidate.ReportedTime); parseErr == nil {
		normalized := parsed.UTC()
		reportedAt = &normalized
		state["reported_at"] = reportedAt
	}

	return MappedCallbackCandidate{
		DeviceID: deviceIDs[0], ProviderDeviceID: candidate.ProviderDeviceID,
		Command: candidate.Command, SerialNumber: candidate.SerialNumber,
		NormalizedState: state, ReportedAt: reportedAt,
		EvidenceStatus: domain.EvidenceUnverified, SignatureVerified: false,
	}, nil
}

func (candidate CallbackCandidate) DiagnosticFields() map[string]any {
	result := make(map[string]any, len(candidate.raw))
	for key, raw := range candidate.raw {
		if key == "userid" || key == "sign" {
			continue
		}
		decoder := json.NewDecoder(bytes.NewReader(raw))
		decoder.UseNumber()
		var value any
		if err := decoder.Decode(&value); err == nil {
			result[key] = value
		}
	}
	return result
}

func callbackString(raw map[string]json.RawMessage, field string) (string, error) {
	var value string
	if err := json.Unmarshal(raw[field], &value); err != nil || strings.TrimSpace(value) == "" {
		return "", CallbackError{Code: CallbackInvalidField, Detail: field}
	}
	return value, nil
}

func callbackInteger(raw map[string]json.RawMessage, field string) (int64, error) {
	value := strings.TrimSpace(string(raw[field]))
	if len(value) >= 2 && value[0] == '"' && value[len(value)-1] == '"' {
		var text string
		if err := json.Unmarshal(raw[field], &text); err != nil || text == "" || strings.TrimSpace(text) != text {
			return 0, CallbackError{Code: CallbackInvalidField, Detail: field}
		}
		value = text
	}
	if value == "" || strings.ContainsAny(value, ".eE+") {
		return 0, CallbackError{Code: CallbackInvalidField, Detail: field}
	}
	parsed, err := strconv.ParseInt(value, 10, 64)
	if err != nil {
		return 0, CallbackError{Code: CallbackInvalidField, Detail: field}
	}
	return parsed, nil
}
