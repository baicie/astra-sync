package auth

import (
	"context"
	"fmt"
)

type IdentityResolver interface {
	ResolveOrCreatePrincipal(context.Context, ExternalIdentity) (Principal, error)
}

type BearerAuthenticator struct {
	Validator *OIDCValidator
	Resolver  IdentityResolver
}

func (a BearerAuthenticator) Authenticate(ctx context.Context, token string) (Principal, error) {
	if a.Validator == nil || a.Resolver == nil {
		return Principal{}, fmt.Errorf("bearer authenticator is not configured")
	}
	identity, err := a.Validator.Validate(ctx, token)
	if err != nil {
		return Principal{}, ErrUnauthenticated
	}
	principal, err := a.Resolver.ResolveOrCreatePrincipal(ctx, identity)
	if err != nil {
		return Principal{}, fmt.Errorf("resolve authenticated principal: %w", err)
	}
	if !principal.Active {
		return Principal{}, ErrUnauthenticated
	}
	return principal, nil
}
