package oidc_test

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
	"net/url"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"io.astrasync/console/internal/oidc"
)

func TestClientUsesPKCEAndValidatesAccessAndIDTokens(t *testing.T) {
	privateKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("generate RSA key: %v", err)
	}
	now := time.Date(2026, 8, 9, 10, 0, 0, 0, time.UTC)
	var issuer string
	var tokenRequestObserved atomic.Bool
	server := httptest.NewTLSServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		response.Header().Set("Content-Type", "application/json")
		switch request.URL.Path {
		case "/.well-known/openid-configuration":
			_ = json.NewEncoder(response).Encode(map[string]any{
				"issuer": issuer, "jwks_uri": issuer + "/keys", "authorization_endpoint": issuer + "/authorize",
				"token_endpoint": issuer + "/token", "token_endpoint_auth_methods_supported": []string{"client_secret_basic"},
			})
		case "/keys":
			_ = json.NewEncoder(response).Encode(map[string]any{"keys": []map[string]string{{
				"kty": "RSA", "kid": "key-1", "alg": "RS256", "use": "sig",
				"n": base64.RawURLEncoding.EncodeToString(privateKey.PublicKey.N.Bytes()),
				"e": base64.RawURLEncoding.EncodeToString(big.NewInt(int64(privateKey.PublicKey.E)).Bytes()),
			}}})
		case "/token":
			clientID, secret, ok := request.BasicAuth()
			if !ok || clientID != "console-client" || secret != "client-secret" || request.ParseForm() != nil ||
				request.Form.Get("code") != "authorization-code" || request.Form.Get("code_verifier") != strings.Repeat("v", 43) {
				http.Error(response, "invalid request", http.StatusBadRequest)
				return
			}
			tokenRequestObserved.Store(true)
			_ = json.NewEncoder(response).Encode(map[string]any{
				"access_token": signedToken(t, privateKey, "at+jwt", map[string]any{
					"iss": issuer, "sub": "operator-1", "aud": "astra-api", "iat": now.Unix(), "exp": now.Add(time.Hour).Unix(),
				}),
				"id_token": signedToken(t, privateKey, "JWT", map[string]any{
					"iss": issuer, "sub": "operator-1", "aud": "console-client", "iat": now.Unix(), "exp": now.Add(time.Hour).Unix(), "nonce": "nonce-1",
				}),
				"refresh_token": "refresh-token", "token_type": "Bearer", "expires_in": 3600,
			})
		default:
			http.NotFound(response, request)
		}
	}))
	defer server.Close()
	issuer = server.URL
	client, err := oidc.New(context.Background(), oidc.Config{Issuer: issuer, Audience: "astra-api",
		ClientID: "console-client", ClientSecret: "client-secret", RedirectURL: "https://console.example/auth/callback",
		HTTPClient: server.Client(), Clock: func() time.Time { return now }})
	if err != nil {
		t.Fatalf("create OIDC client: %v", err)
	}
	authorizationURL, err := client.AuthorizationURL("state-1", "nonce-1", "challenge-1")
	if err != nil {
		t.Fatalf("build authorization URL: %v", err)
	}
	parsed, _ := url.Parse(authorizationURL)
	if parsed.Query().Get("code_challenge_method") != "S256" || parsed.Query().Get("state") != "state-1" ||
		parsed.Query().Get("nonce") != "nonce-1" || parsed.Query().Get("redirect_uri") != "https://console.example/auth/callback" {
		t.Fatalf("unexpected authorization URL: %s", authorizationURL)
	}
	tokens, err := client.Exchange(context.Background(), "authorization-code", strings.Repeat("v", 43))
	if err != nil || !tokenRequestObserved.Load() || tokens.RefreshToken != "refresh-token" {
		t.Fatalf("exchange authorization code: tokens=%+v observed=%t err=%v", tokens, tokenRequestObserved.Load(), err)
	}
	access, err := client.ValidateAccessToken(context.Background(), tokens.AccessToken)
	if err != nil || access.Identity.Subject != "operator-1" {
		t.Fatalf("validate access token: token=%+v err=%v", access, err)
	}
	idToken, err := client.ValidateIDToken(context.Background(), tokens.IDToken, "nonce-1")
	if err != nil || idToken.Identity != access.Identity {
		t.Fatalf("validate ID token: token=%+v err=%v", idToken, err)
	}
	if _, err := client.ValidateIDToken(context.Background(), tokens.IDToken, "wrong-nonce"); err == nil {
		t.Fatal("expected nonce mismatch rejection")
	}
}

func TestClientRejectsTokenEndpointRedirectWithoutLeakingResponse(t *testing.T) {
	privateKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("generate RSA key: %v", err)
	}
	var attackerCalled atomic.Bool
	attacker := httptest.NewTLSServer(http.HandlerFunc(func(response http.ResponseWriter, _ *http.Request) {
		attackerCalled.Store(true)
		http.Error(response, "credential-capture-sentinel", http.StatusBadRequest)
	}))
	defer attacker.Close()
	var issuer string
	server := httptest.NewTLSServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		response.Header().Set("Content-Type", "application/json")
		switch request.URL.Path {
		case "/.well-known/openid-configuration":
			_ = json.NewEncoder(response).Encode(map[string]any{
				"issuer": issuer, "jwks_uri": issuer + "/keys", "authorization_endpoint": issuer + "/authorize",
				"token_endpoint": issuer + "/token", "token_endpoint_auth_methods_supported": []string{"client_secret_basic"},
			})
		case "/keys":
			_ = json.NewEncoder(response).Encode(map[string]any{"keys": []map[string]string{{
				"kty": "RSA", "kid": "key-1", "alg": "RS256", "use": "sig",
				"n": base64.RawURLEncoding.EncodeToString(privateKey.PublicKey.N.Bytes()), "e": "AQAB",
			}}})
		case "/token":
			http.Redirect(response, request, attacker.URL+"/capture", http.StatusTemporaryRedirect)
		default:
			http.NotFound(response, request)
		}
	}))
	defer server.Close()
	issuer = server.URL
	client, err := oidc.New(context.Background(), oidc.Config{Issuer: issuer, Audience: "astra-api",
		ClientID: "console-client", ClientSecret: "client-secret", RedirectURL: "https://console.example/auth/callback",
		HTTPClient: server.Client()})
	if err != nil {
		t.Fatalf("create OIDC client: %v", err)
	}
	_, err = client.Exchange(context.Background(), "authorization-code", strings.Repeat("v", 43))
	if err == nil || attackerCalled.Load() || strings.Contains(err.Error(), "credential-capture-sentinel") ||
		strings.Contains(err.Error(), "authorization-code") {
		t.Fatalf("token redirect was not safely rejected: called=%t err=%v", attackerCalled.Load(), err)
	}
}

func signedToken(t *testing.T, key *rsa.PrivateKey, tokenType string, claims map[string]any) string {
	t.Helper()
	header, err := json.Marshal(map[string]string{"alg": "RS256", "kid": "key-1", "typ": tokenType})
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
