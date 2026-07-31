package webhookworker

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"strings"
	"time"
)

var ErrTargetRejected = errors.New("Webhook target rejected by egress policy")

var nonPublicAddressPrefixes = []netip.Prefix{
	netip.MustParsePrefix("0.0.0.0/8"),
	netip.MustParsePrefix("10.0.0.0/8"),
	netip.MustParsePrefix("100.64.0.0/10"),
	netip.MustParsePrefix("127.0.0.0/8"),
	netip.MustParsePrefix("169.254.0.0/16"),
	netip.MustParsePrefix("172.16.0.0/12"),
	netip.MustParsePrefix("192.0.0.0/24"),
	netip.MustParsePrefix("192.0.2.0/24"),
	netip.MustParsePrefix("192.88.99.0/24"),
	netip.MustParsePrefix("192.168.0.0/16"),
	netip.MustParsePrefix("198.18.0.0/15"),
	netip.MustParsePrefix("198.51.100.0/24"),
	netip.MustParsePrefix("203.0.113.0/24"),
	netip.MustParsePrefix("224.0.0.0/4"),
	netip.MustParsePrefix("240.0.0.0/4"),
	netip.MustParsePrefix("::/128"),
	netip.MustParsePrefix("::1/128"),
	netip.MustParsePrefix("64:ff9b::/96"),
	netip.MustParsePrefix("64:ff9b:1::/48"),
	netip.MustParsePrefix("100::/64"),
	netip.MustParsePrefix("2001::/23"),
	netip.MustParsePrefix("2001:db8::/32"),
	netip.MustParsePrefix("2002::/16"),
	netip.MustParsePrefix("fc00::/7"),
	netip.MustParsePrefix("fe80::/10"),
	netip.MustParsePrefix("fec0::/10"),
	netip.MustParsePrefix("ff00::/8"),
}

type addressResolver interface {
	LookupNetIP(context.Context, string, string) ([]netip.Addr, error)
}

type contextDialer interface {
	DialContext(context.Context, string, string) (net.Conn, error)
}

type guardedDialer struct {
	resolver addressResolver
	dialer   contextDialer
	policy   egressPolicy
}

func (d guardedDialer) DialContext(ctx context.Context, network, address string) (net.Conn, error) {
	host, port, err := net.SplitHostPort(address)
	if err != nil {
		return nil, fmt.Errorf("%w: invalid address", ErrTargetRejected)
	}
	d.policy.resolver = d.resolver
	addresses, err := d.policy.resolve(ctx, host)
	if err != nil {
		return nil, err
	}
	var lastErr error
	for _, target := range addresses {
		connection, dialErr := d.dialer.DialContext(ctx, network, net.JoinHostPort(target.String(), port))
		if dialErr == nil {
			return connection, nil
		}
		lastErr = dialErr
	}
	return nil, lastErr
}

func NewSecureHTTPClient(timeout time.Duration, allowlist []netip.Prefix) (*http.Client, error) {
	if timeout <= 0 {
		return nil, ErrInvalidConfig
	}
	dialer := &net.Dialer{Timeout: timeout, KeepAlive: 30 * time.Second}
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.Proxy = nil
	guard := guardedDialer{
		resolver: net.DefaultResolver,
		dialer:   dialer,
		policy:   egressPolicy{allowlist: append([]netip.Prefix(nil), allowlist...)},
	}
	transport.DialContext = guard.DialContext
	return &http.Client{
		Timeout: timeout, Transport: transport,
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}, nil
}

func ParseEgressAllowlist(raw string) ([]netip.Prefix, error) {
	if strings.TrimSpace(raw) == "" {
		return nil, nil
	}
	parts := strings.Split(raw, ",")
	result := make([]netip.Prefix, 0, len(parts))
	for _, part := range parts {
		value := strings.TrimSpace(part)
		if address, err := netip.ParseAddr(value); err == nil && address.Zone() == "" {
			address = address.Unmap()
			result = append(result, netip.PrefixFrom(address, address.BitLen()))
			continue
		}
		prefix, err := netip.ParsePrefix(value)
		if err != nil || prefix.Addr().Zone() != "" {
			return nil, fmt.Errorf("%w: invalid egress allowlist entry", ErrInvalidConfig)
		}
		if prefix.Addr().Is4In6() && prefix.Bits() >= 96 {
			prefix = netip.PrefixFrom(prefix.Addr().Unmap(), prefix.Bits()-96)
		}
		result = append(result, prefix.Masked())
	}
	return result, nil
}

func ValidateTargetURL(raw string) error {
	parsed, err := url.Parse(raw)
	if err != nil || parsed.Host == "" || parsed.User != nil || parsed.Fragment != "" || parsed.Opaque != "" {
		return fmt.Errorf("%w: invalid URL", ErrTargetRejected)
	}
	host := strings.ToLower(parsed.Hostname())
	switch strings.ToLower(parsed.Scheme) {
	case "https":
		return nil
	case "http":
		if host == "localhost" {
			return nil
		}
		address, err := netip.ParseAddr(host)
		if err == nil && address.IsLoopback() {
			return nil
		}
	}
	return fmt.Errorf("%w: non-local target must use HTTPS", ErrTargetRejected)
}

type egressPolicy struct {
	resolver  addressResolver
	allowlist []netip.Prefix
}

func (p *egressPolicy) resolve(ctx context.Context, host string) ([]netip.Addr, error) {
	resolver := p.resolver
	if resolver == nil {
		resolver = net.DefaultResolver
	}
	addresses, err := resolver.LookupNetIP(ctx, "ip", host)
	if err != nil || len(addresses) == 0 {
		return nil, fmt.Errorf("%w: target resolution failed", ErrTargetRejected)
	}
	result := make([]netip.Addr, 0, len(addresses))
	for _, address := range addresses {
		address = address.Unmap()
		if !p.allows(address) {
			return nil, fmt.Errorf("%w: target address is not allowed", ErrTargetRejected)
		}
		result = append(result, address)
	}
	return result, nil
}

func (p egressPolicy) allows(address netip.Addr) bool {
	address = address.Unmap()
	return !forbiddenMetadataAddress(address) && (publicAddress(address) || p.explicitlyAllowed(address))
}

func (p *egressPolicy) explicitlyAllowed(address netip.Addr) bool {
	for _, prefix := range p.allowlist {
		candidate := address
		if prefix.Addr().Is4() {
			candidate = address.Unmap()
		}
		if prefix.Contains(candidate) {
			return true
		}
	}
	return false
}

func publicAddress(address netip.Addr) bool {
	if !address.IsValid() {
		return false
	}
	address = address.Unmap()
	for _, prefix := range nonPublicAddressPrefixes {
		if prefix.Contains(address) {
			return false
		}
	}
	return address.IsGlobalUnicast()
}

func forbiddenMetadataAddress(address netip.Addr) bool {
	for _, raw := range []string{
		"100.100.100.200",
		"168.63.129.16",
		"169.254.0.23",
		"169.254.169.254",
		"169.254.170.2",
		"fd00:ec2::254",
	} {
		if address == netip.MustParseAddr(raw) {
			return true
		}
	}
	return false
}
