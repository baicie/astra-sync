package main

import (
	"strings"
	"testing"
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
	configuration, err := loadConfig(func(key string) string { return values[key] })
	if err != nil {
		t.Fatalf("load production config: %v", err)
	}
	if configuration.publicOrigin != "https://console.example" || len(configuration.sessionKey) != 32 || configuration.namespace != "" {
		t.Fatalf("unexpected production config: %+v", configuration)
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
