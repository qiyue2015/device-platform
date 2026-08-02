package projectservice

import (
	"encoding/hex"
	"fmt"
	"net"
	"net/netip"
	"net/url"
	"strings"
	"unicode/utf8"
)

func normalizeName(value string) (string, error) {
	value = strings.TrimSpace(value)
	if !utf8.ValidString(value) || utf8.RuneCountInString(value) < 1 || utf8.RuneCountInString(value) > 120 {
		return "", fmt.Errorf("%w: name must contain 1..120 characters", ErrInvalidRequest)
	}
	return value, nil
}

func normalizeWebhookURL(value *string) (*string, error) {
	if value == nil {
		return nil, nil
	}
	raw := strings.TrimSpace(*value)
	parsed, err := url.Parse(raw)
	if err != nil || raw == "" || !parsed.IsAbs() || parsed.Host == "" || parsed.Opaque != "" || parsed.User != nil || parsed.Fragment != "" {
		return nil, fmt.Errorf("%w: webhook_url is invalid", ErrInvalidRequest)
	}
	host := strings.ToLower(parsed.Hostname())
	switch strings.ToLower(parsed.Scheme) {
	case "https":
	case "http":
		address, addressErr := netip.ParseAddr(host)
		if host != "localhost" && (addressErr != nil || !address.IsLoopback()) {
			return nil, fmt.Errorf("%w: non-local webhook_url must use HTTPS", ErrInvalidRequest)
		}
	default:
		return nil, fmt.Errorf("%w: webhook_url scheme is invalid", ErrInvalidRequest)
	}
	parsed.Scheme = strings.ToLower(parsed.Scheme)
	result := parsed.String()
	return &result, nil
}

func normalizeWhitelist(values []string) ([]string, error) {
	result := make([]string, 0, len(values))
	seen := make(map[string]struct{}, len(values))
	for _, value := range values {
		canonical, err := canonicalIPRange(value)
		if err != nil {
			return nil, fmt.Errorf("%w: ip_whitelist contains %q", ErrInvalidRequest, value)
		}
		if _, exists := seen[canonical]; exists {
			continue
		}
		seen[canonical] = struct{}{}
		result = append(result, canonical)
	}
	return result, nil
}

func canonicalIPRange(value string) (string, error) {
	value = strings.TrimSpace(value)
	if address, err := netip.ParseAddr(value); err == nil {
		if address.Zone() != "" {
			return "", fmt.Errorf("scoped IP addresses are not supported")
		}
		return address.Unmap().String(), nil
	}
	prefix, err := netip.ParsePrefix(value)
	if err != nil {
		return "", err
	}
	if prefix.Addr().Is4In6() && prefix.Bits() >= 96 {
		prefix = netip.PrefixFrom(prefix.Addr().Unmap(), prefix.Bits()-96)
	}
	return prefix.Masked().String(), nil
}

func parsePeerAddress(value string) (netip.Addr, error) {
	value = strings.TrimSpace(value)
	if address, err := netip.ParseAddr(value); err == nil {
		return address.Unmap(), nil
	}
	host, _, err := net.SplitHostPort(value)
	if err != nil {
		return netip.Addr{}, err
	}
	address, err := netip.ParseAddr(host)
	if err != nil {
		return netip.Addr{}, err
	}
	return address.Unmap(), nil
}

func whitelistAllows(values []string, address netip.Addr) (bool, error) {
	if len(values) == 0 {
		return true, nil
	}
	for _, value := range values {
		if allowed, err := netip.ParseAddr(value); err == nil {
			if allowed.Unmap() == address.Unmap() {
				return true, nil
			}
			continue
		}
		prefix, err := netip.ParsePrefix(value)
		if err != nil {
			return false, err
		}
		candidate := address
		if prefix.Addr().Is4() {
			candidate = candidate.Unmap()
		}
		if prefix.Contains(candidate) {
			return true, nil
		}
	}
	return false, nil
}

func validateRequestMetadata(metadata RequestMetadata) (RequestMetadata, error) {
	metadata.ActorUserID = strings.TrimSpace(metadata.ActorUserID)
	metadata.RequestID = strings.TrimSpace(metadata.RequestID)
	if !validUUID(metadata.ActorUserID) || metadata.RequestID == "" || len(metadata.RequestID) > 255 {
		return RequestMetadata{}, fmt.Errorf("%w: request_id is required", ErrInvalidRequest)
	}
	if metadata.IPAddress != "" {
		address, err := netip.ParseAddr(strings.TrimSpace(metadata.IPAddress))
		if err != nil || address.Zone() != "" {
			return RequestMetadata{}, fmt.Errorf("%w: ip_address is invalid", ErrInvalidRequest)
		}
		metadata.IPAddress = address.Unmap().String()
	}
	return metadata, nil
}

func validUUID(value string) bool {
	if len(value) != 36 || value[8] != '-' || value[13] != '-' || value[18] != '-' || value[23] != '-' {
		return false
	}
	compact := strings.ReplaceAll(value, "-", "")
	decoded, err := hex.DecodeString(compact)
	return err == nil && len(decoded) == 16 && value == strings.ToLower(value)
}
