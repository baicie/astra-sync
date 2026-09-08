// Package syncjobcr owns the Kubernetes REST client the Console uses to create,
// update, and delete the SyncJob CR that the controller-runtime controller
// reconciles. ADR-073.
//
// The Console uses a plain net/http client (no controller-runtime dependency)
// because controller-runtime's full dependency tree (k8s.io/apiserver,
// k8s.io/component-base, k8s.io/streaming) is not present in the module cache
// and the Go proxy is intermittently unavailable. The HTTP client communicates
// with the Kubernetes API server using the in-cluster ServiceAccount (token +
// CA cert), which is functionally equivalent to what client-go does under the
// hood.
//
// The package deliberately does NOT import io.astrasync/control-plane/controller
// to avoid pulling the controller-runtime dependency chain into the console
// module. The CRD types (SyncJob, SyncJobSpec, TenantIDLabel) are copied
// locally so the package is self-contained.
package syncjobcr

import (
	"bytes"
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"sync"
	"time"
)

// TenantLabelKey is the Kubernetes label key for the tenant identifier.
// The value MUST be a canonical lowercase UUID. ADR-071 §2.
const TenantLabelKey = "astrasync.io/tenant-id"

// SyncJob is the minimal subset of the controller module's SyncJob CRD type
// needed for CR construction. Keeping this in-package avoids importing
// control-plane/controller (and its controller-runtime dependency chain).
// The fields must stay in sync with control-plane/controller/api/v1.SyncJob.
type SyncJob struct {
	APIVersion string         `json:"apiVersion,omitempty"`
	Kind       string         `json:"kind,omitempty"`
	Metadata   SyncJobMetadata `json:"metadata"`
	Spec       SyncJobSpec   `json:"spec,omitempty"`
	Status     *SyncJobStatus `json:"status,omitempty"`
}

type SyncJobMetadata struct {
	Name            string            `json:"name,omitempty"`
	Namespace       string            `json:"namespace,omitempty"`
	Labels         map[string]string `json:"labels,omitempty"`
	ResourceVersion string            `json:"resourceVersion,omitempty"`
}

type SyncJobSpec struct {
	Source     json.RawMessage   `json:"source,omitempty"`
	Sink       json.RawMessage   `json:"sink,omitempty"`
	Transforms []json.RawMessage `json:"transforms,omitempty"`
	Delivery   json.RawMessage   `json:"delivery,omitempty"`
	Runtime    json.RawMessage   `json:"runtime,omitempty"`
	State      string            `json:"state,omitempty"`
}

type SyncJobStatus struct {
	Desired      string     `json:"desiredState,omitempty"`
	State       string     `json:"state,omitempty"`
	Epoch       int64      `json:"epoch,omitempty"`
	RestartCount int32     `json:"restartCount,omitempty"`
	StartTime   *time.Time `json:"startTime,omitempty"`
	EndTime     *time.Time `json:"endTime,omitempty"`
}

// Scope is the Console scope needed to address a SyncJob CR.
type Scope struct {
	TenantID  string
	Namespace string
}

// Manager owns the HTTP client used by the Console for SyncJob CR writes.
// The Manager is configured once at startup; per-request calls reuse the
// shared http.Client (with keep-alive). A Manager may be disabled (local
// dev mode); in that case Writes are no-ops and the metric records
// OutcomeDisabled.
type Manager interface {
	Enabled() bool
	// BaseURL returns the Kubernetes API server base URL for SyncJob resources.
	// Returns "" when disabled.
	BaseURL() string
	// HTTPClient returns the underlying http.Client. Returns nil when disabled.
	HTTPClient() *http.Client
	// DisabledReason returns a non-empty string when Enabled() == false.
	// Used by tests and startup logs.
	DisabledReason() string
	// Token returns the bearer token used for in-cluster API server auth.
	// Returns "" for disabled managers or test managers that do not require auth.
	Token() string
}

// NewDisabled returns a Manager that never writes CRs. Used in local dev
// and in unit tests that focus on the Console's non-CR behaviour.
func NewDisabled() Manager { return disabledManager{} }

// NewInCluster builds a Manager that reads the in-cluster ServiceAccount
// token and CA cert and configures an http.Client with TLS using the cluster
// CA. If the in-cluster token is not present, the returned Manager falls
// back to disabled (logs a warning at startup).
func NewInCluster() Manager {
	token, err := os.ReadFile("/var/run/secrets/kubernetes.io/serviceaccount/token")
	if err != nil {
		return disabledManager{reason: fmt.Sprintf("in-cluster token not available: %v", err)}
	}
	caCert, err := os.ReadFile("/var/run/secrets/kubernetes.io/serviceaccount/ca.crt")
	if err != nil {
		return disabledManager{reason: fmt.Sprintf("in-cluster CA cert not available: %v", err)}
	}
	pool := x509.NewCertPool()
	if !pool.AppendCertsFromPEM(caCert) {
		return disabledManager{reason: "in-cluster CA cert is not valid PEM"}
	}
	apiServer := os.Getenv("KUBERNETES_SERVICE_HOST")
	if apiServer == "" {
		return disabledManager{reason: "KUBERNETES_SERVICE_HOST not set"}
	}
	port := os.Getenv("KUBERNETES_SERVICE_PORT")
	if port == "" {
		port = "443"
	}
	baseURL := fmt.Sprintf("https://%s:%s/apis/sync.astrasync.io/v1", apiServer, port)
	return &inClusterManager{
		token:   string(token),
		baseURL: baseURL,
		client: &http.Client{
			Timeout: 10 * time.Second,
			Transport: &http.Transport{
				TLSClientConfig: &tls.Config{
					RootCAs: pool,
				},
			},
		},
	}
}

// EnvLookup abstracts os.LookupEnv for tests.
type EnvLookup func(string) string

// NewFromEnv picks the right manager based on environment:
//
//   - KUBERNETES_SERVICE_HOST set: in-cluster.
//   - otherwise: disabled.
func NewFromEnv(lookup EnvLookup) Manager {
	if lookup == nil {
		lookup = os.Getenv
	}
	if lookup("KUBERNETES_SERVICE_HOST") != "" {
		return NewInCluster()
	}
	return NewDisabled()
}

// inClusterManager is the production Manager.
type inClusterManager struct {
	token   string
	baseURL string
	client  *http.Client
}

func (m *inClusterManager) Enabled() bool    { return true }
func (m *inClusterManager) BaseURL() string  { return m.baseURL }
func (m *inClusterManager) HTTPClient() *http.Client { return m.client }

// Token returns the cached in-cluster bearer token.
func (m *inClusterManager) Token() string { return m.token }

// DisabledReason is always "" for the production manager.
func (m *inClusterManager) DisabledReason() string { return "" }

// disabledManager is the no-op Manager used in local dev and unit tests.
type disabledManager struct{ reason string }

func (disabledManager) Enabled() bool             { return false }
func (disabledManager) BaseURL() string         { return "" }
func (disabledManager) HTTPClient() *http.Client { return nil }
func (m disabledManager) DisabledReason() string { return m.reason }
func (disabledManager) Token() string           { return "" }

// MutationKind distinguishes Create / Update / Delete.
type MutationKind string

const (
	MutationCreate MutationKind = "create"
	MutationUpdate MutationKind = "update"
	MutationDelete MutationKind = "delete"
)

// Outcome is the result of a dual-write CR operation. Metric:
// controller_syncjob_console_dual_write_total{outcome}. Cardinality bounded
// at 5.
type Outcome string

const (
	OutcomeSuccess            Outcome = "success"
	OutcomeAdmissionRejected Outcome = "admission_rejected"
	OutcomeTimeout          Outcome = "timeout"
	OutcomeInvalid          Outcome = "invalid"
	OutcomeDisabled         Outcome = "disabled"
)

// Recorder is the metric sink for DualWriter outcomes.
type Recorder interface {
	RecordDualWrite(MutationKind, Outcome)
}

// DualWriter is the high-level CR write API the Console mutation handlers call.
type DualWriter interface {
	Write(ctx context.Context, input WriteInput) Outcome
}

// WriteInput bundles the inputs to a SyncJob CR write.
type WriteInput struct {
	Scope    Scope
	Name     string
	Spec     *SyncJobSpec
	Mutation MutationKind
}

// DefaultRetryDelays: 3 attempts at 100ms / 400ms / 1.6s. ADR-073 §5.
var DefaultRetryDelays = []time.Duration{
	100 * time.Millisecond,
	400 * time.Millisecond,
	1600 * time.Millisecond,
}

// NoopDualWriter returns a DualWriter that does nothing and reports
// OutcomeSuccess. Used by tests that do not exercise the CR path.
func NoopDualWriter() DualWriter { return noopDualWriter{} }

type noopDualWriter struct{}

func (noopDualWriter) Write(context.Context, WriteInput) Outcome { return OutcomeSuccess }

// WriterAdapter adapts a DualWriter to the transport-agnostic crWriter
// interface used by the Console's mutation handlers. The handler side
// speaks a simpler "scope + name + mutation" vocabulary; the adapter
// fans those out to WriteInput.
type WriterAdapter struct {
	Writer DualWriter
}

// WriteCR satisfies the crWriter contract from console/internal/server.
func (a WriterAdapter) WriteCR(ctx context.Context, scope Scope, name string, mutation MutationKind, spec *SyncJobSpec) {
	a.Writer.Write(ctx, WriteInput{
		Scope:    scope,
		Name:     name,
		Spec:     spec,
		Mutation: mutation,
	})
}

// NoopCRWriter is a crWriter that does nothing. It returns OutcomeSuccess
// for every call without performing any HTTP request. Used in tests that
// do not exercise the CR path.
type NoopCRWriter struct{}

// WriteCR satisfies the crWriter contract.
func (NoopCRWriter) WriteCR(context.Context, Scope, string, MutationKind, *SyncJobSpec) {}

// NewDualWriter wires a real DualWriter. retryDelays may be nil to use DefaultRetryDelays.
func NewDualWriter(manager Manager, recorder Recorder, retryDelays []time.Duration) DualWriter {
	if recorder == nil {
		recorder = noopRecorder{}
	}
	if retryDelays == nil {
		retryDelays = DefaultRetryDelays
	}
	return &realDualWriter{
		manager:     manager,
		recorder:   recorder,
		retryDelays: retryDelays,
	}
}

type realDualWriter struct {
	manager     Manager
	recorder   Recorder
	retryDelays []time.Duration
}

func (w *realDualWriter) Write(ctx context.Context, input WriteInput) Outcome {
	if !w.manager.Enabled() {
		w.recorder.RecordDualWrite(input.Mutation, OutcomeDisabled)
		return OutcomeDisabled
	}
	client := w.manager.HTTPClient()
	if client == nil {
		w.recorder.RecordDualWrite(input.Mutation, OutcomeInvalid)
		return OutcomeInvalid
	}
	outcome := w.executeWithRetry(ctx, client, w.manager.BaseURL(), input)
	w.recorder.RecordDualWrite(input.Mutation, outcome)
	return outcome
}

func (w *realDualWriter) executeWithRetry(ctx context.Context, client *http.Client, baseURL string, input WriteInput) Outcome {
	for attempt := 0; ; attempt++ {
		outcome := w.attempt(ctx, client, baseURL, input)
		if outcome == OutcomeSuccess || outcome == OutcomeAdmissionRejected {
			return outcome
		}
		if attempt == len(w.retryDelays) {
			return outcome
		}
		select {
		case <-ctx.Done():
			return OutcomeTimeout
		case <-time.After(w.retryDelays[attempt]):
		}
	}
}

func (w *realDualWriter) attempt(ctx context.Context, client *http.Client, baseURL string, input WriteInput) Outcome {
	switch input.Mutation {
	case MutationCreate:
		return w.create(ctx, client, baseURL, input)
	case MutationUpdate:
		return w.update(ctx, client, baseURL, input)
	case MutationDelete:
		return w.delete(ctx, client, baseURL, input)
	default:
		return OutcomeInvalid
	}
}

func (w *realDualWriter) create(ctx context.Context, client *http.Client, baseURL string, input WriteInput) Outcome {
	sj := SyncJob{
		APIVersion: "sync.astrasync.io/v1",
		Kind:       "SyncJob",
		Metadata: SyncJobMetadata{
			Name:      input.Name,
			Namespace: input.Scope.Namespace,
			Labels:    map[string]string{TenantLabelKey: input.Scope.TenantID},
		},
		Spec: *input.Spec,
	}
	body, err := json.Marshal(sj)
	if err != nil {
		return OutcomeInvalid
	}
	url := fmt.Sprintf("%s/namespaces/%s/syncjobs", baseURL, input.Scope.Namespace)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return OutcomeInvalid
	}
	w.setK8sAuth(req)
	req.Header.Set("Content-Type", "application/json")
	resp, err := client.Do(req)
	if err != nil {
		if ctx.Err() == context.DeadlineExceeded || ctx.Err() == context.Canceled {
			return OutcomeTimeout
		}
		return OutcomeInvalid
	}
	defer resp.Body.Close()
	return classifyHTTP(resp.StatusCode)
}

func (w *realDualWriter) update(ctx context.Context, client *http.Client, baseURL string, input WriteInput) Outcome {
	getURL := fmt.Sprintf("%s/namespaces/%s/syncjobs/%s", baseURL, input.Scope.Namespace, input.Name)
	getReq, err := http.NewRequestWithContext(ctx, http.MethodGet, getURL, nil)
	if err != nil {
		return OutcomeInvalid
	}
	w.setK8sAuth(getReq)
	getResp, err := client.Do(getReq)
	if err != nil {
		if ctx.Err() == context.DeadlineExceeded || ctx.Err() == context.Canceled {
			return OutcomeTimeout
		}
		return OutcomeInvalid
	}
	defer getResp.Body.Close()
	if getResp.StatusCode == http.StatusNotFound {
		return OutcomeSuccess // CR gone; PostgreSQL is authoritative
	}
	if getResp.StatusCode != http.StatusOK {
		return classifyHTTP(getResp.StatusCode)
	}
	var existing SyncJob
	if err := json.NewDecoder(getResp.Body).Decode(&existing); err != nil {
		return OutcomeInvalid
	}
	existing.Metadata.Labels = map[string]string{TenantLabelKey: input.Scope.TenantID}
	existing.Spec = *input.Spec
	body, err := json.Marshal(existing)
	if err != nil {
		return OutcomeInvalid
	}
	putReq, err := http.NewRequestWithContext(ctx, http.MethodPut, getURL, bytes.NewReader(body))
	if err != nil {
		return OutcomeInvalid
	}
	w.setK8sAuth(putReq)
	putReq.Header.Set("Content-Type", "application/json")
	putResp, err := client.Do(putReq)
	if err != nil {
		if ctx.Err() == context.DeadlineExceeded || ctx.Err() == context.Canceled {
			return OutcomeTimeout
		}
		return OutcomeInvalid
	}
	defer putResp.Body.Close()
	return classifyHTTP(putResp.StatusCode)
}

func (w *realDualWriter) delete(ctx context.Context, client *http.Client, baseURL string, input WriteInput) Outcome {
	url := fmt.Sprintf("%s/namespaces/%s/syncjobs/%s", baseURL, input.Scope.Namespace, input.Name)
	req, err := http.NewRequestWithContext(ctx, http.MethodDelete, url, nil)
	if err != nil {
		return OutcomeInvalid
	}
	w.setK8sAuth(req)
	resp, err := client.Do(req)
	if err != nil {
		if ctx.Err() == context.DeadlineExceeded || ctx.Err() == context.Canceled {
			return OutcomeTimeout
		}
		return OutcomeInvalid
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusNotFound {
		return OutcomeSuccess
	}
	return classifyHTTP(resp.StatusCode)
}

func (w *realDualWriter) setK8sAuth(req *http.Request) {
	// Every Manager implementation that supports writes exposes its bearer
	// token via the Token() method. Disabled managers return "" which
	// produces no Authorization header; that path is guarded by Enabled()
	// above, so disabled managers never reach here.
	if token := w.manager.Token(); token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
}

func classifyHTTP(statusCode int) Outcome {
	switch {
	case statusCode >= 200 && statusCode < 300:
		return OutcomeSuccess
	case statusCode == http.StatusUnprocessableEntity,
		statusCode == http.StatusForbidden:
		return OutcomeAdmissionRejected
	case statusCode == http.StatusRequestTimeout,
		statusCode == http.StatusTooManyRequests,
		statusCode == http.StatusBadGateway,
		statusCode == http.StatusServiceUnavailable,
		statusCode == http.StatusGatewayTimeout:
		return OutcomeTimeout
	default:
		return OutcomeInvalid
	}
}

// ErrManagerDisabled is returned by Write when the Manager is disabled.
// Callers should treat this as a no-op rather than an error.
var ErrManagerDisabled = errors.New("syncjobcr: manager disabled")

type noopRecorder struct{}

func (noopRecorder) RecordDualWrite(MutationKind, Outcome) {}

// Recording is an in-memory Recorder for tests.
type Recording struct {
	mu          sync.Mutex
	Observations []RecordedDualWrite
}

type RecordedDualWrite struct {
	Mutation MutationKind
	Outcome  Outcome
}

func (r *Recording) RecordDualWrite(m MutationKind, o Outcome) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.Observations = append(r.Observations, RecordedDualWrite{Mutation: m, Outcome: o})
}

func (r *Recording) Count(outcome Outcome) int {
	r.mu.Lock()
	defer r.mu.Unlock()
	n := 0
	for _, o := range r.Observations {
		if o.Outcome == outcome {
			n++
		}
	}
	return n
}
