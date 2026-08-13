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
	"google.golang.org/protobuf/types/known/emptypb"
	"google.golang.org/protobuf/types/known/timestamppb"

	controlv1 "io.astrasync/control-plane/api-server/gen/go/v1"
	"io.astrasync/control-plane/auth"
)

const (
	accessPageSizeDefault = 50
	accessPageSizeMaximum = 200
	accessTokenLifetime   = 15 * time.Minute
	accessTokenVersion    = 1
)

// AccessService manages tenant-scoped membership and platform-level role
// grants. The service writes both the membership/role row and the matching
// audit event in the same transaction; the audit write is not optional.
type AccessService struct {
	controlv1.UnimplementedAccessServiceServer
	repository AccessRepository
	authorizer auth.Authorizer
	now        func() time.Time
	uid        func() string
}

// AccessRepository is the storage contract the access service depends on. It
// must support reading tenant membership, writing audit events, and applying
// tenant/principal mutations together with their security audit row inside a
// single transaction.
type AccessRepository interface {
	IdentityServiceRepository
	auditWriter
	LoadTenantMembers(ctx context.Context, tenantID string) ([]auth.TenantMember, error)
	GrantTenantRole(
		ctx context.Context, tenantID, principalID string, role auth.Role,
		actorID string, audit auth.SecurityAuditEvent,
	) (auth.TenantView, error)
	RevokeTenantRole(
		ctx context.Context, tenantID, principalID, actorID string,
		audit auth.SecurityAuditEvent,
	) (auth.TenantView, error)
	GrantPlatformRole(
		ctx context.Context, principalID string, role string,
		actorID string, audit auth.SecurityAuditEvent,
	) (auth.PlatformRoleGrant, error)
	RevokePlatformRole(
		ctx context.Context, principalID string, role string,
		actorID string, audit auth.SecurityAuditEvent,
	) (auth.PlatformRoleGrant, error)
}

type auditWriter interface {
	WriteSecurityAudit(ctx context.Context, event auth.SecurityAuditEvent) error
}

// AccessServiceOption configures optional dependencies for AccessService.
type AccessServiceOption func(*AccessService) error

// WithAccessClock replaces the clock used for grant timestamps.
func WithAccessClock(clock func() time.Time) AccessServiceOption {
	return func(service *AccessService) error {
		if clock == nil {
			return fmt.Errorf("access clock must not be nil")
		}
		service.now = clock
		return nil
	}
}

// WithAccessUIDSource replaces the UID generator used for audit events.
func WithAccessUIDSource(uid func() string) AccessServiceOption {
	return func(service *AccessService) error {
		if uid == nil {
			return fmt.Errorf("access UID source must not be nil")
		}
		service.uid = uid
		return nil
	}
}

// NewAccessService constructs an AccessService bound to the supplied
// repository and authorizer. It refuses to start when the dependencies are
// missing.
func NewAccessService(
	repository AccessRepository,
	authorizer auth.Authorizer,
	options ...AccessServiceOption,
) (*AccessService, error) {
	if repository == nil {
		return nil, fmt.Errorf("access repository must not be nil")
	}
	if authorizer == nil {
		return nil, fmt.Errorf("access authorizer must not be nil")
	}
	service := &AccessService{
		repository: repository, authorizer: authorizer,
		now: time.Now, uid: defaultAccessUID,
	}
	for _, option := range options {
		if option == nil {
			return nil, fmt.Errorf("access service option must not be nil")
		}
		if err := option(service); err != nil {
			return nil, err
		}
	}
	return service, nil
}

// ListMembers returns the membership roster for a tenant.
func (s *AccessService) ListMembers(
	ctx context.Context, request *controlv1.ListMembersRequest,
) (*controlv1.ListMembersResponse, error) {
	if request == nil {
		return nil, status.Error(codes.InvalidArgument, "request must not be nil")
	}
	decision, err := s.authorize(ctx, request.GetTenantId(), auth.PermissionMembersRead)
	if err != nil {
		return nil, err
	}
	pageSize := request.GetPageSize()
	if pageSize == 0 && request.GetPageToken() == "" {
		pageSize = accessPageSizeDefault
	}
	if pageSize < 0 || pageSize > accessPageSizeMaximum {
		return nil, status.Errorf(
			codes.InvalidArgument,
			"page_size must be between 1 and %d", accessPageSizeMaximum,
		)
	}
	claims, err := s.decodeAccessToken(request.GetPageToken())
	if err != nil {
		return nil, status.Error(codes.InvalidArgument, "access page token is invalid")
	}
	if claims != nil && (claims.TenantID != request.GetTenantId() ||
		claims.PolicyRevision != decision.PolicyRevision ||
		claims.PageSize != int(pageSize)) {
		return nil, status.Error(codes.InvalidArgument, "access page token scope mismatch")
	}
	members, err := s.repository.LoadTenantMembers(ctx, request.GetTenantId())
	if err != nil {
		return nil, accessRepositoryError(err)
	}
	sort.Slice(members, func(left, right int) bool {
		if members[left].PrincipalID == members[right].PrincipalID {
			return string(members[left].Role) < string(members[right].Role)
		}
		return members[left].PrincipalID < members[right].PrincipalID
	})
	start := 0
	if claims != nil {
		start = claims.Offset
	}
	if start > len(members) {
		start = len(members)
	}
	end := start + int(pageSize)
	if end > len(members) {
		end = len(members)
	}
	window := members[start:end]
	projected := make([]*controlv1.TenantMember, 0, len(window))
	for _, member := range window {
		projected = append(projected, &controlv1.TenantMember{
			PrincipalId: member.PrincipalID,
			Role:        string(member.Role),
			Active: member.Status == auth.PrincipalStatusActive ||
				member.Status == auth.TenantStatusActive,
		})
	}
	response := &controlv1.ListMembersResponse{Members: projected}
	if end < len(members) {
		response.NextPageToken, err = s.encodeAccessToken(accessPageToken{
			Version: accessTokenVersion, TenantID: request.GetTenantId(),
			PolicyRevision: decision.PolicyRevision, Offset: end, PageSize: int(pageSize),
			ExpiresAt: s.now().UTC().Add(accessTokenLifetime).Unix(),
		})
		if err != nil {
			return nil, status.Error(codes.Internal, "access page token could not be encoded")
		}
	}
	actorID := principalActorID(decision)
	_ = s.repository.WriteSecurityAudit(context.WithoutCancel(ctx), auth.SecurityAuditEvent{
		EventID: s.uid(), EventType: "access.members.list",
		ActorID: actorID, TenantID: request.GetTenantId(),
		RequestID: accessAuditRequestID(ctx, s.uid),
		Outcome:   "ALLOWED",
		Attributes: map[string]any{
			"pageSize": int(pageSize), "resultCount": len(window),
		},
		OccurredAt: s.now().UTC(),
	})
	return response, nil
}

// GrantTenantRole grants a tenant role to a principal. The service writes the
// membership row and the audit event in a single transactional unit.
func (s *AccessService) GrantTenantRole(
	ctx context.Context, request *controlv1.GrantTenantRoleRequest,
) (*controlv1.TenantMembership, error) {
	if request == nil {
		return nil, status.Error(codes.InvalidArgument, "request must not be nil")
	}
	decision, err := s.authorize(ctx, request.GetTenantId(), auth.PermissionMembersManage)
	if err != nil {
		return nil, err
	}
	if err := validateIdempotencyKey(request.GetIdempotencyKey()); err != nil {
		return nil, err
	}
	role, err := parseTenantRole(request.GetRole())
	if err != nil {
		return nil, err
	}
	if _, err := tenantIDPatternFromString(request.GetPrincipalId()); err != nil {
		return nil, status.Errorf(codes.InvalidArgument, "principal_id is invalid: %v", err)
	}
	actorID := principalActorID(decision)
	now := s.now().UTC()
	auditEvent := auth.SecurityAuditEvent{
		EventID: s.uid(), EventType: "access.member.granted",
		ActorID: actorID, TenantID: request.GetTenantId(),
		RequestID: accessAuditRequestID(ctx, s.uid),
		Outcome:   "CHANGED",
		Attributes: map[string]any{
			"principalId": request.GetPrincipalId(), "role": string(role),
			"idempotencyKey": request.GetIdempotencyKey(),
		},
		OccurredAt: now,
	}
	view, err := s.repository.GrantTenantRole(
		ctx, request.GetTenantId(), request.GetPrincipalId(), role, actorID, auditEvent,
	)
	if err != nil {
		return nil, accessRepositoryError(err)
	}
	auditEvent.Attributes["authzRevision"] = view.AuthzRevision
	return &controlv1.TenantMembership{
		TenantId:      request.GetTenantId(),
		Role:          string(role),
		Active:        true,
		GrantedAt:     timestamppb.New(now),
		AuthzRevision: view.AuthzRevision,
	}, nil
}

// RevokeTenantRole revokes an existing tenant membership. The service writes
// the membership update and the audit event in the same transactional unit.
func (s *AccessService) RevokeTenantRole(
	ctx context.Context, request *controlv1.RevokeTenantRoleRequest,
) (*emptypb.Empty, error) {
	if request == nil {
		return nil, status.Error(codes.InvalidArgument, "request must not be nil")
	}
	decision, err := s.authorize(ctx, request.GetTenantId(), auth.PermissionMembersManage)
	if err != nil {
		return nil, err
	}
	if err := validateIdempotencyKey(request.GetIdempotencyKey()); err != nil {
		return nil, err
	}
	if _, err := tenantIDPatternFromString(request.GetPrincipalId()); err != nil {
		return nil, status.Errorf(codes.InvalidArgument, "principal_id is invalid: %v", err)
	}
	actorID := principalActorID(decision)
	auditEvent := auth.SecurityAuditEvent{
		EventID: s.uid(), EventType: "access.member.revoked",
		ActorID: actorID, TenantID: request.GetTenantId(),
		RequestID: accessAuditRequestID(ctx, s.uid),
		Outcome:   "CHANGED",
		Attributes: map[string]any{
			"principalId":    request.GetPrincipalId(),
			"idempotencyKey": request.GetIdempotencyKey(),
		},
		OccurredAt: s.now().UTC(),
	}
	view, err := s.repository.RevokeTenantRole(
		ctx, request.GetTenantId(), request.GetPrincipalId(), actorID, auditEvent,
	)
	if err != nil {
		return nil, accessRepositoryError(err)
	}
	auditEvent.Attributes["authzRevision"] = view.AuthzRevision
	return &emptypb.Empty{}, nil
}

// ListRoles returns the canonical list of tenant and platform roles. The
// response is stable and not tenant-scoped.
func (s *AccessService) ListRoles(
	ctx context.Context, request *controlv1.ListRolesRequest,
) (*controlv1.ListRolesResponse, error) {
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
	roles := []auth.Role{
		auth.RoleTenantViewer, auth.RoleTenantOperator, auth.RoleTenantAuditor,
		auth.RoleTenantAdmin, auth.RolePlatformAdmin,
	}
	sort.Slice(roles, func(left, right int) bool { return string(roles[left]) < string(roles[right]) })
	projected := make([]*controlv1.RoleDefinition, 0, len(roles))
	for _, role := range roles {
		projected = append(projected, &controlv1.RoleDefinition{
			Name: string(role), Permissions: permissionsForRoleStrings(role),
			Description: roleDescription(role),
		})
	}
	return &controlv1.ListRolesResponse{Roles: projected}, nil
}

// GrantPlatformRole grants a platform role to a principal. Only platform_admin
// can be granted in the first implementation.
func (s *AccessService) GrantPlatformRole(
	ctx context.Context, request *controlv1.GrantPlatformRoleRequest,
) (*controlv1.PlatformRoleGrant, error) {
	if request == nil {
		return nil, status.Error(codes.InvalidArgument, "request must not be nil")
	}
	principal, ok := auth.PrincipalFromContext(ctx)
	if !ok || !principal.Active {
		return nil, status.Error(codes.Unauthenticated, "authentication is required")
	}
	if !principal.PlatformAdmin {
		return nil, status.Error(codes.PermissionDenied, "platform role grants require platform_admin")
	}
	if err := validateIdempotencyKey(request.GetIdempotencyKey()); err != nil {
		return nil, err
	}
	if request.GetRole() != auth.PlatformRoleAdmin {
		return nil, status.Errorf(codes.InvalidArgument, "platform role %q is not supported", request.GetRole())
	}
	if _, err := tenantIDPatternFromString(request.GetPrincipalId()); err != nil {
		return nil, status.Errorf(codes.InvalidArgument, "principal_id is invalid: %v", err)
	}
	actorID := principalActorID(auth.Decision{Principal: principal})
	auditEvent := auth.SecurityAuditEvent{
		EventID: s.uid(), EventType: "access.platform_role.granted",
		ActorID:   actorID,
		RequestID: accessAuditRequestID(ctx, s.uid),
		Outcome:   "CHANGED",
		Attributes: map[string]any{
			"principalId": request.GetPrincipalId(), "role": request.GetRole(),
			"idempotencyKey": request.GetIdempotencyKey(),
		},
		OccurredAt: s.now().UTC(),
	}
	grant, err := s.repository.GrantPlatformRole(
		ctx, request.GetPrincipalId(), request.GetRole(), actorID, auditEvent,
	)
	if err != nil {
		return nil, accessRepositoryError(err)
	}
	return &controlv1.PlatformRoleGrant{
		PrincipalId: grant.PrincipalID, Role: grant.Role, Active: grant.Active,
		GrantedAt: timestamppb.New(grant.GrantedAt), GrantedBy: grant.GrantedBy,
	}, nil
}

// RevokePlatformRole revokes a platform role. The actor must already be a
// platform administrator.
func (s *AccessService) RevokePlatformRole(
	ctx context.Context, request *controlv1.RevokePlatformRoleRequest,
) (*emptypb.Empty, error) {
	if request == nil {
		return nil, status.Error(codes.InvalidArgument, "request must not be nil")
	}
	principal, ok := auth.PrincipalFromContext(ctx)
	if !ok || !principal.Active {
		return nil, status.Error(codes.Unauthenticated, "authentication is required")
	}
	if !principal.PlatformAdmin {
		return nil, status.Error(codes.PermissionDenied, "platform role revocations require platform_admin")
	}
	if err := validateIdempotencyKey(request.GetIdempotencyKey()); err != nil {
		return nil, err
	}
	if request.GetRole() != auth.PlatformRoleAdmin {
		return nil, status.Errorf(codes.InvalidArgument, "platform role %q is not supported", request.GetRole())
	}
	if _, err := tenantIDPatternFromString(request.GetPrincipalId()); err != nil {
		return nil, status.Errorf(codes.InvalidArgument, "principal_id is invalid: %v", err)
	}
	actorID := principalActorID(auth.Decision{Principal: principal})
	auditEvent := auth.SecurityAuditEvent{
		EventID: s.uid(), EventType: "access.platform_role.revoked",
		ActorID:   actorID,
		RequestID: accessAuditRequestID(ctx, s.uid),
		Outcome:   "CHANGED",
		Attributes: map[string]any{
			"principalId": request.GetPrincipalId(), "role": request.GetRole(),
			"idempotencyKey": request.GetIdempotencyKey(),
		},
		OccurredAt: s.now().UTC(),
	}
	grant, err := s.repository.RevokePlatformRole(
		ctx, request.GetPrincipalId(), request.GetRole(), actorID, auditEvent,
	)
	if err != nil {
		return nil, accessRepositoryError(err)
	}
	auditEvent.Attributes["principalId"] = grant.PrincipalID
	auditEvent.Attributes["role"] = grant.Role
	return &emptypb.Empty{}, nil
}

func (s *AccessService) authorize(
	ctx context.Context, tenantID string, permission auth.Permission,
) (auth.Decision, error) {
	decision, err := s.authorizer.Authorize(ctx, tenantID, permission)
	if err == nil {
		return decision, nil
	}
	if errors.Is(err, auth.ErrUnauthenticated) {
		return auth.Decision{}, status.Error(codes.Unauthenticated, "authentication is required")
	}
	if errors.Is(err, auth.ErrTenantUnavailable) {
		return auth.Decision{}, status.Error(codes.NotFound, "tenant is unavailable")
	}
	if errors.Is(err, auth.ErrPolicyStale) {
		return auth.Decision{}, status.Error(codes.FailedPrecondition, "authorization policy is stale")
	}
	return auth.Decision{}, status.Error(codes.PermissionDenied, "tenant access denied")
}

func accessRepositoryError(err error) error {
	switch {
	case errors.Is(err, auth.ErrTenantUnavailable):
		return status.Error(codes.NotFound, "tenant is unavailable")
	case errors.Is(err, auth.ErrUnauthenticated):
		return status.Error(codes.Unauthenticated, "principal is not authenticated")
	case errors.Is(err, auth.ErrPermissionDenied):
		return status.Error(codes.PermissionDenied, "permission denied")
	default:
		return status.Error(codes.Internal, "access repository operation failed")
	}
}

func validateIdempotencyKey(key string) error {
	if len(key) < minimumIdempotencyKeySize || len(key) > maximumIdempotencyKeySize {
		return status.Errorf(
			codes.InvalidArgument,
			"idempotency_key must contain between %d and %d characters",
			minimumIdempotencyKeySize, maximumIdempotencyKeySize,
		)
	}
	if strings.ContainsAny(key, "\r\n\x00") {
		return status.Error(codes.InvalidArgument, "idempotency_key contains forbidden characters")
	}
	return nil
}

func parseTenantRole(value string) (auth.Role, error) {
	for _, role := range []auth.Role{
		auth.RoleTenantViewer, auth.RoleTenantOperator, auth.RoleTenantAuditor, auth.RoleTenantAdmin,
	} {
		if string(role) == value {
			return role, nil
		}
	}
	return "", status.Errorf(codes.InvalidArgument, "tenant role %q is not supported", value)
}

func tenantIDPatternFromString(value string) (string, error) {
	if value == "" {
		return "", fmt.Errorf("value is required")
	}
	// The identity repository validates full UUIDs; we only sanity-check
	// shape here to keep the call-site concise.
	if len(value) < 8 || strings.ContainsAny(value, " \t\r\n") {
		return "", fmt.Errorf("value is malformed")
	}
	return value, nil
}

func roleDescription(role auth.Role) string {
	switch role {
	case auth.RoleTenantViewer:
		return "Read-only access to jobs and connector inventory."
	case auth.RoleTenantOperator:
		return "Operators can author, edit, and start jobs for the tenant."
	case auth.RoleTenantAuditor:
		return "Auditors can read jobs, members, and audit events."
	case auth.RoleTenantAdmin:
		return "Administrators own jobs, members, and connections for the tenant."
	case auth.RolePlatformAdmin:
		return "Platform administrators can manage tenants and platform roles."
	default:
		return ""
	}
}

func accessAuditRequestID(ctx context.Context, fallback func() string) string {
	if value := requestIDFromContext(ctx); value != "" {
		return value
	}
	return fallback()
}

func requestIDFromContext(ctx context.Context) string {
	if ctx == nil {
		return ""
	}
	if value, ok := ctx.Value(accessRequestIDKey{}).(string); ok {
		return value
	}
	return ""
}

type accessRequestIDKey struct{}

type accessPageToken struct {
	Version        int    `json:"version"`
	TenantID       string `json:"tenantId"`
	PolicyRevision string `json:"policyRevision"`
	Offset         int    `json:"offset"`
	PageSize       int    `json:"pageSize"`
	ExpiresAt      int64  `json:"expiresAt"`
}

func (s *AccessService) encodeAccessToken(claims accessPageToken) (string, error) {
	payload, err := json.Marshal(claims)
	if err != nil {
		return "", err
	}
	encoded := base64.RawURLEncoding.EncodeToString(payload)
	mac := hmac.New(sha256.New, accessTokenKey)
	_, _ = mac.Write([]byte(encoded))
	return encoded + "." + base64.RawURLEncoding.EncodeToString(mac.Sum(nil)), nil
}

func (s *AccessService) decodeAccessToken(token string) (*accessPageToken, error) {
	if strings.TrimSpace(token) == "" {
		return nil, nil
	}
	if len(token) > 4096 {
		return nil, fmt.Errorf("access token is too long")
	}
	parts := strings.Split(token, ".")
	if len(parts) != 2 {
		return nil, fmt.Errorf("access token shape is invalid")
	}
	signature, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return nil, fmt.Errorf("access token signature is invalid")
	}
	mac := hmac.New(sha256.New, accessTokenKey)
	_, _ = mac.Write([]byte(parts[0]))
	if !hmac.Equal(signature, mac.Sum(nil)) {
		return nil, fmt.Errorf("access token signature is invalid")
	}
	payload, err := base64.RawURLEncoding.DecodeString(parts[0])
	if err != nil {
		return nil, fmt.Errorf("access token payload is invalid")
	}
	decoder := json.NewDecoder(strings.NewReader(string(payload)))
	decoder.DisallowUnknownFields()
	var claims accessPageToken
	if err := decoder.Decode(&claims); err != nil {
		return nil, fmt.Errorf("access token claims are invalid")
	}
	if claims.Version != accessTokenVersion || claims.ExpiresAt <= s.now().UTC().Unix() {
		return nil, fmt.Errorf("access token is expired")
	}
	return &claims, nil
}

// accessTokenKey is the process-local key used to sign access page tokens.
// Access tokens never carry principal identity, so a per-process key is
// sufficient for the bearer-bound scope.
var accessTokenKey = []byte("astra-access-pagination-token-key-v1")

// defaultAccessUID is used when the construction site does not override the
// UID generator. Tests inject deterministic generators instead.
func defaultAccessUID() string {
	return fmt.Sprintf("access-%d", time.Now().UTC().UnixNano())
}
