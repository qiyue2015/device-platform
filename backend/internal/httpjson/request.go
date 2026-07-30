package httpjson

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strings"
)

const MaxJSONBodyBytes int64 = 1 << 20

var (
	ErrInvalidJSON  = errors.New("invalid JSON body")
	ErrUnknownField = errors.New("unknown JSON field")
	clientRequestID = regexp.MustCompile(`^[A-Za-z0-9._:+-]{1,128}$`)
)

type requestIDContextKey struct{}
type clientRequestIDContextKey struct{}

// DecodeStrict accepts one JSON object and rejects duplicate keys, unknown
// fields, trailing values, and oversized input.
func DecodeStrict(reader io.Reader, out any) error {
	limited := io.LimitReader(reader, MaxJSONBodyBytes+1)
	data, err := io.ReadAll(limited)
	if err != nil {
		return fmt.Errorf("%w: read body", ErrInvalidJSON)
	}
	if int64(len(data)) > MaxJSONBodyBytes {
		return fmt.Errorf("%w: body exceeds %d bytes", ErrInvalidJSON, MaxJSONBodyBytes)
	}
	if err := validateJSONObject(data); err != nil {
		return err
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(out); err != nil {
		if strings.HasPrefix(err.Error(), "json: unknown field ") {
			return fmt.Errorf("%w: %v", ErrUnknownField, err)
		}
		return fmt.Errorf("%w: %v", ErrInvalidJSON, err)
	}
	return nil
}

func validateJSONObject(data []byte) error {
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.UseNumber()
	first, err := decoder.Token()
	if err != nil {
		return fmt.Errorf("%w: %v", ErrInvalidJSON, err)
	}
	if first != json.Delim('{') {
		return fmt.Errorf("%w: body must be an object", ErrInvalidJSON)
	}
	if err := validateObject(decoder); err != nil {
		return err
	}
	if _, err := decoder.Token(); !errors.Is(err, io.EOF) {
		if err == nil {
			return fmt.Errorf("%w: trailing JSON value", ErrInvalidJSON)
		}
		return fmt.Errorf("%w: %v", ErrInvalidJSON, err)
	}
	return nil
}

func validateObject(decoder *json.Decoder) error {
	seen := make(map[string]struct{})
	for decoder.More() {
		keyToken, err := decoder.Token()
		if err != nil {
			return fmt.Errorf("%w: %v", ErrInvalidJSON, err)
		}
		key, ok := keyToken.(string)
		if !ok {
			return fmt.Errorf("%w: object key must be a string", ErrInvalidJSON)
		}
		if _, duplicate := seen[key]; duplicate {
			return fmt.Errorf("%w: duplicate key %q", ErrInvalidJSON, key)
		}
		seen[key] = struct{}{}
		if err := validateValue(decoder); err != nil {
			return err
		}
	}
	closing, err := decoder.Token()
	if err != nil || closing != json.Delim('}') {
		return fmt.Errorf("%w: malformed object", ErrInvalidJSON)
	}
	return nil
}

func validateValue(decoder *json.Decoder) error {
	token, err := decoder.Token()
	if err != nil {
		return fmt.Errorf("%w: %v", ErrInvalidJSON, err)
	}
	delim, ok := token.(json.Delim)
	if !ok {
		return nil
	}
	switch delim {
	case '{':
		return validateObject(decoder)
	case '[':
		for decoder.More() {
			if err := validateValue(decoder); err != nil {
				return err
			}
		}
		closing, err := decoder.Token()
		if err != nil || closing != json.Delim(']') {
			return fmt.Errorf("%w: malformed array", ErrInvalidJSON)
		}
		return nil
	default:
		return fmt.Errorf("%w: unexpected delimiter", ErrInvalidJSON)
	}
}

func WithRequestID(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestID, err := newRequestID()
		if err != nil {
			Error(w, http.StatusInternalServerError, "request_id_generation_failed", "internal server error")
			return
		}
		w.Header().Set("X-Request-ID", requestID)
		ctx := context.WithValue(r.Context(), requestIDContextKey{}, requestID)
		if candidate := strings.TrimSpace(r.Header.Get("X-Request-ID")); clientRequestID.MatchString(candidate) {
			ctx = context.WithValue(ctx, clientRequestIDContextKey{}, candidate)
		}
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

func RequestID(ctx context.Context) string {
	value, _ := ctx.Value(requestIDContextKey{}).(string)
	return value
}

func ClientRequestID(ctx context.Context) string {
	value, _ := ctx.Value(clientRequestIDContextKey{}).(string)
	return value
}

func newRequestID() (string, error) {
	var raw [16]byte
	if _, err := rand.Read(raw[:]); err != nil {
		return "", err
	}
	raw[6] = (raw[6] & 0x0f) | 0x40
	raw[8] = (raw[8] & 0x3f) | 0x80
	encoded := hex.EncodeToString(raw[:])
	return encoded[0:8] + "-" + encoded[8:12] + "-" + encoded[12:16] + "-" + encoded[16:20] + "-" + encoded[20:32], nil
}
