package wwtiot

import (
	"bytes"
	"crypto/md5"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

const (
	AccessType = "cloud_api"
	Adapter    = "wwtiot_cloud_api"
	Provider   = "wwtiot"

	StatusAcked  = "acked"
	StatusFailed = "failed"
)

type Config struct {
	APIURL  string
	UserID  string
	UserKey string
	DryRun  bool
}

type Client struct {
	cfg        Config
	httpClient *http.Client
}

type CommandRequest struct {
	CommandType      string                 `json:"command_type"`
	ProviderDeviceID string                 `json:"provider_device_id"`
	Body             map[string]interface{} `json:"body"`
	RawMessage       RawMessage             `json:"raw_message"`
}

type CommandAck struct {
	Status          string                 `json:"status"`
	Acked           bool                   `json:"acked"`
	DryRun          bool                   `json:"dry_run"`
	Message         string                 `json:"message,omitempty"`
	RawMessage      RawMessage             `json:"raw_message"`
	ParsedResponse  map[string]interface{} `json:"parsed_response,omitempty"`
	CallbackPending bool                   `json:"callback_pending"`
}

type RawMessage struct {
	Direction      string                 `json:"direction"`
	AccessType     string                 `json:"access_type"`
	Adapter        string                 `json:"adapter"`
	ProviderCode   string                 `json:"provider_code"`
	RawPayload     map[string]interface{} `json:"raw_payload"`
	DecodedPayload map[string]interface{} `json:"decoded_payload,omitempty"`
	ParseStatus    string                 `json:"parse_status"`
	ReceivedAt     string                 `json:"received_at"`
}

type Callback struct {
	ProviderDeviceID string                 `json:"provider_device_id"`
	CommandType      string                 `json:"command_type,omitempty"`
	VendorCommand    string                 `json:"vendor_command,omitempty"`
	LockStatus       string                 `json:"lock_status,omitempty"`
	BatteryPercent   string                 `json:"battery_percent,omitempty"`
	Location         map[string]string      `json:"location,omitempty"`
	RawState         map[string]interface{} `json:"raw_state"`
}

func NewClient(cfg Config) *Client {
	if strings.TrimSpace(cfg.APIURL) == "" {
		cfg.APIURL = "http://gps.wwtiot.com/api"
	}
	return &Client{
		cfg:        cfg,
		httpClient: &http.Client{Timeout: 10 * time.Second},
	}
}

func (c *Client) Configured() bool {
	return !c.cfg.DryRun && strings.TrimSpace(c.cfg.UserID) != "" && strings.TrimSpace(c.cfg.UserKey) != ""
}

func (c *Client) BuildCommand(commandType, providerDeviceID string) (CommandRequest, error) {
	userID := defaultString(c.cfg.UserID, "dry-run-user")
	userKey := defaultString(c.cfg.UserKey, "dry-run-key")
	serial := int(time.Now().Unix()) % 100000

	var body map[string]interface{}
	switch commandType {
	case "unlock", "lock":
		cmd := map[string]string{"unlock": "open", "lock": "close"}[commandType]
		body = map[string]interface{}{
			"userid":    userID,
			"cmd":       cmd,
			"deviceid":  providerDeviceID,
			"serialnum": serial,
			"sign":      MD5Sign([]interface{}{userID, cmd, providerDeviceID, serial}, userKey),
		}
	case "query_status":
		body = map[string]interface{}{
			"userid":    userID,
			"cmd":       "control",
			"type":      23,
			"value":     4,
			"deviceid":  providerDeviceID,
			"serialnum": serial,
			"sign":      MD5Sign([]interface{}{userID, "control", 23, 4, providerDeviceID, serial}, userKey),
		}
	default:
		return CommandRequest{}, fmt.Errorf("unsupported cloud_api command %q", commandType)
	}

	return CommandRequest{
		CommandType:      commandType,
		ProviderDeviceID: providerDeviceID,
		Body:             body,
		RawMessage:       NewRawMessage("downlink", body, nil, "success"),
	}, nil
}

func (c *Client) SendCommand(req CommandRequest) (CommandAck, error) {
	if !c.Configured() {
		return CommandAck{
			Status:          StatusAcked,
			Acked:           true,
			DryRun:          true,
			Message:         "WWTIOT_DRY_RUN enabled or credentials missing; vendor request was not sent",
			RawMessage:      req.RawMessage,
			CallbackPending: true,
		}, nil
	}

	data, err := json.Marshal(req.Body)
	if err != nil {
		return CommandAck{}, err
	}
	httpReq, err := http.NewRequest(http.MethodPost, c.cfg.APIURL, bytes.NewReader(data))
	if err != nil {
		return CommandAck{}, err
	}
	httpReq.Header.Set("Content-Type", "application/json")
	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return CommandAck{}, err
	}
	defer resp.Body.Close()

	limited, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	parsed := ParseResponse(resp.StatusCode, limited)
	if !parsed.Acked {
		return parsed, errors.New(parsed.Message)
	}
	return parsed, nil
}

func ParseResponse(statusCode int, body []byte) CommandAck {
	decoded := map[string]interface{}{}
	_ = json.Unmarshal(body, &decoded)
	result := strings.ToLower(strings.TrimSpace(fmt.Sprint(decoded["result"])))
	successText := strings.ToLower(strings.TrimSpace(fmt.Sprint(decoded["success"])))
	message := strings.TrimSpace(fmt.Sprint(decoded["info"]))
	if message == "" || message == "<nil>" {
		message = strings.TrimSpace(fmt.Sprint(decoded["message"]))
	}
	acked := statusCode >= 200 && statusCode < 300 && (result == "ok" || result == "success" || successText == "true")
	status := StatusAcked
	if !acked {
		status = StatusFailed
		if message == "" || message == "<nil>" {
			message = fmt.Sprintf("wwtiot rejected request with HTTP %d", statusCode)
		}
	}
	raw := map[string]interface{}{
		"http_status": statusCode,
		"body":        string(body),
	}
	return CommandAck{
		Status:          status,
		Acked:           acked,
		Message:         message,
		RawMessage:      NewRawMessage("uplink", raw, decoded, parseStatus(acked)),
		ParsedResponse:  decoded,
		CallbackPending: acked,
	}
}

func NormalizeCallback(payload map[string]interface{}) Callback {
	cmd := strings.ToLower(firstString(payload, "cmd", "command", "type"))
	lockStatus := normalizeLockStatus(firstString(payload, "lockstatus", "lock_status", "status"))
	callback := Callback{
		ProviderDeviceID: firstString(payload, "deviceid", "device_id", "imei", "sn"),
		CommandType:      commandTypeFromVendor(cmd),
		VendorCommand:    cmd,
		LockStatus:       lockStatus,
		RawState:         RedactMap(payload),
	}
	if battery := firstString(payload, "battery", "battery_percent", "power"); battery != "" {
		callback.BatteryPercent = battery
	}
	lat := firstString(payload, "lat", "latitude")
	lng := firstString(payload, "lng", "lon", "longitude")
	if lat != "" || lng != "" {
		callback.Location = map[string]string{"lat": lat, "lng": lng}
	}
	return callback
}

func PhysicalActionMatchesState(commandType, lockStatus string) bool {
	switch commandType {
	case "unlock":
		return lockStatus == "unlocked"
	case "lock":
		return lockStatus == "locked"
	default:
		return true
	}
}

func MD5Sign(fields []interface{}, userKey string) string {
	var b strings.Builder
	for _, field := range fields {
		b.WriteString(fmt.Sprint(field))
	}
	b.WriteString(userKey)
	sum := md5.Sum([]byte(b.String()))
	return hex.EncodeToString(sum[:])
}

func NewRawMessage(direction string, raw, decoded map[string]interface{}, parseStatus string) RawMessage {
	if raw == nil {
		raw = map[string]interface{}{}
	}
	if decoded == nil {
		decoded = map[string]interface{}{}
	}
	return RawMessage{
		Direction:      direction,
		AccessType:     AccessType,
		Adapter:        Adapter,
		ProviderCode:   Provider,
		RawPayload:     RedactMap(raw),
		DecodedPayload: RedactMap(decoded),
		ParseStatus:    parseStatus,
		ReceivedAt:     time.Now().UTC().Format(time.RFC3339),
	}
}

func RedactMap(input map[string]interface{}) map[string]interface{} {
	out := make(map[string]interface{}, len(input))
	for key, value := range input {
		if isSensitiveKey(key) {
			out[key] = "[REDACTED]"
			continue
		}
		switch typed := value.(type) {
		case map[string]interface{}:
			out[key] = RedactMap(typed)
		case []interface{}:
			items := make([]interface{}, len(typed))
			for i, item := range typed {
				if nested, ok := item.(map[string]interface{}); ok {
					items[i] = RedactMap(nested)
				} else {
					items[i] = item
				}
			}
			out[key] = items
		default:
			out[key] = value
		}
	}
	return out
}

func isSensitiveKey(key string) bool {
	normalized := strings.ToLower(strings.ReplaceAll(key, "_", ""))
	switch normalized {
	case "userkey", "authorization", "apikey", "token", "password", "webhooksecret", "secret":
		return true
	default:
		return false
	}
}

func parseStatus(ok bool) string {
	if ok {
		return "success"
	}
	return "failed"
}

func defaultString(value, fallback string) string {
	if strings.TrimSpace(value) == "" {
		return fallback
	}
	return value
}

func firstString(payload map[string]interface{}, keys ...string) string {
	for _, key := range keys {
		if value, ok := payload[key]; ok {
			text := strings.TrimSpace(fmt.Sprint(value))
			if text != "" && text != "<nil>" {
				return text
			}
		}
	}
	return ""
}

func commandTypeFromVendor(cmd string) string {
	switch strings.ToLower(strings.TrimSpace(cmd)) {
	case "open", "unlock":
		return "unlock"
	case "close", "lock":
		return "lock"
	default:
		return ""
	}
}

func normalizeLockStatus(raw string) string {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "0", "locked", "close", "closed":
		return "locked"
	case "1", "unlocked", "open", "opened":
		return "unlocked"
	default:
		return "unknown"
	}
}
