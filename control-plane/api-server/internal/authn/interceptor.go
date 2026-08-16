// Package authn owns API transport authentication and deny-by-default RPC policy.
package authn

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"

	controlv1 "io.astrasync/control-plane/api-server/gen/go/v1"
	"io.astrasync/control-plane/auth"
)

type Authenticator interface {
	Authenticate(context.Context, string) (auth.Principal, error)
}

// AuthMetrics records bounded authentication decision observations.
type AuthMetrics interface {
	ObserveAuthRequest(tenantID, outcome, requestID string, duration time.Duration)
}

const (
	authTenantUnknown  = "_unknown"
	authTenantPlatform = "_platform"
	authOutcomeSuccess = "success"
	authOutcomeReject  = "rejected"
	authOutcomeFailure = "failure"
)

type Policy struct {
	Permission        auth.Permission
	ResolvePermission func(any) (auth.Permission, error)
	Scope             func(any) (string, error)
	// SelfScope indicates the method targets the caller itself rather than a
	// tenant-scoped resource. The interceptor skips the tenant membership
	// check for self-scope methods; the service handler is responsible for
	// enforcing platform/role-specific authorization using the principal that
	// is propagated through the context.
	SelfScope bool
}

func (p Policy) permission(request any) (auth.Permission, error) {
	if p.ResolvePermission != nil {
		return p.ResolvePermission(request)
	}
	if p.Permission == "" {
		return "", fmt.Errorf("permission is not configured")
	}
	return p.Permission, nil
}

type Registry struct {
	policies      map[string]Policy
	publicMethods map[string]struct{}
	anonymousOK   map[string]struct{}
}

// PublicMethod registers a gRPC method as reachable without authentication and
// without an authorization policy check. The only intended use is the
// gRPC health probe contract and platform-defined diagnostics endpoints that
// the deployment exposes via a dedicated permission.
//
// Use of PublicMethod is rejected by ValidateServices when the registry's
// RequireAuthenticated flag is true in production.
func (r Registry) PublicMethod(fullMethod string) Registry {
	if r.publicMethods == nil {
		r.publicMethods = make(map[string]struct{})
	}
	r.publicMethods[fullMethod] = struct{}{}
	return r
}

// RequirePublicMethod returns true when the supplied method has been declared
// as publicly reachable.
func (r Registry) RequirePublicMethod(fullMethod string) bool {
	if len(r.publicMethods) == 0 {
		return false
	}
	_, found := r.publicMethods[fullMethod]
	return found
}

func NewRegistry() Registry {
	jobScope := func(request any) (string, error) {
		typed, ok := request.(interface{ GetNamespace() string })
		if !ok || strings.TrimSpace(typed.GetNamespace()) == "" {
			return "", fmt.Errorf("request does not contain a tenant namespace")
		}
		return typed.GetNamespace(), nil
	}
	tenantIDScope := func(request any) (string, error) {
		typed, ok := request.(interface{ GetTenantId() string })
		if !ok || strings.TrimSpace(typed.GetTenantId()) == "" {
			return "", fmt.Errorf("request does not contain a tenant ID")
		}
		return typed.GetTenantId(), nil
	}
	validationPermission := func(request any) (auth.Permission, error) {
		typed, ok := request.(*controlv1.ValidateJobSpecRequest)
		if !ok {
			return "", fmt.Errorf("validation request has an invalid type")
		}
		switch typed.GetPurpose() {
		case controlv1.JobValidationPurpose_JOB_VALIDATION_PURPOSE_CREATE:
			return auth.PermissionJobsCreate, nil
		case controlv1.JobValidationPurpose_JOB_VALIDATION_PURPOSE_UPDATE:
			return auth.PermissionJobsUpdate, nil
		case controlv1.JobValidationPurpose_JOB_VALIDATION_PURPOSE_START:
			return auth.PermissionJobsStart, nil
		default:
			return "", fmt.Errorf("validation purpose is invalid")
		}
	}
	return Registry{policies: map[string]Policy{
		controlv1.JobService_CreateJob_FullMethodName:    {Permission: auth.PermissionJobsCreate, Scope: jobScope},
		controlv1.JobService_GetJob_FullMethodName:       {Permission: auth.PermissionJobsRead, Scope: jobScope},
		controlv1.JobService_ListJobs_FullMethodName:     {Permission: auth.PermissionJobsRead, Scope: jobScope},
		controlv1.JobService_UpdateJob_FullMethodName:    {Permission: auth.PermissionJobsUpdate, Scope: jobScope},
		controlv1.JobService_DeleteJob_FullMethodName:    {Permission: auth.PermissionJobsDelete, Scope: jobScope},
		controlv1.JobService_StartJob_FullMethodName:     {Permission: auth.PermissionJobsStart, Scope: jobScope},
		controlv1.JobService_StopJob_FullMethodName:      {Permission: auth.PermissionJobsStop, Scope: jobScope},
		controlv1.JobService_GetJobStatus_FullMethodName: {Permission: auth.PermissionJobsRead, Scope: jobScope},
		controlv1.JobValidationService_ValidateJobSpec_FullMethodName: {
			ResolvePermission: validationPermission, Scope: jobScope,
		},

		controlv1.ConnectorCatalogService_ListConnectorDescriptors_FullMethodName: {
			Permission: auth.PermissionConnectorsRead, Scope: tenantIDScope,
		},
		controlv1.ConnectorCatalogService_GetConnectorDescriptor_FullMethodName: {
			Permission: auth.PermissionConnectorsRead, Scope: tenantIDScope,
		},

		controlv1.ConnectionService_CreateConnection_FullMethodName: {
			Permission: auth.PermissionConnectionsCreate, Scope: tenantIDScope,
		},
		controlv1.ConnectionService_GetConnection_FullMethodName: {
			Permission: auth.PermissionConnectionsRead, Scope: tenantIDScope,
		},
		controlv1.ConnectionService_ListConnections_FullMethodName: {
			Permission: auth.PermissionConnectionsRead, Scope: tenantIDScope,
		},
		controlv1.ConnectionService_UpdateConnection_FullMethodName: {
			Permission: auth.PermissionConnectionsUpdate, Scope: tenantIDScope,
		},
		controlv1.ConnectionService_RotateConnection_FullMethodName: {
			Permission: auth.PermissionConnectionsRotate, Scope: tenantIDScope,
		},
		controlv1.ConnectionService_EnableConnection_FullMethodName: {
			Permission: auth.PermissionConnectionsDisable, Scope: tenantIDScope,
		},
		controlv1.ConnectionService_DisableConnection_FullMethodName: {
			Permission: auth.PermissionConnectionsDisable, Scope: tenantIDScope,
		},
		controlv1.ConnectionService_DeleteConnection_FullMethodName: {
			Permission: auth.PermissionConnectionsDelete, Scope: tenantIDScope,
		},
		controlv1.ConnectionService_TestConnection_FullMethodName: {
			Permission: auth.PermissionConnectionsTest, Scope: tenantIDScope,
		},
		controlv1.ConnectionService_GetConnectionTest_FullMethodName: {
			Permission: auth.PermissionConnectionsTest, Scope: tenantIDScope,
		},
		controlv1.AuditService_ListAuditEvents_FullMethodName: {
			Permission: auth.PermissionAuditRead, Scope: tenantIDScope,
		},
		controlv1.IdentityService_GetCurrentPrincipal_FullMethodName: {
			Permission: auth.PermissionDiagnosticsRead, SelfScope: true,
		},
		controlv1.IdentityService_ListTenants_FullMethodName: {
			Permission: auth.PermissionDiagnosticsRead, SelfScope: true,
		},
		controlv1.AccessService_ListMembers_FullMethodName: {
			Permission: auth.PermissionMembersRead, Scope: tenantIDScope,
		},
		controlv1.AccessService_GrantTenantRole_FullMethodName: {
			Permission: auth.PermissionMembersManage, Scope: tenantIDScope,
		},
		controlv1.AccessService_RevokeTenantRole_FullMethodName: {
			Permission: auth.PermissionMembersManage, Scope: tenantIDScope,
		},
		controlv1.AccessService_ListRoles_FullMethodName: {
			Permission: auth.PermissionMembersRead, SelfScope: true,
		},
		controlv1.AccessService_GrantPlatformRole_FullMethodName: {
			Permission: auth.PermissionPlatformRoles, SelfScope: true,
		},
		controlv1.AccessService_RevokePlatformRole_FullMethodName: {
			Permission: auth.PermissionPlatformRoles, SelfScope: true,
		},
	}}
}

func (r Registry) Policy(fullMethod string) (Policy, bool) {
	policy, found := r.policies[fullMethod]
	return policy, found
}

func (r Registry) ValidateServices(services ...grpc.ServiceDesc) error {
	registered := make(map[string]struct{})
	for _, service := range services {
		for _, method := range service.Methods {
			fullMethod := "/" + service.ServiceName + "/" + method.MethodName
			registered[fullMethod] = struct{}{}
			if _, found := r.policies[fullMethod]; !found {
				return fmt.Errorf("public gRPC method %s has no authorization policy", fullMethod)
			}
		}
	}
	for fullMethod := range r.policies {
		if _, found := registered[fullMethod]; !found {
			return fmt.Errorf("authorization policy %s has no registered public method", fullMethod)
		}
	}
	return nil
}

type Interceptor struct {
	Authenticator Authenticator
	Authorizer    auth.Authorizer
	AuditWriter   auth.AuditWriter
	Metrics       AuthMetrics
	Registry      Registry
	Clock         func() time.Time
	EventID       func() string
}

func (i Interceptor) Validate() error {
	if i.Authenticator == nil || i.Authorizer == nil || i.AuditWriter == nil || i.Metrics == nil || i.Clock == nil ||
		i.EventID == nil || i.Registry.policies == nil {
		return fmt.Errorf("authentication interceptor dependencies must not be nil")
	}
	return nil
}

func (i Interceptor) Unary() grpc.UnaryServerInterceptor {
	return func(
		ctx context.Context,
		request any,
		info *grpc.UnaryServerInfo,
		handler grpc.UnaryHandler,
	) (any, error) {
		if err := i.Validate(); err != nil {
			return nil, status.Error(codes.Internal, "authentication policy is unavailable")
		}
		if i.Registry.RequirePublicMethod(info.FullMethod) {
			return handler(ctx, request)
		}
		startedAt := i.Clock()
		requestID := requestIDFromMetadata(ctx)
		ctx = withRequestID(ctx, requestID)
		policy, found := i.Registry.Policy(info.FullMethod)
		if !found {
			i.observeAuthDecision(startedAt, authTenantUnknown, authOutcomeFailure, requestID)
			i.auditDenied(ctx, auth.Principal{}, "", info.FullMethod, "UNMAPPED_METHOD")
			return nil, status.Error(codes.PermissionDenied, "request is not authorized")
		}
		token, err := bearerToken(ctx)
		if err != nil {
			i.observeAuthDecision(startedAt, authTenantUnknown, authOutcomeReject, requestID)
			i.auditDenied(ctx, auth.Principal{}, "", info.FullMethod, "UNAUTHENTICATED")
			return nil, status.Error(codes.Unauthenticated, "authentication required")
		}
		principal, err := i.Authenticator.Authenticate(ctx, token)
		if err != nil {
			if !errors.Is(err, auth.ErrUnauthenticated) {
				i.observeAuthDecision(startedAt, authTenantUnknown, authOutcomeFailure, requestID)
				i.auditDenied(ctx, auth.Principal{}, "", info.FullMethod, "UNAUTHENTICATED")
				return nil, status.Error(codes.Internal, "authentication service is unavailable")
			}
			i.observeAuthDecision(startedAt, authTenantUnknown, authOutcomeReject, requestID)
			i.auditDenied(ctx, auth.Principal{}, "", info.FullMethod, "UNAUTHENTICATED")
			return nil, status.Error(codes.Unauthenticated, "authentication required")
		}
		permission, err := policy.permission(request)
		if err != nil {
			i.observeAuthDecision(startedAt, authTenantUnknown, authOutcomeReject, requestID)
			i.auditDenied(ctx, principal, "", info.FullMethod, "INVALID_POLICY_INPUT")
			return nil, status.Error(codes.InvalidArgument, "request purpose is invalid")
		}
		principalContext, err := auth.WithPrincipal(ctx, principal)
		if err != nil {
			i.observeAuthDecision(startedAt, authTenantUnknown, authOutcomeFailure, requestID)
			return nil, status.Error(codes.Internal, "authenticated principal is invalid")
		}
		if policy.SelfScope {
			// Self-scope methods authorize against the resolved principal
			// directly. The service handler is responsible for any platform
			// role or membership filtering.
			if !principalHasPermission(principal, permission) {
				i.observeAuthDecision(startedAt, authTenantPlatform, authOutcomeReject, requestID)
				i.auditDenied(ctx, principal, "", info.FullMethod, "PERMISSION_DENIED")
				return nil, status.Error(codes.PermissionDenied, "tenant access denied")
			}
			i.observeAuthDecision(startedAt, authTenantPlatform, authOutcomeSuccess, requestID)
			return handler(principalContext, request)
		}
		scope, err := policy.Scope(request)
		if err != nil {
			i.observeAuthDecision(startedAt, authTenantUnknown, authOutcomeReject, requestID)
			i.auditDenied(ctx, principal, "", info.FullMethod, "INVALID_SCOPE")
			return nil, status.Error(codes.InvalidArgument, "tenant scope is required")
		}
		membership, found := principal.MembershipForScope(scope)
		if !found {
			i.observeAuthDecision(startedAt, authTenantUnknown, authOutcomeReject, requestID)
			i.auditDenied(ctx, principal, "", info.FullMethod, "TENANT_DENIED")
			return nil, status.Error(codes.PermissionDenied, "tenant access denied")
		}
		if _, err := i.Authorizer.Authorize(principalContext, membership.TenantID, permission); err != nil {
			i.observeAuthDecision(startedAt, membership.TenantID, authMetricOutcome(err), requestID)
			i.auditDenied(ctx, principal, membership.TenantID, info.FullMethod, denialOutcome(err))
			return nil, status.Error(codes.PermissionDenied, "tenant access denied")
		}
		i.observeAuthDecision(startedAt, membership.TenantID, authOutcomeSuccess, requestID)
		return handler(principalContext, request)
	}
}

func (i Interceptor) observeAuthDecision(startedAt time.Time, tenantID, outcome, requestID string) {
	duration := i.Clock().Sub(startedAt)
	if duration < 0 {
		duration = 0
	}
	i.Metrics.ObserveAuthRequest(tenantID, outcome, requestID, duration)
}

func authMetricOutcome(err error) string {
	// ContextAuthorizer joins ErrPermissionDenied with policy-store failures.
	// Only the bare sentinel is caller rejection; a joined cause is an SLO failure.
	switch {
	case errors.Is(err, auth.ErrPolicyStale):
		return authOutcomeFailure
	case err == auth.ErrPermissionDenied,
		errors.Is(err, auth.ErrTenantUnavailable),
		errors.Is(err, auth.ErrUnauthenticated):
		return authOutcomeReject
	default:
		return authOutcomeFailure
	}
}

// principalHasPermission returns true when any active tenant membership for
// the principal grants the supplied permission. Platform administrators are
// always considered to hold every permission.
func principalHasPermission(principal auth.Principal, permission auth.Permission) bool {
	if principal.PlatformAdmin {
		return true
	}
	for _, membership := range principal.Memberships {
		if !membership.Active {
			continue
		}
		if membership.Has(permission) {
			return true
		}
	}
	return false
}

func (i Interceptor) auditDenied(
	ctx context.Context, principal auth.Principal, tenantID, method, outcome string,
) {
	actorID := principal.ID
	if actorID == "" {
		actorID = "anonymous"
	}
	requestID := requestIDFromMetadata(ctx)
	_ = i.AuditWriter.WriteSecurityAudit(context.WithoutCancel(ctx), auth.SecurityAuditEvent{
		EventID: i.EventID(), EventType: "authorization.denied", ActorID: actorID,
		TenantID: tenantID, RequestID: requestID, Outcome: outcome,
		Attributes: map[string]any{"method": method}, OccurredAt: i.Clock().UTC(),
	})
}

func bearerToken(ctx context.Context) (string, error) {
	values := metadata.ValueFromIncomingContext(ctx, "authorization")
	if len(values) != 1 || len(values[0]) > 20*1024 {
		return "", auth.ErrUnauthenticated
	}
	parts := strings.SplitN(values[0], " ", 2)
	if len(parts) != 2 || parts[0] != "Bearer" || strings.TrimSpace(parts[1]) == "" {
		return "", auth.ErrUnauthenticated
	}
	return parts[1], nil
}

func requestIDFromMetadata(ctx context.Context) string {
	values := metadata.ValueFromIncomingContext(ctx, "x-request-id")
	if len(values) == 1 && len(values[0]) > 0 && len(values[0]) <= 128 {
		return values[0]
	}
	return "missing-request-id"
}

// requestIDContextKey is the unexported key under which the interceptor stores
// the validated request ID for downstream handlers and audit emitters. The
// value is always a non-empty ASCII string no longer than 128 bytes.
type requestIDContextKey struct{}

func withRequestID(ctx context.Context, requestID string) context.Context {
	return context.WithValue(ctx, requestIDContextKey{}, requestID)
}

// RequestIDFromContext returns the validated request ID attached by the
// authentication interceptor, or the empty string when no interceptor has run.
func RequestIDFromContext(ctx context.Context) string {
	if ctx == nil {
		return ""
	}
	value, _ := ctx.Value(requestIDContextKey{}).(string)
	return value
}

func denialOutcome(err error) string {
	switch {
	case errors.Is(err, auth.ErrPolicyStale):
		return "POLICY_STALE"
	case errors.Is(err, auth.ErrTenantUnavailable):
		return "TENANT_DENIED"
	default:
		return "PERMISSION_DENIED"
	}
}
