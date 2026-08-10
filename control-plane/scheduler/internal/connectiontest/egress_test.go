package connectiontest

import (
	"context"
	"errors"
	"net/netip"
	"strings"
	"testing"
	"time"

	"io.astrasync/control-plane/connection"
)

type staticResolver struct {
	addresses []netip.Addr
	err       error
	host      string
}

func (r *staticResolver) Lookup(_ context.Context, host string) ([]netip.Addr, error) {
	r.host = host
	return append([]netip.Addr(nil), r.addresses...), r.err
}

func TestEgressGuardPinsPublicResolution(t *testing.T) {
	t.Parallel()
	resolver := &staticResolver{addresses: []netip.Addr{
		netip.MustParseAddr("203.0.113.20"), netip.MustParseAddr("203.0.113.20"),
		netip.MustParseAddr("2001:db8::20"),
	}}
	guard := newTestGuard(t, resolver)
	endpoint, err := guard.Resolve(
		context.Background(), "db.example.test.", 5432, connection.DefaultTestEgressPolicy(),
	)
	if err != nil {
		t.Fatalf("resolve public destination: %v", err)
	}
	if resolver.host != "db.example.test" || endpoint.ServerName() != "db.example.test" ||
		len(endpoint.addresses) != 2 {
		t.Fatalf("resolution was not normalized and pinned: %+v", endpoint)
	}
	if _, err := endpoint.DialContext(context.Background(), "udp", "db.example.test:5432"); !errors.Is(err, ErrPolicyDenied) {
		t.Fatalf("non-TCP dial was not denied: %v", err)
	}
	if _, err := endpoint.DialContext(context.Background(), "tcp", "db.example.test:5433"); !errors.Is(err, ErrPolicyDenied) {
		t.Fatalf("port change was not denied: %v", err)
	}
}

func TestEgressGuardRequiresTenantPolicyForPrivateDestinations(t *testing.T) {
	t.Parallel()
	resolver := &staticResolver{addresses: []netip.Addr{netip.MustParseAddr("10.23.4.5")}}
	guard := newTestGuard(t, resolver)
	if _, err := guard.Resolve(
		context.Background(), "db.internal", 5432, connection.DefaultTestEgressPolicy(),
	); !errors.Is(err, ErrPolicyDenied) {
		t.Fatalf("private destination was not denied by default: %v", err)
	}
	policy, err := connection.NewTestEgressPolicy([]string{"10.23.0.0/16"})
	if err != nil {
		t.Fatalf("construct tenant policy: %v", err)
	}
	if _, err := guard.Resolve(context.Background(), "db.internal", 5432, policy); err != nil {
		t.Fatalf("explicitly allowed private destination was denied: %v", err)
	}
}

func TestEgressGuardAlwaysDeniesMetadataLoopbackAndMixedAnswers(t *testing.T) {
	t.Parallel()
	policy, err := connection.NewTestEgressPolicy([]string{"0.0.0.0/0", "::/0"})
	if err != nil {
		t.Fatalf("construct broad policy: %v", err)
	}
	for _, address := range []string{
		"127.0.0.1", "169.254.169.254", "100.100.100.200", "::1", "fd00:ec2::254",
	} {
		guard := newTestGuard(t, &staticResolver{addresses: []netip.Addr{netip.MustParseAddr(address)}})
		if _, err := guard.Resolve(context.Background(), "blocked.example", 80, policy); !errors.Is(err, ErrPolicyDenied) {
			t.Fatalf("hard-denied address %s was allowed: %v", address, err)
		}
	}
	mixed := newTestGuard(t, &staticResolver{addresses: []netip.Addr{
		netip.MustParseAddr("203.0.113.10"), netip.MustParseAddr("10.0.0.10"),
	}})
	if _, err := mixed.Resolve(
		context.Background(), "mixed.example", 443, connection.DefaultTestEgressPolicy(),
	); !errors.Is(err, ErrPolicyDenied) {
		t.Fatalf("mixed public/private DNS answer was not denied: %v", err)
	}
}

func TestEgressGuardBoundsAndSanitizesDNSFailures(t *testing.T) {
	t.Parallel()
	tooMany := make([]netip.Addr, 9)
	for index := range tooMany {
		tooMany[index] = netip.MustParseAddr("203.0.113." + string(rune('1'+index)))
	}
	guard := newTestGuard(t, &staticResolver{addresses: tooMany})
	if _, err := guard.Resolve(
		context.Background(), "large.example", 443, connection.DefaultTestEgressPolicy(),
	); !errors.Is(err, ErrDNSFailed) {
		t.Fatalf("oversized DNS answer was not rejected: %v", err)
	}
	sentinel := "vendor-password-sentinel"
	guard = newTestGuard(t, &staticResolver{err: errors.New(sentinel)})
	_, err := guard.Resolve(context.Background(), "failed.example", 443, connection.DefaultTestEgressPolicy())
	if !errors.Is(err, ErrDNSFailed) || strings.Contains(err.Error(), sentinel) {
		t.Fatalf("DNS failure crossed redaction boundary: %v", err)
	}
}

func newTestGuard(t *testing.T, resolver DNSResolver) *EgressGuard {
	t.Helper()
	guard, err := NewEgressGuard(resolver, []string{"10.96.0.0/12"}, 8, time.Second)
	if err != nil {
		t.Fatalf("construct egress guard: %v", err)
	}
	return guard
}
