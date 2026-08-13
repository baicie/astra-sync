# Slice 18.3 Console Backend-for-Frontend Integration Tests

## Purpose

The Console BFF must satisfy the Slice 18 authentication boundary without
introducing a parallel authorization policy. These tests verify that:

- A session with a single active tenant membership is auto-selected without
  browser-supplied tenant input.
- A session with multiple active memberships must receive an explicit
  `X-Astra-Tenant-ID` selection. Auto-picking is rejected to prevent scope
  confusion.
- Inactive memberships are ignored: the BFF refuses to forward an
  authenticated backend request when no candidate tenant is selectable.
- Mutating endpoints fail closed when CSRF or same-origin requirements are
  not met; the backend is never invoked.
- The `/api/session` endpoint never leaks access or refresh tokens, neither in
  the response body nor in any response header.
- Unauthenticated requests to read endpoints (audit, jobs) return
  `UNAUTHENTICATED` and never reach the backend.
- Logout revokes the server-side session before the cookie is cleared, so a
  captured cookie cannot outlive the user's intent.

## Coverage Matrix

| Behaviour | Test |
|---|---|
| Sole membership auto-selection | `TestBFFAutoSelectsSoleMembership` |
| Multi-membership explicit selection | `TestBFFRequiresExplicitSelectionWhenMultipleMemberships` |
| Inactive memberships ignored | `TestBFFIgnoresInactiveMemberships` |
| CSRF rejection on mutation | `TestBFFMutationRequiresValidCSRF` |
| Token never exposed via `/api/session` | `TestBFFSessionEndpointDoesNotExposeTokens` |
| Unauthenticated audit denial | `TestBFFAuthenticationRequiredForAudit` |
| Logout order (revoke → cookie) | `TestBFFPreservesSessionIdentityOnLogout` |

The previously delivered coverage is preserved:

| Behaviour | Test |
|---|---|
| Bearer token never reaches the browser | `TestBFFSessionAndTenantScopeNeverExposeBearer` |
| Origin + CSRF + redacted locator on Connection create | `TestBFFConnectionMutationRequiresOriginCSRFAndRedactsLocator` |
| Raw secret rejection and CAS conflict mapping | `TestBFFRejectsRawSecretAndMapsCASConflict` |
| Audit BFF derives tenant + bounded filters | `TestBFFAuditQueryDerivesTenantAndForwardsBoundedFilters` |
| Audit BFF enforces `audit.read` permission | `TestBFFAuditQueryRejectsMembershipWithoutAuditPermission` |

## Test Harness

The BFF tests use an in-process `httptest.Server` plus the production
`server.NewWithConfig` entry point. The `fakeBFFBackend` records every
backend call so that tests can assert that no backend traffic occurred
during denial paths. `fakeSessions` resolves a fixed opaque session ID; the
helper `bffRequest` automatically attaches the session cookie so each test
focuses on a single property.

## Future Coverage

- IdP discovery / JWKS rotation tests require a live OIDC mock and are out
  of scope for these in-process tests. They are covered by the Slice 21
  PostgreSQL integration suite plus manual staging validation.
- Cross-origin browser fetch behavior is covered by the CSP test fixtures
  shipped with the static web assets and is not repeated here.
