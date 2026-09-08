package syncjobcr

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestNewInClusterReadsTokenAndCA(t *testing.T) {
	// NewInCluster reads the ServiceAccount token and CA cert from the
	// standard mount paths. When the token file is absent (non-in-cluster
	// env), it returns a disabled manager with a reason.
	mgr := NewInCluster()
	if mgr.Enabled() {
		t.Fatal("expected disabled manager outside cluster")
	}
	if mgr.DisabledReason() == "" {
		t.Fatal("expected non-empty disabled reason")
	}
}

func TestNewDisabled(t *testing.T) {
	mgr := NewDisabled()
	if mgr.Enabled() {
		t.Fatal("NewDisabled must return disabled manager")
	}
	if mgr.BaseURL() != "" {
		t.Fatalf("BaseURL: got %q, want empty", mgr.BaseURL())
	}
	if mgr.HTTPClient() != nil {
		t.Fatal("HTTPClient: got non-nil, want nil for disabled manager")
	}
}

func TestNewFromEnvWithK8sHost(t *testing.T) {
	lookup := func(key string) string {
		if key == "KUBERNETES_SERVICE_HOST" {
			return "kubernetes.default.svc"
		}
		return ""
	}
	// Even with KUBERNETES_SERVICE_HOST set, if the token file is absent
	// (no real K8s pod), NewInCluster falls back to disabled.
	// So NewFromEnv also returns disabled — but Enabled() is still false.
	mgr := NewFromEnv(lookup)
	if mgr.Enabled() {
		t.Fatal("expected disabled when token file absent")
	}
}

func TestNewFromEnvWithoutK8sHost(t *testing.T) {
	lookup := func(string) string { return "" }
	mgr := NewFromEnv(lookup)
	if mgr.Enabled() {
		t.Fatal("expected disabled manager without KUBERNETES_SERVICE_HOST")
	}
}

func TestRealDualWriterDisabledManager(t *testing.T) {
	mgr := NewDisabled()
	rec := &Recording{}
	dw := NewDualWriter(mgr, rec, nil)
	spec := &SyncJobSpec{}
	input := WriteInput{
		Scope:    Scope{TenantID: "11111111-1111-4111-8111-111111111111", Namespace: "default"},
		Name:     "orders-job",
		Spec:     spec,
		Mutation: MutationCreate,
	}
	ctx := context.Background()
	outcome := dw.Write(ctx, input)
	if outcome != OutcomeDisabled {
		t.Fatalf("outcome: got %v, want %v", outcome, OutcomeDisabled)
	}
	if n := rec.Count(OutcomeDisabled); n != 1 {
		t.Fatalf("recording count: got %d, want 1", n)
	}
}

func TestRealDualWriterCreateSuccess(t *testing.T) {
	var (
		gotToken  string
		gotBody   SyncJob
	)
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotToken = r.Header.Get("Authorization")
		if r.Method == http.MethodPost && r.URL.Path == "/apis/sync.astrasync.io/v1/namespaces/default/syncjobs" {
			json.NewDecoder(r.Body).Decode(&gotBody)
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusCreated)
			json.NewEncoder(w).Encode(map[string]string{"apiVersion": "sync.astrasync.io/v1", "kind": "SyncJob"})
			return
		}
		http.Error(w, "unexpected request", http.StatusBadRequest)
	}))
	defer server.Close()

	// Build a manager with the test server's CA.
	mgr := &fixedManager{
		token:   "test-token",
		baseURL: server.URL + "/apis/sync.astrasync.io/v1",
		client:  server.Client(),
	}
	rec := &Recording{}
	dw := NewDualWriter(mgr, rec, nil)
	spec := &SyncJobSpec{
		Source: json.RawMessage(`{"connector":"csv"}`),
	}
	input := WriteInput{
		Scope:    Scope{TenantID: "22222222-2222-4222-8222-222222222222", Namespace: "default"},
		Name:     "analytics-job",
		Spec:     spec,
		Mutation: MutationCreate,
	}
	ctx := context.Background()
	outcome := dw.Write(ctx, input)

	if outcome != OutcomeSuccess {
		t.Fatalf("outcome: got %v, want %v", outcome, OutcomeSuccess)
	}
	if gotToken != "Bearer test-token" {
		t.Fatalf("Authorization: got %q, want %q", gotToken, "Bearer test-token")
	}
	if rec.Count(OutcomeSuccess) != 1 {
		t.Fatalf("success recording: got %d, want 1", rec.Count(OutcomeSuccess))
	}
}

func TestRealDualWriterCreateAdmissionRejected(t *testing.T) {
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost {
			http.Error(w, "field label: [astrasync.io/tenant-id: required]", http.StatusUnprocessableEntity)
			return
		}
		http.Error(w, "unexpected", http.StatusBadRequest)
	}))
	defer server.Close()
	mgr := &fixedManager{token: "tok", baseURL: server.URL + "/apis/sync.astrasync.io/v1", client: server.Client()}
	rec := &Recording{}
	dw := NewDualWriter(mgr, rec, nil)
	outcome := dw.Write(context.Background(), WriteInput{
		Scope:    Scope{TenantID: "", Namespace: "default"}, // empty tenant → invalid label
		Name:     "job",
		Spec:     &SyncJobSpec{},
		Mutation: MutationCreate,
	})
	if outcome != OutcomeAdmissionRejected {
		t.Fatalf("outcome: got %v, want %v", outcome, OutcomeAdmissionRejected)
	}
	if rec.Count(OutcomeAdmissionRejected) != 1 {
		t.Fatalf("recording: got %d, want 1", rec.Count(OutcomeAdmissionRejected))
	}
}

func TestRealDualWriterUpdateGetNotFound(t *testing.T) {
	// When the CR doesn't exist during Update (e.g., deleted out-of-band),
	// the update path treats it as success (PostgreSQL is authoritative).
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet {
			http.Error(w, "not found", http.StatusNotFound)
			return
		}
		http.Error(w, "unexpected", http.StatusBadRequest)
	}))
	defer server.Close()
	mgr := &fixedManager{token: "tok", baseURL: server.URL + "/apis/sync.astrasync.io/v1", client: server.Client()}
	rec := &Recording{}
	dw := NewDualWriter(mgr, rec, nil)
	outcome := dw.Write(context.Background(), WriteInput{
		Scope:    Scope{TenantID: "11111111-1111-4111-8111-111111111111", Namespace: "default"},
		Name:     "job",
		Spec:     &SyncJobSpec{},
		Mutation: MutationUpdate,
	})
	if outcome != OutcomeSuccess {
		t.Fatalf("outcome: got %v, want %v", outcome, OutcomeSuccess)
	}
}

func TestRealDualWriterDeleteSuccess(t *testing.T) {
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodDelete && r.URL.Path == "/apis/sync.astrasync.io/v1/namespaces/default/syncjobs/orders-job" {
			w.WriteHeader(http.StatusOK)
			return
		}
		http.Error(w, "unexpected", http.StatusBadRequest)
	}))
	defer server.Close()
	mgr := &fixedManager{token: "tok", baseURL: server.URL + "/apis/sync.astrasync.io/v1", client: server.Client()}
	rec := &Recording{}
	dw := NewDualWriter(mgr, rec, nil)
	outcome := dw.Write(context.Background(), WriteInput{
		Scope:    Scope{TenantID: "11111111-1111-4111-8111-111111111111", Namespace: "default"},
		Name:     "orders-job",
		Mutation: MutationDelete,
	})
	if outcome != OutcomeSuccess {
		t.Fatalf("outcome: got %v, want %v", outcome, OutcomeSuccess)
	}
	if rec.Count(OutcomeSuccess) != 1 {
		t.Fatalf("recording: got %d, want 1", rec.Count(OutcomeSuccess))
	}
}

func TestRealDualWriterDeleteNotFoundIsSuccess(t *testing.T) {
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodDelete {
			http.Error(w, "not found", http.StatusNotFound)
			return
		}
		http.Error(w, "unexpected", http.StatusBadRequest)
	}))
	defer server.Close()
	mgr := &fixedManager{token: "tok", baseURL: server.URL + "/apis/sync.astrasync.io/v1", client: server.Client()}
	rec := &Recording{}
	dw := NewDualWriter(mgr, rec, nil)
	outcome := dw.Write(context.Background(), WriteInput{
		Scope:    Scope{TenantID: "11111111-1111-4111-8111-111111111111", Namespace: "default"},
		Name:     "job",
		Mutation: MutationDelete,
	})
	if outcome != OutcomeSuccess {
		t.Fatalf("outcome: got %v, want %v", outcome, OutcomeSuccess)
	}
}

func TestRealDualWriterRetryOnTimeout(t *testing.T) {
	var attempts int
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attempts++
		if attempts < 3 {
			http.Error(w, "timeout", http.StatusRequestTimeout)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()
	mgr := &fixedManager{token: "tok", baseURL: server.URL + "/apis/sync.astrasync.io/v1", client: server.Client()}
	rec := &Recording{}
	// Short retry delays so the test doesn't take long.
	dw := NewDualWriter(mgr, rec, []time.Duration{1 * time.Millisecond, 1 * time.Millisecond})
	outcome := dw.Write(context.Background(), WriteInput{
		Scope:    Scope{TenantID: "11111111-1111-4111-8111-111111111111", Namespace: "default"},
		Name:     "job",
		Spec:     &SyncJobSpec{},
		Mutation: MutationCreate,
	})
	if outcome != OutcomeSuccess {
		t.Fatalf("outcome after retries: got %v, want %v", outcome, OutcomeSuccess)
	}
	if attempts != 3 {
		t.Fatalf("attempts: got %d, want 3", attempts)
	}
}

func TestRealDualWriterExhaustedRetries(t *testing.T) {
	var attempts int
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attempts++
		http.Error(w, "internal error", http.StatusInternalServerError)
	}))
	defer server.Close()
	mgr := &fixedManager{token: "tok", baseURL: server.URL + "/apis/sync.astrasync.io/v1", client: server.Client()}
	rec := &Recording{}
	dw := NewDualWriter(mgr, rec, []time.Duration{1 * time.Millisecond})
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	outcome := dw.Write(ctx, WriteInput{
		Scope:    Scope{TenantID: "11111111-1111-4111-8111-111111111111", Namespace: "default"},
		Name:     "job",
		Spec:     &SyncJobSpec{},
		Mutation: MutationCreate,
	})
	if outcome != OutcomeInvalid {
		t.Fatalf("outcome after exhausted retries: got %v, want %v", outcome, OutcomeInvalid)
	}
	if attempts != 2 { // 1 original + 1 retry
		t.Fatalf("attempts: got %d, want 2", attempts)
	}
}

func TestRealDualWriterCRLabelStable(t *testing.T) {
	var created SyncJob
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost {
			json.NewDecoder(r.Body).Decode(&created)
			w.WriteHeader(http.StatusCreated)
		}
	}))
	defer server.Close()
	mgr := &fixedManager{token: "tok", baseURL: server.URL + "/apis/sync.astrasync.io/v1", client: server.Client()}
	rec := &Recording{}
	dw := NewDualWriter(mgr, rec, nil)
	tenantID := "33333333-3333-4333-8333-333333333333"
	dw.Write(context.Background(), WriteInput{
		Scope:    Scope{TenantID: tenantID, Namespace: "default"},
		Name:     "stable-labels-job",
		Spec:     &SyncJobSpec{},
		Mutation: MutationCreate,
	})
	if created.Metadata.Labels == nil {
		t.Fatal("Labels must not be nil")
	}
	if got := created.Metadata.Labels[TenantLabelKey]; got != tenantID {
		t.Fatalf("Labels[%q]: got %q, want %q", TenantLabelKey, got, tenantID)
	}
	if len(created.Metadata.Labels) != 1 {
		t.Fatalf("Labels map must contain exactly 1 entry, got %d: %v", len(created.Metadata.Labels), created.Metadata.Labels)
	}
}

func TestClassifyHTTPCode(t *testing.T) {
	tests := []struct {
		code    int
		outcome Outcome
	}{
		{200, OutcomeSuccess},
		{201, OutcomeSuccess},
		{299, OutcomeSuccess},
		{400, OutcomeInvalid},
		{404, OutcomeInvalid},
		{409, OutcomeInvalid},
		{422, OutcomeAdmissionRejected},
		{403, OutcomeAdmissionRejected},
		{408, OutcomeTimeout},
		{502, OutcomeTimeout},
		{429, OutcomeTimeout},
		{500, OutcomeInvalid},
		{502, OutcomeTimeout},
		{503, OutcomeTimeout},
	}
	for _, tc := range tests {
		if got := classifyHTTP(tc.code); got != tc.outcome {
			t.Errorf("classifyHTTP(%d): got %v, want %v", tc.code, got, tc.outcome)
		}
	}
}

func TestRecording(t *testing.T) {
	r := &Recording{}
	r.RecordDualWrite(MutationCreate, OutcomeSuccess)
	r.RecordDualWrite(MutationUpdate, OutcomeTimeout)
	r.RecordDualWrite(MutationCreate, OutcomeSuccess)
	if n := r.Count(OutcomeSuccess); n != 2 {
		t.Fatalf("OutcomeSuccess count: got %d, want 2", n)
	}
	if n := r.Count(OutcomeTimeout); n != 1 {
		t.Fatalf("OutcomeTimeout count: got %d, want 1", n)
	}
}

// fixedManager is a test manager that sends requests to an httptest.Server.
type fixedManager struct {
	token   string
	baseURL string
	client  *http.Client
}

func (m *fixedManager) Enabled() bool    { return true }
func (m *fixedManager) BaseURL() string  { return m.baseURL }
func (m *fixedManager) HTTPClient() *http.Client { return m.client }

func (m *fixedManager) Token() string { return m.token }
func (m *fixedManager) DisabledReason() string { return "" }