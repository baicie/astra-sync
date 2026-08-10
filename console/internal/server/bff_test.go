package server_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/emptypb"

	"io.astrasync/console/internal/authflow"
	"io.astrasync/console/internal/server"
	jobv1 "io.astrasync/control-plane/api-server/gen/go/v1"
	"io.astrasync/control-plane/auth"
)

const (
	testTenantID  = "11111111-1111-4111-8111-111111111111"
	testNamespace = "tenant-a"
)

type fakeSessions struct {
	session authflow.Session
}

func newFakeSessions(t *testing.T) *fakeSessions {
	t.Helper()
	membership, err := auth.NewMembership(testTenantID, true, auth.AllTenantPermissions()...)
	if err != nil {
		t.Fatalf("create membership: %v", err)
	}
	membership.TenantNamespace = testNamespace
	membership.TenantDisplayName = "Tenant A"
	membership.Role = auth.RoleTenantAdmin
	principal := auth.Principal{ID: "principal-1", Issuer: "https://issuer.example", Subject: "operator-1",
		Active: true, PolicyRevision: "1", Memberships: map[string]auth.Membership{testTenantID: membership}}
	return &fakeSessions{session: authflow.Session{Principal: principal, SessionID: "opaque-session",
		Record: auth.ConsoleSession{PrincipalID: principal.ID, CSRFToken: "csrf-proof", Revision: 1,
			Tokens:            auth.ConsoleTokens{AccessToken: "access-token-sentinel", TokenType: "Bearer", ExpiresAt: time.Now().Add(time.Hour)},
			AbsoluteExpiresAt: time.Now().Add(time.Hour)}}}
}

func (f *fakeSessions) BeginLogin(context.Context, string) (authflow.LoginStart, error) {
	return authflow.LoginStart{}, status.Error(codes.Unimplemented, "not used")
}
func (f *fakeSessions) CompleteLogin(context.Context, string, string, string) (authflow.Session, string, error) {
	return authflow.Session{}, "", status.Error(codes.Unimplemented, "not used")
}
func (f *fakeSessions) Resolve(context.Context, string) (authflow.Session, error) {
	return f.session, nil
}
func (f *fakeSessions) ValidateCSRF(session authflow.Session, token string) bool {
	return session.SessionID == f.session.SessionID && token == f.session.Record.CSRFToken
}
func (f *fakeSessions) Logout(context.Context, authflow.Session) error { return nil }

type fakeBFFBackend struct {
	createRequest *jobv1.CreateConnectionRequest
	updateRequest *jobv1.UpdateConnectionRequest
	updateError   error
	authorization string
}

func (f *fakeBFFBackend) capture(ctx context.Context) {
	outgoing, _ := metadata.FromOutgoingContext(ctx)
	values := outgoing.Get("authorization")
	if len(values) == 1 {
		f.authorization = values[0]
	}
}

func (f *fakeBFFBackend) ListJobs(context.Context, *jobv1.ListJobsRequest) (*jobv1.ListJobsResponse, error) {
	return &jobv1.ListJobsResponse{}, nil
}
func (f *fakeBFFBackend) GetJob(context.Context, *jobv1.GetJobRequest) (*jobv1.Job, error) {
	return &jobv1.Job{}, nil
}
func (f *fakeBFFBackend) GetJobStatus(context.Context, *jobv1.GetJobStatusRequest) (*jobv1.JobStatus, error) {
	return &jobv1.JobStatus{}, nil
}
func (f *fakeBFFBackend) CreateJob(context.Context, *jobv1.CreateJobRequest) (*jobv1.Job, error) {
	return &jobv1.Job{}, nil
}
func (f *fakeBFFBackend) UpdateJob(context.Context, *jobv1.UpdateJobRequest) (*jobv1.Job, error) {
	return &jobv1.Job{}, nil
}
func (f *fakeBFFBackend) DeleteJob(context.Context, *jobv1.DeleteJobRequest) (*emptypb.Empty, error) {
	return &emptypb.Empty{}, nil
}
func (f *fakeBFFBackend) StartJob(context.Context, *jobv1.StartJobRequest) (*jobv1.Job, error) {
	return &jobv1.Job{}, nil
}
func (f *fakeBFFBackend) StopJob(context.Context, *jobv1.StopJobRequest) (*jobv1.Job, error) {
	return &jobv1.Job{}, nil
}
func (f *fakeBFFBackend) ValidateJobSpec(context.Context, *jobv1.ValidateJobSpecRequest) (*jobv1.JobValidationResult, error) {
	return &jobv1.JobValidationResult{Valid: true}, nil
}

func (f *fakeBFFBackend) ListConnectorDescriptors(ctx context.Context, _ *jobv1.ListConnectorDescriptorsRequest) (*jobv1.ListConnectorDescriptorsResponse, error) {
	f.capture(ctx)
	return &jobv1.ListConnectorDescriptorsResponse{Descriptors: []*jobv1.ConnectorDescriptor{testDescriptor()}}, nil
}
func (f *fakeBFFBackend) GetConnectorDescriptor(ctx context.Context, _ *jobv1.GetConnectorDescriptorRequest) (*jobv1.GetConnectorDescriptorResponse, error) {
	f.capture(ctx)
	return &jobv1.GetConnectorDescriptorResponse{ConnectorDescriptor: testDescriptor()}, nil
}
func (f *fakeBFFBackend) CreateConnection(ctx context.Context, request *jobv1.CreateConnectionRequest) (*jobv1.Connection, error) {
	f.capture(ctx)
	f.createRequest = request
	return &jobv1.Connection{TenantId: request.GetTenantId(), Name: request.GetName(), Connector: request.GetConnector(),
		Version: 1, Generation: 1, State: jobv1.ConnectionState_CONNECTION_STATE_DISABLED, SecretConfigured: true}, nil
}
func (f *fakeBFFBackend) GetConnection(context.Context, *jobv1.GetConnectionRequest) (*jobv1.Connection, error) {
	return &jobv1.Connection{TenantId: testTenantID, Name: "orders-db", Connector: "jdbc", Version: 2,
		State: jobv1.ConnectionState_CONNECTION_STATE_DISABLED}, nil
}
func (f *fakeBFFBackend) ListConnections(context.Context, *jobv1.ListConnectionsRequest) (*jobv1.ListConnectionsResponse, error) {
	return &jobv1.ListConnectionsResponse{}, nil
}
func (f *fakeBFFBackend) UpdateConnection(_ context.Context, request *jobv1.UpdateConnectionRequest) (*jobv1.Connection, error) {
	f.updateRequest = request
	if f.updateError != nil {
		return nil, f.updateError
	}
	return &jobv1.Connection{Name: request.GetName(), Version: request.GetExpectedVersion() + 1}, nil
}
func (f *fakeBFFBackend) RotateConnection(context.Context, *jobv1.RotateConnectionRequest) (*jobv1.Connection, error) {
	return &jobv1.Connection{}, nil
}
func (f *fakeBFFBackend) EnableConnection(context.Context, *jobv1.EnableConnectionRequest) (*jobv1.Connection, error) {
	return &jobv1.Connection{}, nil
}
func (f *fakeBFFBackend) DisableConnection(context.Context, *jobv1.DisableConnectionRequest) (*jobv1.Connection, error) {
	return &jobv1.Connection{}, nil
}
func (f *fakeBFFBackend) DeleteConnection(context.Context, *jobv1.DeleteConnectionRequest) (*jobv1.DeleteConnectionResponse, error) {
	return &jobv1.DeleteConnectionResponse{}, nil
}
func (f *fakeBFFBackend) TestConnection(context.Context, *jobv1.TestConnectionRequest) (*jobv1.ConnectionTest, error) {
	return &jobv1.ConnectionTest{}, nil
}
func (f *fakeBFFBackend) GetConnectionTest(context.Context, *jobv1.GetConnectionTestRequest) (*jobv1.ConnectionTest, error) {
	return &jobv1.ConnectionTest{}, nil
}

func testDescriptor() *jobv1.ConnectorDescriptor {
	return &jobv1.ConnectorDescriptor{Name: "jdbc", Options: []*jobv1.ConnectorOptionDefinition{
		{Key: "host", Owner: jobv1.ConnectorOptionOwner_CONNECTOR_OPTION_OWNER_CONNECTION,
			Sensitivity: jobv1.ConnectorOptionSensitivity_CONNECTOR_OPTION_SENSITIVITY_PUBLIC},
		{Key: "password", Owner: jobv1.ConnectorOptionOwner_CONNECTOR_OPTION_OWNER_CONNECTION,
			Sensitivity: jobv1.ConnectorOptionSensitivity_CONNECTOR_OPTION_SENSITIVITY_SECRET},
	}}
}

func newBFFHandler(t *testing.T, backend *fakeBFFBackend) http.Handler {
	t.Helper()
	console, err := server.NewWithConfig(server.Config{Backend: backend, Sessions: newFakeSessions(t),
		AuthMode: "oidc", PublicOrigin: "https://console.example"})
	if err != nil {
		t.Fatalf("create BFF server: %v", err)
	}
	return console.Handler()
}

func TestBFFSessionAndTenantScopeNeverExposeBearer(t *testing.T) {
	backend := &fakeBFFBackend{}
	handler := newBFFHandler(t, backend)
	response := bffRequest(handler, http.MethodGet, "/api/session", "", nil)
	if response.Code != http.StatusOK || strings.Contains(response.Body.String(), "access-token-sentinel") ||
		!strings.Contains(response.Body.String(), testTenantID) {
		t.Fatalf("unexpected session projection: %d %s", response.Code, response.Body.String())
	}
	response = bffRequest(handler, http.MethodGet, "/api/connectors", "", map[string]string{"X-Astra-Tenant-ID": testTenantID})
	if response.Code != http.StatusOK || backend.authorization != "Bearer access-token-sentinel" {
		t.Fatalf("unexpected authenticated backend request: code=%d authorization=%q", response.Code, backend.authorization)
	}
	response = bffRequest(handler, http.MethodGet, "/api/connectors", "", map[string]string{
		"X-Astra-Tenant-ID": "22222222-2222-4222-8222-222222222222",
	})
	if response.Code != http.StatusForbidden {
		t.Fatalf("expected cross-tenant denial, got %d", response.Code)
	}
}

func TestBFFConnectionMutationRequiresOriginCSRFAndRedactsLocator(t *testing.T) {
	backend := &fakeBFFBackend{}
	handler := newBFFHandler(t, backend)
	body := `{"name":"orders-db","connector":"jdbc","settings":[{"key":"host","value":"db.internal"}],"secretBinding":{"provider":"kubernetes","secretName":"secret-name-sentinel","secretUid":"secret-uid-sentinel","fields":[{"logicalField":"password","secretKey":"password"}]}}`
	baseHeaders := map[string]string{"X-Astra-Tenant-ID": testTenantID, "Content-Type": "application/json", "X-CSRF-Token": "csrf-proof", "Idempotency-Key": "11111111-1111-4111-8111-111111111111"}
	response := bffRequest(handler, http.MethodPost, "/api/connections", body, baseHeaders)
	if response.Code != http.StatusForbidden || backend.createRequest != nil {
		t.Fatalf("expected missing-origin denial, got %d", response.Code)
	}
	headers := cloneHeaders(baseHeaders)
	headers["Origin"] = "https://console.example"
	response = bffRequest(handler, http.MethodPost, "/api/connections", body, headers)
	if response.Code != http.StatusOK || backend.createRequest == nil || backend.createRequest.GetTenantId() != testTenantID {
		t.Fatalf("unexpected create response/request: code=%d body=%s request=%+v", response.Code, response.Body.String(), backend.createRequest)
	}
	if strings.Contains(response.Body.String(), "secret-name-sentinel") || strings.Contains(response.Body.String(), "secret-uid-sentinel") {
		t.Fatalf("secret locator leaked in response: %s", response.Body.String())
	}
	locator := backend.createRequest.GetSecretBinding().GetKubernetesSecretV1()
	if locator.GetSecretName() != "secret-name-sentinel" || locator.GetSecretUid() != "secret-uid-sentinel" {
		t.Fatalf("write-only locator was not forwarded: %+v", locator)
	}
}

func TestBFFRejectsRawSecretAndMapsCASConflict(t *testing.T) {
	backend := &fakeBFFBackend{}
	handler := newBFFHandler(t, backend)
	headers := map[string]string{"X-Astra-Tenant-ID": testTenantID, "Content-Type": "application/json",
		"X-CSRF-Token": "csrf-proof", "Origin": "https://console.example", "Idempotency-Key": "11111111-1111-4111-8111-111111111111"}
	rawSecret := `{"name":"orders-db","connector":"jdbc","settings":[{"key":"password","value":"raw-secret-sentinel"}]}`
	response := bffRequest(handler, http.MethodPost, "/api/connections", rawSecret, headers)
	if response.Code != http.StatusBadRequest || backend.createRequest != nil || strings.Contains(response.Body.String(), "raw-secret-sentinel") {
		t.Fatalf("raw secret was not safely rejected: code=%d body=%s", response.Code, response.Body.String())
	}
	backend.updateError = status.Error(codes.Aborted, "CONNECTION_VERSION_CONFLICT")
	response = bffRequest(handler, http.MethodPut, "/api/connections/orders-db", `{"expectedVersion":2,"settings":[{"key":"host","value":"db.internal"}]}`, headers)
	if response.Code != http.StatusConflict || backend.updateRequest == nil || strings.Contains(response.Body.String(), "CONNECTION_VERSION_CONFLICT") {
		t.Fatalf("unexpected CAS response: code=%d body=%s", response.Code, response.Body.String())
	}
}

func bffRequest(handler http.Handler, method, path, body string, headers map[string]string) *httptest.ResponseRecorder {
	request := httptest.NewRequest(method, path, strings.NewReader(body))
	for key, value := range headers {
		request.Header.Set(key, value)
	}
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	return response
}

func cloneHeaders(source map[string]string) map[string]string {
	result := make(map[string]string, len(source)+1)
	for key, value := range source {
		result[key] = value
	}
	return result
}
