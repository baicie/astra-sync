package auth_test

import (
	"context"
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"math/big"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"io.astrasync/control-plane/auth"
)

func TestOIDCValidatorValidatesIssuerAudienceLifetimeSignatureAndBounds(t *testing.T) {
	privateKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("generate RSA key: %v", err)
	}
	now := time.Date(2026, 8, 8, 8, 0, 0, 0, time.UTC)
	var issuer string
	server := httptest.NewTLSServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		response.Header().Set("Content-Type", "application/json")
		switch request.URL.Path {
		case "/.well-known/openid-configuration":
			_ = json.NewEncoder(response).Encode(map[string]string{
				"issuer": issuer, "jwks_uri": issuer + "/keys",
			})
		case "/keys":
			_ = json.NewEncoder(response).Encode(map[string]any{"keys": []map[string]string{{
				"kty": "RSA", "kid": "key-1", "alg": "RS256", "use": "sig",
				"n": base64.RawURLEncoding.EncodeToString(privateKey.PublicKey.N.Bytes()),
				"e": base64.RawURLEncoding.EncodeToString(big.NewInt(int64(privateKey.PublicKey.E)).Bytes()),
			}}})
		default:
			http.NotFound(response, request)
		}
	}))
	defer server.Close()
	issuer = server.URL

	validator, err := auth.NewOIDCValidator(auth.OIDCConfig{
		Issuer:             issuer,
		Audience:           "astra-api",
		HTTPClient:         server.Client(),
		Clock:              func() time.Time { return now },
		AcceptedTokenTypes: []string{"at+jwt"},
	})
	if err != nil {
		t.Fatalf("create OIDC validator: %v", err)
	}
	valid := signedToken(t, privateKey, "key-1", map[string]any{
		"iss": issuer, "sub": "operator-1", "aud": []string{"other", "astra-api"},
		"iat": now.Add(-time.Minute).Unix(), "exp": now.Add(time.Hour).Unix(), "nonce": "nonce-1",
	})
	identity, err := validator.Validate(context.Background(), valid)
	if err != nil || identity.Issuer != issuer || identity.Subject != "operator-1" {
		t.Fatalf("validate token: identity=%+v err=%v", identity, err)
	}
	validated, err := validator.ValidateToken(context.Background(), valid)
	if err != nil || validated.Nonce != "nonce-1" || !validated.ExpiresAt.Equal(now.Add(time.Hour)) ||
		!validated.IssuedAt.Equal(now.Add(-time.Minute)) {
		t.Fatalf("validate token claims: token=%+v err=%v", validated, err)
	}

	for name, claims := range map[string]map[string]any{
		"wrong issuer": {
			"iss": "https://issuer.invalid", "sub": "operator-1", "aud": "astra-api", "exp": now.Add(time.Hour).Unix(),
		},
		"wrong audience": {
			"iss": issuer, "sub": "operator-1", "aud": "another-api", "exp": now.Add(time.Hour).Unix(),
		},
		"expired": {
			"iss": issuer, "sub": "operator-1", "aud": "astra-api", "exp": now.Add(-time.Minute).Unix(),
		},
		"future nbf": {
			"iss": issuer, "sub": "operator-1", "aud": "astra-api", "nbf": now.Add(time.Hour).Unix(), "exp": now.Add(2 * time.Hour).Unix(),
		},
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := validator.Validate(context.Background(), signedToken(t, privateKey, "key-1", claims)); err == nil {
				t.Fatal("expected token rejection")
			}
		})
	}
	parts := strings.Split(valid, ".")
	if parts[2][0] == 'A' {
		parts[2] = "B" + parts[2][1:]
	} else {
		parts[2] = "A" + parts[2][1:]
	}
	tampered := strings.Join(parts, ".")
	if _, err := validator.Validate(context.Background(), tampered); err == nil {
		t.Fatal("expected signature tampering rejection")
	}
}

func TestOIDCValidatorRejectsMalformedNonceClaim(t *testing.T) {
	privateKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("generate RSA key: %v", err)
	}
	now := time.Date(2026, 8, 8, 8, 0, 0, 0, time.UTC)
	var issuer string
	server := httptest.NewTLSServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		response.Header().Set("Content-Type", "application/json")
		if request.URL.Path == "/.well-known/openid-configuration" {
			_ = json.NewEncoder(response).Encode(map[string]string{"issuer": issuer, "jwks_uri": issuer + "/keys"})
			return
		}
		_ = json.NewEncoder(response).Encode(map[string]any{"keys": []map[string]string{{
			"kty": "RSA", "kid": "key-1", "alg": "RS256", "use": "sig",
			"n": base64.RawURLEncoding.EncodeToString(privateKey.PublicKey.N.Bytes()), "e": "AQAB",
		}}})
	}))
	defer server.Close()
	issuer = server.URL
	validator, err := auth.NewOIDCValidator(auth.OIDCConfig{
		Issuer: issuer, Audience: "astra-api", HTTPClient: server.Client(), Clock: func() time.Time { return now },
	})
	if err != nil {
		t.Fatalf("create validator: %v", err)
	}
	token := signedToken(t, privateKey, "key-1", map[string]any{
		"iss": issuer, "sub": "operator-1", "aud": "astra-api", "exp": now.Add(time.Hour).Unix(),
		"nonce": []string{"not", "a", "string"},
	})
	if _, err := validator.ValidateToken(context.Background(), token); err == nil {
		t.Fatal("expected malformed nonce rejection")
	}
}

func TestOIDCValidatorUsesBoundedStaleKeyOnlyAfterRefreshFailure(t *testing.T) {
	privateKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("generate RSA key: %v", err)
	}
	now := time.Date(2026, 8, 8, 8, 0, 0, 0, time.UTC)
	var issuer string
	var unavailable atomic.Bool
	server := httptest.NewTLSServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		if unavailable.Load() {
			http.Error(response, "unavailable", http.StatusServiceUnavailable)
			return
		}
		response.Header().Set("Content-Type", "application/json")
		if request.URL.Path == "/.well-known/openid-configuration" {
			_ = json.NewEncoder(response).Encode(map[string]string{"issuer": issuer, "jwks_uri": issuer + "/keys"})
			return
		}
		_ = json.NewEncoder(response).Encode(map[string]any{"keys": []map[string]string{{
			"kty": "RSA", "kid": "key-1", "alg": "RS256", "use": "sig",
			"n": base64.RawURLEncoding.EncodeToString(privateKey.PublicKey.N.Bytes()),
			"e": "AQAB",
		}}})
	}))
	defer server.Close()
	issuer = server.URL
	validator, err := auth.NewOIDCValidator(auth.OIDCConfig{
		Issuer: issuer, Audience: "astra-api", HTTPClient: server.Client(), Clock: func() time.Time { return now },
		CacheTTL: time.Minute, StaleIfError: time.Minute,
	})
	if err != nil {
		t.Fatalf("create validator: %v", err)
	}
	token := signedToken(t, privateKey, "key-1", map[string]any{
		"iss": issuer, "sub": "operator-1", "aud": "astra-api", "exp": now.Add(time.Hour).Unix(),
	})
	if _, err := validator.Validate(context.Background(), token); err != nil {
		t.Fatalf("prime key cache: %v", err)
	}
	unavailable.Store(true)
	now = now.Add(90 * time.Second)
	if _, err := validator.Validate(context.Background(), token); err != nil {
		t.Fatalf("use key inside stale-if-error window: %v", err)
	}
	now = now.Add(31 * time.Second)
	if _, err := validator.Validate(context.Background(), token); err == nil {
		t.Fatal("expected stale key rejection after bounded window")
	}
}

func signedToken(t *testing.T, key *rsa.PrivateKey, keyID string, claims map[string]any) string {
	t.Helper()
	header, err := json.Marshal(map[string]string{"alg": "RS256", "kid": keyID, "typ": "at+jwt"})
	if err != nil {
		t.Fatalf("encode JWT header: %v", err)
	}
	payload, err := json.Marshal(claims)
	if err != nil {
		t.Fatalf("encode JWT claims: %v", err)
	}
	encodedHeader := base64.RawURLEncoding.EncodeToString(header)
	encodedPayload := base64.RawURLEncoding.EncodeToString(payload)
	digest := sha256.Sum256([]byte(encodedHeader + "." + encodedPayload))
	signature, err := rsa.SignPKCS1v15(rand.Reader, key, crypto.SHA256, digest[:])
	if err != nil {
		t.Fatalf("sign JWT: %v", err)
	}
	return encodedHeader + "." + encodedPayload + "." + base64.RawURLEncoding.EncodeToString(signature)
}
