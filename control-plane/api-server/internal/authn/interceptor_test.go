package authn_test

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"

	controlv1 "io.astrasync/control-plane/api-server/gen/go/v1"
	"io.astrasync/control-plane/api-server/internal/authn"
	"io.astrasync/control-plane/auth"
)

const (
	interceptorTenantID = "8d58d674-7cc7-4b15-a46c-9e7768bbf103"
	authMetricRequestID = "d724ad9a-30a2-4dab-9704-2b01ea1f67e1"
)

func TestRegistryCoversEveryPublicMethodExactly(t *testing.T) {
	registry := authn.NewRegistry()
	if err := registry.ValidateServices(
		controlv1.JobService_ServiceDesc,
		controlv1.JobValidationService_ServiceDesc,
		controlv1.ConnectorCatalogService_ServiceDesc,
		controlv1.ConnectionService_ServiceDesc,
		controlv1.AuditService_ServiceDesc,
		controlv1.IdentityService_ServiceDesc,
		controlv1.AccessService_ServiceDesc,
	); err != nil {
		t.Fatalf("validate public method policy registry: %v", err)
	}
}

func TestRegistryPublicMethodSkipsAuthorization(t *testing.T) {
	const fullMethod = "/grpc.health.v1.Health/Check"
	registry := authn.NewRegistry().PublicMethod(fullMethod)
	if !registry.RequirePublicMethod(fullMethod) {
		t.Fatalf("expected %s to be declared public", fullMethod)
	}
	membership, _ := auth.NewMembership(interceptorTenantID, true, auth.PermissionJobsRead)
	principal := auth.Principal{ID: "p", Subject: "p", Active: true,
		Memberships: map[string]auth.Membership{interceptorTenantID: membership}}
	called := false
	_, err := authn.Interceptor{
		Authenticator: staticAuthenticator{principal: principal},
		Authorizer:    auth.ContextAuthorizer{},
		AuditWriter:   &memoryAudit{},
		Metrics:       &memoryAuthMetrics{},
		Registry:      registry,
		Clock:         time.Now,
		EventID:       func() string { return "ignored" },
	}.Unary()(
		context.Background(), nil,
		&grpc.UnaryServerInfo{FullMethod: fullMethod},
		func(context.Context, any) (any, error) { called = true; return nil, nil },
	)
	if err != nil || !called {
		t.Fatalf("public method was not invoked: called=%v err=%v", called, err)
	}
}

func TestRequestIDFromContextPropagatesAuditLabel(t *testing.T) {
	registry := authn.NewRegistry()
	audit := &memoryAudit{}
	interceptor := authn.Interceptor{
		Authenticator: staticAuthenticator{principal: auth.Principal{}},
		Authorizer:    auth.ContextAuthorizer{},
		AuditWriter:   audit,
		Metrics:       &memoryAuthMetrics{},
		Registry:      registry,
		Clock:         time.Now,
		EventID:       func() string { return "id" },
	}
	ctx := metadata.NewIncomingContext(context.Background(), metadata.Pairs(
		"x-request-id", "audit-7",
	))
	_, _ = interceptor.Unary()(
		ctx, &controlv1.GetJobRequest{Name: "orders"},
		&grpc.UnaryServerInfo{FullMethod: controlv1.JobService_GetJob_FullMethodName},
		func(context.Context, any) (any, error) { return nil, nil },
	)
	if len(audit.events) != 1 || audit.events[0].RequestID != "audit-7" {
		t.Fatalf("audit request ID not propagated: %+v", audit.events)
	}
}

func TestInterceptorAuthenticatesResolvesNamespaceAndAuthorizesBeforeHandler(t *testing.T) {
	membership, err := auth.NewMembership(interceptorTenantID, true, auth.PermissionJobsStart)
	if err != nil {
		t.Fatalf("membership: %v", err)
	}
	membership.TenantNamespace = "tenant-a"
	membership.PolicyRevision = "7"
	principal := auth.Principal{
		ID: "principal-1", Issuer: "https://issuer.example", Subject: "operator-1", Active: true,
		PolicyRevision: "principal-1", Memberships: map[string]auth.Membership{interceptorTenantID: membership},
	}
	audit := &memoryAudit{}
	metricRecorder := &memoryAuthMetrics{}
	interceptor := authn.Interceptor{
		Authenticator: staticAuthenticator{principal: principal},
		Authorizer: auth.ContextAuthorizer{CurrentPolicyRevision: func(context.Context, string) (string, error) {
			return "7", nil
		}},
		AuditWriter: audit,
		Metrics:     metricRecorder,
		Registry:    authn.NewRegistry(),
		Clock:       func() time.Time { return time.Date(2026, 8, 8, 9, 0, 0, 0, time.UTC) },
		EventID:     func() string { return "event-1" },
	}
	ctx := metadata.NewIncomingContext(context.Background(), metadata.Pairs(
		"authorization", "Bearer valid-token", "x-request-id", "request-1",
	))
	called := false
	response, err := interceptor.Unary()(
		ctx,
		&controlv1.StartJobRequest{Namespace: "tenant-a", Name: "orders", ExpectedVersion: 1},
		&grpc.UnaryServerInfo{FullMethod: controlv1.JobService_StartJob_FullMethodName},
		func(ctx context.Context, request any) (any, error) {
			called = true
			resolved, found := auth.PrincipalFromContext(ctx)
			if !found || resolved.ID != principal.ID {
				t.Fatalf("handler did not receive authenticated principal: %+v", resolved)
			}
			return request, nil
		},
	)
	if err != nil || !called || response == nil || len(audit.events) != 0 {
		t.Fatalf("authorized call: called=%v response=%+v audit=%+v err=%v", called, response, audit.events, err)
	}
	assertAuthObservation(t, metricRecorder, interceptorTenantID, "success", "request-1")
}

func TestInterceptorDeniesCrossTenantBeforeHandlerAndAuditsSynchronously(t *testing.T) {
	membership, err := auth.NewMembership(interceptorTenantID, true, auth.PermissionConnectionsRead)
	if err != nil {
		t.Fatalf("membership: %v", err)
	}
	membership.TenantNamespace = "tenant-a"
	principal := auth.Principal{
		ID: "principal-1", Subject: "operator-1", Active: true, PolicyRevision: "1",
		Memberships: map[string]auth.Membership{interceptorTenantID: membership},
	}
	audit := &memoryAudit{}
	metricRecorder := &memoryAuthMetrics{}
	interceptor := authn.Interceptor{
		Authenticator: staticAuthenticator{principal: principal},
		Authorizer:    auth.ContextAuthorizer{},
		AuditWriter:   audit,
		Metrics:       metricRecorder,
		Registry:      authn.NewRegistry(),
		Clock:         time.Now,
		EventID:       func() string { return "event-denied" },
	}
	ctx := metadata.NewIncomingContext(context.Background(), metadata.Pairs("authorization", "Bearer valid-token"))
	called := false
	_, err = interceptor.Unary()(
		ctx,
		&controlv1.GetConnectionRequest{TenantId: "30982542-e097-4c66-99a9-31081fd6b285", Name: "orders"},
		&grpc.UnaryServerInfo{FullMethod: controlv1.ConnectionService_GetConnection_FullMethodName},
		func(context.Context, any) (any, error) { called = true; return nil, nil },
	)
	if status.Code(err) != codes.PermissionDenied || called {
		t.Fatalf("cross-tenant request was not denied before handler: called=%v err=%v", called, err)
	}
	if len(audit.events) != 1 || audit.events[0].Outcome != "TENANT_DENIED" || audit.events[0].TenantID != "" {
		t.Fatalf("unexpected denial audit: %+v", audit.events)
	}
	assertAuthObservation(t, metricRecorder, "_unknown", "rejected", "missing-request-id")
}

func TestInterceptorSelfScopeAcceptsCallerWithMatchingPermission(t *testing.T) {
	membership, err := auth.NewMembership(interceptorTenantID, true, auth.PermissionDiagnosticsRead)
	if err != nil {
		t.Fatalf("membership: %v", err)
	}
	membership.TenantNamespace = "tenant-a"
	principal := auth.Principal{
		ID: "principal-1", Subject: "operator-1", Active: true, PolicyRevision: "1",
		Memberships: map[string]auth.Membership{interceptorTenantID: membership},
	}
	audit := &memoryAudit{}
	metricRecorder := &memoryAuthMetrics{}
	interceptor := authn.Interceptor{
		Authenticator: staticAuthenticator{principal: principal},
		Authorizer:    auth.ContextAuthorizer{},
		AuditWriter:   audit,
		Metrics:       metricRecorder,
		Registry:      authn.NewRegistry(),
		Clock:         time.Now,
		EventID:       func() string { return "self-ok" },
	}
	ctx := metadata.NewIncomingContext(context.Background(), metadata.Pairs("authorization", "Bearer valid-token"))
	called := false
	_, err = interceptor.Unary()(
		ctx,
		&controlv1.GetCurrentPrincipalRequest{},
		&grpc.UnaryServerInfo{FullMethod: controlv1.IdentityService_GetCurrentPrincipal_FullMethodName},
		func(context.Context, any) (any, error) { called = true; return nil, nil },
	)
	if err != nil || !called {
		t.Fatalf("self-scope call was not allowed: called=%v err=%v", called, err)
	}
	assertAuthObservation(t, metricRecorder, "_platform", "success", "missing-request-id")
}

func TestInterceptorSelfScopeRejectsCallerWithoutPermission(t *testing.T) {
	membership, err := auth.NewMembership(interceptorTenantID, true, auth.PermissionJobsRead)
	if err != nil {
		t.Fatalf("membership: %v", err)
	}
	principal := auth.Principal{
		ID: "principal-2", Subject: "viewer-1", Active: true, PolicyRevision: "1",
		Memberships: map[string]auth.Membership{interceptorTenantID: membership},
	}
	audit := &memoryAudit{}
	metricRecorder := &memoryAuthMetrics{}
	interceptor := authn.Interceptor{
		Authenticator: staticAuthenticator{principal: principal},
		Authorizer:    auth.ContextAuthorizer{},
		AuditWriter:   audit,
		Metrics:       metricRecorder,
		Registry:      authn.NewRegistry(),
		Clock:         time.Now,
		EventID:       func() string { return "self-deny" },
	}
	ctx := metadata.NewIncomingContext(context.Background(), metadata.Pairs("authorization", "Bearer valid-token"))
	called := false
	_, err = interceptor.Unary()(
		ctx,
		&controlv1.GetCurrentPrincipalRequest{},
		&grpc.UnaryServerInfo{FullMethod: controlv1.IdentityService_GetCurrentPrincipal_FullMethodName},
		func(context.Context, any) (any, error) { called = true; return nil, nil },
	)
	if status.Code(err) != codes.PermissionDenied || called {
		t.Fatalf("self-scope call without permission was not denied: called=%v err=%v", called, err)
	}
	if len(audit.events) != 1 || audit.events[0].Outcome != "PERMISSION_DENIED" {
		t.Fatalf("unexpected denial audit: %+v", audit.events)
	}
	assertAuthObservation(t, metricRecorder, "_platform", "rejected", "missing-request-id")
}

func TestInterceptorRecordsAuthenticationInfrastructureFailure(t *testing.T) {
	metricRecorder := &memoryAuthMetrics{}
	interceptor := authn.Interceptor{
		Authenticator: staticAuthenticator{err: errors.New("identity repository unavailable")},
		Authorizer:    auth.ContextAuthorizer{},
		AuditWriter:   &memoryAudit{},
		Metrics:       metricRecorder,
		Registry:      authn.NewRegistry(),
		Clock:         time.Now,
		EventID:       func() string { return "auth-failure" },
	}
	ctx := metadata.NewIncomingContext(context.Background(), metadata.Pairs(
		"authorization", "Bearer valid-token", "x-request-id", authMetricRequestID,
	))
	called := false
	_, err := interceptor.Unary()(
		ctx,
		&controlv1.GetJobRequest{Namespace: "tenant-a", Name: "orders"},
		&grpc.UnaryServerInfo{FullMethod: controlv1.JobService_GetJob_FullMethodName},
		func(context.Context, any) (any, error) { called = true; return nil, nil },
	)
	if status.Code(err) != codes.Internal || called {
		t.Fatalf("infrastructure failure: called=%v err=%v", called, err)
	}
	assertAuthObservation(t, metricRecorder, "_unknown", "failure", authMetricRequestID)
}

func TestInterceptorClassifiesAuthorizationOutcomesForSLO(t *testing.T) {
	membership, err := auth.NewMembership(interceptorTenantID, true, auth.PermissionJobsRead)
	if err != nil {
		t.Fatalf("membership: %v", err)
	}
	membership.TenantNamespace = "tenant-a"
	principal := auth.Principal{
		ID: "principal-1", Subject: "operator-1", Active: true, PolicyRevision: "7",
		Memberships: map[string]auth.Membership{interceptorTenantID: membership},
	}
	tests := []struct {
		name    string
		err     error
		outcome string
	}{
		{name: "permission_rejected", err: auth.ErrPermissionDenied, outcome: "rejected"},
		{name: "policy_stale_failure", err: auth.ErrPolicyStale, outcome: "failure"},
		{
			name:    "policy_repository_failure",
			err:     errors.Join(auth.ErrPermissionDenied, errors.New("policy repository unavailable")),
			outcome: "failure",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			metricRecorder := &memoryAuthMetrics{}
			interceptor := authn.Interceptor{
				Authenticator: staticAuthenticator{principal: principal},
				Authorizer:    staticAuthorizer{err: test.err},
				AuditWriter:   &memoryAudit{},
				Metrics:       metricRecorder,
				Registry:      authn.NewRegistry(),
				Clock:         time.Now,
				EventID:       func() string { return "authorization-outcome" },
			}
			ctx := metadata.NewIncomingContext(context.Background(), metadata.Pairs(
				"authorization", "Bearer valid-token",
			))
			_, callErr := interceptor.Unary()(
				ctx,
				&controlv1.GetJobRequest{Namespace: "tenant-a", Name: "orders"},
				&grpc.UnaryServerInfo{FullMethod: controlv1.JobService_GetJob_FullMethodName},
				func(context.Context, any) (any, error) { return nil, nil },
			)
			if status.Code(callErr) != codes.PermissionDenied {
				t.Fatalf("authorization error = %v, want PermissionDenied", callErr)
			}
			assertAuthObservation(t, metricRecorder, interceptorTenantID, test.outcome, "missing-request-id")
		})
	}
}

type staticAuthenticator struct {
	principal auth.Principal
	err       error
}

type staticAuthorizer struct {
	err error
}

func (a staticAuthorizer) Authorize(context.Context, string, auth.Permission) (auth.Decision, error) {
	return auth.Decision{}, a.err
}

func (a staticAuthenticator) Authenticate(context.Context, string) (auth.Principal, error) {
	return a.principal, a.err
}

type memoryAudit struct {
	mu     sync.Mutex
	events []auth.SecurityAuditEvent
}

func (a *memoryAudit) WriteSecurityAudit(_ context.Context, event auth.SecurityAuditEvent) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.events = append(a.events, event)
	return nil
}

type authObservation struct {
	tenantID  string
	outcome   string
	requestID string
	duration  time.Duration
}

type memoryAuthMetrics struct {
	mu           sync.Mutex
	observations []authObservation
}

func (m *memoryAuthMetrics) ObserveAuthRequest(tenantID, outcome, requestID string, duration time.Duration) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.observations = append(m.observations, authObservation{
		tenantID: tenantID, outcome: outcome, requestID: requestID, duration: duration,
	})
}

func assertAuthObservation(
	t *testing.T, recorder *memoryAuthMetrics, tenantID, outcome, requestID string,
) {
	t.Helper()
	recorder.mu.Lock()
	defer recorder.mu.Unlock()
	if len(recorder.observations) != 1 {
		t.Fatalf("authentication observation count = %d, want 1: %+v", len(recorder.observations), recorder.observations)
	}
	observation := recorder.observations[0]
	if observation.tenantID != tenantID || observation.outcome != outcome || observation.requestID != requestID ||
		observation.duration < 0 {
		t.Fatalf("unexpected authentication observation: %+v", observation)
	}
}
