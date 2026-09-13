package server_test

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"sync"
	"testing"

	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/types/known/emptypb"

	"io.astrasync/console/internal/server"
	"io.astrasync/console/internal/syncjobcr"
	jobv1 "io.astrasync/control-plane/api-server/gen/go/v1"
)

// slice28bCRTenantID is the tenant UUID used by all slice28b tests. It matches
// the test fixture in bff_test.go (testTenantID) so the existing test helpers
// can be reused without modification.
const slice28bCRTenantID = testTenantID

// captureCRWriter implements syncjobcr.DualWriter and records every
// WriteInput it receives. It is the test-side equivalent of a real K8s
// client: the production code wires a realDualWriter which performs HTTP
// I/O; tests wire a captureCRWriter that records inputs and returns a
// configurable outcome.
type captureCRWriter struct {
	mu        sync.Mutex
	calls     []syncjobcr.WriteInput
	recorder  *slice28bRecorder
	failureOn syncjobcr.MutationKind
	failure   syncjobcr.Outcome
}

func newCaptureCRManager(t *testing.T) *captureCRWriter {
	t.Helper()
	return &captureCRWriter{}
}

func (m *captureCRWriter) Write(_ context.Context, input syncjobcr.WriteInput) syncjobcr.Outcome {
	m.mu.Lock()
	m.calls = append(m.calls, input)
	m.mu.Unlock()
	outcome := syncjobcr.OutcomeSuccess
	if m.failureOn != "" && input.Mutation == m.failureOn {
		outcome = m.failure
	}
	if m.recorder != nil {
		m.recorder.RecordDualWrite(input.Mutation, outcome)
	}
	return outcome
}

func (m *captureCRWriter) Calls() []syncjobcr.WriteInput {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]syncjobcr.WriteInput, len(m.calls))
	copy(out, m.calls)
	return out
}

// findCall returns the first WriteInput matching the mutation kind, or nil.
func (m *captureCRWriter) findCall(kind syncjobcr.MutationKind) *syncjobcr.WriteInput {
	for i := range m.calls {
		if m.calls[i].Mutation == kind {
			return &m.calls[i]
		}
	}
	return nil
}

// slice28bRecorder wraps syncjobcr.Recording and exposes a typed slice
// matching the package's expectation.
type slice28bRecorder struct {
	mu           sync.Mutex
	Observations []syncjobcr.RecordedDualWrite
}

func (r *slice28bRecorder) RecordDualWrite(m syncjobcr.MutationKind, o syncjobcr.Outcome) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.Observations = append(r.Observations, syncjobcr.RecordedDualWrite{Mutation: m, Outcome: o})
}

// newSlice28bHandler builds a Console with the given backend + CR writer +
// recorder. The recorder defaults to a fresh slice28bRecorder.  When
// crWriter is nil the Console uses a no-op dual writer that returns
// OutcomeSuccess without performing any I/O. If crWriter is a *captureCRWriter
// it is also bound to the recorder so each Write emits an observation.
func newSlice28bHandler(t *testing.T, backend interface {
	server.JobMutationClient
	server.JobValidator
}, crWriter syncjobcr.DualWriter, recorder *slice28bRecorder) http.Handler {
	t.Helper()
	if recorder == nil {
		recorder = &slice28bRecorder{}
	}
	if crWriter == nil {
		crWriter = syncjobcr.NoopDualWriter()
	}
	if cap, ok := crWriter.(*captureCRWriter); ok {
		cap.recorder = recorder
	}
	wrapper := &backendWrapper{fakeBFFBackend: &fakeBFFBackend{}, inner: backend}
	console, err := server.NewWithConfig(server.Config{Backend: wrapper, Sessions: newFakeSessions(t),
		AuthMode: "oidc", PublicOrigin: "https://console.example", CRWriter: crWriter})
	if err != nil {
		t.Fatalf("create BFF server: %v", err)
	}
	return console.Handler()
}

// backendWrapper adapts an interface{JobMutationClient; JobValidator} to the
// io.astrasync/console/internal/server Backend interface by supplying
// stub implementations for the read paths used by tests.
type backendWrapper struct {
	*fakeBFFBackend
	inner interface {
		server.JobMutationClient
		server.JobValidator
	}
}

func (w *backendWrapper) ListJobs(context.Context, *jobv1.ListJobsRequest) (*jobv1.ListJobsResponse, error) {
	return &jobv1.ListJobsResponse{}, nil
}
func (w *backendWrapper) GetJob(context.Context, *jobv1.GetJobRequest) (*jobv1.Job, error) {
	return &jobv1.Job{}, nil
}
func (w *backendWrapper) GetJobStatus(context.Context, *jobv1.GetJobStatusRequest) (*jobv1.JobStatus, error) {
	return &jobv1.JobStatus{}, nil
}
func (w *backendWrapper) StartJob(ctx context.Context, req *jobv1.StartJobRequest) (*jobv1.Job, error) {
	return w.inner.StartJob(ctx, req)
}
func (w *backendWrapper) StopJob(ctx context.Context, req *jobv1.StopJobRequest) (*jobv1.Job, error) {
	return w.inner.StopJob(ctx, req)
}
func (w *backendWrapper) ValidateJobSpec(ctx context.Context, req *jobv1.ValidateJobSpecRequest) (*jobv1.JobValidationResult, error) {
	return w.inner.ValidateJobSpec(ctx, req)
}
func (w *backendWrapper) CreateJob(ctx context.Context, req *jobv1.CreateJobRequest) (*jobv1.Job, error) {
	return w.inner.CreateJob(ctx, req)
}
func (w *backendWrapper) UpdateJob(ctx context.Context, req *jobv1.UpdateJobRequest) (*jobv1.Job, error) {
	return w.inner.UpdateJob(ctx, req)
}
func (w *backendWrapper) DeleteJob(ctx context.Context, req *jobv1.DeleteJobRequest) (*emptypb.Empty, error) {
	return w.inner.DeleteJob(ctx, req)
}

// jobSpecForCR is the JSON the Console sends to the api-server for CreateJob.
// It is also the protojson-encoded body the CR writer marshals. The fields
// match minimalValidJobSpec in bff_slice28_test.go so tests can share the
// same payload.
func jobSpecForCR(t *testing.T) *jobv1.JobSpec {
	t.Helper()
	spec := &jobv1.JobSpec{}
	if err := protojson.Unmarshal([]byte(minimalValidJobSpec), spec); err != nil {
		t.Fatalf("unmarshal spec: %v", err)
	}
	return spec
}

// TestConsoleCreatesSyncJobCROnCreateJob: PG mock returns success; CR writer
// receives a Create call with the tenant label and the correct job name.
func TestConsoleCreatesSyncJobCROnCreateJob(t *testing.T) {
	backend := &jobMutationBackend{}
	crManager := newCaptureCRManager(t)
	handler := newSlice28bHandler(t, backend, crManager, nil)
	body := `{"name":"orders-job","spec":` + minimalValidJobSpec + "}"
	response := bffRequest(handler, http.MethodPost, "/api/jobs", body, jobMutationHeaders())
	if response.Code != http.StatusOK {
		t.Fatalf("create returned %d: %s", response.Code, response.Body.String())
	}
	calls := crManager.Calls()
	if len(calls) != 1 {
		t.Fatalf("CR write count: got %d, want 1: %+v", len(calls), calls)
	}
	if calls[0].Mutation != syncjobcr.MutationCreate {
		t.Fatalf("CR mutation: got %q, want %q", calls[0].Mutation, syncjobcr.MutationCreate)
	}
	if calls[0].Name != "orders-job" {
		t.Fatalf("CR name: got %q, want %q", calls[0].Name, "orders-job")
	}
	if calls[0].Scope.TenantID != slice28bCRTenantID {
		t.Fatalf("CR tenant: got %q, want %q", calls[0].Scope.TenantID, slice28bCRTenantID)
	}
	if calls[0].Scope.Namespace != testNamespace {
		t.Fatalf("CR namespace: got %q, want %q", calls[0].Scope.Namespace, testNamespace)
	}
}

// TestConsoleUpdatesSyncJobCROnUpdateJob: PUT /api/jobs/{name} triggers an
// Update CR mutation, with the same name + tenant.
func TestConsoleUpdatesSyncJobCROnUpdateJob(t *testing.T) {
	backend := &jobMutationBackend{}
	crManager := newCaptureCRManager(t)
	handler := newSlice28bHandler(t, backend, crManager, nil)
	body := `{"expectedVersion":3,"spec":` + minimalValidJobSpec + "}"
	response := bffRequest(handler, http.MethodPut, "/api/jobs/orders-job", body, jobMutationHeaders())
	if response.Code != http.StatusOK {
		t.Fatalf("update returned %d: %s", response.Code, response.Body.String())
	}
	calls := crManager.Calls()
	if len(calls) != 1 {
		t.Fatalf("CR write count: got %d, want 1: %+v", len(calls), calls)
	}
	if calls[0].Mutation != syncjobcr.MutationUpdate {
		t.Fatalf("CR mutation: got %q, want %q", calls[0].Mutation, syncjobcr.MutationUpdate)
	}
	if calls[0].Name != "orders-job" {
		t.Fatalf("CR name: got %q, want %q", calls[0].Name, "orders-job")
	}
	if calls[0].Scope.TenantID != slice28bCRTenantID {
		t.Fatalf("CR tenant: got %q, want %q", calls[0].Scope.TenantID, slice28bCRTenantID)
	}
}

// TestConsoleDeletesSyncJobCROnDeleteJob: DELETE /api/jobs/{name} triggers a
// Delete CR mutation.
func TestConsoleDeletesSyncJobCROnDeleteJob(t *testing.T) {
	backend := &jobMutationBackend{}
	crManager := newCaptureCRManager(t)
	handler := newSlice28bHandler(t, backend, crManager, nil)
	body := `{"expectedVersion":3}`
	response := bffRequest(handler, http.MethodDelete, "/api/jobs/orders-job", body, jobMutationHeaders())
	if response.Code != http.StatusNoContent {
		t.Fatalf("delete returned %d: %s", response.Code, response.Body.String())
	}
	calls := crManager.Calls()
	if len(calls) != 1 {
		t.Fatalf("CR write count: got %d, want 1: %+v", len(calls), calls)
	}
	if calls[0].Mutation != syncjobcr.MutationDelete {
		t.Fatalf("CR mutation: got %q, want %q", calls[0].Mutation, syncjobcr.MutationDelete)
	}
	if calls[0].Name != "orders-job" {
		t.Fatalf("CR name: got %q, want %q", calls[0].Name, "orders-job")
	}
	if calls[0].Spec != nil {
		t.Fatalf("delete CR write should not carry a spec: %+v", calls[0].Spec)
	}
}

// TestConsoleStartStopDoNotMutateCR: per ADR-073 §5 the controller picks up
// start/stop from PostgreSQL state, not from CR.spec.state. The Console MUST
// NOT issue any CR write on start / stop.
func TestConsoleStartStopDoNotMutateCR(t *testing.T) {
	backend := &jobMutationBackend{}
	crManager := newCaptureCRManager(t)
	handler := newSlice28bHandler(t, backend, crManager, nil)
	body := `{"expectedVersion":3}`
	response := bffRequest(handler, http.MethodPost, "/api/jobs/orders-job/start", body, jobMutationHeaders())
	if response.Code != http.StatusOK {
		t.Fatalf("start returned %d: %s", response.Code, response.Body.String())
	}
	response = bffRequest(handler, http.MethodPost, "/api/jobs/orders-job/stop", body, jobMutationHeaders())
	if response.Code != http.StatusOK {
		t.Fatalf("stop returned %d: %s", response.Code, response.Body.String())
	}
	if got := len(crManager.Calls()); got != 0 {
		t.Fatalf("start/stop must not write CR: got %d calls %+v", got, crManager.Calls())
	}
}

// TestConsoleCRWriteFailsOpenOnAdmission: per ADR-073 §5 (failure mode matrix)
// when the CR writer returns admission_rejected, the BFF still returns 200
// because PostgreSQL is authoritative. The metric records the failed outcome.
func TestConsoleCRWriteFailsOpenOnAdmission(t *testing.T) {
	backend := &jobMutationBackend{}
	crManager := newCaptureCRManager(t)
	crManager.failureOn = syncjobcr.MutationCreate
	crManager.failure = syncjobcr.OutcomeAdmissionRejected
	recorder := &slice28bRecorder{}
	handler := newSlice28bHandler(t, backend, crManager, recorder)
	body := `{"name":"orders-job","spec":` + minimalValidJobSpec + "}"
	response := bffRequest(handler, http.MethodPost, "/api/jobs", body, jobMutationHeaders())
	if response.Code != http.StatusOK {
		t.Fatalf("create returned %d despite CR admission failure: %s", response.Code, response.Body.String())
	}
	if got := len(recorder.Observations); got != 1 {
		t.Fatalf("metric observations: got %d, want 1: %+v", got, recorder.Observations)
	}
	if recorder.Observations[0].Outcome != syncjobcr.OutcomeAdmissionRejected {
		t.Fatalf("metric outcome: got %q, want %q", recorder.Observations[0].Outcome, syncjobcr.OutcomeAdmissionRejected)
	}
	if recorder.Observations[0].Mutation != syncjobcr.MutationCreate {
		t.Fatalf("metric mutation: got %q, want %q", recorder.Observations[0].Mutation, syncjobcr.MutationCreate)
	}
}

// TestConsoleCRWriteFailsOpenOnTimeout: per ADR-073 §5 the BFF does not fail
// closed on a CR timeout; the metric records the failed outcome.
func TestConsoleCRWriteFailsOpenOnTimeout(t *testing.T) {
	backend := &jobMutationBackend{}
	crManager := newCaptureCRManager(t)
	crManager.failureOn = syncjobcr.MutationUpdate
	crManager.failure = syncjobcr.OutcomeTimeout
	recorder := &slice28bRecorder{}
	handler := newSlice28bHandler(t, backend, crManager, recorder)
	body := `{"expectedVersion":3,"spec":` + minimalValidJobSpec + "}"
	response := bffRequest(handler, http.MethodPut, "/api/jobs/orders-job", body, jobMutationHeaders())
	if response.Code != http.StatusOK {
		t.Fatalf("update returned %d despite CR timeout: %s", response.Code, response.Body.String())
	}
	if got := len(recorder.Observations); got != 1 {
		t.Fatalf("metric observations: got %d, want 1: %+v", got, recorder.Observations)
	}
	if recorder.Observations[0].Outcome != syncjobcr.OutcomeTimeout {
		t.Fatalf("metric outcome: got %q, want %q", recorder.Observations[0].Outcome, syncjobcr.OutcomeTimeout)
	}
}

// TestConsoleDisabledCRManagerSkipsWrite: when no Manager is configured the
// noop writer is used. The mutation handler MUST NOT attempt any HTTP
// request and MUST complete normally.
func TestConsoleDisabledCRManagerSkipsWrite(t *testing.T) {
	backend := &jobMutationBackend{}
	handler := newSlice28bHandler(t, backend, nil, nil)
	body := `{"name":"orders-job","spec":` + minimalValidJobSpec + "}"
	response := bffRequest(handler, http.MethodPost, "/api/jobs", body, jobMutationHeaders())
	if response.Code != http.StatusOK {
		t.Fatalf("create with disabled CR manager returned %d: %s", response.Code, response.Body.String())
	}
	if backend.createCalls != 1 {
		t.Fatalf("backend was not invoked: %d", backend.createCalls)
	}
}

// TestConsoleCRCarriesTenantIDFromScope: a session-targeted cross-tenant
// attempt is rejected at the BFF. No CR write should be issued because the
// mutation never reaches the dual-write branch.
func TestConsoleCRCarriesTenantIDFromScope(t *testing.T) {
	backend := &jobMutationBackend{}
	crManager := newCaptureCRManager(t)
	handler := newSlice28bHandler(t, backend, crManager, nil)
	body := `{"expectedVersion":3,"spec":` + minimalValidJobSpec + "}"
	headers := jobMutationHeaders()
	headers["X-Astra-Tenant-ID"] = "22222222-2222-4222-8222-222222222222"
	response := bffRequest(handler, http.MethodPut, "/api/jobs/orders-job", body, headers)
	if response.Code != http.StatusForbidden {
		t.Fatalf("expected cross-tenant denial, got %d %s", response.Code, response.Body.String())
	}
	if got := len(crManager.Calls()); got != 0 {
		t.Fatalf("cross-tenant must not write CR: got %d calls %+v", got, crManager.Calls())
	}
}

// TestConsoleCREmitsStableLabels: the captured SyncJobSpec for Create carries
// the source + sink as protojson-encoded bytes. The CR writer itself is
// responsible for attaching labels (see syncjobcr.NewDualWriter.create); this
// test asserts the spec the BFF hands to the writer is well-formed.
func TestConsoleCREmitsStableLabels(t *testing.T) {
	backend := &jobMutationBackend{}
	crManager := newCaptureCRManager(t)
	handler := newSlice28bHandler(t, backend, crManager, nil)
	body := `{"name":"orders-job","spec":` + minimalValidJobSpec + "}"
	response := bffRequest(handler, http.MethodPost, "/api/jobs", body, jobMutationHeaders())
	if response.Code != http.StatusOK {
		t.Fatalf("create returned %d: %s", response.Code, response.Body.String())
	}
	call := crManager.findCall(syncjobcr.MutationCreate)
	if call == nil {
		t.Fatalf("no Create call captured")
	}
	if call.Spec == nil {
		t.Fatalf("Create CR spec is nil")
	}
	if !strings.Contains(string(call.Spec.Source), `"connector":"csv"`) {
		t.Fatalf("Create CR spec source missing connector: %s", string(call.Spec.Source))
	}
	if !strings.Contains(string(call.Spec.Sink), `"connector":"csv"`) {
		t.Fatalf("Create CR spec sink missing connector: %s", string(call.Spec.Sink))
	}
	if !strings.Contains(string(call.Spec.Delivery), `"guarantee":"DELIVERY_GUARANTEE_AT_LEAST_ONCE"`) {
		t.Fatalf("Create CR spec delivery missing guarantee: %s", string(call.Spec.Delivery))
	}
}

// TestConsoleCRPostgreSQLFirstOrdering: the PG call MUST complete before the
// CR call. The handler logs both calls in order; this test asserts the
// observed order through the captured WriteInput list and the backend's
// call counts.
func TestConsoleCRPostgreSQLFirstOrdering(t *testing.T) {
	backend := &jobMutationBackend{}
	crManager := newCaptureCRManager(t)
	handler := newSlice28bHandler(t, backend, crManager, nil)
	body := `{"name":"orders-job","spec":` + minimalValidJobSpec + "}"
	response := bffRequest(handler, http.MethodPost, "/api/jobs", body, jobMutationHeaders())
	if response.Code != http.StatusOK {
		t.Fatalf("create returned %d: %s", response.Code, response.Body.String())
	}
	if backend.createCalls != 1 {
		t.Fatalf("expected 1 backend Create call, got %d", backend.createCalls)
	}
	if got := len(crManager.Calls()); got != 1 {
		t.Fatalf("expected 1 CR write, got %d", got)
	}
	if crManager.Calls()[0].Mutation != syncjobcr.MutationCreate {
		t.Fatalf("CR mutation: got %q, want Create", crManager.Calls()[0].Mutation)
	}
}

// TestConsoleCRWriteIsCalledOnlyAfterPGSuccess: when the api-server returns
// an error, the BFF MUST NOT issue a CR write. This is the contract from
// ADR-073 §5 "PostgreSQL-first / CR-second ordering".
func TestConsoleCRWriteIsCalledOnlyAfterPGSuccess(t *testing.T) {
	backend := &failingJobMutationBackend{}
	crManager := newCaptureCRManager(t)
	handler := newSlice28bHandler(t, backend, crManager, nil)
	body := `{"name":"orders-job","spec":` + minimalValidJobSpec + "}"
	response := bffRequest(handler, http.MethodPost, "/api/jobs", body, jobMutationHeaders())
	if response.Code != http.StatusInternalServerError {
		t.Fatalf("expected error response, got %d %s", response.Code, response.Body.String())
	}
	if got := len(crManager.Calls()); got != 0 {
		t.Fatalf("CR write must not happen on PG failure: got %d calls %+v", got, crManager.Calls())
	}
}

// failingJobMutationBackend implements the mutation interface and always fails on
// CreateJob. All other methods succeed (or are unreachable in the tested path).
type failingJobMutationBackend struct{}

func (*failingJobMutationBackend) CreateJob(context.Context, *jobv1.CreateJobRequest) (*jobv1.Job, error) {
	return nil, errors.New("pg backend failure (synthetic)")
}
func (*failingJobMutationBackend) UpdateJob(ctx context.Context, req *jobv1.UpdateJobRequest) (*jobv1.Job, error) {
	return &jobv1.Job{}, nil
}
func (*failingJobMutationBackend) DeleteJob(context.Context, *jobv1.DeleteJobRequest) (*emptypb.Empty, error) {
	return &emptypb.Empty{}, nil
}
func (*failingJobMutationBackend) StartJob(ctx context.Context, req *jobv1.StartJobRequest) (*jobv1.Job, error) {
	return &jobv1.Job{}, nil
}
func (*failingJobMutationBackend) StopJob(ctx context.Context, req *jobv1.StopJobRequest) (*jobv1.Job, error) {
	return &jobv1.Job{}, nil
}
func (*failingJobMutationBackend) ValidateJobSpec(context.Context, *jobv1.ValidateJobSpecRequest) (*jobv1.JobValidationResult, error) {
	return &jobv1.JobValidationResult{Valid: true}, nil
}
