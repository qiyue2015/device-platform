package main

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"
)

func TestJWTContractAndMemorySessionInvalidation(t *testing.T) {
	auth, err := newMemoryAuthenticator("admin@example.test", "Admin", "StrongPass123!", testJWTSecret)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 7, 31, 12, 0, 0, 0, time.UTC)
	auth.now = func() time.Time { return now }
	user, err := auth.Login(context.Background(), " ADMIN@example.test ", "StrongPass123!", authRequestMetadata{})
	if err != nil {
		t.Fatal(err)
	}
	token, err := auth.IssueToken(user)
	if err != nil {
		t.Fatal(err)
	}
	claims := decodeJWTClaimsForTest(t, token)
	if claims["iss"] != jwtIssuer || claims["aud"] != jwtAudience || claims["jti"] == "" || claims["session_generation"] != float64(0) {
		t.Fatalf("JWT claims do not match the frozen contract: %+v", claims)
	}
	if _, err := auth.ParseToken(context.Background(), token); err != nil {
		t.Fatalf("fresh token rejected: %v", err)
	}
	if err := auth.Logout(context.Background(), user, authRequestMetadata{}); err != nil {
		t.Fatal(err)
	}
	if _, err := auth.ParseToken(context.Background(), token); !errors.Is(err, errUnauthorized) {
		t.Fatalf("logged-out token error = %v", err)
	}
	user, err = auth.Login(context.Background(), "admin@example.test", "StrongPass123!", authRequestMetadata{})
	if err != nil {
		t.Fatal(err)
	}
	if user.SessionGeneration != 1 {
		t.Fatalf("session generation = %d", user.SessionGeneration)
	}
	secondToken, err := auth.IssueToken(user)
	if err != nil {
		t.Fatal(err)
	}
	if secondToken == token {
		t.Fatal("each token must have a unique jti")
	}
}

func TestParseJWTRejectsWrongIssuerAndAlgorithmEvenWithValidSignature(t *testing.T) {
	user := currentUser{ID: "test-admin", Email: "admin@example.test", DisplayName: "Admin", IsSuperAdmin: true, Status: "active"}
	now := time.Date(2026, 7, 31, 12, 0, 0, 0, time.UTC)
	token, err := createJWT(user, testJWTSecret, now)
	if err != nil {
		t.Fatal(err)
	}
	parts := strings.Split(token, ".")
	claims := decodeJWTClaimsForTest(t, token)
	claims["iss"] = "other-service"
	parts[1] = encodeJSONForTest(t, claims)
	wrongIssuer := signJWTForTest(parts[0]+"."+parts[1], testJWTSecret)
	if _, err := parseJWT(wrongIssuer, testJWTSecret, now); err == nil {
		t.Fatal("wrong issuer must be rejected")
	}

	claims["iss"] = jwtIssuer
	parts[1] = encodeJSONForTest(t, claims)
	header := map[string]any{"alg": "HS512", "typ": "JWT"}
	parts[0] = encodeJSONForTest(t, header)
	wrongAlgorithm := signJWTForTest(parts[0]+"."+parts[1], testJWTSecret)
	if _, err := parseJWT(wrongAlgorithm, testJWTSecret, now); err == nil {
		t.Fatal("non-HS256 algorithm must be rejected")
	}
}

func TestParseJWTRejectsWrongAudienceAndInvalidTimes(t *testing.T) {
	user := currentUser{ID: "test-admin", Email: "admin@example.test", DisplayName: "Admin", IsSuperAdmin: true, Status: "active"}
	now := time.Date(2026, 7, 31, 12, 0, 0, 0, time.UTC)
	token, err := createJWT(user, testJWTSecret, now)
	if err != nil {
		t.Fatal(err)
	}
	parts := strings.Split(token, ".")
	baseClaims := decodeJWTClaimsForTest(t, token)
	tests := []struct {
		name   string
		mutate func(map[string]any)
	}{
		{name: "audience", mutate: func(claims map[string]any) { claims["aud"] = "other-client" }},
		{name: "expired", mutate: func(claims map[string]any) { claims["exp"] = float64(now.Unix()) }},
		{name: "future issued at", mutate: func(claims map[string]any) { claims["iat"] = float64(now.Add(time.Second).Unix()) }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			claims := make(map[string]any, len(baseClaims))
			for key, value := range baseClaims {
				claims[key] = value
			}
			tt.mutate(claims)
			payload := encodeJSONForTest(t, claims)
			mutated := signJWTForTest(parts[0]+"."+payload, testJWTSecret)
			if _, err := parseJWT(mutated, testJWTSecret, now); err == nil {
				t.Fatal("invalid claim must be rejected")
			}
		})
	}
}

func decodeJWTClaimsForTest(t *testing.T, token string) map[string]any {
	t.Helper()
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		t.Fatalf("invalid JWT: %q", token)
	}
	payload, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		t.Fatal(err)
	}
	var claims map[string]any
	if err := json.Unmarshal(payload, &claims); err != nil {
		t.Fatal(err)
	}
	return claims
}

func encodeJSONForTest(t *testing.T, value any) string {
	t.Helper()
	data, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	return base64.RawURLEncoding.EncodeToString(data)
}

func signJWTForTest(signingInput, secret string) string {
	mac := hmac.New(sha256.New, []byte(secret))
	_, _ = mac.Write([]byte(signingInput))
	return signingInput + "." + base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
}
