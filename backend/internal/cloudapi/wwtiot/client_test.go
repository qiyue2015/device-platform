package wwtiot

import "testing"

func TestMD5SignMatchesVendorScript(t *testing.T) {
	got := MD5Sign([]interface{}{"example-user", "open", "TEST-DEVICE-001", 12345}, "example-user-key")
	want := "442ff57340136242be6b5f46395f037a"
	if got != want {
		t.Fatalf("MD5Sign() = %s, want %s", got, want)
	}
}

func TestBuildQueryStatusRequest(t *testing.T) {
	client := NewClient(Config{UserID: "example-user", UserKey: "secret", DryRun: true})
	req, err := client.BuildCommand("query_status", "TEST-DEVICE-001")
	if err != nil {
		t.Fatalf("BuildCommand(): %v", err)
	}
	if req.Body["cmd"] != "control" || req.Body["type"] != 23 || req.Body["value"] != 4 {
		t.Fatalf("query request body = %#v", req.Body)
	}
	if req.Body["sign"] == "" {
		t.Fatalf("query request missing sign: %#v", req.Body)
	}
	if req.RawMessage.RawPayload["UserKey"] != nil {
		t.Fatalf("raw request leaked UserKey: %#v", req.RawMessage.RawPayload)
	}
}

func TestDryRunOnlyAcksVendorSync(t *testing.T) {
	client := NewClient(Config{DryRun: true})
	req, err := client.BuildCommand("unlock", "TEST-DEVICE-001")
	if err != nil {
		t.Fatalf("BuildCommand(): %v", err)
	}
	ack, err := client.SendCommand(req)
	if err != nil {
		t.Fatalf("SendCommand dry run: %v", err)
	}
	if !ack.DryRun || !ack.Acked || ack.Status != StatusAcked || !ack.CallbackPending {
		t.Fatalf("dry-run ack = %#v", ack)
	}
}

func TestParseResponse(t *testing.T) {
	ack := ParseResponse(200, []byte(`{"result":"ok","info":"accepted"}`))
	if !ack.Acked || ack.Status != StatusAcked || !ack.CallbackPending {
		t.Fatalf("ack response = %#v", ack)
	}
	failed := ParseResponse(200, []byte(`{"result":"fail","info":"bad device"}`))
	if failed.Acked || failed.Status != StatusFailed {
		t.Fatalf("failed response = %#v", failed)
	}
}

func TestNormalizeCallback(t *testing.T) {
	got := NormalizeCallback(map[string]interface{}{
		"deviceid":   "TEST-DEVICE-001",
		"cmd":        "open",
		"lockstatus": "1",
		"battery":    "87",
	})
	if got.ProviderDeviceID != "TEST-DEVICE-001" {
		t.Fatalf("ProviderDeviceID = %q", got.ProviderDeviceID)
	}
	if got.CommandType != "unlock" {
		t.Fatalf("CommandType = %q", got.CommandType)
	}
	if got.LockStatus != "unlocked" {
		t.Fatalf("LockStatus = %q", got.LockStatus)
	}
	if got.BatteryPercent != "87" {
		t.Fatalf("BatteryPercent = %q", got.BatteryPercent)
	}
}

func TestRawMessageRedactsSecrets(t *testing.T) {
	got := NewRawMessage("uplink", map[string]interface{}{
		"UserKey":        "secret",
		"Authorization":  "Bearer token",
		"webhook_secret": "hook",
		"sign":           "debug-signature",
	}, nil, "success")
	if got.RawPayload["UserKey"] != "[REDACTED]" || got.RawPayload["Authorization"] != "[REDACTED]" || got.RawPayload["webhook_secret"] != "[REDACTED]" {
		t.Fatalf("secrets not redacted: %#v", got.RawPayload)
	}
	if got.RawPayload["sign"] != "debug-signature" {
		t.Fatalf("sign should be preserved for vendor debugging: %#v", got.RawPayload["sign"])
	}
}

func TestPhysicalActionMatchesState(t *testing.T) {
	if PhysicalActionMatchesState("unlock", "locked") {
		t.Fatal("unlock callback with locked state must not be success")
	}
	if !PhysicalActionMatchesState("unlock", "unlocked") {
		t.Fatal("unlock callback with unlocked state should be success")
	}
	if PhysicalActionMatchesState("lock", "unlocked") {
		t.Fatal("lock callback with unlocked state must not be success")
	}
	if !PhysicalActionMatchesState("query_status", "unknown") {
		t.Fatal("non-physical command should not require lock state match")
	}
}
