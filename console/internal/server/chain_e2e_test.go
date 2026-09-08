package server_test

import (
	"net/http"
	"testing"

	"io.astrasync/console/internal/syncjobcr"
)

// TestConsoleTenantIDEnvelopeChain_WritesSurface is the Phase 30
// (ADR-075 §1) end-to-end regression test for the tenant-id envelope
// contract over the Console-side slice of the chain.
//
// The chain has three deliverables:
//
//  1. Console BFF egress: append `x-astra-tenant-id` to outgoing gRPC
//     metadata on the mutation backend call. ADR-072.
//  2. Console dual-write: inject `astrasync.io/tenant-id` into the
//     captured SyncJob CR write payload's tenant-id signal. ADR-073.
//  3. Controller reconcile observer: read that label and emit
//     `controller_job_state_total{tenant_id=<verified>}` on state
//     transition (covered by
//     control-plane/controller/internal/controller/syncjob_emission_test.go
//     — the Console module deliberately does NOT depend on the
//     controller-runtime module, see ADR-073 §1).
//
// The chain test cross-wires steps 1 and 2 inside the same request:
// the same `testTenantID` must surface verbatim on the api-server
// call AND on the CR writer payload. If a future refactor swaps
// `scope.tenantID` for `session.Principal.ID` (or any other origin)
// the cross-wired assertion fails with a clear chain[X] message:
//
//   "chain[egress] mismatch: backend.lastTenantID = X, want Y"
//   "chain[cr-write] mismatch: cr.Scope.TenantID = X, want Y".
func TestConsoleTenantIDEnvelopeChain_WritesSurface(t *testing.T) {
	verifiedTenantID := testTenantID // the sole membership in fakeSessions

	backend := &jobMutationBackend{}
	crManager := newCaptureCRManager(t)
	handler := newSlice28bHandler(t, backend, crManager, nil)
	body := `{"name":"orders-job","spec":` + minimalValidJobSpec + "}"

	response := bffRequest(handler, http.MethodPost, "/api/jobs", body, jobMutationHeaders())
	if response.Code != http.StatusOK {
		t.Fatalf("create returned %d: %s", response.Code, response.Body.String())
	}

	// 1. BFF egress: the api-server must have received the metadata
	// verbatim. Sourced from the same `verifiedTenantID` that the
	// session establishes, so step 2 has a stable counterpart.
	if got := backend.lastTenantID; got != verifiedTenantID {
		t.Fatalf("chain[egress] mismatch: backend.lastTenantID = %q, want %q",
			got, verifiedTenantID)
	}

	// 2. Console dual-write: the CR writer must have received the same
	// tenant-id — its `create` uses it as the K8s label
	// `astrasync.io/tenant-id` per syncjobcr.TenantLabelKey (ADR-073 §4).
	crCalls := crManager.Calls()
	if len(crCalls) != 1 {
		t.Fatalf("chain[cr-write] expected 1 CR write, got %d", len(crCalls))
	}
	if got := crCalls[0].Scope.TenantID; got != verifiedTenantID {
		t.Fatalf("chain[cr-write] mismatch: cr.Scope.TenantID = %q, want %q",
			got, verifiedTenantID)
	}
	if crCalls[0].Mutation != syncjobcr.MutationCreate {
		t.Fatalf("chain[cr-write] mismatch: cr.Mutation = %q, want %q",
			crCalls[0].Mutation, syncjobcr.MutationCreate)
	}
	if crCalls[0].Name != "orders-job" {
		t.Fatalf("chain[cr-write] mismatch: cr.Name = %q, want %q",
			crCalls[0].Name, "orders-job")
	}
}

// TestConsoleTenantIDEnvelopeChain_NoUnknownLeak is the negative guard:
// when the operator presents an X-Astra-Tenant-ID that is NOT in the
// session's membership set, the BFF MUST reject the mutation with 403
// BEFORE touching either the api-server egress OR the CR write. The
// cross-tenant denial must be atomic at the scope-check boundary: a
// future refactor that lazily evaluates scope must fail this test.
func TestConsoleTenantIDEnvelopeChain_NoUnknownLeak(t *testing.T) {
	const unverifiedTenantID = "0190f7c4-6c8d-7a01-9d2b-1ecabdff0022"

	backend := &jobMutationBackend{}
	crManager := newCaptureCRManager(t)
	handler := newSlice28bHandler(t, backend, crManager, nil)
	headers := jobMutationHeaders()
	headers["X-Astra-Tenant-ID"] = unverifiedTenantID
	body := `{"name":"orders-job","spec":` + minimalValidJobSpec + "}"
	response := bffRequest(handler, http.MethodPost, "/api/jobs", body, headers)
	if response.Code != http.StatusForbidden {
		t.Fatalf("expected cross-tenant denial, got %d %s", response.Code, response.Body.String())
	}
	if backend.createCalls != 0 {
		t.Fatalf("chain[egress] backend leaked despite rejection: calls=%d", backend.createCalls)
	}
	if got := len(crManager.Calls()); got != 0 {
		t.Fatalf("chain[cr-write] leaked despite rejection: got %d calls %+v",
			got, crManager.Calls())
	}
}

// TestConsoleTenantIDEnvelopeChain_LabelPropagatesFromBFFToCR is the
// "expected label" assertion: when the BFF forwards `X-Astra-Tenant-ID`,
// the CR writer MUST receive that same tenant-id via
// `WriteInput.Scope.TenantID`. This pins the chain directly — the
// `TestConsoleTenantIDEnvelopeChain_WritesSurface` chains both ends,
// but the CR writer itself is the producer of the tenant-id label
// (see syncjobcr.realDualWriter.create) and the propagation MUST be
// pinned independently so a future refactor of createJob cannot
// silently swap scope.tenantID for some other identity (e.g.
// session.Principal.ID).
func TestConsoleTenantIDEnvelopeChain_LabelPropagatesFromBFFToCR(t *testing.T) {
	verifiedTenantID := testTenantID

	backend := &jobMutationBackend{}
	crManager := newCaptureCRManager(t)
	handler := newSlice28bHandler(t, backend, crManager, nil)
	body := `{"name":"orders-job","spec":` + minimalValidJobSpec + "}"
	response := bffRequest(handler, http.MethodPost, "/api/jobs", body, jobMutationHeaders())
	if response.Code != http.StatusOK {
		t.Fatalf("create returned %d: %s", response.Code, response.Body.String())
	}
	crCalls := crManager.Calls()
	if len(crCalls) != 1 {
		t.Fatalf("expected 1 CR write, got %d", len(crCalls))
	}
	if got := crCalls[0].Scope.TenantID; got != verifiedTenantID {
		t.Fatalf("CR.Scope.TenantID = %q, want %q", got, verifiedTenantID)
	}
	if crCalls[0].Mutation != syncjobcr.MutationCreate {
		t.Fatalf("CR mutation = %q, want Create", crCalls[0].Mutation)
	}
	if crCalls[0].Name != "orders-job" {
		t.Fatalf("CR name = %q, want orders-job", crCalls[0].Name)
	}
}
