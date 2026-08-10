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

type Policy struct {
	Permission        auth.Permission
	ResolvePermission func(any) (auth.Permission, error)
	Scope             func(any) (string, error)
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
	policies map[string]Policy
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
	Registry      Registry
	Clock         func() time.Time
	EventID       func() string
}

func (i Interceptor) Validate() error {
	if i.Authenticator == nil || i.Authorizer == nil || i.AuditWriter == nil || i.Clock == nil || i.EventID == nil ||
		i.Registry.policies == nil {
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
		policy, found := i.Registry.Policy(info.FullMethod)
		if !found {
			i.auditDenied(ctx, auth.Principal{}, "", info.FullMethod, "UNMAPPED_METHOD")
			return nil, status.Error(codes.PermissionDenied, "request is not authorized")
		}
		token, err := bearerToken(ctx)
		if err != nil {
			i.auditDenied(ctx, auth.Principal{}, "", info.FullMethod, "UNAUTHENTICATED")
			return nil, status.Error(codes.Unauthenticated, "authentication required")
		}
		principal, err := i.Authenticator.Authenticate(ctx, token)
		if err != nil {
			i.auditDenied(ctx, auth.Principal{}, "", info.FullMethod, "UNAUTHENTICATED")
			return nil, status.Error(codes.Unauthenticated, "authentication required")
		}
		scope, err := policy.Scope(request)
		if err != nil {
			i.auditDenied(ctx, principal, "", info.FullMethod, "INVALID_SCOPE")
			return nil, status.Error(codes.InvalidArgument, "tenant scope is required")
		}
		permission, err := policy.permission(request)
		if err != nil {
			i.auditDenied(ctx, principal, "", info.FullMethod, "INVALID_POLICY_INPUT")
			return nil, status.Error(codes.InvalidArgument, "request purpose is invalid")
		}
		membership, found := principal.MembershipForScope(scope)
		if !found {
			i.auditDenied(ctx, principal, "", info.FullMethod, "TENANT_DENIED")
			return nil, status.Error(codes.PermissionDenied, "tenant access denied")
		}
		principalContext, err := auth.WithPrincipal(ctx, principal)
		if err != nil {
			return nil, status.Error(codes.Internal, "authenticated principal is invalid")
		}
		if _, err := i.Authorizer.Authorize(principalContext, membership.TenantID, permission); err != nil {
			i.auditDenied(ctx, principal, membership.TenantID, info.FullMethod, denialOutcome(err))
			return nil, status.Error(codes.PermissionDenied, "tenant access denied")
		}
		return handler(principalContext, request)
	}
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
