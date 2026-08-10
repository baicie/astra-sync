package auth

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"
)

const maximumStoredTokenBytes = 20 * 1024

var ErrSessionConflict = errors.New("console session revision conflict")

type ConsoleTokens struct {
	AccessToken  string    `json:"access_token"`
	RefreshToken string    `json:"refresh_token,omitempty"`
	TokenType    string    `json:"token_type"`
	ExpiresAt    time.Time `json:"expires_at"`
}

func (t ConsoleTokens) Validate() error {
	if strings.TrimSpace(t.AccessToken) == "" || len(t.AccessToken) > maximumStoredTokenBytes ||
		len(t.RefreshToken) > maximumStoredTokenBytes || !strings.EqualFold(t.TokenType, "Bearer") ||
		t.ExpiresAt.IsZero() {
		return fmt.Errorf("console token set is invalid")
	}
	return nil
}

type LoginTransaction struct {
	Nonce        string `json:"nonce"`
	CodeVerifier string `json:"code_verifier"`
	ReturnTo     string `json:"return_to"`
}

func (t LoginTransaction) Validate() error {
	if len(t.Nonce) < 32 || len(t.Nonce) > 256 || len(t.CodeVerifier) < 43 ||
		len(t.CodeVerifier) > 128 || len(t.ReturnTo) == 0 || len(t.ReturnTo) > 2048 {
		return fmt.Errorf("OIDC login transaction is invalid")
	}
	return nil
}

type LoginCredentials struct {
	State          string
	BrowserBinding string
	ExpiresAt      time.Time
}

type SessionCredentials struct {
	SessionID string
	CSRFToken string
	ExpiresAt time.Time
}

type ConsoleSession struct {
	PrincipalID       string
	Tokens            ConsoleTokens
	CSRFToken         string
	Revision          int64
	IdleExpiresAt     time.Time
	AbsoluteExpiresAt time.Time
}

type ConsoleSessionStore interface {
	CreateLoginTransaction(context.Context, LoginTransaction, time.Duration) (LoginCredentials, error)
	ConsumeLoginTransaction(context.Context, string, string) (LoginTransaction, error)
	CreateConsoleSession(context.Context, string, ConsoleTokens, time.Duration, time.Duration) (SessionCredentials, error)
	ResolveConsoleSession(context.Context, string, time.Duration) (ConsoleSession, error)
	UpdateConsoleSessionTokens(context.Context, string, string, int64, ConsoleTokens) (int64, error)
	DeleteConsoleSession(context.Context, string) error
}
