package transport

import (
	"crypto/tls"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"testing"
)

func TestSecurityHeadersAlwaysSetsXContentTypeOptionsAndReferrerPolicy(t *testing.T) {
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "http://api.example.com/health", nil)
	SecurityHeaders()(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		writer.WriteHeader(http.StatusOK)
	})).ServeHTTP(recorder, request)

	if got := recorder.Header().Get(HeaderXContentTypeOptions); got != ValueNoSniff {
		t.Fatalf("unexpected X-Content-Type-Options: %q", got)
	}
	if got := recorder.Header().Get(HeaderReferrerPolicy); got != ValueReferrerPolicy {
		t.Fatalf("unexpected Referrer-Policy: %q", got)
	}
}

func TestSecurityHeadersSkipsHSTSOnPlaintext(t *testing.T) {
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "http://api.example.com/health", nil)
	SecurityHeaders()(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {})).ServeHTTP(recorder, request)
	if got := recorder.Header().Get(HeaderStrictTransportSecurity); got != "" {
		t.Fatalf("expected no HSTS on plaintext, got %q", got)
	}
}

func TestSecurityHeadersSetsHSTSWhenTLSRequested(t *testing.T) {
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "https://api.example.com/health", nil)
	request.TLS = &tls.ConnectionState{}
	SecurityHeaders()(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {})).ServeHTTP(recorder, request)
	if got := recorder.Header().Get(HeaderStrictTransportSecurity); got != ValueStrictTransportSecurity {
		t.Fatalf("expected HSTS, got %q", got)
	}
}

func TestSecurityHeadersSetsHSTSWhenTrustedProxyReportedHTTPS(t *testing.T) {
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "http://api.example.com/health", nil)
	request = request.WithContext(WithClientAddress(request.Context(), ClientAddress{
		IP:     netip.MustParseAddr("203.0.113.7"),
		Scheme: "https", Trusted: true,
	}))
	SecurityHeaders()(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {})).ServeHTTP(recorder, request)
	if got := recorder.Header().Get(HeaderStrictTransportSecurity); got != ValueStrictTransportSecurity {
		t.Fatalf("expected HSTS via trusted proxy, got %q", got)
	}
}

func TestSecurityHeadersPreservesUpstreamValues(t *testing.T) {
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "https://api.example.com/health", nil)
	request.TLS = &tls.ConnectionState{}
	handler := SecurityHeaders()(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		writer.Header().Set(HeaderStrictTransportSecurity, "max-age=60")
		writer.WriteHeader(http.StatusOK)
	}))
	handler.ServeHTTP(recorder, request)
	if got := recorder.Header().Get(HeaderStrictTransportSecurity); got != "max-age=60" {
		t.Fatalf("expected upstream HSTS to win, got %q", got)
	}
}