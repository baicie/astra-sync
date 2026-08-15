// Package transport provides shared HTTP transport hardening primitives used by
// the API Server and the Console BFF.
//
// The package encodes the decisions recorded in ADR-043 and the Slice 22 design
// document: a strict, deployment-declared trusted-proxy boundary that governs when
// X-Forwarded-For, X-Forwarded-Proto, and X-Forwarded-Host may be trusted, and a
// small set of security response headers that every public listener must emit.
package transport

import (
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/netip"
	"strings"
)

// ClientAddress is the result of inspecting a single HTTP request. It records the
// observed client, the scheme and host the request arrived over (from the wire or
// from a trusted proxy), and whether any forwarded header was trusted to override
// the immediate peer.
type ClientAddress struct {
	IP      netip.Addr
	Port    uint16
	Scheme  string
	Host    string
	Trusted bool
	// Forward is the raw X-Forwarded-For value the helper consumed. It is set only
	// when Trusted is true and the forwarded chain was well-formed.
	Forward string
}

// ParseCIDRList parses a comma-separated list of CIDR ranges. The result is
// always non-empty when err is nil; an empty input list is rejected because the
// deployment has chosen to declare the boundary.
func ParseCIDRList(raw string) ([]netip.Prefix, error) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return nil, errors.New("trusted proxy CIDR list must not be empty")
	}
	parts := strings.Split(trimmed, ",")
	prefixes := make([]netip.Prefix, 0, len(parts))
	for _, part := range parts {
		entry := strings.TrimSpace(part)
		if entry == "" {
			continue
		}
		prefix, err := netip.ParsePrefix(entry)
		if err != nil {
			return nil, fmt.Errorf("trusted proxy CIDR %q is invalid: %w", entry, err)
		}
		if !prefix.IsValid() {
			return nil, fmt.Errorf("trusted proxy CIDR %q is invalid", entry)
		}
		prefixes = append(prefixes, prefix.Masked())
	}
	if len(prefixes) == 0 {
		return nil, errors.New("trusted proxy CIDR list must contain at least one entry")
	}
	return prefixes, nil
}

// trustedMaxForwardedHops caps the length of an X-Forwarded-For chain the helper
// will consume. Anything beyond that is treated as untrusted. Sixteen hops matches
// the most generous real-world ingress chain while still bounding the cost of the
// scan.
const trustedMaxForwardedHops = 16

// TrustedProxy inspects a request and returns the client address, scheme, and host
// the rest of the application should observe. When the immediate peer lies within
// one of the trusted prefixes the left-most valid IP from X-Forwarded-For, the
// value of X-Forwarded-Proto, and the value of X-Forwarded-Host are consumed. When
// the peer is untrusted, the helper returns the locally observed values and marks
// the request as untrusted so the audit writer can suppress the forwarded field.
func TrustedProxy(request *http.Request, trusted []netip.Prefix) ClientAddress {
	if request == nil {
		return ClientAddress{Scheme: "http", Host: "", Trusted: false}
	}
	peerAddr, peerPort := parsePeer(request.RemoteAddr)
	address := ClientAddress{
		IP:     peerAddr,
		Port:   peerPort,
		Scheme: schemeFromRequest(request),
		Host:   request.Host,
		Trusted: false,
	}
	if peerAddr.IsValid() && len(trusted) > 0 {
		if peerInTrustedPrefix(peerAddr, trusted) {
			address.Trusted = true
			if forwarded, ok := consumeForwardedFor(request.Header.Get("X-Forwarded-For")); ok {
				if forwardedAddr, forwardedPort, ok := parseIPPort(forwarded); ok {
					address.IP = forwardedAddr
					address.Port = forwardedPort
					address.Forward = forwarded
				}
			}
			if scheme := strings.TrimSpace(request.Header.Get("X-Forwarded-Proto")); scheme != "" {
				address.Scheme = strings.ToLower(scheme)
			}
			if host := strings.TrimSpace(request.Header.Get("X-Forwarded-Host")); host != "" {
				address.Host = host
			}
		}
	}
	return address
}

// TrustedProxyMiddleware installs TrustedProxy on the request context and exposes
// the resolved ClientAddress via WithClientAddress. The middleware never logs the
// forwarded chain and never short-circuits the handler chain.
func TrustedProxyMiddleware(trusted []netip.Prefix) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
			address := TrustedProxy(request, trusted)
			ctx := WithClientAddress(request.Context(), address)
			next.ServeHTTP(writer, request.WithContext(ctx))
		})
	}
}

func peerInTrustedPrefix(addr netip.Addr, trusted []netip.Prefix) bool {
	if !addr.IsValid() {
		return false
	}
	for _, prefix := range trusted {
		if prefix.Contains(addr) {
			return true
		}
	}
	return false
}

func consumeForwardedFor(raw string) (string, bool) {
	if raw == "" {
		return "", false
	}
	entries := strings.Split(raw, ",")
	valid := make([]string, 0, len(entries))
	for _, entry := range entries {
		trimmed := strings.TrimSpace(entry)
		if trimmed == "" {
			continue
		}
		valid = append(valid, trimmed)
	}
	if len(valid) == 0 {
		return "", false
	}
	if len(valid) > trustedMaxForwardedHops {
		// A chain longer than the documented cap is treated as malformed. We
		// deliberately ignore the entire chain rather than silently consume the
		// left-most entry, because an over-long chain is a strong signal of
		// header injection or a misconfigured multi-hop topology.
		return "", false
	}
	return valid[0], true
}

func parsePeer(remoteAddr string) (netip.Addr, uint16) {
	if remoteAddr == "" {
		return netip.Addr{}, 0
	}
	host, port, err := net.SplitHostPort(remoteAddr)
	if err != nil {
		if addr, err := netip.ParseAddr(remoteAddr); err == nil {
			return addr, 0
		}
		return netip.Addr{}, 0
	}
	addr, err := netip.ParseAddr(host)
	if err != nil {
		return netip.Addr{}, 0
	}
	var portNum uint16
	if p, err := net.LookupPort("tcp", port); err == nil {
		portNum = uint16(p)
	}
	return addr, portNum
}

func parseIPPort(raw string) (netip.Addr, uint16, bool) {
	host, port, err := net.SplitHostPort(raw)
	if err != nil {
		if addr, err := netip.ParseAddr(raw); err == nil {
			return addr, 0, true
		}
		return netip.Addr{}, 0, false
	}
	addr, err := netip.ParseAddr(host)
	if err != nil {
		return netip.Addr{}, 0, false
	}
	var portNum uint16
	if p, err := net.LookupPort("tcp", port); err == nil {
		portNum = uint16(p)
	}
	return addr, portNum, true
}

func schemeFromRequest(request *http.Request) string {
	if request.TLS != nil {
		return "https"
	}
	return "http"
}