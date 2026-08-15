package main

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"io.astrasync/control-plane/auth/transport"
)

func TestLoadConfigKeepsDevelopmentModeExplicit(t *testing.T) {
	configuration, err := loadConfig(func(string) string { return "" })
	if err != nil {
		t.Fatalf("load development config: %v", err)
	}
	if configuration.environment != "development" || configuration.authMode != "disabled" ||
		configuration.namespace != "default" || configuration.publicOrigin != "" {
		t.Fatalf("unexpected development config: %+v", configuration)
	}
}

func TestLoadConfigEnforcesProductionOIDCAndAPITLS(t *testing.T) {
	values := map[string]string{"APP_ENV": "production"}
	if _, err := loadConfig(func(key string) string { return values[key] }); err == nil || !strings.Contains(err.Error(), "CONSOLE_AUTH_MODE") {
		t.Fatalf("expected production auth gate, got %v", err)
	}
	values = map[string]string{
		"APP_ENV": "production", "CONSOLE_AUTH_MODE": "oidc", "DATABASE_URL": "postgres://database",
		"OIDC_ISSUER": "https://issuer.example", "OIDC_AUDIENCE": "astra-api",
		"CONSOLE_OIDC_CLIENT_ID": "astra-console", "CONSOLE_PUBLIC_URL": "https://console.example",
		"CONSOLE_SESSION_KEY": strings.Repeat("k", 32),
	}
	if _, err := loadConfig(func(key string) string { return values[key] }); err == nil || !strings.Contains(err.Error(), "Console-to-API TLS") {
		t.Fatalf("expected production API TLS gate, got %v", err)
	}
	values["CONSOLE_API_TLS_CA_FILE"] = "api-ca.crt"
	values["CONSOLE_TLS_CERTIFICATE_FILE"] = "server.crt"
	values["CONSOLE_TLS_PRIVATE_KEY_FILE"] = "server.key"
	values["TRUSTED_PROXY_CIDRS"] = "10.0.0.0/8"
	if _, err := loadConfig(func(key string) string { return values[key] }); err == nil ||
		!strings.Contains(err.Error(), "CONSOLE_API_CLIENT_CERT_FILE") {
		t.Fatalf("expected production Console client certificate gate, got %v", err)
	}
	values["CONSOLE_API_CLIENT_CERT_FILE"] = "client.crt"
	if _, err := loadConfig(func(key string) string { return values[key] }); err == nil ||
		!strings.Contains(err.Error(), "must be configured together") {
		t.Fatalf("expected paired Console client certificate gate, got %v", err)
	}
	values["CONSOLE_API_CLIENT_KEY_FILE"] = "client.key"
	configuration, err := loadConfig(func(key string) string { return values[key] })
	if err != nil {
		t.Fatalf("load production config: %v", err)
	}
	if configuration.publicOrigin != "https://console.example" || len(configuration.sessionKey) != 32 || configuration.namespace != "" {
		t.Fatalf("unexpected production config: %+v", configuration)
	}
}

func TestLoadConfigEnforcesProductionTLSAndTrustedProxy(t *testing.T) {
	values := map[string]string{
		"APP_ENV": "production", "CONSOLE_AUTH_MODE": "oidc", "DATABASE_URL": "postgres://database",
		"OIDC_ISSUER": "https://issuer.example", "OIDC_AUDIENCE": "astra-api",
		"CONSOLE_OIDC_CLIENT_ID": "astra-console", "CONSOLE_PUBLIC_URL": "https://console.example",
		"CONSOLE_SESSION_KEY":     strings.Repeat("k", 32),
		"CONSOLE_API_TLS_CA_FILE": "api-ca.crt",
	}
if _, err := loadConfig(func(key string) string { return values[key] }); err == nil ||
		!strings.Contains(err.Error(), "CONSOLE_API_CLIENT_CERT_FILE") {
		t.Fatalf("expected production Console client certificate gate, got %v", err)
	}
	values["CONSOLE_API_CLIENT_CERT_FILE"] = "client.crt"
	if _, err := loadConfig(func(key string) string { return values[key] }); err == nil ||
		!strings.Contains(err.Error(), "must be configured together") {
		t.Fatalf("expected paired Console client certificate gate, got %v", err)
	}
	values["CONSOLE_API_CLIENT_KEY_FILE"] = "client.key"
	if _, err := loadConfig(func(key string) string { return values[key] }); err == nil ||
		!strings.Contains(err.Error(), "CONSOLE_TLS_CERTIFICATE_FILE") {
		t.Fatalf("expected Console TLS gate, got %v", err)
	}
	values["CONSOLE_TLS_CERTIFICATE_FILE"] = "server.crt"
	if _, err := loadConfig(func(key string) string { return values[key] }); err == nil ||
		!strings.Contains(err.Error(), "must be configured together") {
		t.Fatalf("expected paired Console TLS files gate, got %v", err)
	}
	values["CONSOLE_TLS_PRIVATE_KEY_FILE"] = "server.key"
	if _, err := loadConfig(func(key string) string { return values[key] }); err == nil ||
		!strings.Contains(err.Error(), "TRUSTED_PROXY_CIDRS") {
		t.Fatalf("expected TRUSTED_PROXY_CIDRS gate, got %v", err)
	}
	values["TRUSTED_PROXY_CIDRS"] = "10.0.0.0/8, garbage"
	if _, err := loadConfig(func(key string) string { return values[key] }); err == nil {
		t.Fatalf("expected malformed CIDR list to be rejected")
	}
	values["TRUSTED_PROXY_CIDRS"] = "10.0.0.0/8"
	if _, err := loadConfig(func(key string) string { return values[key] }); err != nil {
		t.Fatalf("expected full production config to load: %v", err)
	}
}

func TestConsoleHandlerEmitsSecurityHeadersAndHonoursTrustedProxy(t *testing.T) {
	cidrs, err := loadTrustedProxyPrefixes(config{trustedProxyCIDRs: "10.0.0.0/8"})
	if err != nil || len(cidrs) != 1 {
		t.Fatalf("parse trusted CIDRs: prefixes=%v err=%v", cidrs, err)
	}
	downstream := http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		writer.WriteHeader(http.StatusTeapot)
	})
	handler := transport.TrustedProxyMiddleware(cidrs)(transport.SecurityHeaders()(downstream))

	request := httptest.NewRequest(http.MethodGet, "https://console.example/auth/login", nil)
	request.RemoteAddr = "10.0.0.5:51234"
	request.Header.Set("X-Forwarded-For", "203.0.113.7")
	request.Header.Set("X-Forwarded-Proto", "https")
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusTeapot {
		t.Fatalf("unexpected status: %d", recorder.Code)
	}
	if got := recorder.Header().Get(transport.HeaderXContentTypeOptions); got != transport.ValueNoSniff {
		t.Fatalf("unexpected X-Content-Type-Options: %q", got)
	}
	if got := recorder.Header().Get(transport.HeaderReferrerPolicy); got != transport.ValueReferrerPolicy {
		t.Fatalf("unexpected Referrer-Policy: %q", got)
	}
	if got := recorder.Header().Get(transport.HeaderStrictTransportSecurity); got != transport.ValueStrictTransportSecurity {
		t.Fatalf("expected HSTS via trusted proxy, got %q", got)
	}

	request2 := httptest.NewRequest(http.MethodGet, "http://console.example/auth/login", nil)
	request2.RemoteAddr = "203.0.113.42:51234"
	request2.Header.Set("X-Forwarded-Proto", "https")
	recorder2 := httptest.NewRecorder()
	handler.ServeHTTP(recorder2, request2)
	if got := recorder2.Header().Get(transport.HeaderStrictTransportSecurity); got != "" {
		t.Fatalf("expected no HSTS from untrusted peer, got %q", got)
	}
}

func TestLoadTrustedProxyPrefixesConsole(t *testing.T) {
	if prefixes, err := loadTrustedProxyPrefixes(config{}); err != nil || prefixes != nil {
		t.Fatalf("expected nil prefixes without configuration: prefixes=%v err=%v", prefixes, err)
	}
	configuration := config{trustedProxyCIDRs: "10.0.0.0/8, 192.168.0.0/16"}
	prefixes, err := loadTrustedProxyPrefixes(configuration)
	if err != nil || len(prefixes) != 2 {
		t.Fatalf("parse configured prefixes: prefixes=%v err=%v", prefixes, err)
	}
	if _, err := loadTrustedProxyPrefixes(config{trustedProxyCIDRs: "garbage"}); err == nil {
		t.Fatal("expected invalid prefix to be rejected")
	}
}

func TestLoadConfigRejectsUnsafeOIDCInputs(t *testing.T) {
	base := map[string]string{
		"CONSOLE_AUTH_MODE": "oidc", "DATABASE_URL": "postgres://database",
		"OIDC_ISSUER": "https://issuer.example", "OIDC_AUDIENCE": "astra-api",
		"CONSOLE_OIDC_CLIENT_ID": "astra-console", "CONSOLE_PUBLIC_URL": "https://console.example/path",
		"CONSOLE_SESSION_KEY": strings.Repeat("k", 32),
	}
	if _, err := loadConfig(func(key string) string { return base[key] }); err == nil || !strings.Contains(err.Error(), "HTTPS origin") {
		t.Fatalf("expected public-origin rejection, got %v", err)
	}
	base["CONSOLE_PUBLIC_URL"] = "https://console.example"
	base["CONSOLE_SESSION_KEY"] = "short"
	if _, err := loadConfig(func(key string) string { return base[key] }); err == nil || !strings.Contains(err.Error(), "32 bytes") {
		t.Fatalf("expected short session key rejection, got %v", err)
	}
}
