package omni

import (
	"bytes"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/qiyue2015/device-platform/internal/domain"
)

const (
	downlinkPrefix   = "\xff\xff"
	wireTerminator   = "#\n"
	vendorCode       = "OM"
	bikeRequestHead  = "*CMDS"
	bikeResponseHead = "*CMDR"
	iotRequestHead   = "*SCOS"
	iotResponseHead  = "*SCOR"
)

type FrameErrorCode string

const (
	FrameInvalidProfile   FrameErrorCode = "invalid_profile"
	FrameEmpty            FrameErrorCode = "empty_frame"
	FrameTooLarge         FrameErrorCode = "frame_too_large"
	FrameInvalidPrefix    FrameErrorCode = "invalid_prefix"
	FrameInvalidHeader    FrameErrorCode = "invalid_header"
	FrameInvalidVendor    FrameErrorCode = "invalid_vendor"
	FrameInvalidIMEI      FrameErrorCode = "invalid_imei"
	FrameInvalidTimestamp FrameErrorCode = "invalid_timestamp"
	FrameUnknownCommand   FrameErrorCode = "unknown_command"
	FrameInvalidFields    FrameErrorCode = "invalid_fields"
	FrameTrailingData     FrameErrorCode = "trailing_data"
	FrameInvalid          FrameErrorCode = "invalid_frame"
)

type FrameError struct {
	Code FrameErrorCode
}

func (e *FrameError) Error() string {
	if e == nil {
		return "invalid Omni frame"
	}
	return fmt.Sprintf("invalid Omni frame: %s", e.Code)
}

func newFrameError(code FrameErrorCode) error {
	return &FrameError{Code: code}
}

func ErrorCode(err error) FrameErrorCode {
	var frameError *FrameError
	if errors.As(err, &frameError) {
		return frameError.Code
	}
	return FrameInvalid
}

type Frame struct {
	Profile   string
	IMEI      string
	Timestamp string
	Command   string
	Fields    []string
}

func EncodeQueryStatus(profile, imei string, now time.Time) ([]byte, error) {
	if !validProfile(profile) {
		return nil, newFrameError(FrameInvalidProfile)
	}
	if !validIMEI(imei) {
		return nil, newFrameError(FrameInvalidIMEI)
	}
	var body string
	switch profile {
	case domain.ProviderProfileOmniBikeV207:
		body = strings.Join([]string{bikeRequestHead, vendorCode, imei, now.UTC().Format("060102150405"), "S5"}, ",")
	case domain.ProviderProfileOmniIoTV135:
		body = strings.Join([]string{iotRequestHead, vendorCode, imei, "S6"}, ",")
	}
	return []byte(downlinkPrefix + body + wireTerminator), nil
}

func ParseFrame(profile string, raw []byte) (Frame, error) {
	if !validProfile(profile) {
		return Frame{}, newFrameError(FrameInvalidProfile)
	}
	if len(raw) == len(wireTerminator) && bytes.Equal(raw, []byte(wireTerminator)) {
		return Frame{}, newFrameError(FrameEmpty)
	}
	if bytes.HasPrefix(raw, []byte(downlinkPrefix)) || len(raw) == 0 || raw[0] != '*' {
		return Frame{}, newFrameError(FrameInvalidPrefix)
	}
	if !bytes.HasSuffix(raw, []byte(wireTerminator)) {
		return Frame{}, newFrameError(FrameTrailingData)
	}
	body := raw[:len(raw)-len(wireTerminator)]
	if len(body) == 0 {
		return Frame{}, newFrameError(FrameEmpty)
	}
	if bytes.Contains(body, []byte(wireTerminator)) {
		return Frame{}, newFrameError(FrameTrailingData)
	}
	for _, value := range body {
		if value < 0x20 || value > 0x7e || value == '#' {
			return Frame{}, newFrameError(FrameInvalid)
		}
	}
	parts := strings.Split(string(body), ",")
	minimumFields := 4
	wantHeader := iotResponseHead
	if profile == domain.ProviderProfileOmniBikeV207 {
		minimumFields = 5
		wantHeader = bikeResponseHead
	}
	if len(parts) < minimumFields {
		return Frame{}, newFrameError(FrameInvalid)
	}
	if parts[0] != wantHeader {
		return Frame{}, newFrameError(FrameInvalidHeader)
	}
	if parts[1] != vendorCode {
		return Frame{}, newFrameError(FrameInvalidVendor)
	}
	if !validIMEI(parts[2]) {
		return Frame{}, newFrameError(FrameInvalidIMEI)
	}
	commandIndex := 3
	timestamp := ""
	if profile == domain.ProviderProfileOmniBikeV207 {
		timestamp = parts[3]
		if !validTimestamp(timestamp) {
			return Frame{}, newFrameError(FrameInvalidTimestamp)
		}
		commandIndex = 4
	}
	if !knownResponseCommand(profile, parts[commandIndex]) {
		return Frame{}, newFrameError(FrameUnknownCommand)
	}
	fields := append([]string(nil), parts[commandIndex+1:]...)
	for _, field := range fields {
		if field == "" {
			return Frame{}, newFrameError(FrameInvalidFields)
		}
	}
	if !validResponseFields(profile, parts[commandIndex], fields) {
		return Frame{}, newFrameError(FrameInvalidFields)
	}
	return Frame{
		Profile: profile, IMEI: parts[2], Timestamp: timestamp,
		Command: parts[commandIndex], Fields: fields,
	}, nil
}

func validProfile(profile string) bool {
	return profile == domain.ProviderProfileOmniBikeV207 || profile == domain.ProviderProfileOmniIoTV135
}

func validIMEI(imei string) bool {
	if len(imei) != 15 {
		return false
	}
	for _, value := range imei {
		if value < '0' || value > '9' {
			return false
		}
	}
	return true
}

func validTimestamp(value string) bool {
	if len(value) != 12 {
		return false
	}
	for _, digit := range value {
		if digit < '0' || digit > '9' {
			return false
		}
	}
	_, err := time.Parse("060102150405", value)
	return err == nil
}

func knownResponseCommand(profile, command string) bool {
	switch profile {
	case domain.ProviderProfileOmniBikeV207:
		switch command {
		case "Q0", "H0", "L0", "L1", "S5":
			return true
		}
	case domain.ProviderProfileOmniIoTV135:
		switch command {
		case "Q0", "H0", "R0", "L0", "L1", "S6":
			return true
		}
	}
	return false
}

func validResponseFields(profile, command string, fields []string) bool {
	switch profile {
	case domain.ProviderProfileOmniBikeV207:
		switch command {
		case "Q0":
			return len(fields) == 1 && decimalInRange(fields[0], 320, 420)
		case "H0":
			return len(fields) == 3 && decimalInRange(fields[0], 0, 1) &&
				decimalInRange(fields[1], 320, 420) && decimalInRange(fields[2], 2, 32)
		case "S5":
			return len(fields) == 5 && decimalInRange(fields[0], 320, 420) &&
				decimalInRange(fields[1], 2, 32) && decimalInRange(fields[2], 0, 4294967295) &&
				decimalInRange(fields[3], 0, 1) && fields[4] == "0"
		}
	case domain.ProviderProfileOmniIoTV135:
		switch command {
		case "Q0":
			return len(fields) == 3 && unsignedDecimal(fields[0]) && decimalInRange(fields[1], 0, 100) &&
				decimalInRange(fields[2], 2, 32)
		case "H0":
			return len(fields) == 5 && decimalInRange(fields[0], 0, 1) && unsignedDecimal(fields[1]) &&
				decimalInRange(fields[2], 2, 32) && decimalInRange(fields[3], 0, 100) &&
				decimalInRange(fields[4], 0, 1)
		case "S6":
			return len(fields) == 10 && decimalInRange(fields[0], 0, 100) &&
				decimalInRange(fields[1], 1, 3) && unsignedDecimal(fields[2]) &&
				decimalInRange(fields[3], 0, 1) && unsignedDecimal(fields[4]) && unsignedDecimal(fields[5]) &&
				decimalInRange(fields[6], 0, 1) && decimalInRange(fields[7], 2, 32) &&
				unsignedDecimal(fields[8]) && unsignedDecimal(fields[9])
		}
	}
	// Other in-scope action responses are retained only as unverified technical
	// messages. They cannot establish a session or update trusted state.
	return len(fields) > 0
}

func decimalInRange(value string, minimum, maximum uint64) bool {
	parsed, ok := parseUnsignedDecimal(value)
	return ok && parsed >= minimum && parsed <= maximum
}

func unsignedDecimal(value string) bool {
	_, ok := parseUnsignedDecimal(value)
	return ok
}

func parseUnsignedDecimal(value string) (uint64, bool) {
	if value == "" {
		return 0, false
	}
	for _, digit := range value {
		if digit < '0' || digit > '9' {
			return 0, false
		}
	}
	parsed, err := strconv.ParseUint(value, 10, 32)
	return parsed, err == nil
}
