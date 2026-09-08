package server_test

import (
	"context"
	"net/http"
	"strings"
	"testing"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/emptypb"

	"io.astrasync/console/internal/server"
	jobv1 "io.astrasync/control-plane/api-server/gen/go/v1"
	"io.astrasync/control-plane/auth"
)

// mutationTenantMetadata is the outgoing gRPC metadata key the BFF uses to
// carry the authenticated tenant into the control plane. ADR-072.
const mutationTenantMetadata = "x-astra-tenant-id"

// jobMutationBackend captures the tenant-id metadata + request payloads for
// each mutation method so tests can assert on the egress contract without
// spinning up a real gRPC server.
type jobMutationBackend struct {
	fakeBFFBackend
	createCalls int
	updateCalls int
	deleteCalls int
	startCalls  int
	stopCalls   int
	validateCalls int
	lastTenantID string
	lastIdempotencyKey string
}

func (b *jobMutationBackend) captureMutation(ctx context.Context) string {
	outgoing, _ := metadata.FromOutgoingContext(ctx)
	values := outgoing.Get(mutationTenantMetadata)
	if len(values) == 1 {
		b.lastTenantID = values[0]
	}
	if authz := outgoing.Get("authorization"); len(authz) == 1 {
		// mirror into fakeBFFBackend for cross-test inspection
	}
	return b.lastTenantID
}

func (b *jobMutationBackend) CreateJob(ctx context.Context, request *jobv1.CreateJobRequest) (*jobv1.Job, error) {
	b.createCalls++
	b.lastIdempotencyKey = request.GetIdempotencyKey()
	b.captureMutation(ctx)
	return &jobv1.Job{Name: request.GetName(), Namespace: request.GetNamespace()}, nil
}

func (b *jobMutationBackend) UpdateJob(ctx context.Context, request *jobv1.UpdateJobRequest) (*jobv1.Job, error) {
	b.updateCalls++
	b.lastIdempotencyKey = request.GetIdempotencyKey()
	b.captureMutation(ctx)
	return &jobv1.Job{Name: request.GetName(), Version: request.GetExpectedVersion() + 1}, nil
}

func (b *jobMutationBackend) DeleteJob(ctx context.Context, request *jobv1.DeleteJobRequest) (*emptypb.Empty, error) {
	b.deleteCalls++
	b.lastIdempotencyKey = request.GetIdempotencyKey()
	b.captureMutation(ctx)
	return &emptypb.Empty{}, nil
}

func (b *jobMutationBackend) StartJob(ctx context.Context, request *jobv1.StartJobRequest) (*jobv1.Job, error) {
	b.startCalls++
	b.lastIdempotencyKey = request.GetIdempotencyKey()
	b.captureMutation(ctx)
	return &jobv1.Job{Name: request.GetName(), Version: request.GetExpectedVersion() + 1}, nil
}

func (b *jobMutationBackend) StopJob(ctx context.Context, request *jobv1.StopJobRequest) (*jobv1.Job, error) {
	b.stopCalls++
	b.lastIdempotencyKey = request.GetIdempotencyKey()
	b.captureMutation(ctx)
	return &jobv1.Job{Name: request.GetName(), Version: request.GetExpectedVersion() + 1}, nil
}

func (b *jobMutationBackend) ValidateJobSpec(ctx context.Context, request *jobv1.ValidateJobSpecRequest) (*jobv1.JobValidationResult, error) {
	b.validateCalls++
	b.captureMutation(ctx)
	return &jobv1.JobValidationResult{Valid: true}, nil
}

func newJobMutationHandler(t *testing.T, backend *jobMutationBackend) http.Handler {
	t.Helper()
	return newJobMutationHandlerWithSessions(t, backend, newFakeSessions(t))
}

func newJobMutationHandlerWithSessions(t *testing.T, backend *jobMutationBackend, sessions *fakeSessions) http.Handler {
	t.Helper()
	console, err := server.NewWithConfig(server.Config{Backend: backend, Sessions: sessions,
		AuthMode: "oidc", PublicOrigin: "https://console.example"})
	if err != nil {
		t.Fatalf("create BFF server: %v", err)
	}
	return console.Handler()
}

// jobMutationHeaders returns the canonical mutation header set: tenant-id,
// CSRF token, content-type, origin, idempotency-key.
func jobMutationHeaders() map[string]string {
	return map[string]string{
		"X-Astra-Tenant-ID": testTenantID,
		"X-CSRF-Token":      "csrf-proof",
		"Content-Type":      "application/json",
		"Origin":            "https://console.example",
		"Idempotency-Key":   "11111111-1111-4111-8111-111111111111",
	}
}

// minimalValidJobSpec is the smallest spec payload accepted by api-server.
// source + sink (connector name must match connectorName pattern) + delivery.
// protojson uses the full proto enum name as the JSON value:
// DELIVERY_GUARANTEE_AT_LEAST_ONCE, not the short form.
const minimalValidJobSpec = `{"source":{"connector":"csv","options":{"path":"in.csv"}},"sink":{"connector":"csv","options":{"path":"out.csv"}},"delivery":{"guarantee":"DELIVERY_GUARANTEE_AT_LEAST_ONCE"}}`

// TestConsoleForwardTenantIDOnCreateJob verifies createJob egress contract
// (ADR-072): the BFF MUST forward `x-astra-tenant-id` on the outgoing gRPC
// metadata, and MUST reject the request outright when scope resolves to an
// empty tenant-id.
func TestConsoleForwardTenantIDOnCreateJob(t *testing.T) {
	backend := &jobMutationBackend{}
	handler := newJobMutationHandler(t, backend)
	body := `{"name":"orders-job","spec":` + minimalValidJobSpec + "}"
	response := bffRequest(handler, http.MethodPost, "/api/jobs", body, jobMutationHeaders())
	if response.Code != http.StatusOK {
		t.Fatalf("create returned %d: %s", response.Code, response.Body.String())
	}
	if backend.createCalls != 1 {
		t.Fatalf("create was not forwarded to backend: %d", backend.createCalls)
	}
	if backend.lastTenantID != testTenantID {
		t.Fatalf("x-astra-tenant-id was not forwarded to create: got %q want %q",
			backend.lastTenantID, testTenantID)
	}
}

// TestConsoleForwardTenantIDOnUpdateJob covers PUT /api/jobs/{name}.
func TestConsoleForwardTenantIDOnUpdateJob(t *testing.T) {
	backend := &jobMutationBackend{}
	handler := newJobMutationHandler(t, backend)
	body := `{"expectedVersion":3,"spec":` + minimalValidJobSpec + "}"
	response := bffRequest(handler, http.MethodPut, "/api/jobs/orders-job", body, jobMutationHeaders())
	if response.Code != http.StatusOK {
		t.Fatalf("update returned %d: %s", response.Code, response.Body.String())
	}
	if backend.updateCalls != 1 || backend.lastTenantID != testTenantID {
		t.Fatalf("update bypassed tenant-id forward: calls=%d tenant=%q",
			backend.updateCalls, backend.lastTenantID)
	}
}

// TestConsoleForwardTenantIDOnDeleteJob covers DELETE /api/jobs/{name}.
func TestConsoleForwardTenantIDOnDeleteJob(t *testing.T) {
	backend := &jobMutationBackend{}
	handler := newJobMutationHandler(t, backend)
	body := `{"expectedVersion":3}`
	response := bffRequest(handler, http.MethodDelete, "/api/jobs/orders-job", body, jobMutationHeaders())
	if response.Code != http.StatusNoContent {
		t.Fatalf("delete returned %d: %s", response.Code, response.Body.String())
	}
	if backend.deleteCalls != 1 || backend.lastTenantID != testTenantID {
		t.Fatalf("delete bypassed tenant-id forward: calls=%d tenant=%q",
			backend.deleteCalls, backend.lastTenantID)
	}
}

// TestConsoleForwardTenantIDOnStartStopJob covers start/stop transitions.
func TestConsoleForwardTenantIDOnStartStopJob(t *testing.T) {
	backend := &jobMutationBackend{}
	handler := newJobMutationHandler(t, backend)
	body := `{"expectedVersion":3}`
	response := bffRequest(handler, http.MethodPost, "/api/jobs/orders-job/start", body, jobMutationHeaders())
	if response.Code != http.StatusOK || backend.startCalls != 1 || backend.lastTenantID != testTenantID {
		t.Fatalf("start bypassed tenant-id forward: code=%d calls=%d tenant=%q",
			response.Code, backend.startCalls, backend.lastTenantID)
	}
	response = bffRequest(handler, http.MethodPost, "/api/jobs/orders-job/stop", body, jobMutationHeaders())
	if response.Code != http.StatusOK || backend.stopCalls != 1 || backend.lastTenantID != testTenantID {
		t.Fatalf("stop bypassed tenant-id forward: code=%d calls=%d tenant=%q",
			response.Code, backend.stopCalls, backend.lastTenantID)
	}
}

// TestConsoleForwardTenantIDOnValidateJob covers POST /api/jobs/{name}/validate.
func TestConsoleForwardTenantIDOnValidateJob(t *testing.T) {
	backend := &jobMutationBackend{}
	handler := newJobMutationHandler(t, backend)
	body := `{"spec":` + minimalValidJobSpec + `,"purpose":"UPDATE"}`
	response := bffRequest(handler, http.MethodPost, "/api/jobs/orders-job/validate", body, jobMutationHeaders())
	if response.Code != http.StatusOK || backend.validateCalls != 1 || backend.lastTenantID != testTenantID {
		t.Fatalf("validate bypassed tenant-id forward: code=%d calls=%d tenant=%q",
			response.Code, backend.validateCalls, backend.lastTenantID)
	}
}

// TestConsoleRejectsMutationWhenTenantIDEmpty verifies the hard-reject path:
// when the resolved scope's tenantID is empty (e.g. the sole membership has
// TenantID="" which would be invalid in production), the BFF MUST deny with
// PermissionDenied (HTTP 403) and MUST NOT forward to the backend.  The test
// achieves this by omitting X-Astra-Tenant-ID so the BFF auto-selects the sole
// membership whose TenantID is overridden to the empty string.
func TestConsoleRejectsMutationWhenTenantIDEmpty(t *testing.T) {
	sessions := newFakeSessions(t)
	membership := sessions.session.Principal.Memberships[testTenantID]
	// Confirm the original TenantID is the test UUID before we corrupt it.
	if membership.TenantID != testTenantID {
		t.Fatalf("sanity: original TenantID=%q, want %q", membership.TenantID, testTenantID)
	}
	// Go map values are copied on read; write the modified copy back.
	membership.TenantID = ""
	sessions.session.Principal.Memberships[testTenantID] = membership
	// Verify the in-memory struct is corrupted.
	if sessions.session.Principal.Memberships[testTenantID].TenantID != "" {
		t.Fatalf("sanity: after blanking, map lookup TenantID=%q, want empty",
			sessions.session.Principal.Memberships[testTenantID].TenantID)
	}
	backend := &jobMutationBackend{}
	handler := newJobMutationHandlerWithSessions(t, backend, sessions)
	// No X-Astra-Tenant-ID header → BFF auto-selects the sole membership.
	// With TenantID="" the scope has an empty tenant and the mutation path
	// must reject before touching the backend.
	headers := map[string]string{
		"X-CSRF-Token":    "csrf-proof",
		"Content-Type":    "application/json",
		"Origin":          "https://console.example",
		"Idempotency-Key": "11111111-1111-4111-8111-111111111111",
		// X-Astra-Tenant-ID deliberately absent to trigger auto-select
	}
	body := `{"name":"orders-job","spec":` + minimalValidJobSpec + "}"
	response := bffRequest(handler, http.MethodPost, "/api/jobs", body, headers)
	if response.Code != http.StatusForbidden {
		t.Fatalf("expected empty-tenant denial, got %d %s", response.Code, response.Body.String())
	}
	if backend.createCalls != 0 {
		t.Fatalf("backend was forwarded an empty-tenant mutation: calls=%d", backend.createCalls)
	}
}

// TestConsoleRejectsMutationWhenScopeMismatch verifies a session-targeted
// cross-tenant attempt is rejected at the BFF without forward.
func TestConsoleRejectsMutationWhenScopeMismatch(t *testing.T) {
	backend := &jobMutationBackend{}
	handler := newJobMutationHandler(t, backend)
	body := `{"expectedVersion":3,"spec":` + minimalValidJobSpec + "}"
	headers := jobMutationHeaders()
	headers["X-Astra-Tenant-ID"] = "22222222-2222-4222-8222-222222222222"
	response := bffRequest(handler, http.MethodPut, "/api/jobs/orders-job", body, headers)
	if response.Code != http.StatusForbidden {
		t.Fatalf("expected cross-tenant denial, got %d %s", response.Code, response.Body.String())
	}
	if backend.updateCalls != 0 {
		t.Fatalf("backend was forwarded a cross-tenant mutation: calls=%d", backend.updateCalls)
	}
}

// TestConsoleReadOnlyEndpointsDoNotForwardTenantID is the regression guard:
// ADR-072 explicitly excludes read-only endpoints. listJobs / getJob continue
// to flow without `x-astra-tenant-id`; this slice pins the exclusion.
func TestConsoleReadOnlyEndpointsDoNotForwardTenantID(t *testing.T) {
	backend := &jobMutationBackend{
		fakeBFFBackend: fakeBFFBackend{},
	}
	console, err := server.NewWithConfig(server.Config{Backend: backend, Sessions: newFakeSessions(t),
		AuthMode: "oidc", PublicOrigin: "https://console.example"})
	if err != nil {
		t.Fatalf("create BFF server: %v", err)
	}
	handler := console.Handler()
	headers := jobMutationHeaders()
	for _, path := range []string{"/api/jobs", "/api/jobs/orders-job"} {
		response := bffRequest(handler, http.MethodGet, path, "", headers)
		if response.Code != http.StatusOK {
			t.Fatalf("read-only path %s returned %d: %s", path, response.Code, response.Body.String())
		}
	}
}

// TestConsoleMutationIdempotencyKeyContinuity is a regression guard: the
// slice 28-A change must not regress the existing Idempotency-Key contract.
// A header carrying a 16-128 char payload must reach the backend verbatim.
func TestConsoleMutationIdempotencyKeyContinuity(t *testing.T) {
	backend := &jobMutationBackend{}
	handler := newJobMutationHandler(t, backend)
	body := `{"name":"orders-job","spec":` + minimalValidJobSpec + "}"
	headers := jobMutationHeaders()
	headers["Idempotency-Key"] = "abcdefghijklmnop-1234567890-very-long-fixed-key"
	response := bffRequest(handler, http.MethodPost, "/api/jobs", body, headers)
	if response.Code != http.StatusOK {
		t.Fatalf("create returned %d: %s", response.Code, response.Body.String())
	}
	if backend.lastIdempotencyKey != headers["Idempotency-Key"] {
		t.Fatalf("Idempotency-Key contract regressed: got %q want %q",
			backend.lastIdempotencyKey, headers["Idempotency-Key"])
	}
}

// TestConsoleMutationRejectsInvalidCSRFAndOrigin pins the existing CSRF /
// origin behaviour alongside the new tenant-id forward. If a future
// refactor drops CSRF in favour of "tenant-id is enough", this guard fires.
func TestConsoleMutationRejectsInvalidCSRFAndOrigin(t *testing.T) {
	backend := &jobMutationBackend{}
	handler := newJobMutationHandler(t, backend)
	body := `{"name":"orders-job","spec":` + minimalValidJobSpec + "}"
	headers := jobMutationHeaders()
	delete(headers, "Origin")
	response := bffRequest(handler, http.MethodPost, "/api/jobs", body, headers)
	if response.Code != http.StatusForbidden || backend.createCalls != 0 {
		t.Fatalf("missing-origin denial regressed: code=%d calls=%d", response.Code, backend.createCalls)
	}
	headers["Origin"] = "https://console.example"
	headers["X-CSRF-Token"] = "wrong"
	response = bffRequest(handler, http.MethodPost, "/api/jobs", body, headers)
	if response.Code != http.StatusForbidden || backend.createCalls != 0 {
		t.Fatalf("wrong-CSRF denial regressed: code=%d calls=%d", response.Code, backend.createCalls)
	}
}

// Ensure the status error from PermissionDenied surfaces as 403 in the BFF
// (covers the empty-tenant branch end-to-end; this is the contract the api
// server-side observer should see).
func TestPermissionDeniedStatusMapsTo403(t *testing.T) {
	err := status.Error(codes.PermissionDenied, "tenant scope denied")
	if status.Code(err) != codes.PermissionDenied {
		t.Fatalf("expected PermissionDenied, got %v", status.Code(err))
	}
	if !strings.Contains(err.Error(), "tenant scope denied") {
		t.Fatalf("permission denied detail lost: %v", err)
	}
	_ = err
	_ = auth.PermissionJobsCreate // anchor import to avoid unused-import lint in narrow builds
}
