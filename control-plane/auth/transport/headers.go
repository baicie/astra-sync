package transport

import (
	"context"
	"net/http"
	"strings"
)

// Response header values emitted by SecurityHeaders. They are constants so tests
// and downstream callers can assert on the exact bytes the middleware writes.
const (
	HeaderStrictTransportSecurity = "Strict-Transport-Security"
	HeaderXContentTypeOptions     = "X-Content-Type-Options"
	HeaderReferrerPolicy          = "Referrer-Policy"
	ValueNoSniff                  = "nosniff"
	ValueReferrerPolicy           = "strict-origin-when-cross-origin"
	ValueStrictTransportSecurity  = "max-age=63072000; includeSubDomains"
)

// clientAddressKey is the unexported context key under which TrustedProxyMiddleware
// stores the resolved ClientAddress. Use ClientAddressFromContext to retrieve it.
type clientAddressKey struct{}

// WithClientAddress returns a copy of parent that carries addr.
func WithClientAddress(parent context.Context, addr ClientAddress) context.Context {
	return context.WithValue(parent, clientAddressKey{}, addr)
}

// ClientAddressFromContext returns the ClientAddress installed by
// TrustedProxyMiddleware, if any.
func ClientAddressFromContext(ctx context.Context) (ClientAddress, bool) {
	if ctx == nil {
		return ClientAddress{}, false
	}
	value, ok := ctx.Value(clientAddressKey{}).(ClientAddress)
	if !ok {
		return ClientAddress{}, false
	}
	return value, true
}

// SecurityHeaders returns an HTTP middleware that emits the security response
// headers documented by ADR-043. Strict-Transport-Security is only set when the
// request was received over TLS or when the trusted-proxy helper reports that the
// peer claimed https. The middleware is always safe to install; the strict
// transport security header simply has no effect on plaintext responses.
//
// The middleware is idempotent: an upstream that already set one of the headers
// is honoured, which avoids surprising proxies that fold response headers into
// their own state.
func SecurityHeaders() func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
			if writer.Header().Get(HeaderXContentTypeOptions) == "" {
				writer.Header().Set(HeaderXContentTypeOptions, ValueNoSniff)
			}
			if writer.Header().Get(HeaderReferrerPolicy) == "" {
				writer.Header().Set(HeaderReferrerPolicy, ValueReferrerPolicy)
			}
			if writer.Header().Get(HeaderStrictTransportSecurity) == "" && requestWarrantsHSTS(request) {
				writer.Header().Set(HeaderStrictTransportSecurity, ValueStrictTransportSecurity)
			}
			next.ServeHTTP(writer, request)
		})
	}
}

func requestWarrantsHSTS(request *http.Request) bool {
	if request == nil {
		return false
	}
	if request.TLS != nil {
		return true
	}
	if addr, ok := ClientAddressFromContext(request.Context()); ok {
		return strings.EqualFold(addr.Scheme, "https")
	}
	return false
}
