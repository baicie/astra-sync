package auth

import (
	"context"
	"crypto"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math/big"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"
)

const (
	defaultMaximumTokenBytes = 16 * 1024
	maximumJWKSBytes         = 1024 * 1024
	maximumOIDCKeys          = 128
	maximumClaims            = 128
	maximumClaimString       = 4096
)

type ExternalIdentity struct {
	Issuer  string
	Subject string
}

// ValidatedToken contains only bounded claims that have passed signature,
// issuer, audience, and lifetime validation.
type ValidatedToken struct {
	Identity  ExternalIdentity
	Nonce     string
	ExpiresAt time.Time
	IssuedAt  time.Time
}

type OIDCConfig struct {
	Issuer                string
	Audience              string
	HTTPClient            *http.Client
	Clock                 func() time.Time
	ClockSkew             time.Duration
	CacheTTL              time.Duration
	StaleIfError          time.Duration
	MaximumTokenBytes     int
	MaximumTokenLifetime  time.Duration
	AcceptedTokenTypes    []string
	AllowMissingTokenType bool
}

type OIDCValidator struct {
	issuer               *url.URL
	audience             string
	client               *http.Client
	clock                func() time.Time
	clockSkew            time.Duration
	cacheTTL             time.Duration
	staleIfError         time.Duration
	maximumTokenBytes    int
	maximumTokenLifetime time.Duration
	acceptedTypes        map[string]struct{}
	allowMissingType     bool

	mu         sync.Mutex
	keys       map[string]verificationKey
	fetchedAt  time.Time
	expiresAt  time.Time
	staleUntil time.Time
}

type verificationKey struct {
	algorithm string
	publicKey any
}

func NewOIDCValidator(configuration OIDCConfig) (*OIDCValidator, error) {
	issuer, err := url.Parse(configuration.Issuer)
	if err != nil || issuer.Scheme != "https" || issuer.Host == "" || issuer.RawQuery != "" || issuer.Fragment != "" {
		return nil, fmt.Errorf("OIDC issuer must be an exact HTTPS URL")
	}
	issuer.Path = strings.TrimSuffix(issuer.Path, "/")
	if strings.TrimSpace(configuration.Audience) == "" || len(configuration.Audience) > maximumClaimString {
		return nil, fmt.Errorf("OIDC audience must not be blank")
	}
	client := configuration.HTTPClient
	if client == nil {
		client = &http.Client{Timeout: 5 * time.Second}
	}
	clock := configuration.Clock
	if clock == nil {
		clock = time.Now
	}
	clockSkew := configuration.ClockSkew
	if clockSkew == 0 {
		clockSkew = 30 * time.Second
	}
	cacheTTL := configuration.CacheTTL
	if cacheTTL == 0 {
		cacheTTL = 15 * time.Minute
	}
	staleIfError := configuration.StaleIfError
	if staleIfError == 0 {
		staleIfError = 5 * time.Minute
	}
	maximumTokenBytes := configuration.MaximumTokenBytes
	if maximumTokenBytes == 0 {
		maximumTokenBytes = defaultMaximumTokenBytes
	}
	maximumLifetime := configuration.MaximumTokenLifetime
	if maximumLifetime == 0 {
		maximumLifetime = 24 * time.Hour
	}
	if clockSkew < 0 || clockSkew > 5*time.Minute || cacheTTL <= 0 || cacheTTL > 24*time.Hour ||
		staleIfError < 0 || staleIfError > time.Hour || maximumTokenBytes < 512 ||
		maximumTokenBytes > 64*1024 || maximumLifetime <= 0 || maximumLifetime > 7*24*time.Hour {
		return nil, fmt.Errorf("OIDC validation bounds are invalid")
	}
	acceptedTypes := make(map[string]struct{})
	for _, value := range configuration.AcceptedTokenTypes {
		if strings.TrimSpace(value) == "" || len(value) > 64 {
			return nil, fmt.Errorf("OIDC accepted token type is invalid")
		}
		acceptedTypes[value] = struct{}{}
	}
	return &OIDCValidator{
		issuer:               issuer,
		audience:             configuration.Audience,
		client:               client,
		clock:                clock,
		clockSkew:            clockSkew,
		cacheTTL:             cacheTTL,
		staleIfError:         staleIfError,
		maximumTokenBytes:    maximumTokenBytes,
		maximumTokenLifetime: maximumLifetime,
		acceptedTypes:        acceptedTypes,
		allowMissingType:     configuration.AllowMissingTokenType,
		keys:                 make(map[string]verificationKey),
	}, nil
}

func (v *OIDCValidator) Validate(ctx context.Context, rawToken string) (ExternalIdentity, error) {
	validated, err := v.ValidateToken(ctx, rawToken)
	if err != nil {
		return ExternalIdentity{}, err
	}
	return validated.Identity, nil
}

func (v *OIDCValidator) ValidateToken(ctx context.Context, rawToken string) (ValidatedToken, error) {
	if ctx == nil || len(rawToken) == 0 || len(rawToken) > v.maximumTokenBytes {
		return ValidatedToken{}, ErrUnauthenticated
	}
	parts := strings.Split(rawToken, ".")
	if len(parts) != 3 || len(parts[0]) > 4096 || len(parts[1]) > v.maximumTokenBytes {
		return ValidatedToken{}, ErrUnauthenticated
	}
	headerBytes, err := base64.RawURLEncoding.DecodeString(parts[0])
	if err != nil {
		return ValidatedToken{}, ErrUnauthenticated
	}
	var header struct {
		Algorithm string `json:"alg"`
		KeyID     string `json:"kid"`
		Type      string `json:"typ"`
	}
	if err := decodeBoundedJSON(headerBytes, &header); err != nil ||
		(header.Algorithm != "RS256" && header.Algorithm != "ES256") ||
		strings.TrimSpace(header.KeyID) == "" || len(header.KeyID) > 256 {
		return ValidatedToken{}, ErrUnauthenticated
	}
	if len(v.acceptedTypes) > 0 {
		_, accepted := v.acceptedTypes[header.Type]
		if !accepted && !(header.Type == "" && v.allowMissingType) {
			return ValidatedToken{}, ErrUnauthenticated
		}
	}
	key, err := v.verificationKey(ctx, header.KeyID)
	if err != nil || key.algorithm != "" && key.algorithm != header.Algorithm {
		return ValidatedToken{}, errors.Join(ErrUnauthenticated, err)
	}
	signature, err := base64.RawURLEncoding.DecodeString(parts[2])
	if err != nil || !verifyJWTSignature(header.Algorithm, key.publicKey, []byte(parts[0]+"."+parts[1]), signature) {
		return ValidatedToken{}, ErrUnauthenticated
	}
	payload, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return ValidatedToken{}, ErrUnauthenticated
	}
	claims := make(map[string]json.RawMessage)
	if err := decodeBoundedJSON(payload, &claims); err != nil || len(claims) == 0 || len(claims) > maximumClaims {
		return ValidatedToken{}, ErrUnauthenticated
	}
	if err := validateClaimDimensions(claims); err != nil {
		return ValidatedToken{}, ErrUnauthenticated
	}
	issuer, ok := claimString(claims, "iss")
	if !ok || issuer != v.issuer.String() {
		return ValidatedToken{}, ErrUnauthenticated
	}
	subject, ok := claimString(claims, "sub")
	if !ok || strings.TrimSpace(subject) == "" || len(subject) > 512 {
		return ValidatedToken{}, ErrUnauthenticated
	}
	if !claimAudienceContains(claims["aud"], v.audience) {
		return ValidatedToken{}, ErrUnauthenticated
	}
	expiresAt, ok := claimNumericDate(claims, "exp")
	if !ok {
		return ValidatedToken{}, ErrUnauthenticated
	}
	now := v.clock().UTC()
	if now.After(expiresAt.Add(v.clockSkew)) {
		return ValidatedToken{}, ErrUnauthenticated
	}
	if notBefore, present := claimNumericDate(claims, "nbf"); present && now.Add(v.clockSkew).Before(notBefore) {
		return ValidatedToken{}, ErrUnauthenticated
	}
	issuedAt, issuedAtPresent := claimNumericDate(claims, "iat")
	if issuedAtPresent {
		if now.Add(v.clockSkew).Before(issuedAt) || expiresAt.Sub(issuedAt) > v.maximumTokenLifetime {
			return ValidatedToken{}, ErrUnauthenticated
		}
	}
	nonce := ""
	if rawNonce, present := claims["nonce"]; present {
		var valid bool
		nonce, valid = claimString(claims, "nonce")
		if !valid || strings.TrimSpace(nonce) == "" || len(nonce) > 512 || len(rawNonce) > maximumClaimString+2 {
			return ValidatedToken{}, ErrUnauthenticated
		}
	}
	return ValidatedToken{
		Identity: ExternalIdentity{Issuer: issuer, Subject: subject},
		Nonce:    nonce, ExpiresAt: expiresAt, IssuedAt: issuedAt,
	}, nil
}

func (v *OIDCValidator) verificationKey(ctx context.Context, keyID string) (verificationKey, error) {
	v.mu.Lock()
	defer v.mu.Unlock()
	now := v.clock().UTC()
	if key, found := v.keys[keyID]; found && now.Before(v.expiresAt) {
		return key, nil
	}
	refreshErr := v.refreshLocked(ctx, now)
	if refreshErr == nil {
		if key, found := v.keys[keyID]; found {
			return key, nil
		}
		return verificationKey{}, fmt.Errorf("OIDC key is unknown")
	}
	if key, found := v.keys[keyID]; found && now.Before(v.staleUntil) {
		return key, nil
	}
	return verificationKey{}, fmt.Errorf("refresh OIDC keys: %w", refreshErr)
}

func (v *OIDCValidator) refreshLocked(ctx context.Context, now time.Time) error {
	discoveryURI := strings.TrimSuffix(v.issuer.String(), "/") + "/.well-known/openid-configuration"
	var discovery struct {
		Issuer  string `json:"issuer"`
		JWKSURI string `json:"jwks_uri"`
	}
	if err := v.fetchJSON(ctx, discoveryURI, maximumJWKSBytes, &discovery, true); err != nil {
		return err
	}
	if discovery.Issuer != v.issuer.String() {
		return fmt.Errorf("OIDC discovery issuer mismatch")
	}
	jwksURI, err := url.Parse(discovery.JWKSURI)
	if err != nil || jwksURI.Scheme != "https" || jwksURI.Host == "" || jwksURI.Fragment != "" {
		return fmt.Errorf("OIDC JWKS URI is invalid")
	}
	var document struct {
		Keys []jsonWebKey `json:"keys"`
	}
	if err := v.fetchJSON(ctx, jwksURI.String(), maximumJWKSBytes, &document, false); err != nil {
		return err
	}
	if len(document.Keys) == 0 || len(document.Keys) > maximumOIDCKeys {
		return fmt.Errorf("OIDC JWKS key count is invalid")
	}
	keys := make(map[string]verificationKey, len(document.Keys))
	for _, encoded := range document.Keys {
		key, err := encoded.verificationKey()
		if err != nil {
			return err
		}
		if _, duplicate := keys[encoded.KeyID]; duplicate {
			return fmt.Errorf("OIDC JWKS contains duplicate key IDs")
		}
		keys[encoded.KeyID] = key
	}
	v.keys = keys
	v.fetchedAt = now
	v.expiresAt = now.Add(v.cacheTTL)
	v.staleUntil = v.expiresAt.Add(v.staleIfError)
	return nil
}

func (v *OIDCValidator) fetchJSON(
	ctx context.Context, address string, maximumBytes int64, destination any, discovery bool,
) error {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, address, nil)
	if err != nil {
		return err
	}
	request.Header.Set("Accept", "application/json")
	client := *v.client
	configuredRedirect := client.CheckRedirect
	client.CheckRedirect = func(request *http.Request, via []*http.Request) error {
		if len(via) >= 3 {
			return fmt.Errorf("OIDC redirect limit exceeded")
		}
		if discovery && (request.URL.Scheme != v.issuer.Scheme || request.URL.Host != v.issuer.Host) {
			return fmt.Errorf("OIDC discovery cross-origin redirect rejected")
		}
		if configuredRedirect != nil {
			return configuredRedirect(request, via)
		}
		return nil
	}
	response, err := client.Do(request)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return fmt.Errorf("OIDC endpoint returned status %d", response.StatusCode)
	}
	limited := io.LimitReader(response.Body, maximumBytes+1)
	payload, err := io.ReadAll(limited)
	if err != nil {
		return err
	}
	if int64(len(payload)) > maximumBytes {
		return fmt.Errorf("OIDC response exceeds size limit")
	}
	if err := json.Unmarshal(payload, destination); err != nil {
		return fmt.Errorf("decode OIDC response: %w", err)
	}
	return nil
}

type jsonWebKey struct {
	KeyType   string `json:"kty"`
	KeyID     string `json:"kid"`
	Algorithm string `json:"alg"`
	Use       string `json:"use"`
	Modulus   string `json:"n"`
	Exponent  string `json:"e"`
	Curve     string `json:"crv"`
	X         string `json:"x"`
	Y         string `json:"y"`
}

func (j jsonWebKey) verificationKey() (verificationKey, error) {
	if strings.TrimSpace(j.KeyID) == "" || len(j.KeyID) > 256 || j.Use != "" && j.Use != "sig" {
		return verificationKey{}, fmt.Errorf("OIDC signing key metadata is invalid")
	}
	switch j.KeyType {
	case "RSA":
		if j.Algorithm != "" && j.Algorithm != "RS256" {
			return verificationKey{}, fmt.Errorf("OIDC RSA key algorithm is not allowed")
		}
		modulusBytes, err := base64.RawURLEncoding.DecodeString(j.Modulus)
		if err != nil || len(modulusBytes) < 256 || len(modulusBytes) > 1024 {
			return verificationKey{}, fmt.Errorf("OIDC RSA modulus is invalid")
		}
		exponentBytes, err := base64.RawURLEncoding.DecodeString(j.Exponent)
		if err != nil || len(exponentBytes) == 0 || len(exponentBytes) > 4 {
			return verificationKey{}, fmt.Errorf("OIDC RSA exponent is invalid")
		}
		exponent := 0
		for _, value := range exponentBytes {
			exponent = exponent<<8 + int(value)
		}
		if exponent < 3 || exponent%2 == 0 {
			return verificationKey{}, fmt.Errorf("OIDC RSA exponent is invalid")
		}
		return verificationKey{
			algorithm: j.Algorithm,
			publicKey: &rsa.PublicKey{N: new(big.Int).SetBytes(modulusBytes), E: exponent},
		}, nil
	case "EC":
		if j.Curve != "P-256" || j.Algorithm != "" && j.Algorithm != "ES256" {
			return verificationKey{}, fmt.Errorf("OIDC EC key is not supported")
		}
		xBytes, xErr := base64.RawURLEncoding.DecodeString(j.X)
		yBytes, yErr := base64.RawURLEncoding.DecodeString(j.Y)
		if xErr != nil || yErr != nil || len(xBytes) != 32 || len(yBytes) != 32 {
			return verificationKey{}, fmt.Errorf("OIDC EC coordinates are invalid")
		}
		publicKey := &ecdsa.PublicKey{Curve: elliptic.P256(), X: new(big.Int).SetBytes(xBytes), Y: new(big.Int).SetBytes(yBytes)}
		if !publicKey.Curve.IsOnCurve(publicKey.X, publicKey.Y) {
			return verificationKey{}, fmt.Errorf("OIDC EC key is not on the declared curve")
		}
		return verificationKey{algorithm: j.Algorithm, publicKey: publicKey}, nil
	default:
		return verificationKey{}, fmt.Errorf("OIDC signing key type is not supported")
	}
}

func verifyJWTSignature(algorithm string, publicKey any, signingInput, signature []byte) bool {
	digest := sha256.Sum256(signingInput)
	switch algorithm {
	case "RS256":
		key, ok := publicKey.(*rsa.PublicKey)
		return ok && rsa.VerifyPKCS1v15(key, crypto.SHA256, digest[:], signature) == nil
	case "ES256":
		key, ok := publicKey.(*ecdsa.PublicKey)
		if !ok || len(signature) != 64 {
			return false
		}
		return ecdsa.Verify(key, digest[:], new(big.Int).SetBytes(signature[:32]), new(big.Int).SetBytes(signature[32:]))
	default:
		return false
	}
}

func decodeBoundedJSON(payload []byte, destination any) error {
	if len(payload) == 0 || len(payload) > defaultMaximumTokenBytes {
		return fmt.Errorf("JWT JSON is outside the supported size range")
	}
	decoder := json.NewDecoder(strings.NewReader(string(payload)))
	decoder.UseNumber()
	if err := decoder.Decode(destination); err != nil {
		return err
	}
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		return fmt.Errorf("JWT JSON contains trailing content")
	}
	return nil
}

func validateClaimDimensions(claims map[string]json.RawMessage) error {
	for key, value := range claims {
		if len(key) == 0 || len(key) > 256 || len(value) > defaultMaximumTokenBytes {
			return fmt.Errorf("JWT claim exceeds dimension limits")
		}
		var decoded any
		if err := json.Unmarshal(value, &decoded); err != nil {
			return err
		}
		if err := validateJSONValue(decoded, 0); err != nil {
			return err
		}
	}
	return nil
}

func validateJSONValue(value any, depth int) error {
	if depth > 4 {
		return fmt.Errorf("JWT claim nesting is too deep")
	}
	switch typed := value.(type) {
	case string:
		if len(typed) > maximumClaimString {
			return fmt.Errorf("JWT claim string is too long")
		}
	case []any:
		if len(typed) > maximumClaims {
			return fmt.Errorf("JWT claim array is too large")
		}
		for _, item := range typed {
			if err := validateJSONValue(item, depth+1); err != nil {
				return err
			}
		}
	case map[string]any:
		if len(typed) > maximumClaims {
			return fmt.Errorf("JWT claim object is too large")
		}
		for key, item := range typed {
			if len(key) > 256 {
				return fmt.Errorf("JWT claim key is too long")
			}
			if err := validateJSONValue(item, depth+1); err != nil {
				return err
			}
		}
	}
	return nil
}

func claimString(claims map[string]json.RawMessage, name string) (string, bool) {
	var value string
	if err := json.Unmarshal(claims[name], &value); err != nil {
		return "", false
	}
	return value, true
}

func claimAudienceContains(raw json.RawMessage, audience string) bool {
	var single string
	if json.Unmarshal(raw, &single) == nil {
		return single == audience
	}
	var multiple []string
	if json.Unmarshal(raw, &multiple) != nil || len(multiple) == 0 || len(multiple) > 32 {
		return false
	}
	for _, value := range multiple {
		if value == audience {
			return true
		}
	}
	return false
}

func claimNumericDate(claims map[string]json.RawMessage, name string) (time.Time, bool) {
	raw, found := claims[name]
	if !found {
		return time.Time{}, false
	}
	var value json.Number
	if err := json.Unmarshal(raw, &value); err != nil {
		return time.Time{}, false
	}
	seconds, err := value.Int64()
	if err != nil || seconds <= 0 {
		return time.Time{}, false
	}
	return time.Unix(seconds, 0).UTC(), true
}
