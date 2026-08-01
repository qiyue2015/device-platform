package omni

import (
	"bytes"
	"testing"
	"time"

	"github.com/qiyue2015/device-platform/internal/domain"
)

const (
	testIMEI      = "123456789012345"
	testDeviceID  = "10000000-0000-0000-0000-000000000001"
	testProjectID = "20000000-0000-0000-0000-000000000001"
)

func TestEncodeQueryStatusUsesFrozenProfileFrame(t *testing.T) {
	now := time.Date(2026, time.August, 1, 11, 22, 33, 0, time.UTC)
	tests := []struct {
		name    string
		profile string
		want    []byte
	}{
		{
			name:    "bike S5",
			profile: domain.ProviderProfileOmniBikeV207,
			want:    []byte("\xff\xff*CMDS,OM," + testIMEI + ",260801112233,S5#\n"),
		},
		{
			name:    "IoT S6",
			profile: domain.ProviderProfileOmniIoTV135,
			want:    []byte("\xff\xff*SCOS,OM," + testIMEI + ",S6#\n"),
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, err := EncodeQueryStatus(test.profile, testIMEI, now)
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(got, test.want) {
				t.Fatalf("encoded frame = %q, want %q", got, test.want)
			}
		})
	}
}

func TestEncodeQueryStatusRejectsUnknownIdentityOrProfile(t *testing.T) {
	for _, test := range []struct {
		name, profile, imei string
		want                FrameErrorCode
	}{
		{name: "profile", profile: "omni-guessed", imei: testIMEI, want: FrameInvalidProfile},
		{name: "IMEI", profile: domain.ProviderProfileOmniBikeV207, imei: "123", want: FrameInvalidIMEI},
	} {
		t.Run(test.name, func(t *testing.T) {
			_, err := EncodeQueryStatus(test.profile, test.imei, time.Time{})
			if ErrorCode(err) != test.want {
				t.Fatalf("error = %v, want %s", err, test.want)
			}
		})
	}
}

func TestParseFrameUsesExplicitProfile(t *testing.T) {
	tests := []struct {
		name      string
		profile   string
		raw       []byte
		command   string
		timestamp string
		fields    []string
	}{
		{
			name: "bike response", profile: domain.ProviderProfileOmniBikeV207,
			raw:     []byte("*CMDR,OM," + testIMEI + ",260801112233,S5,412,30,5,0,0#\n"),
			command: "S5", timestamp: "260801112233", fields: []string{"412", "30", "5", "0", "0"},
		},
		{
			name: "bike connect", profile: domain.ProviderProfileOmniBikeV207,
			raw:     []byte("*CMDR,OM," + testIMEI + ",260801112233,Q0,412#\n"),
			command: "Q0", timestamp: "260801112233", fields: []string{"412"},
		},
		{
			name: "bike heartbeat", profile: domain.ProviderProfileOmniBikeV207,
			raw:     []byte("*CMDR,OM," + testIMEI + ",260801112233,H0,0,412,28#\n"),
			command: "H0", timestamp: "260801112233", fields: []string{"0", "412", "28"},
		},
		{
			name: "IoT response", profile: domain.ProviderProfileOmniIoTV135,
			raw:     []byte("*SCOR,OM," + testIMEI + ",S6,80,3,221,0,372,372,0,28,0,0#\n"),
			command: "S6", fields: []string{"80", "3", "221", "0", "372", "372", "0", "28", "0", "0"},
		},
		{
			name: "IoT connect", profile: domain.ProviderProfileOmniIoTV135,
			raw: []byte("*SCOR,OM," + testIMEI + ",Q0,412,80,28#\n"), command: "Q0",
			fields: []string{"412", "80", "28"},
		},
		{
			name: "IoT heartbeat", profile: domain.ProviderProfileOmniIoTV135,
			raw: []byte("*SCOR,OM," + testIMEI + ",H0,0,412,28,80,0#\n"), command: "H0",
			fields: []string{"0", "412", "28", "80", "0"},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			frame, err := ParseFrame(test.profile, test.raw)
			if err != nil {
				t.Fatal(err)
			}
			if frame.Profile != test.profile || frame.IMEI != testIMEI || frame.Command != test.command || frame.Timestamp != test.timestamp ||
				len(frame.Fields) != len(test.fields) || len(frame.Fields) > 0 && frame.Fields[0] != test.fields[0] {
				t.Fatalf("frame = %+v", frame)
			}
		})
	}

	_, err := ParseFrame(domain.ProviderProfileOmniIoTV135,
		[]byte("*CMDR,OM,"+testIMEI+",260801112233,S5,0#\n"))
	if ErrorCode(err) != FrameInvalidHeader {
		t.Fatalf("cross-profile error = %v", err)
	}
}

func TestParseFrameEnforcesHandshakeHeartbeatAndStatusSchemas(t *testing.T) {
	tests := []struct {
		name    string
		profile string
		raw     []byte
	}{
		{name: "bike Q0 missing voltage", profile: domain.ProviderProfileOmniBikeV207, raw: []byte("*CMDR,OM," + testIMEI + ",260801112233,Q0#\n")},
		{name: "bike Q0 voltage range", profile: domain.ProviderProfileOmniBikeV207, raw: []byte("*CMDR,OM," + testIMEI + ",260801112233,Q0,999#\n")},
		{name: "bike H0 missing signal", profile: domain.ProviderProfileOmniBikeV207, raw: []byte("*CMDR,OM," + testIMEI + ",260801112233,H0,0,412#\n")},
		{name: "bike S5 reserved", profile: domain.ProviderProfileOmniBikeV207, raw: []byte("*CMDR,OM," + testIMEI + ",260801112233,S5,412,30,5,0,1#\n")},
		{name: "IoT Q0 missing fields", profile: domain.ProviderProfileOmniIoTV135, raw: []byte("*SCOR,OM," + testIMEI + ",Q0#\n")},
		{name: "IoT Q0 battery range", profile: domain.ProviderProfileOmniIoTV135, raw: []byte("*SCOR,OM," + testIMEI + ",Q0,412,101,28#\n")},
		{name: "IoT H0 missing charge", profile: domain.ProviderProfileOmniIoTV135, raw: []byte("*SCOR,OM," + testIMEI + ",H0,0,412,28,80#\n")},
		{name: "IoT S6 mode range", profile: domain.ProviderProfileOmniIoTV135, raw: []byte("*SCOR,OM," + testIMEI + ",S6,80,4,221,0,372,372,0,28,0,0#\n")},
		{name: "IoT S6 non-decimal", profile: domain.ProviderProfileOmniIoTV135, raw: []byte("*SCOR,OM," + testIMEI + ",S6,80,3,22.1,0,372,372,0,28,0,0#\n")},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, err := ParseFrame(test.profile, test.raw)
			if ErrorCode(err) != FrameInvalidFields {
				t.Fatalf("error = %v, want %s", err, FrameInvalidFields)
			}
		})
	}
}

func TestParseFrameRejectsMalformedInput(t *testing.T) {
	tests := []struct {
		name    string
		profile string
		raw     []byte
		want    FrameErrorCode
	}{
		{name: "empty", profile: domain.ProviderProfileOmniIoTV135, raw: []byte("#\n"), want: FrameEmpty},
		{name: "downlink prefix", profile: domain.ProviderProfileOmniIoTV135, raw: []byte("\xff\xff*SCOR,OM," + testIMEI + ",S6#\n"), want: FrameInvalidPrefix},
		{name: "missing star", profile: domain.ProviderProfileOmniIoTV135, raw: []byte("SCOR,OM," + testIMEI + ",S6#\n"), want: FrameInvalidPrefix},
		{name: "header", profile: domain.ProviderProfileOmniIoTV135, raw: []byte("*SCOS,OM," + testIMEI + ",S6#\n"), want: FrameInvalidHeader},
		{name: "vendor", profile: domain.ProviderProfileOmniIoTV135, raw: []byte("*SCOR,XX," + testIMEI + ",S6#\n"), want: FrameInvalidVendor},
		{name: "IMEI", profile: domain.ProviderProfileOmniIoTV135, raw: []byte("*SCOR,OM,123,S6#\n"), want: FrameInvalidIMEI},
		{name: "timestamp", profile: domain.ProviderProfileOmniBikeV207, raw: []byte("*CMDR,OM," + testIMEI + ",261332999999,S5#\n"), want: FrameInvalidTimestamp},
		{name: "command", profile: domain.ProviderProfileOmniIoTV135, raw: []byte("*SCOR,OM," + testIMEI + ",ZZ#\n"), want: FrameUnknownCommand},
		{name: "trailing", profile: domain.ProviderProfileOmniIoTV135, raw: []byte("*SCOR,OM," + testIMEI + ",S6#\nX#\n"), want: FrameTrailingData},
		{name: "control", profile: domain.ProviderProfileOmniIoTV135, raw: []byte("*SCOR,OM," + testIMEI + ",S6,\r#\n"), want: FrameInvalid},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, err := ParseFrame(test.profile, test.raw)
			if ErrorCode(err) != test.want {
				t.Fatalf("error = %v, want %s", err, test.want)
			}
		})
	}
}

func TestDecoderHandlesSplitAndCoalescedFrames(t *testing.T) {
	decoder, err := NewDecoder(domain.ProviderProfileOmniIoTV135, 128)
	if err != nil {
		t.Fatal(err)
	}
	first := []byte("*SCOR,OM," + testIMEI + ",Q0,412,80,28#\n")
	second := []byte("*SCOR,OM," + testIMEI + ",H0,0,412,28,80,0#\n")
	if got := decoder.Feed(first[:7]); len(got) != 0 {
		t.Fatalf("partial frame produced %d results", len(got))
	}
	results := decoder.Feed(append(first[7:], second...))
	if len(results) != 2 || results[0].Err != nil || results[1].Err != nil ||
		results[0].Frame.Command != "Q0" || results[1].Frame.Command != "H0" {
		t.Fatalf("decoded frames = %+v", results)
	}
	if tail := decoder.Close(); len(tail) != 0 {
		t.Fatalf("unexpected decoder tail = %+v", tail)
	}
}

func TestDecoderBoundsOversizedFramesAndRecovers(t *testing.T) {
	decoder, err := NewDecoder(domain.ProviderProfileOmniIoTV135, 128)
	if err != nil {
		t.Fatal(err)
	}
	results := decoder.Feed(bytes.Repeat([]byte{'A'}, 129))
	if len(results) != 1 || ErrorCode(results[0].Err) != FrameTooLarge || len(results[0].Raw) != 0 {
		t.Fatalf("oversized result = %+v", results)
	}
	valid := []byte("*SCOR,OM," + testIMEI + ",S6,80,3,221,0,372,372,0,28,0,0#\n")
	results = decoder.Feed(append([]byte(wireTerminator), valid...))
	if len(results) != 1 || results[0].Err != nil || results[0].Frame.Command != "S6" {
		t.Fatalf("recovery result = %+v", results)
	}

	if got := decoder.Feed([]byte("trailing")); len(got) != 0 {
		t.Fatalf("trailing partial result = %+v", got)
	}
	tail := decoder.Close()
	if len(tail) != 1 || ErrorCode(tail[0].Err) != FrameTrailingData {
		t.Fatalf("close result = %+v", tail)
	}
}
