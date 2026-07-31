package projectservice

import (
	"bytes"
	"errors"
	"net/netip"
	"testing"
)

func TestNormalizeWhitelistCanonicalizesAndDeduplicates(t *testing.T) {
	got, err := normalizeWhitelist([]string{
		"192.0.2.9", "192.0.2.9/32", "192.0.2.0/24", "192.0.2.42/24",
		"2001:0db8::1", "2001:db8::/64", "::ffff:192.0.2.9",
	})
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"192.0.2.9", "192.0.2.9/32", "192.0.2.0/24", "2001:db8::1", "2001:db8::/64"}
	if len(got) != len(want) {
		t.Fatalf("canonical whitelist = %#v", got)
	}
	for index := range want {
		if got[index] != want[index] {
			t.Fatalf("canonical whitelist[%d] = %q, want %q", index, got[index], want[index])
		}
	}
}

func TestNormalizeWhitelistRejectsInvalidAndScopedAddresses(t *testing.T) {
	for _, value := range []string{"not-an-ip", "fe80::1%lo0", "192.0.2.1/33"} {
		if _, err := normalizeWhitelist([]string{value}); !errors.Is(err, ErrInvalidRequest) {
			t.Fatalf("invalid whitelist value %q error = %v", value, err)
		}
	}
}

func TestWhitelistAllowsSingleAddressesAndCIDRs(t *testing.T) {
	tests := []struct {
		name    string
		values  []string
		peer    string
		allowed bool
	}{
		{name: "empty allows", peer: "203.0.113.7", allowed: true},
		{name: "single IPv4", values: []string{"203.0.113.7"}, peer: "203.0.113.7", allowed: true},
		{name: "IPv4 CIDR", values: []string{"203.0.113.0/24"}, peer: "203.0.113.7", allowed: true},
		{name: "IPv6 CIDR", values: []string{"2001:db8::/64"}, peer: "2001:db8::7", allowed: true},
		{name: "outside", values: []string{"203.0.113.0/24"}, peer: "198.51.100.7", allowed: false},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			allowed, err := whitelistAllows(test.values, netip.MustParseAddr(test.peer))
			if err != nil || allowed != test.allowed {
				t.Fatalf("allowed=%v err=%v", allowed, err)
			}
		})
	}
}

func TestNormalizeWebhookURLSecurityRules(t *testing.T) {
	valid := []string{"https://hooks.example.test/events", "http://localhost:8080/events", "http://127.0.0.1/hook", "http://[::1]:8080/hook"}
	for _, raw := range valid {
		if _, err := normalizeWebhookURL(&raw); err != nil {
			t.Fatalf("valid URL %q rejected: %v", raw, err)
		}
	}
	invalid := []string{"http://example.test/hook", "https://user@example.test/hook", "https://example.test/hook#secret", "ftp://example.test/hook", "/hook"}
	for _, raw := range invalid {
		if _, err := normalizeWebhookURL(&raw); !errors.Is(err, ErrInvalidRequest) {
			t.Fatalf("invalid URL %q error = %v", raw, err)
		}
	}
}

func TestWebhookSecretEncryptionBindsProjectAndVersions(t *testing.T) {
	key := bytes.Repeat([]byte{0x42}, 32)
	random := bytes.NewReader(bytes.Repeat([]byte{0x11}, 12))
	ciphertext, nonce, err := encryptSecret(random, key, "10000000-0000-0000-0000-000000000001", 2, 3, "whsec_secret")
	if err != nil {
		t.Fatal(err)
	}
	plaintext, err := decryptSecret(key, ciphertext, nonce, "10000000-0000-0000-0000-000000000001", 2, 3)
	if err != nil || plaintext != "whsec_secret" {
		t.Fatalf("plaintext=%q err=%v", plaintext, err)
	}
	if _, err := decryptSecret(key, ciphertext, nonce, "10000000-0000-0000-0000-000000000002", 2, 3); !errors.Is(err, ErrWebhookSecretDecryption) {
		t.Fatalf("AAD mismatch error = %v", err)
	}
}

func TestCredentialShape(t *testing.T) {
	for _, prefix := range []string{apiKeyPrefix, webhookSecretPrefix} {
		credential, err := generateCredential(bytes.NewReader(bytes.Repeat([]byte{0x7a}, 32)), prefix)
		if err != nil {
			t.Fatal(err)
		}
		if !validCredential(credential, prefix) {
			t.Fatalf("generated credential has invalid shape: %q", credential)
		}
	}
}
