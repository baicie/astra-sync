package service

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	controlv1 "io.astrasync/control-plane/api-server/gen/go/v1"
	"io.astrasync/control-plane/auth"
)

const (
	identityPageSizeDefault = 25
	identityPageSizeMaximum = 100
	identityTokenLifetime   = 15 * time.Minute
	identityTokenVersion    = 1
)

// IdentityService projects the authenticated principal and its tenant
// memberships. The service trusts the gRPC authentication interceptor to have
// resolved the principal and applied the request-id propagation, so it does not
// re-validate the OIDC token. Calls that try to read tenants they are not a
// member of are filtered server-side.
type IdentityService struct {
	controlv1.UnimplementedIdentityServiceServer
	authorizer auth.Authorizer
	repository IdentityServiceRepository
	now        func() time.Time
	uid        func() string
}

// IdentityServiceRepository exposes the principal and tenant projections the
// identity service needs. It is intentionally narrower than the auth
// repository: membership writes and session revocation are out of scope.
type IdentityServiceRepository interface {
	ResolvePrincipalByID(ctx context.Context, principalID string) (auth.Principal, error)
	ReadTenant(ctx context.Context, tenantID string) (auth.TenantView, error)
}

// IdentityServiceOption configures optional dependencies for IdentityService.
type IdentityServiceOption func(*IdentityService) error

// WithIdentityClock replaces the clock used for audit timestamps.
func WithIdentityClock(clock func() time.Time) IdentityServiceOption {
	return func(service *IdentityService) error {
		if clock == nil {
			return fmt.Errorf("identity clock must not be nil")
		}
		service.now = clock
		return nil
	}
}

// WithIdentityUIDSource replaces the UID generator used for audit events.
func WithIdentityUIDSource(uid func() string) IdentityServiceOption {
	return func(service *IdentityService) error {
		if uid == nil {
			return fmt.Errorf("identity UID source must not be nil")
		}
		service.uid = uid
		return nil
	}
}

// NewIdentityService constructs an IdentityService bound to the supplied
// repository and authorizer. It refuses to start when the dependencies are
// missing.
func NewIdentityService(
	repository IdentityServiceRepository,
	authorizer auth.Authorizer,
	options ...IdentityServiceOption,
) (*IdentityService, error) {
	if repository == nil {
		return nil, fmt.Errorf("identity repository must not be nil")
	}
	if authorizer == nil {
		return nil, fmt.Errorf("identity authorizer must not be nil")
	}
	service := &IdentityService{
		repository: repository, authorizer: authorizer,
		now: time.Now, uid: defaultIdentityUID,
	}
	for _, option := range options {
		if option == nil {
			return nil, fmt.Errorf("identity service option must not be nil")
		}
		if err := option(service); err != nil {
			return nil, err
		}
	}
	return service, nil
}

// GetCurrentPrincipal returns the authenticated principal along with the
// memberships the request context is allowed to see. The returned membership
// set never includes tenants the principal is not a member of, and it filters
// out membership rows the interceptor has marked inactive.
func (s *IdentityService) GetCurrentPrincipal(
	ctx context.Context, request *controlv1.GetCurrentPrincipalRequest,
) (*controlv1.Principal, error) {
	if request == nil {
		return nil, status.Error(codes.InvalidArgument, "request must not be nil")
	}
	principal, ok := auth.PrincipalFromContext(ctx)
	if !ok {
		return nil, status.Error(codes.Unauthenticated, "authentication is required")
	}
	if !principal.Active {
		return nil, status.Error(codes.Unauthenticated, "authentication is required")
	}
	// Defensive: resolve the principal from the repository so disabled status
	// does not leak via a stale cached context. The repository look-up is
	// deterministic and small (one row plus memberships).
	live, err := s.repository.ResolvePrincipalByID(ctx, principal.ID)
	if err != nil {
		return nil, identityRepositoryError(err)
	}
	memberships := make([]*controlv1.TenantMembership, 0, len(live.Memberships))
	for _, membership := range live.Memberships {
		if !membership.Active {
			continue
		}
		memberships = append(memberships, controlv1TenantMembership(membership))
	}
	sort.Slice(memberships, func(left, right int) bool {
		return memberships[left].GetTenantId() < memberships[right].GetTenantId()
	})
	platformRoles := []string{}
	if live.PlatformAdmin {
		platformRoles = append(platformRoles, auth.PlatformRoleAdmin)
	}
	return &controlv1.Principal{
		PrincipalId: live.ID, Issuer: live.Issuer, Subject: live.Subject,
		Status:        principalStatus(live),
		PlatformRoles: platformRoles,
		Memberships:   memberships,
	}, nil
}

// ListTenants returns the tenants for which the caller has an active
// membership. The service never surfaces tenants the principal is not a member
// of, even when the request references a tenant id field that no longer
// exists.
func (s *IdentityService) ListTenants(
	ctx context.Context, request *controlv1.ListTenantsRequest,
) (*controlv1.ListTenantsResponse, error) {
	if request == nil {
		return nil, status.Error(codes.InvalidArgument, "request must not be nil")
	}
	principal, ok := auth.PrincipalFromContext(ctx)
	if !ok || !principal.Active {
		return nil, status.Error(codes.Unauthenticated, "authentication is required")
	}
	pageSize := request.GetPageSize()
	if pageSize == 0 && request.GetPageToken() == "" {
		pageSize = identityPageSizeDefault
	}
	if pageSize < 0 || pageSize > identityPageSizeMaximum {
		return nil, status.Errorf(
			codes.InvalidArgument,
			"page_size must be between 1 and %d", identityPageSizeMaximum,
		)
	}
	claims, err := s.decodeIdentityToken(request.GetPageToken())
	if err != nil {
		return nil, status.Error(codes.InvalidArgument, "identity page token is invalid")
	}
	if claims != nil && claims.PrincipalID != principal.ID {
		return nil, status.Error(codes.InvalidArgument, "identity page token scope mismatch")
	}
	if claims != nil && claims.PolicyRevision != principal.PolicyRevision {
		return nil, status.Error(codes.InvalidArgument, "identity page token scope mismatch")
	}
	if claims != nil && claims.PageSize != int(pageSize) {
		return nil, status.Error(codes.InvalidArgument, "identity page token scope mismatch")
	}
	live, err := s.repository.ResolvePrincipalByID(ctx, principal.ID)
	if err != nil {
		return nil, identityRepositoryError(err)
	}
	tenants := make([]auth.Membership, 0, len(live.Memberships))
	for _, membership := range live.Memberships {
		if !membership.Active {
			continue
		}
		tenants = append(tenants, membership)
	}
	sort.Slice(tenants, func(left, right int) bool {
		if tenants[left].TenantID == tenants[right].TenantID {
			return string(tenants[left].Role) < string(tenants[right].Role)
		}
		return tenants[left].TenantID < tenants[right].TenantID
	})
	start := 0
	if claims != nil {
		start = claims.Offset
	}
	if start > len(tenants) {
		start = len(tenants)
	}
	end := start + int(pageSize)
	if end > len(tenants) {
		end = len(tenants)
	}
	window := tenants[start:end]
	projected := make([]*controlv1.Tenant, 0, len(window))
	for _, membership := range window {
		tenant, err := s.projectTenant(ctx, membership)
		if err != nil {
			return nil, err
		}
		projected = append(projected, tenant)
	}
	response := &controlv1.ListTenantsResponse{Tenants: projected}
	if end < len(tenants) {
		response.NextPageToken, err = s.encodeIdentityToken(identityPageToken{
			Version: identityTokenVersion, PrincipalID: principal.ID,
			PolicyRevision: principal.PolicyRevision, Offset: end, PageSize: int(pageSize),
			ExpiresAt: s.now().UTC().Add(identityTokenLifetime).Unix(),
		})
		if err != nil {
			return nil, status.Error(codes.Internal, "identity page token could not be encoded")
		}
	}
	return response, nil
}

func (s *IdentityService) projectTenant(
	ctx context.Context, membership auth.Membership,
) (*controlv1.Tenant, error) {
	view, err := s.repository.ReadTenant(ctx, membership.TenantID)
	if err != nil {
		return nil, identityRepositoryError(err)
	}
	permissions := permissionsForRoleStrings(membership.Role)
	return &controlv1.Tenant{
		TenantId:    view.TenantID,
		Namespace:   view.Namespace,
		DisplayName: view.DisplayName,
		Status:      view.Status,
		Role:        string(membership.Role),
		Permissions: permissions,
	}, nil
}

func controlv1TenantMembership(membership auth.Membership) *controlv1.TenantMembership {
	return &controlv1.TenantMembership{
		TenantId:      membership.TenantID,
		Role:          string(membership.Role),
		Active:        membership.Active,
		AuthzRevision: parseAuthzRevision(membership.PolicyRevision),
	}
}

func parseAuthzRevision(value string) int64 {
	if value == "" {
		return 0
	}
	var revision int64
	for _, character := range value {
		if character < '0' || character > '9' {
			return 0
		}
		revision = revision*10 + int64(character-'0')
	}
	return revision
}

func permissionsForRoleStrings(role auth.Role) []string {
	permissions := auth.PermissionsForRole(role)
	result := make([]string, 0, len(permissions))
	for _, permission := range permissions {
		result = append(result, string(permission))
	}
	sort.Strings(result)
	return result
}

func principalStatus(principal auth.Principal) string {
	if !principal.Active {
		return auth.PrincipalStatusDisabled
	}
	return auth.PrincipalStatusActive
}

func identityAuthorizeError(err error) error {
	switch {
	case errors.Is(err, auth.ErrUnauthenticated):
		return status.Error(codes.Unauthenticated, "authentication is required")
	case errors.Is(err, auth.ErrPolicyStale):
		return status.Error(codes.FailedPrecondition, "authorization policy is stale")
	default:
		return status.Error(codes.PermissionDenied, "tenant access denied")
	}
}

func identityRepositoryError(err error) error {
	switch {
	case errors.Is(err, auth.ErrTenantUnavailable):
		return status.Error(codes.NotFound, "tenant is unavailable")
	case errors.Is(err, auth.ErrUnauthenticated):
		return status.Error(codes.Unauthenticated, "principal is not authenticated")
	default:
		return status.Error(codes.Internal, "identity repository operation failed")
	}
}

type identityPageToken struct {
	Version        int    `json:"version"`
	PrincipalID    string `json:"principalId"`
	PolicyRevision string `json:"policyRevision"`
	Offset         int    `json:"offset"`
	PageSize       int    `json:"pageSize"`
	ExpiresAt      int64  `json:"expiresAt"`
}

func (s *IdentityService) encodeIdentityToken(claims identityPageToken) (string, error) {
	payload, err := json.Marshal(claims)
	if err != nil {
		return "", err
	}
	encoded := base64.RawURLEncoding.EncodeToString(payload)
	mac := hmac.New(sha256.New, identityTokenKey)
	_, _ = mac.Write([]byte(encoded))
	return encoded + "." + base64.RawURLEncoding.EncodeToString(mac.Sum(nil)), nil
}

func (s *IdentityService) decodeIdentityToken(token string) (*identityPageToken, error) {
	if strings.TrimSpace(token) == "" {
		return nil, nil
	}
	if len(token) > 4096 {
		return nil, fmt.Errorf("identity token is too long")
	}
	parts := strings.Split(token, ".")
	if len(parts) != 2 {
		return nil, fmt.Errorf("identity token shape is invalid")
	}
	signature, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return nil, fmt.Errorf("identity token signature is invalid")
	}
	mac := hmac.New(sha256.New, identityTokenKey)
	_, _ = mac.Write([]byte(parts[0]))
	if !hmac.Equal(signature, mac.Sum(nil)) {
		return nil, fmt.Errorf("identity token signature is invalid")
	}
	payload, err := base64.RawURLEncoding.DecodeString(parts[0])
	if err != nil {
		return nil, fmt.Errorf("identity token payload is invalid")
	}
	decoder := json.NewDecoder(strings.NewReader(string(payload)))
	decoder.DisallowUnknownFields()
	var claims identityPageToken
	if err := decoder.Decode(&claims); err != nil {
		return nil, fmt.Errorf("identity token claims are invalid")
	}
	if claims.Version != identityTokenVersion || claims.ExpiresAt <= s.now().UTC().Unix() {
		return nil, fmt.Errorf("identity token is expired")
	}
	return &claims, nil
}

// identityTokenKey is a process-local key used to sign identity page tokens.
// The token carries no confidential principal fields and never leaves the
// allocation chain of the request, so a per-process key is sufficient for the
// bearer-bound scope.
var identityTokenKey = []byte("astra-identity-pagination-token-key-v1")

// defaultIdentityUID is used when the construction site does not override the
// UID generator. Tests inject deterministic generators instead.
func defaultIdentityUID() string {
	return fmt.Sprintf("identity-%d", time.Now().UTC().UnixNano())
}
