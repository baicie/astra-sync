package postgres

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"time"

	"github.com/google/uuid"

	"io.astrasync/control-plane/auth"
)

const (
	randomCredentialBytes = 32
	maximumEnvelopeBytes  = 64 * 1024
)

type SessionStore struct {
	db    *sql.DB
	clock func() time.Time
	codec sessionCodec
}

type sessionCodec struct {
	sessionHashKey []byte
	browserHashKey []byte
	csrfTokenKey   []byte
	csrfHashKey    []byte
	encryptionKey  []byte
}

func NewSessionStore(repository *Repository, masterKey []byte) (*SessionStore, error) {
	if repository == nil || repository.db == nil {
		return nil, fmt.Errorf("authentication repository must not be nil")
	}
	codec, err := newSessionCodec(masterKey)
	if err != nil {
		return nil, err
	}
	return &SessionStore{db: repository.db, clock: repository.clock, codec: codec}, nil
}

func newSessionCodec(masterKey []byte) (sessionCodec, error) {
	if len(masterKey) < 32 {
		return sessionCodec{}, fmt.Errorf("Console session key must contain at least 32 bytes")
	}
	derive := func(label string) []byte {
		mac := hmac.New(sha256.New, masterKey)
		_, _ = mac.Write([]byte("astrasync/console/" + label + "/v1"))
		return mac.Sum(nil)
	}
	return sessionCodec{
		sessionHashKey: derive("session-hash"), browserHashKey: derive("browser-hash"),
		csrfTokenKey: derive("csrf-token"), csrfHashKey: derive("csrf-hash"),
		encryptionKey: derive("envelope-encryption"),
	}, nil
}

func (s *SessionStore) CreateLoginTransaction(
	ctx context.Context, transaction auth.LoginTransaction, ttl time.Duration,
) (auth.LoginCredentials, error) {
	if ctx == nil || transaction.Validate() != nil || ttl < time.Minute || ttl > 30*time.Minute {
		return auth.LoginCredentials{}, fmt.Errorf("OIDC login transaction bounds are invalid")
	}
	state, stateBytes, err := randomCredential()
	if err != nil {
		return auth.LoginCredentials{}, err
	}
	browser, browserBytes, err := randomCredential()
	if err != nil {
		return auth.LoginCredentials{}, err
	}
	stateHash := keyedHash(s.codec.sessionHashKey, stateBytes)
	browserHash := keyedHash(s.codec.browserHashKey, browserBytes)
	payload, err := s.codec.encryptJSON(transaction, appendAAD("login", stateHash, browserHash))
	if err != nil {
		return auth.LoginCredentials{}, err
	}
	now := s.clock().UTC()
	expiresAt := now.Add(ttl)
	if _, err := s.db.ExecContext(ctx,
		`INSERT INTO astrasync_auth_login_transactions
            (state_hash, browser_hash, encrypted_payload, expires_at, created_at)
         VALUES ($1, $2, $3, $4, $5)`, stateHash, browserHash, payload, expiresAt, now); err != nil {
		return auth.LoginCredentials{}, fmt.Errorf("persist OIDC login transaction: %w", err)
	}
	return auth.LoginCredentials{State: state, BrowserBinding: browser, ExpiresAt: expiresAt}, nil
}

func (s *SessionStore) ConsumeLoginTransaction(
	ctx context.Context, state, browserBinding string,
) (auth.LoginTransaction, error) {
	stateBytes, err := decodeCredential(state)
	if err != nil {
		return auth.LoginTransaction{}, auth.ErrUnauthenticated
	}
	browserBytes, err := decodeCredential(browserBinding)
	if err != nil {
		return auth.LoginTransaction{}, auth.ErrUnauthenticated
	}
	stateHash := keyedHash(s.codec.sessionHashKey, stateBytes)
	browserHash := keyedHash(s.codec.browserHashKey, browserBytes)
	var payload []byte
	err = s.db.QueryRowContext(ctx,
		`DELETE FROM astrasync_auth_login_transactions
          WHERE state_hash = $1 AND browser_hash = $2 AND expires_at > $3
      RETURNING encrypted_payload`, stateHash, browserHash, s.clock().UTC()).Scan(&payload)
	if errors.Is(err, sql.ErrNoRows) {
		return auth.LoginTransaction{}, auth.ErrUnauthenticated
	}
	if err != nil {
		return auth.LoginTransaction{}, fmt.Errorf("consume OIDC login transaction: %w", err)
	}
	var transaction auth.LoginTransaction
	if err := s.codec.decryptJSON(payload, appendAAD("login", stateHash, browserHash), &transaction); err != nil || transaction.Validate() != nil {
		return auth.LoginTransaction{}, auth.ErrUnauthenticated
	}
	return transaction, nil
}

func (s *SessionStore) CreateConsoleSession(
	ctx context.Context, principalID string, tokens auth.ConsoleTokens,
	idleTTL, absoluteTTL time.Duration,
) (auth.SessionCredentials, error) {
	if _, err := uuid.Parse(principalID); err != nil || tokens.Validate() != nil ||
		idleTTL < time.Minute || absoluteTTL < idleTTL || absoluteTTL > 7*24*time.Hour {
		return auth.SessionCredentials{}, fmt.Errorf("Console session bounds are invalid")
	}
	sessionID, sessionBytes, err := randomCredential()
	if err != nil {
		return auth.SessionCredentials{}, err
	}
	sessionHash := keyedHash(s.codec.sessionHashKey, sessionBytes)
	csrfToken := s.codec.csrfToken(sessionBytes)
	csrfHash := keyedHash(s.codec.csrfHashKey, []byte(csrfToken))
	payload, err := s.codec.encryptJSON(tokens, appendAAD("session", sessionHash, []byte(principalID)))
	if err != nil {
		return auth.SessionCredentials{}, err
	}
	now := s.clock().UTC()
	absoluteExpiresAt := now.Add(absoluteTTL)
	if tokens.RefreshToken == "" && tokens.ExpiresAt.Before(absoluteExpiresAt) {
		absoluteExpiresAt = tokens.ExpiresAt
	}
	idleExpiresAt := minimumTime(now.Add(idleTTL), absoluteExpiresAt)
	if !absoluteExpiresAt.After(now) || !idleExpiresAt.After(now) {
		return auth.SessionCredentials{}, fmt.Errorf("Console token set is already expired")
	}
	if _, err := s.db.ExecContext(ctx,
		`INSERT INTO astrasync_auth_sessions
            (session_hash, principal_id, encrypted_tokens, csrf_hash, revision,
             idle_expires_at, absolute_expires_at, created_at, updated_at)
         VALUES ($1, $2::uuid, $3, $4, 1, $5, $6, $7, $7)`,
		sessionHash, principalID, payload, csrfHash, idleExpiresAt, absoluteExpiresAt, now); err != nil {
		return auth.SessionCredentials{}, fmt.Errorf("persist Console session: %w", err)
	}
	return auth.SessionCredentials{SessionID: sessionID, CSRFToken: csrfToken, ExpiresAt: absoluteExpiresAt}, nil
}

func (s *SessionStore) ResolveConsoleSession(
	ctx context.Context, sessionID string, idleTTL time.Duration,
) (auth.ConsoleSession, error) {
	if idleTTL < time.Minute || idleTTL > 24*time.Hour {
		return auth.ConsoleSession{}, fmt.Errorf("Console session idle TTL is invalid")
	}
	sessionBytes, err := decodeCredential(sessionID)
	if err != nil {
		return auth.ConsoleSession{}, auth.ErrUnauthenticated
	}
	sessionHash := keyedHash(s.codec.sessionHashKey, sessionBytes)
	now := s.clock().UTC()
	var principalID string
	var payload, csrfHash []byte
	var revision int64
	var idleExpiresAt, absoluteExpiresAt time.Time
	err = s.db.QueryRowContext(ctx,
		`UPDATE astrasync_auth_sessions
            SET idle_expires_at = LEAST(absolute_expires_at, $2), updated_at = $1
          WHERE session_hash = $3 AND revoked_at IS NULL
            AND idle_expires_at > $1 AND absolute_expires_at > $1
      RETURNING principal_id::text, encrypted_tokens, csrf_hash, revision,
                idle_expires_at, absolute_expires_at`,
		now, now.Add(idleTTL), sessionHash,
	).Scan(&principalID, &payload, &csrfHash, &revision, &idleExpiresAt, &absoluteExpiresAt)
	if errors.Is(err, sql.ErrNoRows) {
		return auth.ConsoleSession{}, auth.ErrUnauthenticated
	}
	if err != nil {
		return auth.ConsoleSession{}, fmt.Errorf("resolve Console session: %w", err)
	}
	csrfToken := s.codec.csrfToken(sessionBytes)
	expectedCSRFHash := keyedHash(s.codec.csrfHashKey, []byte(csrfToken))
	if subtle.ConstantTimeCompare(csrfHash, expectedCSRFHash) != 1 {
		return auth.ConsoleSession{}, auth.ErrUnauthenticated
	}
	var tokens auth.ConsoleTokens
	if err := s.codec.decryptJSON(payload, appendAAD("session", sessionHash, []byte(principalID)), &tokens); err != nil || tokens.Validate() != nil {
		return auth.ConsoleSession{}, auth.ErrUnauthenticated
	}
	return auth.ConsoleSession{
		PrincipalID: principalID, Tokens: tokens, CSRFToken: csrfToken, Revision: revision,
		IdleExpiresAt: idleExpiresAt, AbsoluteExpiresAt: absoluteExpiresAt,
	}, nil
}

func (s *SessionStore) UpdateConsoleSessionTokens(
	ctx context.Context, sessionID, principalID string, expectedRevision int64,
	tokens auth.ConsoleTokens,
) (int64, error) {
	if _, err := uuid.Parse(principalID); err != nil || expectedRevision < 1 || tokens.Validate() != nil {
		return 0, fmt.Errorf("Console session token update is invalid")
	}
	sessionBytes, err := decodeCredential(sessionID)
	if err != nil {
		return 0, auth.ErrUnauthenticated
	}
	sessionHash := keyedHash(s.codec.sessionHashKey, sessionBytes)
	payload, err := s.codec.encryptJSON(tokens, appendAAD("session", sessionHash, []byte(principalID)))
	if err != nil {
		return 0, err
	}
	now := s.clock().UTC()
	var revision int64
	err = s.db.QueryRowContext(ctx,
		`UPDATE astrasync_auth_sessions
            SET encrypted_tokens = $1, revision = revision + 1,
                absolute_expires_at = CASE WHEN $2 THEN absolute_expires_at
                                           ELSE LEAST(absolute_expires_at, $3) END,
                updated_at = $4
          WHERE session_hash = $5 AND principal_id = $6::uuid AND revision = $7
            AND revoked_at IS NULL AND absolute_expires_at > $4
      RETURNING revision`, payload, tokens.RefreshToken != "", tokens.ExpiresAt,
		now, sessionHash, principalID, expectedRevision).Scan(&revision)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, auth.ErrSessionConflict
	}
	if err != nil {
		return 0, fmt.Errorf("update Console session tokens: %w", err)
	}
	return revision, nil
}

func (s *SessionStore) DeleteConsoleSession(ctx context.Context, sessionID string) error {
	sessionBytes, err := decodeCredential(sessionID)
	if err != nil {
		return nil
	}
	_, err = s.db.ExecContext(ctx, `DELETE FROM astrasync_auth_sessions WHERE session_hash = $1`,
		keyedHash(s.codec.sessionHashKey, sessionBytes))
	if err != nil {
		return fmt.Errorf("delete Console session: %w", err)
	}
	return nil
}

func (c sessionCodec) csrfToken(sessionID []byte) string {
	return base64.RawURLEncoding.EncodeToString(keyedHash(c.csrfTokenKey, sessionID))
}

func (c sessionCodec) encryptJSON(value any, additionalData []byte) ([]byte, error) {
	payload, err := json.Marshal(value)
	if err != nil || len(payload) == 0 || len(payload) > maximumEnvelopeBytes {
		return nil, fmt.Errorf("encode encrypted Console state")
	}
	block, err := aes.NewCipher(c.encryptionKey)
	if err != nil {
		return nil, fmt.Errorf("configure Console state encryption: %w", err)
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("configure Console state envelope: %w", err)
	}
	nonce := make([]byte, aead.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return nil, fmt.Errorf("generate Console state nonce: %w", err)
	}
	envelope := make([]byte, 1, 1+len(nonce)+len(payload)+aead.Overhead())
	envelope[0] = 1
	envelope = append(envelope, nonce...)
	envelope = aead.Seal(envelope, nonce, payload, additionalData)
	return envelope, nil
}

func (c sessionCodec) decryptJSON(envelope, additionalData []byte, destination any) error {
	block, err := aes.NewCipher(c.encryptionKey)
	if err != nil {
		return err
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return err
	}
	if len(envelope) < 1+aead.NonceSize()+aead.Overhead() || len(envelope) > maximumEnvelopeBytes || envelope[0] != 1 {
		return fmt.Errorf("encrypted Console state envelope is invalid")
	}
	nonce := envelope[1 : 1+aead.NonceSize()]
	payload, err := aead.Open(nil, nonce, envelope[1+aead.NonceSize():], additionalData)
	if err != nil {
		return fmt.Errorf("decrypt Console state")
	}
	if err := json.Unmarshal(payload, destination); err != nil {
		return fmt.Errorf("decode Console state")
	}
	return nil
}

func randomCredential() (string, []byte, error) {
	value := make([]byte, randomCredentialBytes)
	if _, err := io.ReadFull(rand.Reader, value); err != nil {
		return "", nil, fmt.Errorf("generate Console credential: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(value), value, nil
}

func decodeCredential(value string) ([]byte, error) {
	decoded, err := base64.RawURLEncoding.DecodeString(value)
	if err != nil || len(decoded) != randomCredentialBytes {
		return nil, auth.ErrUnauthenticated
	}
	return decoded, nil
}

func keyedHash(key, value []byte) []byte {
	mac := hmac.New(sha256.New, key)
	_, _ = mac.Write(value)
	return mac.Sum(nil)
}

func appendAAD(purpose string, values ...[]byte) []byte {
	result := []byte("astrasync/console/" + purpose + "/v1\x00")
	for _, value := range values {
		result = append(result, value...)
		result = append(result, 0)
	}
	return result
}

func minimumTime(left, right time.Time) time.Time {
	if left.Before(right) {
		return left
	}
	return right
}

var _ auth.ConsoleSessionStore = (*SessionStore)(nil)
