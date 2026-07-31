package webhookworker

import (
	"context"
	"errors"
	"net"
	"net/http"
	"net/netip"
	"testing"
	"time"
)

type staticResolver []netip.Addr

func (r staticResolver) LookupNetIP(context.Context, string, string) ([]netip.Addr, error) {
	return r, nil
}

type recordingDialer struct {
	calls     int
	addresses []string
	err       error
}

func (d *recordingDialer) DialContext(_ context.Context, _, address string) (net.Conn, error) {
	d.calls++
	d.addresses = append(d.addresses, address)
	return nil, d.err
}

type changingResolver struct {
	answers [][]netip.Addr
	calls   int
}

func (r *changingResolver) LookupNetIP(context.Context, string, string) ([]netip.Addr, error) {
	answer := r.answers[r.calls]
	r.calls++
	return answer, nil
}

func TestGuardedDialerValidatesEveryResolvedAddressBeforeDial(t *testing.T) {
	dialer := &recordingDialer{err: errors.New("dial stopped")}
	guard := guardedDialer{
		resolver: staticResolver{netip.MustParseAddr("8.8.8.8"), netip.MustParseAddr("10.0.0.1")},
		dialer:   dialer,
		policy:   egressPolicy{},
	}
	_, err := guard.DialContext(context.Background(), "tcp", "hooks.example.test:443")
	if !errors.Is(err, ErrTargetRejected) || dialer.calls != 0 {
		t.Fatalf("mixed DNS answer err=%v dial calls=%d", err, dialer.calls)
	}
}

func TestSecureHTTPClientIgnoresEnvironmentProxy(t *testing.T) {
	t.Setenv("HTTPS_PROXY", "http://127.0.0.1:3128")
	client, err := NewSecureHTTPClient(time.Second, nil)
	if err != nil {
		t.Fatal(err)
	}
	transport, ok := client.Transport.(*http.Transport)
	if !ok || transport.Proxy != nil {
		t.Fatalf("transport type/proxy=%T/%t", client.Transport, transport.Proxy != nil)
	}
}

func TestGuardedDialerUsesExplicitlyAllowedPrivateAddress(t *testing.T) {
	sentinel := errors.New("dial reached")
	dialer := &recordingDialer{err: sentinel}
	guard := guardedDialer{
		resolver: staticResolver{netip.MustParseAddr("10.0.0.2")},
		dialer:   dialer,
		policy:   egressPolicy{allowlist: []netip.Prefix{netip.MustParsePrefix("10.0.0.0/8")}},
	}
	_, err := guard.DialContext(context.Background(), "tcp", "hooks.internal.test:443")
	if !errors.Is(err, sentinel) || dialer.calls != 1 || len(dialer.addresses) != 1 || dialer.addresses[0] != "10.0.0.2:443" {
		t.Fatalf("allowed private target err=%v dial calls=%d addresses=%v", err, dialer.calls, dialer.addresses)
	}
}

func TestGuardedDialerRevalidatesDNSOnEveryConnection(t *testing.T) {
	sentinel := errors.New("dial stopped")
	resolver := &changingResolver{answers: [][]netip.Addr{
		{netip.MustParseAddr("8.8.8.8")},
		{netip.MustParseAddr("127.0.0.1")},
	}}
	dialer := &recordingDialer{err: sentinel}
	guard := guardedDialer{resolver: resolver, dialer: dialer, policy: egressPolicy{}}
	if _, err := guard.DialContext(context.Background(), "tcp", "hooks.example.test:443"); !errors.Is(err, sentinel) {
		t.Fatalf("first dial error=%v", err)
	}
	if _, err := guard.DialContext(context.Background(), "tcp", "hooks.example.test:443"); !errors.Is(err, ErrTargetRejected) {
		t.Fatalf("rebound dial error=%v", err)
	}
	if resolver.calls != 2 || dialer.calls != 1 || len(dialer.addresses) != 1 || dialer.addresses[0] != "8.8.8.8:443" {
		t.Fatalf("resolver calls=%d dial calls=%d addresses=%v", resolver.calls, dialer.calls, dialer.addresses)
	}
}
