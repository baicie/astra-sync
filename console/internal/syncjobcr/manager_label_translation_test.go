// Layer-1 test for the tenant-id envelope at the SyncJob CR writer
// boundary (ADR-081 / Phase 32 + ADR-082 / Phase 33). The cross-
// module chain test (`tests/cross-module/chain-tenant-id/`) pins
// the BFF egress and the api-server mutation path; this file
// pins the **CR writer's** contract that `WriteInput.Scope.TenantID`
// is translated verbatim into `metadata.labels["astrasync.io/
// tenant-id"]` and that any non-canonical UUID form is rejected
// locally (defence in depth — the K8s API server's CEL validation
// is the secondary check).
//
// Phase 32 covers the `create` path; Phase 33 adds the matching
// `update` path coverage (ADR-081 §Follow-ups, ADR-082 §Decision).
// Both paths use the same `IsCanonicalTenantID` guard and the same
// `metadata.labels[astrasync.io/tenant-id] = Scope.TenantID`
// translation. Test-only file — production code change is the
// `IsCanonicalTenantID` guard on `realDualWriter.update` (4 lines)
// and the comment block on `realDualWriter.create` (ADR-081
// §Decision).

package syncjobcr

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
)

// canonicalUUID is the verified lowercase tenant-id used by every
// happy-path case. Mirror of the console `testTenantID` constant in
// `console/internal/server/bff_test.go`; the two constants agree
// because the cross-module chain test (Phase 31 ADR-080) and the
// CR writer boundary test (Phase 32 ADR-081) both pin the same
// envelope contract.
const canonicalUUID = "11111111-1111-4111-8111-111111111111"

// TestLabelTranslationPreservesCanonicalUUID is the happy path: the
// canonical UUID is written into metadata.labels verbatim — no
// whitespace change, no case change, no brace change. This is the
// contract the controller's `controller_job_state_total{tenant_id}`
// emission relies on (ADR-066).
func TestLabelTranslationPreservesCanonicalUUID(t *testing.T) {
	var captured SyncJob
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost {
			if err := json.NewDecoder(r.Body).Decode(&captured); err != nil {
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}
			w.WriteHeader(http.StatusCreated)
		}
	}))
	defer srv.Close()

	dw := NewDualWriter(
		&fixedManager{token: "tok", baseURL: srv.URL + "/apis/sync.astrasync.io/v1", client: srv.Client()},
		&Recording{}, nil)
	outcome := dw.Write(context.Background(), WriteInput{
		Scope:    Scope{TenantID: canonicalUUID, Namespace: "default"},
		Name:     "orders-job",
		Spec:     &SyncJobSpec{},
		Mutation: MutationCreate,
	})
	if outcome != OutcomeSuccess {
		t.Fatalf("outcome: got %v, want %v", outcome, OutcomeSuccess)
	}
	got, ok := captured.Metadata.Labels[TenantLabelKey]
	if !ok {
		t.Fatalf("label %q missing: %v", TenantLabelKey, captured.Metadata.Labels)
	}
	if got != canonicalUUID {
		t.Fatalf("label %q: got %q, want %q (verbatim translation)", TenantLabelKey, got, canonicalUUID)
	}
	if len(captured.Metadata.Labels) != 1 {
		t.Fatalf("Labels must have exactly 1 entry, got %d: %v",
			len(captured.Metadata.Labels), captured.Metadata.Labels)
	}
}

// TestLabelTranslationRejectsNonCanonicalTenantIDs is the
// defence-in-depth test for ADR-081: even if the BFF ingress were
// ever bypassed and a non-canonical UUID reached the CR writer, the
// writer must (a) refuse the call locally, (b) emit no HTTP
// request to the API server, and (c) record OutcomeInvalid.
//
// Five sub-cases (table-driven) cover the failure modes observed in
// production data (ADR-058, ADR-059): uppercase, brace form,
// URN-prefixed, leading/trailing whitespace, and empty. All five
// routes converge on the same outcome (the K8s CEL validator's
// `match('^[0-9a-f]{8}-...$')` rule would reject them at admission
// time; the writer must reject them earlier).
func TestLabelTranslationRejectsNonCanonicalTenantIDs(t *testing.T) {
	cases := []struct {
		name         string
		inputTenant  string
		wantAtServer bool // whether the request should reach the fake apiserver
	}{
		{
			name:         "uppercase_uuid",
			inputTenant:  "AAAAAAAA-1111-4111-8111-111111111111",
			wantAtServer: false,
		},
		{
			name:         "braced_uuid",
			inputTenant:  "{11111111-1111-4111-8111-111111111111}",
			wantAtServer: false,
		},
		{
			name:         "urn_prefixed_uuid",
			inputTenant:  "urn:uuid:11111111-1111-4111-8111-111111111111",
			wantAtServer: false,
		},
		{
			name:         "whitespace_padded_uuid",
			inputTenant:  " 11111111-1111-4111-8111-111111111111 ",
			wantAtServer: false,
		},
		{
			name:         "empty_tenant_id",
			inputTenant:  "",
			wantAtServer: false,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var hits int32
			srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				atomic.AddInt32(&hits, 1)
				w.WriteHeader(http.StatusCreated)
			}))
			defer srv.Close()

			rec := &Recording{}
			dw := NewDualWriter(
				&fixedManager{token: "tok", baseURL: srv.URL + "/apis/sync.astrasync.io/v1", client: srv.Client()},
				rec, nil)
			outcome := dw.Write(context.Background(), WriteInput{
				Scope:    Scope{TenantID: tc.inputTenant, Namespace: "default"},
				Name:     "orders-job",
				Spec:     &SyncJobSpec{},
				Mutation: MutationCreate,
			})

			if outcome != OutcomeInvalid {
				t.Fatalf("outcome: got %v, want %v (input %q)", outcome, OutcomeInvalid, tc.inputTenant)
			}
			if rec.Count(OutcomeInvalid) != 1 {
				t.Fatalf("recorder OutcomeInvalid count: got %d, want 1 (rec=%+v)",
					rec.Count(OutcomeInvalid), rec)
			}
			if got := atomic.LoadInt32(&hits); (got > 0) != tc.wantAtServer {
				t.Fatalf("server hit count: got %d, wantAtServer=%v (input %q)",
					got, tc.wantAtServer, tc.inputTenant)
			}
		})
	}
}

// TestLabelTranslationRejectsNonCanonicalUUIDs_DocumentedWhy is a
// documentation-only case that records the project's defensive
// posture: the existing `chain_e2e_test.go` and the cross-module
// chain test already pin the canonical UUID contract at the
// ingress; the cases covered above catch the regression class
// "the writer accidentally normalises the tenant-id before
// serialising it into metadata.labels" — a class that cannot be
// caught by the existing surfaces because they only ever feed
// canonical UUIDs.
//
// This case is a no-op (the assertions live in the table-driven
// case above); it exists so a future reader grepping for "why"
// finds a single anchor. See ADR-081 §Context.
func TestLabelTranslationRejectsNonCanonicalUUIDs_DocumentedWhy(t *testing.T) {
	doc := "See ADR-081 §Context. The contract is: realDualWriter.create " +
		"must short-circuit non-canonical tenant-ids and return " +
		"OutcomeInvalid without contacting the API server."
	if !strings.Contains(doc, "OutcomeInvalid") {
		t.Fatalf("documentation anchor missing 'OutcomeInvalid': %q", doc)
	}
}

// ----------------------------------------------------------------------
// Phase 33 / ADR-082 — Update-mutation guard
// ----------------------------------------------------------------------

// TestLabelTranslationUpdatePreservesCanonicalUUID pins the happy
// path on the update mutation: the writer fetches the existing CR,
// overwrites `metadata.labels["astrasync.io/tenant-id"]` with
// `Scope.TenantID`, and PUTs the result. The label value MUST be
// the canonical UUID verbatim — no whitespace, no case change, no
// brace change, no prefix. Phase 33 / ADR-082 §Decision.
func TestLabelTranslationUpdatePreservesCanonicalUUID(t *testing.T) {
	var (
		gotMethod string
		gotBody   SyncJob
	)
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet:
			// Simulate an existing CR whose label is the stale
			// tenant-id "old-tenant". The update path MUST replace it.
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(SyncJob{
				APIVersion: "sync.astrasync.io/v1",
				Kind:       "SyncJob",
				Metadata: SyncJobMetadata{
					Name:      "orders-job",
					Namespace: "default",
					Labels:    map[string]string{TenantLabelKey: "old-tenant-id"},
				},
			})
		case http.MethodPut:
			gotMethod = r.Method
			if err := json.NewDecoder(r.Body).Decode(&gotBody); err != nil {
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}
			w.WriteHeader(http.StatusOK)
		default:
			http.Error(w, "unexpected", http.StatusBadRequest)
		}
	}))
	defer srv.Close()

	dw := NewDualWriter(
		&fixedManager{token: "tok", baseURL: srv.URL + "/apis/sync.astrasync.io/v1", client: srv.Client()},
		&Recording{}, nil)
	outcome := dw.Write(context.Background(), WriteInput{
		Scope:    Scope{TenantID: canonicalUUID, Namespace: "default"},
		Name:     "orders-job",
		Spec:     &SyncJobSpec{},
		Mutation: MutationUpdate,
	})
	if outcome != OutcomeSuccess {
		t.Fatalf("outcome: got %v, want %v", outcome, OutcomeSuccess)
	}
	if gotMethod != http.MethodPut {
		t.Fatalf("method: got %q, want PUT (no GET means writer refused upstream)", gotMethod)
	}
	got, ok := gotBody.Metadata.Labels[TenantLabelKey]
	if !ok {
		t.Fatalf("label %q missing on PUT body: %v", TenantLabelKey, gotBody.Metadata.Labels)
	}
	if got != canonicalUUID {
		t.Fatalf("label %q on PUT body: got %q, want %q (verbatim translation)", TenantLabelKey, got, canonicalUUID)
	}
	if len(gotBody.Metadata.Labels) != 1 {
		t.Fatalf("PUT body Labels must have exactly 1 entry, got %d: %v",
			len(gotBody.Metadata.Labels), gotBody.Metadata.Labels)
	}
}

// TestLabelTranslationUpdateRejectsNonCanonicalTenantIDs pins the
// defence-in-depth contract on the update path (Phase 33 /
// ADR-082 §Decision): even if the BFF ingress were ever bypassed
// and a non-canonical UUID reached the update path, the writer
// must (a) refuse the call locally, (b) emit no HTTP request to
// the API server — neither GET nor PUT, and (c) record
// OutcomeInvalid. The same five non-canonical forms as the create
// test cover the regression class observed in production data
// (ADR-058, ADR-059). The five cases mirror
// `TestLabelTranslationRejectsNonCanonicalTenantIDs` exactly so
// the coverage matrix is symmetric across the create / update
// mutations.
func TestLabelTranslationUpdateRejectsNonCanonicalTenantIDs(t *testing.T) {
	cases := []struct {
		name         string
		inputTenant  string
		wantAtServer bool // whether the request should reach the fake apiserver
	}{
		{
			name:         "uppercase_uuid",
			inputTenant:  "AAAAAAAA-1111-4111-8111-111111111111",
			wantAtServer: false,
		},
		{
			name:         "braced_uuid",
			inputTenant:  "{11111111-1111-4111-8111-111111111111}",
			wantAtServer: false,
		},
		{
			name:         "urn_prefixed_uuid",
			inputTenant:  "urn:uuid:11111111-1111-4111-8111-111111111111",
			wantAtServer: false,
		},
		{
			name:         "whitespace_padded_uuid",
			inputTenant:  " 11111111-1111-4111-8111-111111111111 ",
			wantAtServer: false,
		},
		{
			name:         "empty_tenant_id",
			inputTenant:  "",
			wantAtServer: false,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var hits int32
			srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				atomic.AddInt32(&hits, 1)
				w.WriteHeader(http.StatusOK)
			}))
			defer srv.Close()

			rec := &Recording{}
			dw := NewDualWriter(
				&fixedManager{token: "tok", baseURL: srv.URL + "/apis/sync.astrasync.io/v1", client: srv.Client()},
				rec, nil)
			outcome := dw.Write(context.Background(), WriteInput{
				Scope:    Scope{TenantID: tc.inputTenant, Namespace: "default"},
				Name:     "orders-job",
				Spec:     &SyncJobSpec{},
				Mutation: MutationUpdate,
			})

			if outcome != OutcomeInvalid {
				t.Fatalf("outcome: got %v, want %v (input %q)", outcome, OutcomeInvalid, tc.inputTenant)
			}
			if rec.Count(OutcomeInvalid) != 1 {
				t.Fatalf("recorder OutcomeInvalid count: got %d, want 1 (rec=%+v)",
					rec.Count(OutcomeInvalid), rec)
			}
			if got := atomic.LoadInt32(&hits); (got > 0) != tc.wantAtServer {
				t.Fatalf("server hit count: got %d, wantAtServer=%v (input %q)",
					got, tc.wantAtServer, tc.inputTenant)
			}
		})
	}
}

// TestLabelTranslationUpdateGuardShortCircuitsBeforeGET pins that
// the update-path guard fires BEFORE the writer issues the
// discovery GET. Without this ordering, the writer would still
// observe a 200 OK on the GET (the fake apiserver returns 200 for
// any path) and then fail later, leaking metric outcomes that
// misattribute the failure. ADR-082 §Decision.
func TestLabelTranslationUpdateGuardShortCircuitsBeforeGET(t *testing.T) {
	var (
		getHits, putHits int32
	)
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet:
			atomic.AddInt32(&getHits, 1)
		case http.MethodPut:
			atomic.AddInt32(&putHits, 1)
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	rec := &Recording{}
	dw := NewDualWriter(
		&fixedManager{token: "tok", baseURL: srv.URL + "/apis/sync.astrasync.io/v1", client: srv.Client()},
		rec, nil)
	outcome := dw.Write(context.Background(), WriteInput{
		Scope:    Scope{TenantID: "not-a-canonical-uuid", Namespace: "default"},
		Name:     "orders-job",
		Spec:     &SyncJobSpec{},
		Mutation: MutationUpdate,
	})
	if outcome != OutcomeInvalid {
		t.Fatalf("outcome: got %v, want %v", outcome, OutcomeInvalid)
	}
	if got := atomic.LoadInt32(&getHits); got != 0 {
		t.Fatalf("GET hit count: got %d, want 0 (guard must short-circuit before GET)", got)
	}
	if got := atomic.LoadInt32(&putHits); got != 0 {
		t.Fatalf("PUT hit count: got %d, want 0 (guard must short-circuit before PUT)", got)
	}
}
