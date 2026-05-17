package wwtiot

import (
	"bytes"
	"context"
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
	DefaultAPIURL = "http://gps.wwtiot.com/api/"
)

var (
	ErrNotConfigured      = errors.New("wwtiot client is not configured")
	ErrUnsupportedCommand = errors.New("unsupported wwtiot command")
)

type Config struct {
	APIURL  string
	UserID  string
	UserKey string
	Timeout time.Duration
}

func (cfg Config) Configured() bool {
	return strings.TrimSpace(cfg.UserID) != "" && strings.TrimSpace(cfg.UserKey) != ""
}

type Client struct {
	apiURL     string
	userID     string
	userKey    string
	httpClient *http.Client
	now        func() time.Time
}

type Result struct {
	HTTPRequest  map[string]any `json:"http_request"`
	HTTPStatus   int            `json:"http_status"`
	ResponseBody map[string]any `json:"response_body"`
}

func NewClient(cfg Config, httpClient *http.Client) *Client {
	if httpClient == nil {
		timeout := cfg.Timeout
		if timeout <= 0 {
			timeout = 10 * time.Second
		}
		httpClient = &http.Client{Timeout: timeout}
	}
	apiURL := strings.TrimSpace(cfg.APIURL)
	if apiURL == "" {
		apiURL = DefaultAPIURL
	}
	return &Client{
		apiURL:     apiURL,
		userID:     strings.TrimSpace(cfg.UserID),
		userKey:    strings.TrimSpace(cfg.UserKey),
		httpClient: httpClient,
		now:        time.Now,
	}
}

func (c *Client) Configured() bool {
	return c != nil && c.userID != "" && c.userKey != ""
}

func (c *Client) SendCommand(ctx context.Context, providerDeviceID, commandType string, payload map[string]any) (Result, error) {
	if !c.Configured() {
		return Result{}, ErrNotConfigured
	}
	body, err := c.buildRequest(providerDeviceID, commandType, payload)
	if err != nil {
		return Result{}, err
	}
	result := Result{
		HTTPRequest: map[string]any{
			"method": "POST",
			"url":    c.apiURL,
			"body":   redactSensitiveFields(body),
		},
	}
	payloadBytes, err := json.Marshal(body)
	if err != nil {
		return result, fmt.Errorf("encode wwtiot request: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.apiURL, bytes.NewReader(payloadBytes))
	if err != nil {
		return result, fmt.Errorf("create wwtiot request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return result, fmt.Errorf("send wwtiot request: %w", err)
	}
	defer resp.Body.Close()
	result.HTTPStatus = resp.StatusCode

	respBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return result, fmt.Errorf("read wwtiot response: %w", err)
	}
	if len(respBytes) > 0 {
		if err := json.Unmarshal(respBytes, &result.ResponseBody); err != nil {
			result.ResponseBody = map[string]any{"raw": string(respBytes)}
		}
		result.ResponseBody = redactSensitiveFields(result.ResponseBody)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return result, fmt.Errorf("wwtiot http status %d", resp.StatusCode)
	}
	if value, ok := result.ResponseBody["result"].(string); ok && !strings.EqualFold(value, "ok") {
		return result, fmt.Errorf("wwtiot result %q", value)
	}
	return result, nil
}

func (c *Client) buildRequest(providerDeviceID, commandType string, payload map[string]any) (map[string]any, error) {
	deviceID := strings.TrimSpace(providerDeviceID)
	if deviceID == "" {
		return nil, fmt.Errorf("%w: provider_device_id is required", ErrUnsupportedCommand)
	}
	serial := c.now().UnixNano() / int64(time.Millisecond)
	if serial < 0 {
		serial = -serial
	}
	serial %= 1000000000
	if serial == 0 {
		serial = time.Now().Unix() % 100000
	}

	switch strings.TrimSpace(commandType) {
	case "unlock":
		return c.openCloseBody("open", deviceID, serial), nil
	case "lock":
		return c.openCloseBody("close", deviceID, serial), nil
	case "query_status":
		commandTypeValue := numericPayloadValue(payload, "type", 23)
		value := numericPayloadValue(payload, "value", 4)
		body := map[string]any{
			"userid":    c.userID,
			"cmd":       "control",
			"type":      commandTypeValue,
			"value":     value,
			"deviceid":  deviceID,
			"serialnum": serial,
		}
		body["sign"] = md5Sign(c.userID, "control", commandTypeValue, value, deviceID, serial, c.userKey)
		return body, nil
	default:
		return nil, fmt.Errorf("%w: %s", ErrUnsupportedCommand, commandType)
	}
}

func (c *Client) openCloseBody(cmd, deviceID string, serial int64) map[string]any {
	body := map[string]any{
		"userid":    c.userID,
		"cmd":       cmd,
		"deviceid":  deviceID,
		"serialnum": serial,
	}
	body["sign"] = md5Sign(c.userID, cmd, deviceID, serial, c.userKey)
	return body
}

func numericPayloadValue(payload map[string]any, key string, fallback int64) int64 {
	value, ok := payload[key]
	if !ok {
		return fallback
	}
	switch typed := value.(type) {
	case int:
		return int64(typed)
	case int64:
		return typed
	case float64:
		return int64(typed)
	case json.Number:
		parsed, err := typed.Int64()
		if err == nil {
			return parsed
		}
	}
	return fallback
}

func md5Sign(parts ...any) string {
	var builder strings.Builder
	for _, part := range parts {
		builder.WriteString(fmt.Sprint(part))
	}
	sum := md5.Sum([]byte(builder.String()))
	return hex.EncodeToString(sum[:])
}

func redactSensitiveFields(body map[string]any) map[string]any {
	if body == nil {
		return nil
	}
	redacted := make(map[string]any, len(body))
	for key, value := range body {
		if isSensitiveField(key) {
			redacted[key] = "[redacted]"
			continue
		}
		redacted[key] = value
	}
	return redacted
}

func isSensitiveField(key string) bool {
	switch strings.ToLower(strings.TrimSpace(key)) {
	case "sign", "userid", "user_id", "userkey", "user_key":
		return true
	default:
		return false
	}
}
