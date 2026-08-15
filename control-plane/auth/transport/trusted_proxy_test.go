package transport

import (
	"crypto/tls"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"strings"
	"testing"
)

func mustParsePrefix(t *testing.T, raw string) netip.Prefix {
	t.Helper()
	prefix, err := netip.ParsePrefix(raw)
	if err != nil {
		t.Fatalf("parse prefix %q: %v", raw, err)
	}
	return prefix.Masked()
}

func TestParseCIDRListAcceptsValidEntries(t *testing.T) {
	prefixes, err := ParseCIDRList("10.0.0.0/8, 192.168.0.0/16, ::1/128")
	if err != nil {
		t.Fatalf("parse CIDR list: %v", err)
	}
	if len(prefixes) != 3 {
		t.Fatalf("unexpected prefix count: %d", len(prefixes))
	}
}

func TestParseCIDRListRejectsEmpty(t *testing.T) {
	if _, err := ParseCIDRList(""); err == nil {
		t.Fatalf("expected empty list to be rejected")
	}
	if _, err := ParseCIDRList(" , , "); err == nil {
		t.Fatalf("expected whitespace-only list to be rejected")
	}
}

func TestParseCIDRListRejectsInvalidEntries(t *testing.T) {
	if _, err := ParseCIDRList("not-a-cidr"); err == nil {
		t.Fatalf("expected invalid entry to be rejected")
	}
	if _, err := ParseCIDRList("10.0.0.0/8, garbage"); err == nil {
		t.Fatalf("expected mixed list with garbage to be rejected")
	}
}

func TestTrustedProxyConsumesForwardedHeadersFromTrustedPeer(t *testing.T) {
	trusted := []netip.Prefix{mustParsePrefix(t, "10.0.0.0/8")}
	request := httptest.NewRequest(http.MethodGet, "http://api.example.com/health", nil)
	request.RemoteAddr = "10.0.0.5:51234"
	request.Header.Set("X-Forwarded-For", "203.0.113.7, 10.0.0.5")
	request.Header.Set("X-Forwarded-Proto", "https")
	request.Header.Set("X-Forwarded-Host", "console.example.com")

	address := TrustedProxy(request, trusted)
	if !address.Trusted {
		t.Fatalf("expected trusted marker")
	}
	if address.IP.String() != "203.0.113.7" {
		t.Fatalf("unexpected IP: %s", address.IP.String())
	}
	if address.Scheme != "https" {
		t.Fatalf("unexpected scheme: %s", address.Scheme)
	}
	if address.Host != "console.example.com" {
		t.Fatalf("unexpected host: %s", address.Host)
	}
	if address.Forward != "203.0.113.7" {
		t.Fatalf("unexpected forward: %s", address.Forward)
	}
}

func TestTrustedProxyIgnoresForwardedHeadersFromUntrustedPeer(t *testing.T) {
	trusted := []netip.Prefix{mustParsePrefix(t, "10.0.0.0/8")}
	request := httptest.NewRequest(http.MethodGet, "http://api.example.com/health", nil)
	request.RemoteAddr = "203.0.113.42:51234"
	request.Header.Set("X-Forwarded-For", "127.0.0.1")
	request.Header.Set("X-Forwarded-Proto", "https")
	request.Header.Set("X-Forwarded-Host", "evil.example.com")

	address := TrustedProxy(request, trusted)
	if address.Trusted {
		t.Fatalf("expected untrusted marker")
	}
	if address.IP.String() != "203.0.113.42" {
		t.Fatalf("unexpected IP: %s", address.IP.String())
	}
	if address.Scheme != "http" {
		t.Fatalf("unexpected scheme: %s", address.Scheme)
	}
	if address.Host != "api.example.com" {
		t.Fatalf("unexpected host: %s", address.Host)
	}
}

func TestTrustedProxyWithEmptyTrustedListTreatsAllPeersAsUntrusted(t *testing.T) {
	request := httptest.NewRequest(http.MethodGet, "http://api.example.com/health", nil)
	request.RemoteAddr = "10.0.0.5:51234"
	request.Header.Set("X-Forwarded-For", "203.0.113.7")

	address := TrustedProxy(request, nil)
	if address.Trusted {
		t.Fatalf("expected untrusted marker when no prefix is configured")
	}
	if address.IP.String() != "10.0.0.5" {
		t.Fatalf("unexpected IP: %s", address.IP.String())
	}
}

func TestTrustedProxyRejectsMalformedForwardedChain(t *testing.T) {
	trusted := []netip.Prefix{mustParsePrefix(t, "10.0.0.0/8")}
	request := httptest.NewRequest(http.MethodGet, "http://api.example.com/health", nil)
	request.RemoteAddr = "10.0.0.5:51234"
	request.Header.Set("X-Forwarded-For", "not-an-ip, 203.0.113.7")

	address := TrustedProxy(request, trusted)
	if !address.Trusted {
		t.Fatalf("expected trusted marker")
	}
	if address.Forward != "" {
		t.Fatalf("expected malformed forward to be ignored, got %q", address.Forward)
	}
	if address.IP.String() != "10.0.0.5" {
		t.Fatalf("expected immediate peer, got %s", address.IP.String())
	}
}

func TestTrustedProxyCapsForwardedChainLength(t *testing.T) {
	trusted := []netip.Prefix{mustParsePrefix(t, "10.0.0.0/8")}
	request := httptest.NewRequest(http.MethodGet, "http://api.example.com/health", nil)
	request.RemoteAddr = "10.0.0.5:51234"
	entries := make([]string, 0, trustedMaxForwardedHops+2)
	for i := 0; i < trustedMaxForwardedHops+2; i++ {
		entries = append(entries, "203.0.113."+itoa(i%250+1))
	}
	request.Header.Set("X-Forwarded-For", strings.Join(entries, ", "))

	address := TrustedProxy(request, trusted)
	if address.Forward != "" {
		t.Fatalf("expected forwarded chain to be capped, got %q", address.Forward)
	}
}

func TestTrustedProxyMiddlewareStoresAddressInContext(t *testing.T) {
	trusted := []netip.Prefix{mustParsePrefix(t, "10.0.0.0/8")}
	called := false
	handler := TrustedProxyMiddleware(trusted)(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		called = true
		addr, ok := ClientAddressFromContext(request.Context())
		if !ok {
			t.Fatalf("expected client address in context")
		}
		if !addr.Trusted {
			t.Fatalf("expected trusted marker")
		}
	}))
	request := httptest.NewRequest(http.MethodGet, "http://api.example.com/health", nil)
	request.RemoteAddr = "10.0.0.5:51234"
	handler.ServeHTTP(httptest.NewRecorder(), request)
	if !called {
		t.Fatalf("expected handler to be called")
	}
}

func TestTrustedProxyRecognisesTLSRequest(t *testing.T) {
	request := httptest.NewRequest(http.MethodGet, "https://api.example.com/health", nil)
	request.RemoteAddr = "10.0.0.5:51234"
	request.TLS = &tls.ConnectionState{}
	if schemeFromRequest(request) != "https" {
		t.Fatalf("expected scheme https")
	}
}

func itoa(i int) string {
	if i == 0 {
		return "0"
	}
	digits := make([]byte, 0, 4)
	for i > 0 {
		digits = append([]byte{byte('0' + i%10)}, digits...)
		i /= 10
	}
	return string(digits)
}
