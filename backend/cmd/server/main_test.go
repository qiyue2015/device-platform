package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/qiyue2015/device-platform/internal/cloudapi/wwtiot"
)

func TestWWTIOTCallbackEndpointNormalizesAndRedactsRawMessage(t *testing.T) {
	req := httptest.NewRequest(http.MethodPost, "/v1/provider-callbacks/wwtiot", strings.NewReader(`{
		"deviceid":"TEST-DEVICE-001",
		"cmd":"open",
		"lockstatus":"1",
		"battery":"87",
		"UserKey":"secret"
	}`))
	rr := httptest.NewRecorder()

	handleWWTIOTCallback(rr, req)

	if rr.Code != http.StatusAccepted {
		t.Fatalf("status = %d, body = %s", rr.Code, rr.Body.String())
	}
	var body map[string]interface{}
	if err := json.Unmarshal(rr.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	normalized := body["normalized"].(map[string]interface{})
	if normalized["provider_device_id"] != "TEST-DEVICE-001" || normalized["command_type"] != "unlock" || normalized["lock_status"] != "unlocked" {
		t.Fatalf("normalized callback = %#v", normalized)
	}
	raw := body["raw_message"].(map[string]interface{})
	rawPayload := raw["raw_payload"].(map[string]interface{})
	if rawPayload["UserKey"] != "[REDACTED]" {
		t.Fatalf("raw payload leaked secret: %#v", rawPayload)
	}
}

func TestWWTIOTCommandPreviewUsesDryRunAdapterPath(t *testing.T) {
	client := wwtiot.NewClient(wwtiot.Config{DryRun: true})
	req := httptest.NewRequest(http.MethodPost, "/v1/cloud-api/wwtiot/command-preview", strings.NewReader(`{
		"command_type":"unlock",
		"provider_device_id":"TEST-DEVICE-001"
	}`))
	rr := httptest.NewRecorder()

	handleWWTIOTCommandPreview(client).ServeHTTP(rr, req)

	if rr.Code != http.StatusAccepted {
		t.Fatalf("status = %d, body = %s", rr.Code, rr.Body.String())
	}
	var body map[string]interface{}
	if err := json.Unmarshal(rr.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	ack := body["ack"].(map[string]interface{})
	if ack["dry_run"] != true || ack["status"] != wwtiot.StatusAcked {
		t.Fatalf("ack = %#v", ack)
	}
	request := body["request"].(map[string]interface{})
	if request["command_type"] != "unlock" {
		t.Fatalf("request = %#v", request)
	}
}
