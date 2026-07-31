package wwtiot

import (
	"bytes"
	"context"
	"crypto/md5"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptrace"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"sync/atomic"
	"time"
	"unicode/utf8"

	"github.com/qiyue2015/device-platform/internal/domain"
	"github.com/qiyue2015/device-platform/internal/httpjson"
	"github.com/qiyue2015/device-platform/internal/provideradapter"
)

const (
	DefaultAPIURL       = "http://gps.wwtiot.com/api/"
	requestTimeout      = 10 * time.Second
	maxResponseBytes    = 64 << 10
	maxSummaryTextBytes = 4 << 10
)

var (
	providerDeviceIDPattern = regexp.MustCompile(`^[A-Za-z0-9._:-]{1,128}$`)
	requestKeyPattern       = regexp.MustCompile(`^[1-9][0-9]{0,8}$`)
)

type Config struct {
	APIURL  string
	UserID  string
	UserKey string
}

func (cfg Config) Configured() bool {
	_, _, _, ok := normalizeConfig(cfg)
	return ok
}

type DispatchRequest = provideradapter.DispatchRequest

type DispatchResult = provideradapter.DispatchResult

type Client struct {
	apiURL     string
	userID     string
	userKey    string
	configured bool
	httpClient *http.Client
}

var _ provideradapter.Adapter = (*Client)(nil)

type preparedDispatch struct {
	client  *Client
	body    map[string]any
	payload []byte
	summary map[string]any
}

func NewClient(cfg Config, httpClient *http.Client) *Client {
	apiURL, userID, userKey, configured := normalizeConfig(cfg)
	if httpClient == nil {
		httpClient = &http.Client{}
	}
	clientCopy := *httpClient
	clientCopy.Timeout = requestTimeout
	clientCopy.CheckRedirect = func(*http.Request, []*http.Request) error {
		return http.ErrUseLastResponse
	}
	return &Client{
		apiURL: apiURL, userID: userID, userKey: userKey,
		configured: configured, httpClient: &clientCopy,
	}
}

func (c *Client) Configured() bool {
	return c != nil && c.configured
}

func (c *Client) Prepare(request DispatchRequest) (provideradapter.PreparedDispatch, error) {
	if !c.Configured() {
		return nil, fmt.Errorf("WWTIOT Provider is not configured")
	}
	body, summary, err := c.buildRequest(request)
	if err != nil {
		return nil, err
	}
	payload, err := json.Marshal(body)
	if err != nil {
		return nil, fmt.Errorf("encode WWTIOT request")
	}
	return &preparedDispatch{client: c, body: body, payload: payload, summary: summary}, nil
}

func (c *Client) Dispatch(ctx context.Context, request DispatchRequest) DispatchResult {
	prepared, err := c.Prepare(request)
	if err != nil {
		return invalidRequestResult(err.Error())
	}
	return prepared.Dispatch(ctx)
}

func (p *preparedDispatch) RequestSummary() map[string]any {
	return allowlistSummary(p.summary, []string{"cmd", "deviceid", "serialnum", "type", "value"})
}

func (p *preparedDispatch) Dispatch(ctx context.Context) DispatchResult {
	result := DispatchResult{RequestSummary: p.RequestSummary()}
	httpRequest, err := http.NewRequestWithContext(ctx, http.MethodPost, p.client.apiURL, bytes.NewReader(p.payload))
	if err != nil {
		return invalidRequestResult("create WWTIOT request")
	}
	httpRequest.Header.Set("Content-Type", "application/json")

	var wroteRequest atomic.Bool
	trace := &httptrace.ClientTrace{WroteRequest: func(httptrace.WroteRequestInfo) { wroteRequest.Store(true) }}
	httpRequest = httpRequest.WithContext(httptrace.WithClientTrace(httpRequest.Context(), trace))
	response, err := p.client.httpClient.Do(httpRequest)
	if err != nil {
		if wroteRequest.Load() {
			return transportAfterSend(result)
		}
		return transportBeforeSend(result)
	}
	defer response.Body.Close()
	result.HTTPStatus = response.StatusCode

	responseBytes, err := io.ReadAll(io.LimitReader(response.Body, maxResponseBytes+1))
	if err != nil {
		return transportAfterSend(result)
	}
	if len(responseBytes) > maxResponseBytes {
		return invalidResponse(result, nil, "WWTIOT response exceeds 64 KiB")
	}
	decoded, decodeErr := decodeResponse(responseBytes)
	result.ResponseSummary = responseSummary(decoded)
	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
		return invalidResponse(result, decoded, fmt.Sprintf("WWTIOT HTTP status %d", response.StatusCode))
	}
	if decodeErr != nil {
		return invalidResponse(result, decoded, "WWTIOT response is not a strict JSON object")
	}
	resultValue, ok := decoded["result"].(string)
	if !ok {
		return invalidResponse(result, decoded, "WWTIOT response result must be a string")
	}
	if resultValue != "ok" {
		result.Outcome = domain.AttemptOutcomeProviderRejected
		result.ConfirmationLevel = domain.ConfirmationTransportSent
		result.EvidenceStatus = domain.EvidenceUnverified
		result.ErrorDetail = summaryText(decoded["info"])
		return result
	}
	if !p.client.matchesSuccessEcho(decoded, p.body) {
		return invalidResponse(result, decoded, "WWTIOT success response echo is invalid")
	}
	result.Outcome = domain.AttemptOutcomeProviderAccepted
	result.ConfirmationLevel = domain.ConfirmationProviderAccepted
	result.EvidenceStatus = domain.EvidenceUnverified
	return result
}

func normalizeConfig(cfg Config) (string, string, string, bool) {
	rawURL := strings.TrimSpace(cfg.APIURL)
	parsed, err := url.Parse(rawURL)
	validURL := err == nil && parsed.IsAbs() && parsed.Host != "" && parsed.Opaque == "" && parsed.User == nil && parsed.RawQuery == "" && parsed.Fragment == "" && (parsed.Scheme == "http" || parsed.Scheme == "https")
	userID := strings.TrimSpace(cfg.UserID)
	validUserID := utf8.ValidString(userID) && len(userID) >= 1 && len(userID) <= 128
	validUserKey := utf8.ValidString(cfg.UserKey) && len(cfg.UserKey) >= 1 && len(cfg.UserKey) <= 512
	return rawURL, userID, cfg.UserKey, validURL && validUserID && validUserKey
}

func (c *Client) buildRequest(request DispatchRequest) (map[string]any, map[string]any, error) {
	deviceID := strings.TrimSpace(request.ProviderDeviceID)
	if !providerDeviceIDPattern.MatchString(deviceID) {
		return nil, nil, fmt.Errorf("provider_device_id is invalid")
	}
	if !requestKeyPattern.MatchString(request.ProviderRequestKey) {
		return nil, nil, fmt.Errorf("provider_request_key is invalid")
	}
	if len(request.Payload) != 0 {
		return nil, nil, fmt.Errorf("smart-lock payload must be an empty object")
	}
	serial, err := strconv.ParseInt(request.ProviderRequestKey, 10, 64)
	if err != nil {
		return nil, nil, fmt.Errorf("provider_request_key is invalid")
	}

	var body map[string]any
	switch request.Action {
	case domain.ActionIdentifier("unlock"):
		body = c.openCloseBody("open", deviceID, serial)
	case domain.ActionIdentifier("lock"):
		body = c.openCloseBody("close", deviceID, serial)
	case domain.ActionIdentifier("query_status"):
		body = map[string]any{
			"userid": c.userID, "cmd": "control", "type": int64(23), "value": int64(4),
			"deviceid": deviceID, "serialnum": serial,
		}
		body["sign"] = md5Sign(c.userID, "control", int64(23), int64(4), deviceID, serial, c.userKey)
	default:
		return nil, nil, fmt.Errorf("unsupported smart-lock action")
	}
	return body, requestSummary(body), nil
}

func (c *Client) openCloseBody(command, deviceID string, serial int64) map[string]any {
	body := map[string]any{
		"userid": c.userID, "cmd": command, "deviceid": deviceID, "serialnum": serial,
	}
	body["sign"] = md5Sign(c.userID, command, deviceID, serial, c.userKey)
	return body
}

func (c *Client) matchesSuccessEcho(response, request map[string]any) bool {
	for _, key := range []string{"userid", "deviceid", "cmd"} {
		value, ok := response[key].(string)
		if !ok || value != request[key] {
			return false
		}
	}
	sign, ok := response["sign"].(string)
	if !ok || sign == "" {
		return false
	}
	if !sameInteger(response["serialnum"], request["serialnum"]) {
		return false
	}
	if request["cmd"] == "control" {
		return sameInteger(response["type"], request["type"]) && sameInteger(response["value"], request["value"])
	}
	return true
}

func decodeResponse(data []byte) (map[string]any, error) {
	var decoded map[string]any
	if err := httpjson.DecodeStrict(bytes.NewReader(data), &decoded); err != nil {
		return nil, err
	}
	return decoded, nil
}

func sameInteger(left, right any) bool {
	leftValue, leftOK := integerValue(left)
	rightValue, rightOK := integerValue(right)
	return leftOK && rightOK && leftValue == rightValue
}

func integerValue(value any) (int64, bool) {
	switch typed := value.(type) {
	case float64:
		if typed != float64(int64(typed)) {
			return 0, false
		}
		return int64(typed), true
	case int64:
		return typed, true
	case string:
		if typed == "" || strings.TrimSpace(typed) != typed {
			return 0, false
		}
		parsed, err := strconv.ParseInt(typed, 10, 64)
		return parsed, err == nil
	default:
		return 0, false
	}
}

func invalidRequestResult(detail string) DispatchResult {
	return DispatchResult{
		Outcome: domain.AttemptOutcomeInvalidRequest, ConfirmationLevel: domain.ConfirmationNone,
		EvidenceStatus: domain.EvidenceNone, ErrorDetail: truncateText(detail, maxSummaryTextBytes),
	}
}

func transportBeforeSend(result DispatchResult) DispatchResult {
	result.Outcome = domain.AttemptOutcomeTransportErrorBeforeSend
	result.ConfirmationLevel = domain.ConfirmationNone
	result.EvidenceStatus = domain.EvidenceNone
	result.ErrorDetail = "WWTIOT transport failed before request write"
	return result
}

func transportAfterSend(result DispatchResult) DispatchResult {
	result.Outcome = domain.AttemptOutcomeIndeterminate
	result.ConfirmationLevel = domain.ConfirmationTransportSent
	result.EvidenceStatus = domain.EvidenceVerified
	result.ReasonCode = "provider_delivery_unknown"
	result.ErrorDetail = "WWTIOT transport failed after request write"
	return result
}

func invalidResponse(result DispatchResult, response map[string]any, detail string) DispatchResult {
	result.Outcome = domain.AttemptOutcomeIndeterminate
	result.ConfirmationLevel = domain.ConfirmationTransportSent
	result.EvidenceStatus = domain.EvidenceVerified
	result.ReasonCode = "provider_response_invalid"
	result.ErrorDetail = truncateText(detail, maxSummaryTextBytes)
	if result.ResponseSummary == nil {
		result.ResponseSummary = responseSummary(response)
	}
	return result
}

func requestSummary(body map[string]any) map[string]any {
	return allowlistSummary(body, []string{"cmd", "deviceid", "serialnum", "type", "value"})
}

func responseSummary(body map[string]any) map[string]any {
	return allowlistSummary(body, []string{"result", "info", "cmd", "deviceid", "serialnum", "type", "value"})
}

func allowlistSummary(body map[string]any, keys []string) map[string]any {
	if body == nil {
		return nil
	}
	result := make(map[string]any, len(keys))
	for _, key := range keys {
		value, exists := body[key]
		if !exists {
			continue
		}
		if text, ok := value.(string); ok {
			result[key] = truncateText(text, maxSummaryTextBytes)
			continue
		}
		result[key] = value
	}
	return result
}

func summaryText(value any) string {
	text, _ := value.(string)
	return truncateText(text, maxSummaryTextBytes)
}

func truncateText(value string, limit int) string {
	if len(value) <= limit {
		return value
	}
	value = value[:limit]
	for !utf8.ValidString(value) {
		value = value[:len(value)-1]
	}
	return value
}

func md5Sign(parts ...any) string {
	var builder strings.Builder
	for _, part := range parts {
		builder.WriteString(fmt.Sprint(part))
	}
	sum := md5.Sum([]byte(builder.String()))
	return hex.EncodeToString(sum[:])
}
