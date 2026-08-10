package connectiontest

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/netip"
	"regexp"
	"sort"
	"strings"
	"time"

	"io.astrasync/control-plane/connection"
)

const (
	defaultMaximumDNSAddresses = 8
	maximumConnectionAttempts  = 2
)

var (
	ErrPolicyDenied = errors.New("Connection test egress policy denied destination")
	ErrDNSFailed    = errors.New("Connection test DNS resolution failed")
	ErrDialFailed   = errors.New("Connection test transport failed")
)

var dnsLabel = regexp.MustCompile(`^[A-Za-z0-9](?:[-A-Za-z0-9]{0,61}[A-Za-z0-9])?$`)

var hardDeniedPrefixes = mustPrefixes(
	"0.0.0.0/8",
	"127.0.0.0/8",
	"169.254.0.0/16",
	"224.0.0.0/4",
	"240.0.0.0/4",
	"::/128",
	"::1/128",
	"fe80::/10",
	"ff00::/8",
	"100.100.100.200/32",
	"fd00:ec2::254/128",
)

var restrictedPrefixes = mustPrefixes(
	"100.64.0.0/10",
	"192.0.0.0/24",
	"198.18.0.0/15",
	"fc00::/7",
)

type DNSResolver interface {
	Lookup(context.Context, string) ([]netip.Addr, error)
}

type SystemDNSResolver struct {
	resolver *net.Resolver
}

func NewSystemDNSResolver(resolver *net.Resolver) (*SystemDNSResolver, error) {
	if resolver == nil {
		return nil, fmt.Errorf("Connection test DNS resolver must not be nil")
	}
	return &SystemDNSResolver{resolver: resolver}, nil
}

func (r *SystemDNSResolver) Lookup(ctx context.Context, host string) ([]netip.Addr, error) {
	values, err := r.resolver.LookupNetIP(ctx, "ip", host)
	if err != nil {
		return nil, ErrDNSFailed
	}
	result := make([]netip.Addr, 0, len(values))
	for _, value := range values {
		if !value.IsValid() {
			return nil, ErrDNSFailed
		}
		result = append(result, value.Unmap())
	}
	return result, nil
}

type EgressGuard struct {
	resolver            DNSResolver
	protectedCIDRs      []netip.Prefix
	maximumDNSAddresses int
	dialTimeout         time.Duration
	maximumDialAttempts int
}

func NewEgressGuard(
	resolver DNSResolver,
	protectedCIDRs []string,
	maximumDNSAddresses int,
	dialTimeout time.Duration,
) (*EgressGuard, error) {
	if resolver == nil || dialTimeout <= 0 || dialTimeout > 30*time.Second {
		return nil, fmt.Errorf("Connection test egress guard dependencies are invalid")
	}
	if maximumDNSAddresses == 0 {
		maximumDNSAddresses = defaultMaximumDNSAddresses
	}
	if maximumDNSAddresses < 1 || maximumDNSAddresses > 32 {
		return nil, fmt.Errorf("Connection test DNS address bound is invalid")
	}
	protected := make([]netip.Prefix, 0, len(protectedCIDRs))
	for _, value := range protectedCIDRs {
		prefix, err := netip.ParsePrefix(strings.TrimSpace(value))
		if err != nil {
			return nil, fmt.Errorf("Connection test protected CIDR is invalid")
		}
		protected = append(protected, prefix.Masked())
	}
	return &EgressGuard{
		resolver: resolver, protectedCIDRs: protected,
		maximumDNSAddresses: maximumDNSAddresses, dialTimeout: dialTimeout,
		maximumDialAttempts: maximumConnectionAttempts,
	}, nil
}

type PinnedEndpoint struct {
	host        string
	port        uint16
	addresses   []netip.Addr
	dialTimeout time.Duration
	maxAttempts int
}

func (g *EgressGuard) Resolve(
	ctx context.Context, host string, port uint16, policy connection.TestEgressPolicy,
) (PinnedEndpoint, error) {
	if g == nil || g.resolver == nil || port == 0 || policy.Validate() != nil {
		return PinnedEndpoint{}, ErrPolicyDenied
	}
	host = strings.TrimSuffix(strings.TrimSpace(host), ".")
	if !validHost(host) {
		return PinnedEndpoint{}, ErrPolicyDenied
	}
	addresses := make([]netip.Addr, 0, 1)
	if literal, err := netip.ParseAddr(strings.Trim(host, "[]")); err == nil {
		addresses = append(addresses, literal.Unmap())
	} else {
		resolved, err := g.resolver.Lookup(ctx, host)
		if err != nil || len(resolved) == 0 || len(resolved) > g.maximumDNSAddresses {
			return PinnedEndpoint{}, ErrDNSFailed
		}
		addresses = append(addresses, resolved...)
	}
	addresses = canonicalAddresses(addresses)
	if len(addresses) == 0 || len(addresses) > g.maximumDNSAddresses {
		return PinnedEndpoint{}, ErrDNSFailed
	}
	allowedCIDRs := make([]netip.Prefix, 0, len(policy.AllowedCIDRs))
	for _, value := range policy.AllowedCIDRs {
		prefix, err := netip.ParsePrefix(value)
		if err != nil {
			return PinnedEndpoint{}, ErrPolicyDenied
		}
		allowedCIDRs = append(allowedCIDRs, prefix.Masked())
	}
	for _, address := range addresses {
		if hardDenied(address) || requiresExplicitAllow(address, g.protectedCIDRs) &&
			!prefixesContain(allowedCIDRs, address) {
			return PinnedEndpoint{}, ErrPolicyDenied
		}
	}
	return PinnedEndpoint{
		host: host, port: port, addresses: addresses,
		dialTimeout: g.dialTimeout, maxAttempts: g.maximumDialAttempts,
	}, nil
}

func (e PinnedEndpoint) ServerName() string { return e.host }

func (e PinnedEndpoint) DialContext(ctx context.Context, network, address string) (net.Conn, error) {
	if network != "tcp" && network != "tcp4" && network != "tcp6" {
		return nil, ErrPolicyDenied
	}
	_, requestedPort, err := net.SplitHostPort(address)
	if err != nil || requestedPort != fmt.Sprintf("%d", e.port) || len(e.addresses) == 0 {
		return nil, ErrPolicyDenied
	}
	attempts := e.maxAttempts
	if attempts > len(e.addresses) {
		attempts = len(e.addresses)
	}
	for index := 0; index < attempts; index++ {
		address := e.addresses[index]
		if network == "tcp4" && !address.Is4() || network == "tcp6" && !address.Is6() {
			continue
		}
		dialer := net.Dialer{Timeout: e.dialTimeout, KeepAlive: -1}
		connectionValue, dialErr := dialer.DialContext(
			ctx, network, net.JoinHostPort(address.String(), requestedPort),
		)
		if dialErr == nil {
			return connectionValue, nil
		}
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
	}
	return nil, ErrDialFailed
}

func validHost(host string) bool {
	if len(host) == 0 || len(host) > 253 || strings.ContainsAny(host, "\x00\r\n\t /\\%") {
		return false
	}
	if _, err := netip.ParseAddr(strings.Trim(host, "[]")); err == nil {
		return true
	}
	for _, label := range strings.Split(host, ".") {
		if !dnsLabel.MatchString(label) {
			return false
		}
	}
	return true
}

func hardDenied(address netip.Addr) bool {
	address = address.Unmap()
	if !address.IsValid() || address.IsUnspecified() || address.IsLoopback() ||
		address.IsMulticast() || address.IsLinkLocalUnicast() || address.IsLinkLocalMulticast() {
		return true
	}
	return prefixesContain(hardDeniedPrefixes, address)
}

func requiresExplicitAllow(address netip.Addr, protected []netip.Prefix) bool {
	address = address.Unmap()
	return address.IsPrivate() || prefixesContain(restrictedPrefixes, address) ||
		prefixesContain(protected, address)
}

func prefixesContain(prefixes []netip.Prefix, address netip.Addr) bool {
	for _, prefix := range prefixes {
		if prefix.Contains(address) {
			return true
		}
	}
	return false
}

func canonicalAddresses(values []netip.Addr) []netip.Addr {
	seen := make(map[netip.Addr]struct{}, len(values))
	result := make([]netip.Addr, 0, len(values))
	for _, value := range values {
		value = value.Unmap()
		if !value.IsValid() {
			continue
		}
		if _, duplicate := seen[value]; duplicate {
			continue
		}
		seen[value] = struct{}{}
		result = append(result, value)
	}
	sort.Slice(result, func(left, right int) bool { return result[left].Less(result[right]) })
	return result
}

func mustPrefixes(values ...string) []netip.Prefix {
	result := make([]netip.Prefix, len(values))
	for index, value := range values {
		result[index] = netip.MustParsePrefix(value)
	}
	return result
}
