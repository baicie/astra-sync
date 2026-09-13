// Package chain_test exercises the cross-module tenant-id envelope chain
// defined in ADR-076 (Phase 31) under the public-surface constraint
// defined in ADR-080.
//
// The fixture drives the BFF via the public `console` package façade
// (ADR-080 §1) and asserts on a `bffbackend.Backend` test fake that
// captures the outgoing gRPC metadata. The api-server interceptor's
// ordering rule is pinned by `interceptor_test.go` (Layer 1 of the
// layered test surface per ADR-075 §5).
//
// Test-only. No production code change beyond ADR-080's public
// façade.
package chain_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"google.golang.org/grpc/metadata"
	"google.golang.org/protobuf/types/known/emptypb"

	"io.astrasync/console"
	"io.astrasync/console/pkg/bffbackend"
	jobv1 "io.astrasync/control-plane/api-server/gen/go/v1"
)

// ----------------------------------------------------------------------------
// Constants — see ADR-076 §4 and ADR-079 / 080.
// ----------------------------------------------------------------------------

const (
	// canonicalTenantUUID is the verified tenant-id used by every chain
	// test unless overridden. It must satisfy auth.tenantIDPattern
	// (version digit ∈ [1-5], variant digit ∈ [8-b]) because the BFF
	// fixture builds a real auth.Membership via authflow.NewDevelopmentManager
	// (which routes through auth.NewMembership). The console/internal
	// test fixture uses the same value (`bff_test.go::testTenantID`),
	// keeping the cross-module fixture aligned with the in-package
	// one.
	canonicalTenantUUID = "11111111-1111-4111-8111-111111111111"

	// canonicalNamespace is the tenant namespace the development session
	// manager resolves the sole membership against.
	canonicalNamespace = "tenant-a"

	// mutationTenantMetadata is the canonical outgoing gRPC metadata key the
	// BFF attaches on the mutation path (ADR-072 / ADR-079 §2 / ADR-074 §3).
	mutationTenantMetadata = "x-astra-tenant-id"
)

// ----------------------------------------------------------------------------
// Test backend — implements bffbackend.Backend; captures the outgoing
// metadata on each call so the chain test can assert on the egress
// contract (ADR-076 §2 / §4 row 1).
// ----------------------------------------------------------------------------

type recordingBackend struct {
	mu        sync.Mutex
	calls     []recordedCall
	createErr error
}

type recordedCall struct {
	method         string
	tenantID       string
	idempotencyKey string
}

func (b *recordingBackend) capture(ctx context.Context, method, idempotencyKey string) {
	outgoing, _ := metadata.FromOutgoingContext(ctx)
	values := outgoing.Get(mutationTenantMetadata)
	tenantID := ""
	if len(values) == 1 {
		tenantID = values[0]
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	b.calls = append(b.calls, recordedCall{method: method, tenantID: tenantID, idempotencyKey: idempotencyKey})
}

func (b *recordingBackend) CreateJob(ctx context.Context, request *jobv1.CreateJobRequest) (*jobv1.Job, error) {
	b.capture(ctx, "CreateJob", request.GetIdempotencyKey())
	if b.createErr != nil {
		return nil, b.createErr
	}
	return &jobv1.Job{
		Name: request.GetName(), Namespace: request.GetNamespace(),
		Uid: uuid.NewString(), Version: 1,
	}, nil
}

func (b *recordingBackend) UpdateJob(ctx context.Context, request *jobv1.UpdateJobRequest) (*jobv1.Job, error) {
	b.capture(ctx, "UpdateJob", request.GetIdempotencyKey())
	return &jobv1.Job{Name: request.GetName(), Version: request.GetExpectedVersion() + 1}, nil
}
func (b *recordingBackend) DeleteJob(ctx context.Context, request *jobv1.DeleteJobRequest) (*emptypb.Empty, error) {
	b.capture(ctx, "DeleteJob", request.GetIdempotencyKey())
	return &emptypb.Empty{}, nil
}
func (b *recordingBackend) StartJob(ctx context.Context, request *jobv1.StartJobRequest) (*jobv1.Job, error) {
	b.capture(ctx, "StartJob", request.GetIdempotencyKey())
	return &jobv1.Job{Name: request.GetName(), Version: request.GetExpectedVersion() + 1}, nil
}
func (b *recordingBackend) StopJob(ctx context.Context, request *jobv1.StopJobRequest) (*jobv1.Job, error) {
	b.capture(ctx, "StopJob", request.GetIdempotencyKey())
	return &jobv1.Job{Name: request.GetName(), Version: request.GetExpectedVersion() + 1}, nil
}
func (b *recordingBackend) ValidateJobSpec(ctx context.Context, request *jobv1.ValidateJobSpecRequest) (*jobv1.JobValidationResult, error) {
	b.capture(ctx, "ValidateJobSpec", "")
	return &jobv1.JobValidationResult{Valid: true}, nil
}

func (b *recordingBackend) ListJobs(context.Context, *jobv1.ListJobsRequest) (*jobv1.ListJobsResponse, error) {
	return &jobv1.ListJobsResponse{}, nil
}
func (b *recordingBackend) GetJob(context.Context, *jobv1.GetJobRequest) (*jobv1.Job, error) {
	return &jobv1.Job{}, nil
}
func (b *recordingBackend) GetJobStatus(context.Context, *jobv1.GetJobStatusRequest) (*jobv1.JobStatus, error) {
	return &jobv1.JobStatus{}, nil
}
func (b *recordingBackend) ListConnectorDescriptors(context.Context, *jobv1.ListConnectorDescriptorsRequest) (*jobv1.ListConnectorDescriptorsResponse, error) {
	return &jobv1.ListConnectorDescriptorsResponse{}, nil
}
func (b *recordingBackend) GetConnectorDescriptor(context.Context, *jobv1.GetConnectorDescriptorRequest) (*jobv1.GetConnectorDescriptorResponse, error) {
	return &jobv1.GetConnectorDescriptorResponse{}, nil
}
func (b *recordingBackend) CreateConnection(context.Context, *jobv1.CreateConnectionRequest) (*jobv1.Connection, error) {
	return &jobv1.Connection{}, nil
}
func (b *recordingBackend) GetConnection(context.Context, *jobv1.GetConnectionRequest) (*jobv1.Connection, error) {
	return &jobv1.Connection{}, nil
}
func (b *recordingBackend) ListConnections(context.Context, *jobv1.ListConnectionsRequest) (*jobv1.ListConnectionsResponse, error) {
	return &jobv1.ListConnectionsResponse{}, nil
}
func (b *recordingBackend) UpdateConnection(context.Context, *jobv1.UpdateConnectionRequest) (*jobv1.Connection, error) {
	return &jobv1.Connection{}, nil
}
func (b *recordingBackend) RotateConnection(context.Context, *jobv1.RotateConnectionRequest) (*jobv1.Connection, error) {
	return &jobv1.Connection{}, nil
}
func (b *recordingBackend) EnableConnection(context.Context, *jobv1.EnableConnectionRequest) (*jobv1.Connection, error) {
	return &jobv1.Connection{}, nil
}
func (b *recordingBackend) DisableConnection(context.Context, *jobv1.DisableConnectionRequest) (*jobv1.Connection, error) {
	return &jobv1.Connection{}, nil
}
func (b *recordingBackend) DeleteConnection(context.Context, *jobv1.DeleteConnectionRequest) (*jobv1.DeleteConnectionResponse, error) {
	return &jobv1.DeleteConnectionResponse{}, nil
}
func (b *recordingBackend) TestConnection(context.Context, *jobv1.TestConnectionRequest) (*jobv1.ConnectionTest, error) {
	return &jobv1.ConnectionTest{}, nil
}
func (b *recordingBackend) GetConnectionTest(context.Context, *jobv1.GetConnectionTestRequest) (*jobv1.ConnectionTest, error) {
	return &jobv1.ConnectionTest{}, nil
}
func (b *recordingBackend) ListAuditEvents(context.Context, *jobv1.ListAuditEventsRequest) (*jobv1.ListAuditEventsResponse, error) {
	return &jobv1.ListAuditEventsResponse{}, nil
}

// lastTenantID returns the tenant-id captured on the most recent call.
func (b *recordingBackend) lastTenantID() (string, bool) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if len(b.calls) == 0 {
		return "", false
	}
	return b.calls[len(b.calls)-1].tenantID, true
}

// lastCallCount returns the number of captured calls.
func (b *recordingBackend) lastCallCount() int {
	b.mu.Lock()
	defer b.mu.Unlock()
	return len(b.calls)
}

// Compile-time guard: keep bffbackend.Backend import non-redundant.
var _ bffbackend.Backend = (*recordingBackend)(nil)

// ----------------------------------------------------------------------------
// Fixture wiring — see ADR-080 §1.
// ----------------------------------------------------------------------------

type fixture struct {
	t       *testing.T
	server  http.Handler
	backend *recordingBackend
}

func newFixture(t *testing.T) *fixture {
	t.Helper()
	backend := &recordingBackend{}

	srv, err := console.NewWithDevelopmentSession(console.Config{
		Backend:      bffbackend.Backend(backend),
		AuthMode:     "oidc",
		PublicOrigin: "https://console.example",
		Namespace:    canonicalNamespace,
		Ready:        func(context.Context) error { return nil },
		Clock:        func() time.Time { return time.Unix(100, 0).UTC() },
	}, canonicalTenantUUID, canonicalNamespace)
	if err != nil {
		t.Fatalf("build BFF: %v", err)
	}

	return &fixture{
		t:       t,
		server:  srv.Handler(),
		backend: backend,
	}
}

// developmentCSRFToken is the CSRF token issued by
// authflow.NewDevelopmentManager (the session backing the cross-module
// fixture). The BFF requires X-CSRF-Token to match the session on
// mutating paths (server.go requireMutation → ValidateCSRF).
const developmentCSRFToken = "development-csrf"

// bffPost issues a POST against the BFF with the canonical mutation
// header set: tenant-id (optional), content-type, origin,
// idempotency-key, csrf. CSRF is enforced on every mutating BFF path;
// the development session issues a stable token so the fixture
// mirrors what console/internal/server tests do.
func (f *fixture) bffPost(path, body, tenantID string) *httptest.ResponseRecorder {
	f.t.Helper()
	req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(body))
	if tenantID != "" {
		req.Header.Set("X-Astra-Tenant-ID", tenantID)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Origin", "https://console.example")
	req.Header.Set("Idempotency-Key", "phase31-cross-module-00000001")
	req.Header.Set("X-CSRF-Token", developmentCSRFToken)
	rec := httptest.NewRecorder()
	f.server.ServeHTTP(rec, req)
	return rec
}

// minimalValidJobSpec is the smallest spec payload accepted by the
// api-server. Source + sink (connector name must match connectorName
// pattern) + delivery. protojson uses the full proto enum name as the
// JSON value (DELIVERY_GUARANTEE_AT_LEAST_ONCE).
const minimalValidJobSpec = `{"source":{"connector":"csv","options":{"path":"in.csv"}},"sink":{"connector":"csv","options":{"path":"out.csv"}},"delivery":{"guarantee":"DELIVERY_GUARANTEE_AT_LEAST_ONCE"}}`

const createJobBody = `{"name":"orders-job","spec":` + minimalValidJobSpec + "}"

// ----------------------------------------------------------------------------
// Tests — five cases per ADR-076 §4, recast to public-surface contract.
// ----------------------------------------------------------------------------

// TestCrossModule_ChainMatchesBFFScopeAndAPIServerMutation is the happy-path
// cross-module chain (ADR-076 §4 row 1). The BFF egress tenant-id (the
// X-Astra-Tenant-ID header the BFF reads from the operator session) must
// surface on the outgoing gRPC metadata key `x-astra-tenant-id` to the
// api-server. A regression that drops the value is caught here.
func TestCrossModule_ChainMatchesBFFScopeAndAPIServerMutation(t *testing.T) {
	f := newFixture(t)

	rec := f.bffPost("/api/jobs", createJobBody, canonicalTenantUUID)
	if rec.Code != http.StatusOK {
		t.Fatalf("create returned %d: %s", rec.Code, rec.Body.String())
	}

	got, ok := f.backend.lastTenantID()
	if !ok {
		t.Fatalf("chain[egress]: no captured call")
	}
	if got != canonicalTenantUUID {
		t.Fatalf("chain[egress] mismatch: backend.tenantID = %q, want %q",
			got, canonicalTenantUUID)
	}
	if n := f.backend.lastCallCount(); n != 1 {
		t.Fatalf("chain[egress] expected 1 call, got %d", n)
	}
}

// TestCrossModule_APIServerRejectsBFFMismatch is recast per ADR-080 §4: the
// BFF ingress must reject an X-Astra-Tenant-ID that does not match the
// session's sole membership. The api-server interceptor's rejection is
// pinned by `interceptor_test.go` (Layer 1).
func TestCrossModule_APIServerRejectsBFFMismatch(t *testing.T) {
	f := newFixture(t)

	mismatchedTenantID := uuid.NewString()
	rec := f.bffPost("/api/jobs", createJobBody, mismatchedTenantID)
	if rec.Code == http.StatusOK {
		t.Fatalf("BFF forwarded mismatched tenant-id without rejection: %d", rec.Code)
	}
	if rec.Code < 400 || rec.Code >= 500 {
		t.Fatalf("BFF rejection code = %d, want 4xx (got body=%s)", rec.Code, rec.Body.String())
	}
	if n := f.backend.lastCallCount(); n != 0 {
		t.Fatalf("backend was called %d times despite ingress rejection", n)
	}
}

// TestCrossModule_APIServerRejectsMalformedMetadata is recast per ADR-080 §4:
// non-canonical tenant-id (non-UUID) must be rejected by the BFF before
// reaching the api-server. The api-server's TENANT_ENVELOPE_INVALID audit
// is pinned by `interceptor_test.go`.
func TestCrossModule_APIServerRejectsMalformedMetadata(t *testing.T) {
	f := newFixture(t)

	rec := f.bffPost("/api/jobs", createJobBody, "not-a-uuid")
	if rec.Code == http.StatusOK {
		t.Fatalf("BFF forwarded malformed tenant-id without rejection: %d", rec.Code)
	}
	if rec.Code < 400 || rec.Code >= 500 {
		t.Fatalf("BFF rejection code = %d, want 4xx (got body=%s)", rec.Code, rec.Body.String())
	}
	if n := f.backend.lastCallCount(); n != 0 {
		t.Fatalf("backend was called %d times despite ingress rejection", n)
	}
}

// TestCrossModule_NoTenantIDInMetadataFallsBackToMembership is recast per
// ADR-080 §4: when the operator omits X-Astra-Tenant-ID, the BFF resolves
// the session's sole membership and forwards that value. The api-server
// fallback path is pinned by `interceptor_test.go` (TestNoMetadataFallback).
func TestCrossModule_NoTenantIDInMetadataFallsBackToMembership(t *testing.T) {
	f := newFixture(t)

	rec := f.bffPost("/api/jobs", createJobBody, "") // no tenant header
	if rec.Code != http.StatusOK {
		t.Fatalf("BFF should resolve from session membership: %d %s", rec.Code, rec.Body.String())
	}

	got, ok := f.backend.lastTenantID()
	if !ok {
		t.Fatalf("chain[fallback]: no captured call")
	}
	if got != canonicalTenantUUID {
		t.Fatalf("fallback tenantID = %q, want %q (sole session membership)",
			got, canonicalTenantUUID)
	}
}

// TestCrossModule_APIServerMigration003PersistsVerifiedTenantID is the
// migration-side of the chain, per ADR-080 §4 row 5. Because the
// cross-module fixture cannot drive the api-server mutation repository
// (api-server/internal/ is `internal/`), this test asserts the migration
// file declares the column and index, and that the BFF egress carries the
// verified tenant-id (the value that would be persisted by the production
// INSERT path).
//
// Part A is the SQL parse; Part B is the egress contract. A regression
// that drops the column or the index breaks Part A; a regression that
// drops the egress tenant-id breaks Part B. Both halves must pass for
// the chain to hold.
func TestCrossModule_APIServerMigration003PersistsVerifiedTenantID(t *testing.T) {
	// Part A — SQL parse.
	migrationPath := filepath.Join(
		"..", "..", "..", "control-plane", "job", "postgres", "migrations", "003_jobs_tenant_id.sql",
	)
	contents, err := os.ReadFile(migrationPath)
	if err != nil {
		t.Fatalf("read migration: %v", err)
	}
	// The migration file's CRLF / LF encoding is not part of the SQL
	// contract — normalize to LF before literal substring matching so
	// the assertion stays portable across editors.
	text := strings.ReplaceAll(string(contents), "\r\n", "\n")
	wantColumn := `ALTER TABLE astrasync_control_jobs
    ADD COLUMN IF NOT EXISTS tenant_id UUID`
	if !strings.Contains(text, wantColumn) {
		t.Fatalf("migration %s missing column DDL:\n  want substring: %q\n  got: %q",
			migrationPath, wantColumn, text)
	}
	wantIndex := `CREATE INDEX IF NOT EXISTS astrasync_control_jobs_tenant_id_idx
    ON astrasync_control_jobs (tenant_id)`
	if !strings.Contains(text, wantIndex) {
		t.Fatalf("migration %s missing index DDL:\n  want substring: %q\n  got: %q",
			migrationPath, wantIndex, text)
	}

	// Part B — egress contract (the verified tenant-id is what the
	// production INSERT path writes into astrasync_control_jobs.tenant_id).
	f := newFixture(t)
	rec := f.bffPost("/api/jobs", createJobBody, canonicalTenantUUID)
	if rec.Code != http.StatusOK {
		t.Fatalf("create returned %d: %s", rec.Code, rec.Body.String())
	}
	got, ok := f.backend.lastTenantID()
	if !ok {
		t.Fatalf("no captured call")
	}
	if got != canonicalTenantUUID {
		t.Fatalf("egress tenantID = %q, want %q (verified)",
			got, canonicalTenantUUID)
	}
}
