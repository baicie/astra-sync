package oidc

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"io.astrasync/control-plane/auth"
)

const (
	maximumDiscoveryBytes = 64 * 1024
	maximumTokenBytes     = 64 * 1024
	maximumTokenLifetime  = 7 * 24 * time.Hour
)

type Config struct {
	Issuer       string
	Audience     string
	ClientID     string
	ClientSecret string
	RedirectURL  string
	Scopes       []string
	HTTPClient   *http.Client
	Clock        func() time.Time
}

type Client struct {
	issuer       string
	clientID     string
	clientSecret string
	redirectURL  string
	authorizeURL string
	tokenURL     string
	scopes       []string
	clock        func() time.Time
	client       *http.Client
	access       *auth.OIDCValidator
	idToken      *auth.OIDCValidator
}

type TokenSet struct {
	AccessToken  string
	RefreshToken string
	TokenType    string
	ExpiresAt    time.Time
	IDToken      string
}

func New(ctx context.Context, configuration Config) (*Client, error) {
	issuer, err := exactHTTPSURL(configuration.Issuer)
	if err != nil {
		return nil, fmt.Errorf("configure Console OIDC issuer: %w", err)
	}
	if strings.TrimSpace(configuration.Audience) == "" || strings.TrimSpace(configuration.ClientID) == "" {
		return nil, fmt.Errorf("Console OIDC audience and client ID are required")
	}
	redirect, err := exactHTTPSOrLoopbackURL(configuration.RedirectURL)
	if err != nil {
		return nil, fmt.Errorf("configure Console OIDC redirect URL: %w", err)
	}
	client := configuration.HTTPClient
	if client == nil {
		client = &http.Client{Timeout: 5 * time.Second}
	}
	clock := configuration.Clock
	if clock == nil {
		clock = time.Now
	}
	document, err := discover(ctx, client, issuer)
	if err != nil {
		return nil, err
	}
	if len(document.TokenEndpointAuthMethods) > 0 {
		requiredMethod := "none"
		if configuration.ClientSecret != "" {
			requiredMethod = "client_secret_basic"
		}
		if !contains(document.TokenEndpointAuthMethods, requiredMethod) {
			return nil, fmt.Errorf("OIDC token endpoint does not support the configured client authentication method")
		}
	}
	accessValidator, err := auth.NewOIDCValidator(auth.OIDCConfig{
		Issuer: issuer, Audience: configuration.Audience, HTTPClient: client,
		Clock: clock, AcceptedTokenTypes: []string{"JWT", "at+jwt", "application/at+jwt"},
	})
	if err != nil {
		return nil, fmt.Errorf("configure Console access-token validator: %w", err)
	}
	idValidator, err := auth.NewOIDCValidator(auth.OIDCConfig{
		Issuer: issuer, Audience: configuration.ClientID, HTTPClient: client, Clock: clock,
		AcceptedTokenTypes: []string{"JWT"}, AllowMissingTokenType: true,
	})
	if err != nil {
		return nil, fmt.Errorf("configure Console ID-token validator: %w", err)
	}
	scopes := append([]string(nil), configuration.Scopes...)
	if len(scopes) == 0 {
		scopes = []string{"openid", "profile"}
	}
	for _, scope := range scopes {
		if strings.TrimSpace(scope) == "" || len(scope) > 128 || strings.ContainsAny(scope, "\r\n") {
			return nil, fmt.Errorf("Console OIDC scope is invalid")
		}
	}
	return &Client{
		issuer: issuer, clientID: configuration.ClientID, clientSecret: configuration.ClientSecret,
		redirectURL: redirect, authorizeURL: document.AuthorizationEndpoint, tokenURL: document.TokenEndpoint,
		scopes: scopes, clock: clock, client: client, access: accessValidator, idToken: idValidator,
	}, nil
}

func (c *Client) AuthorizationURL(state, nonce, codeChallenge string) (string, error) {
	if c == nil || strings.TrimSpace(state) == "" || strings.TrimSpace(nonce) == "" ||
		strings.TrimSpace(codeChallenge) == "" || len(state) > 256 || len(nonce) > 512 || len(codeChallenge) > 256 {
		return "", fmt.Errorf("OIDC authorization parameters are invalid")
	}
	address, err := url.Parse(c.authorizeURL)
	if err != nil {
		return "", fmt.Errorf("parse OIDC authorization endpoint: %w", err)
	}
	query := address.Query()
	query.Set("response_type", "code")
	query.Set("client_id", c.clientID)
	query.Set("redirect_uri", c.redirectURL)
	query.Set("scope", strings.Join(c.scopes, " "))
	query.Set("state", state)
	query.Set("nonce", nonce)
	query.Set("code_challenge", codeChallenge)
	query.Set("code_challenge_method", "S256")
	address.RawQuery = query.Encode()
	return address.String(), nil
}

func (c *Client) Exchange(ctx context.Context, code, verifier string) (TokenSet, error) {
	if strings.TrimSpace(code) == "" || len(code) > 16*1024 || len(verifier) < 43 || len(verifier) > 128 {
		return TokenSet{}, fmt.Errorf("OIDC authorization response is invalid")
	}
	return c.tokenRequest(ctx, url.Values{
		"grant_type":    {"authorization_code"},
		"code":          {code},
		"redirect_uri":  {c.redirectURL},
		"client_id":     {c.clientID},
		"code_verifier": {verifier},
	})
}

func (c *Client) Refresh(ctx context.Context, refreshToken string) (TokenSet, error) {
	if strings.TrimSpace(refreshToken) == "" || len(refreshToken) > maximumTokenBytes {
		return TokenSet{}, fmt.Errorf("OIDC refresh token is invalid")
	}
	return c.tokenRequest(ctx, url.Values{
		"grant_type":    {"refresh_token"},
		"refresh_token": {refreshToken},
		"client_id":     {c.clientID},
	})
}

func (c *Client) ValidateAccessToken(ctx context.Context, token string) (auth.ValidatedToken, error) {
	return c.access.ValidateToken(ctx, token)
}

func (c *Client) ValidateIDToken(ctx context.Context, token, expectedNonce string) (auth.ValidatedToken, error) {
	validated, err := c.idToken.ValidateToken(ctx, token)
	if err != nil || validated.Nonce == "" || !subtleConstantTimeEqual(validated.Nonce, expectedNonce) {
		return auth.ValidatedToken{}, auth.ErrUnauthenticated
	}
	return validated, nil
}

func (c *Client) tokenRequest(ctx context.Context, values url.Values) (TokenSet, error) {
	if c == nil || ctx == nil || values.Get("code") == "" && values.Get("refresh_token") == "" {
		return TokenSet{}, fmt.Errorf("OIDC token request is invalid")
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, c.tokenURL, strings.NewReader(values.Encode()))
	if err != nil {
		return TokenSet{}, fmt.Errorf("create OIDC token request: %w", err)
	}
	request.Header.Set("Accept", "application/json")
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	if c.clientSecret != "" {
		request.SetBasicAuth(c.clientID, c.clientSecret)
	}
	client := *c.client
	client.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	response, err := client.Do(request)
	if err != nil {
		return TokenSet{}, fmt.Errorf("OIDC token endpoint unavailable")
	}
	defer response.Body.Close()
	limited := io.LimitReader(response.Body, maximumTokenBytes+1)
	payload, err := io.ReadAll(limited)
	if err != nil || len(payload) > maximumTokenBytes {
		return TokenSet{}, fmt.Errorf("OIDC token response is invalid")
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return TokenSet{}, fmt.Errorf("OIDC token exchange was rejected")
	}
	var tokenResponse struct {
		AccessToken  string `json:"access_token"`
		RefreshToken string `json:"refresh_token"`
		TokenType    string `json:"token_type"`
		ExpiresIn    int64  `json:"expires_in"`
		IDToken      string `json:"id_token"`
	}
	if err := json.Unmarshal(payload, &tokenResponse); err != nil || tokenResponse.ExpiresIn <= 0 ||
		tokenResponse.ExpiresIn > int64(maximumTokenLifetime/time.Second) ||
		strings.TrimSpace(tokenResponse.AccessToken) == "" || len(tokenResponse.AccessToken) > maximumTokenBytes ||
		!strings.EqualFold(tokenResponse.TokenType, "Bearer") {
		return TokenSet{}, fmt.Errorf("OIDC token response is invalid")
	}
	if len(tokenResponse.RefreshToken) > maximumTokenBytes || len(tokenResponse.IDToken) > maximumTokenBytes {
		return TokenSet{}, fmt.Errorf("OIDC token response is invalid")
	}
	return TokenSet{
		AccessToken: tokenResponse.AccessToken, RefreshToken: tokenResponse.RefreshToken,
		TokenType: "Bearer", ExpiresAt: c.clock().UTC().Add(time.Duration(tokenResponse.ExpiresIn) * time.Second),
		IDToken: tokenResponse.IDToken,
	}, nil
}

type discoveryDocument struct {
	Issuer                   string   `json:"issuer"`
	AuthorizationEndpoint    string   `json:"authorization_endpoint"`
	TokenEndpoint            string   `json:"token_endpoint"`
	TokenEndpointAuthMethods []string `json:"token_endpoint_auth_methods_supported"`
}

func discover(ctx context.Context, client *http.Client, issuer string) (discoveryDocument, error) {
	endpoint := strings.TrimSuffix(issuer, "/") + "/.well-known/openid-configuration"
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return discoveryDocument{}, fmt.Errorf("create OIDC discovery request: %w", err)
	}
	request.Header.Set("Accept", "application/json")
	boundedClient := *client
	configuredRedirect := boundedClient.CheckRedirect
	boundedClient.CheckRedirect = func(next *http.Request, via []*http.Request) error {
		if len(via) >= 3 || next.URL.Scheme != "https" || next.URL.Host != mustParseURL(issuer).Host {
			return fmt.Errorf("OIDC discovery redirect rejected")
		}
		if configuredRedirect != nil {
			return configuredRedirect(next, via)
		}
		return nil
	}
	response, err := boundedClient.Do(request)
	if err != nil {
		return discoveryDocument{}, fmt.Errorf("OIDC discovery unavailable")
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return discoveryDocument{}, fmt.Errorf("OIDC discovery was rejected")
	}
	payload, err := io.ReadAll(io.LimitReader(response.Body, maximumDiscoveryBytes+1))
	if err != nil || len(payload) > maximumDiscoveryBytes {
		return discoveryDocument{}, fmt.Errorf("OIDC discovery response is invalid")
	}
	var document discoveryDocument
	if err := json.Unmarshal(payload, &document); err != nil || document.Issuer != issuer {
		return discoveryDocument{}, fmt.Errorf("OIDC discovery document is invalid")
	}
	if _, err := exactHTTPSURL(document.AuthorizationEndpoint); err != nil {
		return discoveryDocument{}, fmt.Errorf("OIDC authorization endpoint is invalid")
	}
	if _, err := exactHTTPSURL(document.TokenEndpoint); err != nil {
		return discoveryDocument{}, fmt.Errorf("OIDC token endpoint is invalid")
	}
	return document, nil
}

func exactHTTPSURL(raw string) (string, error) {
	parsed, err := url.Parse(raw)
	if err != nil || parsed.Scheme != "https" || parsed.Host == "" || parsed.RawQuery != "" || parsed.Fragment != "" {
		return "", fmt.Errorf("URL must be an exact HTTPS URL")
	}
	return strings.TrimSuffix(parsed.String(), "/"), nil
}

func exactHTTPSOrLoopbackURL(raw string) (string, error) {
	parsed, err := url.Parse(raw)
	if err != nil || parsed.Host == "" || parsed.RawQuery != "" || parsed.Fragment != "" ||
		(parsed.Scheme != "https" && !(parsed.Scheme == "http" && strings.HasPrefix(parsed.Hostname(), "127."))) {
		return "", fmt.Errorf("URL must be HTTPS or loopback HTTP")
	}
	return parsed.String(), nil
}

func mustParseURL(raw string) *url.URL {
	parsed, _ := url.Parse(raw)
	return parsed
}

func subtleConstantTimeEqual(left, right string) bool {
	if len(left) != len(right) {
		return false
	}
	var result byte
	for index := range left {
		result |= left[index] ^ right[index]
	}
	return result == 0
}

func contains(values []string, expected string) bool {
	for _, value := range values {
		if value == expected {
			return true
		}
	}
	return false
}
