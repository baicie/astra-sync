# ADR-081: Phase 32 — Tenant-Id Envelope Label Translation at the SyncJob CR Writer

## Status

Accepted

> **Correction:** ADR-086 supersedes ADR-071 §2. The CRD CEL rule
> described below was not installable; ADR-087 restores admission
> enforcement with a ValidatingAdmissionPolicy, and the Phase 32 writer
> guard remains the local boundary.

## Context

The Phase 31 cross-module fixture
(`tests/cross-module/chain-tenant-id/chain_test.go`) closed the
**server-side** half of the tenant-id envelope chain:

```
client → BFF → API Server interceptor → Mutation.TenantID →
astrasync_control_jobs.tenant_id
```

But the **client-side** half of the chain — the chain that flows
through the K8s layer — has **no** dedicated Layer-1 assertion
between these two existing surfaces:

1. `console/internal/server/chain_e2e_test.go::TestConsoleTenantIDEnvelopeChain_LabelPropagatesFromBFFToCR`
   pins `syncjobcr.WriteInput.Scope.TenantID` is the BFF's
   `scope.tenantID`. This is the input boundary of the CR writer.

2. `control-plane/controller/internal/controller/syncjob_emission_test.go::TestObserveTransitionEmitsBoundedSeries`
   (and the `controller_job_state_total` / `controller_epoch_fence_total`
   funnels) pin the **output** — the controller reads
   `astrasync.io/tenant-id` from the SyncJob's labels and emits
   `controller_job_state_total{tenant_id="..."}`.

What is NOT pinned is the **middle** of this chain: the
`console/internal/syncjobcr.realDualWriter` call into the
Kubernetes API server constructs the JSON body with
`Labels: map[string]string{TenantLabelKey: input.Scope.TenantID}`
(`manager.go` line 341). The label value is taken verbatim from
`Scope.TenantID`; the Kubernetes API server then enforces a CEL
validation rule that requires the value to be a canonical
lowercase UUID (ADR-071, kubebuilder CEL `XValidation`:

```
self.labels['astrasync.io/tenant-id'].match('^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$')
```

A regression that

- lower-cases / upper-cases the tenant-id (`strings.ToUpper`),
- strips / adds braces (`{...}` → `...`),
- prepends a `urn:uuid:` prefix,
- whitespace-trims asymmetrically,

passes every existing test. Each Layer-1 surface only sees a
**canonical** UUID — the existing chain test uses
`11111111-1111-4111-8111-111111111111`, the existing emission
test uses
`0190f7c4-6c8d-7a01-9d2b-1ecabdff0011`. Neither tests the
**normalisation guarantee at the CR writer boundary**.

The `mutation.go` `Validate()` function (api-server) rejects
non-canonical UUIDs outright (it returns `codes.InvalidArgument`),
so the api-server will not pass a malformed tenant-id to the CR
writer in production. But the CR writer MUST defend in depth:
even if a future refactor of `tenantIDForConnectionUse` is
broken, the Kubernetes API server's CEL validation will still
reject the SyncJob at admission time, but only **after** the
Console emits a metric that says it wrote a SyncJob it didn't
actually write. The CR writer must refuse the **non-canonical**
`Scope.TenantID` locally and emit the correct metric outcome
(`OutcomeInvalid`), regardless of the api-server's behaviour.

## Decision

Phase 32 ships a single-package Layer-1 test
**`console/internal/syncjobcr/manager_label_translation_test.go`**
that pins the label-translation contract at the CR writer
boundary.

The test exercises `realDualWriter.create` directly (the same
function `chain_e2e_test.go::TestConsoleCRWriteIsCalledOnlyAfterPGSuccess`
already drives indirectly) with a controlled `httptest.Server`
that captures the JSON body the writer emits. Five cases:

| Test | Asserts |
| --- | --- |
| `TestLabelTranslationPreservesCanonicalUUID` | `Scope.TenantID` is written verbatim into `metadata.labels["astrasync.io/tenant-id"]`; no whitespace, no case change, no brace change. |
| `TestLabelTranslationRejectsUppercaseTenantID` | `Scope.TenantID = "AAAAAAAA-1111-4111-8111-111111111111"` → writer returns `OutcomeInvalid`, server is not invoked. |
| `TestLabelTranslationRejectsBracedTenantID` | `Scope.TenantID = "{11111111-1111-4111-8111-111111111111}"` → `OutcomeInvalid`. |
| `TestLabelTranslationRejectsURNPrefixedTenantID` | `Scope.TenantID = "urn:uuid:11111111-1111-4111-8111-111111111111"` → `OutcomeInvalid`. |
| `TestLabelTranslationRejectsEmptyTenantID` | `Scope.TenantID = ""` → `OutcomeInvalid`. |

All five cases use `realDualWriter` (the production writer), a
table-driven `t.Run`, and the existing in-package
`httptest.Server` fake. The test suite follows the project's
existing conventions:

- black-box package (`syncjobcr_test`),
- table-driven `t.Run("case_name", ...)` with snake_case case
  names,
- `t.Cleanup` for resource release,
- no `time.Sleep`,
- no `t.Skip`,
- error checking by `errors.Is` / explicit type assertion where
  applicable (the writer returns `Outcome` strings, not errors).

### Why a single-package test, not a cross-module extension

A cross-module test that wires Console → CR writer → controller
emission would require either:

1. **`envtest`** (kubebuilder's apiserver + etcd launcher) — a
   non-trivial transitive dependency: `k8s.io/apimachinery`,
   `k8s.io/client-go`, `sigs.k8s.io/controller-runtime`, plus
   etcd binaries. These dependencies are deliberately **not**
   imported by `console/` (see manager.go's package doc:
   "The package deliberately does NOT import
   io.astrasync/control-plane/controller to avoid pulling the
   controller-runtime dependency chain into the console
   module"). A Phase 32 cross-module envtest fixture would
   require relaxing that constraint, which is a separate ADR.

2. **An in-process fake apiserver** (e.g.
   `k8s.io/client-go/kubernetes/fake`) — sufficient for
   `controller-runtime`'s reconcile path but does NOT exercise
   the actual CEL validator. A passing Phase 32 cross-module
   test against `fake.NewSimpleClientset` would still let a
   `Scope.TenantID = "AAAAAAAA-..."` through, because the fake
   client does not evaluate the `XValidation` rule.

3. **Acceptance via the existing test surface (chosen)** —
   exercise the JSON body the writer produces against a
   `httptest.Server` that parses the body and validates the
   label shape. This pins the **production** contract (the
   exact JSON the writer emits) without depending on K8s
   machinery.

Option 3 is the only one that **simultaneously**:

- pins the verbatim-translation contract;
- does not depend on K8s machinery;
- does not change the `console` module's dependency surface;
- runs at unit-test speed (sub-second per case);
- fits in Phase 32's "test-only" scope.

A future Phase 33 may revisit envtest under ADR-082 if the
project wishes to drive the full controller reconcile loop
end-to-end. That decision is out of scope here.

### Documentation

- This ADR (ADR-081).
- `docs/phase32/README.md` describing the phase goal, the five
  cases, and the rationale for the single-package choice.
- `CHANGELOG.md` Unreleased entry summarising the contract.
- `docs/adr/README.md` index gains an ADR-081 row marked
  Accepted.

### CI integration

The Phase 32 test runs under the existing
`console` Go module CI lane; no new workflow file. The test is
picked up by `go test ./console/internal/syncjobcr/...`.

## Consequences

### Positive

- The label-translation contract is pinned at the CR writer
  boundary in a single-package Layer-1 test. A regression that
  upper-cases / braces / prefixes / strips the tenant-id breaks
  this test before the JSON reaches the K8s API server.
- The test does not change `console`'s dependency surface
  (no controller-runtime, no envtest, no client-go fake).
- The test runs at unit-test speed (sub-second).
- The contract documented here generalises to any future
  label-translation point (e.g. when the Console emits other
  K8s resources, such as ConfigMaps for connection secrets).

### Negative

- The Phase 32 test still does not exercise the **full** K8s
  round-trip (CEL validation, admission controller, etcd
  persistence). A future Phase 33 with `envtest` would extend
  this coverage. Until then, the unit test pins the JSON body
  shape, and the in-cluster CEL rule (ADR-071) remains the
  authoritative secondary check.
- The test only covers `create`; `update` and `delete` use the
  same translation logic (manager.go line 392 — Update sets
  `existing.Metadata.Labels = map[string]string{TenantLabelKey:
  input.Scope.TenantID}`), so a duplicate `update` test would
  be redundant. The five cases pin `create`, which is the
  contract; a Phase 33 follow-up may add a focused `update`
  variant.

## Alternatives Considered

### Cross-module `envtest` fixture

Use kubebuilder `envtest` to boot a real K8s apiserver + etcd
in-process, register the SyncJob CRD, run the dual writer against
it, and observe admission rejection for non-canonical labels.

**Reject for Phase 32.** envtest depends on etcd binaries, which
inflate the test binary and the CI lane. The Phase 31 ADR-080 §3
explicitly listed this as out of scope. Phase 32 sticks to the
single-package test boundary.

### In-process `fake.NewSimpleClientset` cross-module fixture

Use `k8s.io/client-go/kubernetes/fake` to simulate an apiserver
in-process.

**Reject.** The fake does not evaluate `XValidation` rules, so
the contract under test (canonical UUID enforcement) would be
silently bypassed. A green test would not catch the regression.

### Rely on the existing CEL rule (server-side enforcement)

Drop the Phase 32 test; rely on the K8s API server's CEL rule to
reject non-canonical `Scope.TenantID` at admission time.

**Reject.** The Console emits a metric (`controller_job_state_total`
proxy) that says it wrote a SyncJob it didn't actually write.
Defense in depth: the writer should refuse the malformed input
locally and emit the `OutcomeInvalid` metric.

### Move label translation validation upstream to the BFF

Reject non-canonical `Scope.TenantID` in
`console/internal/server/job_handlers.go` rather than at the CR
writer.

**Partially Accept.** The BFF ingress already validates
canonical UUIDs (Phase 28-A ADR-072, Phase 29 ADR-074). The
CR writer's defence-in-depth is a complementary check; both
must hold. Removing the writer's check leaves a regression
window if the BFF is replaced or the CR writer is reused from
another caller.

## Follow-ups (out of scope for this ADR)

- **Phase 33 (envtest)**: extend the cross-module fixture to
  drive the controller reconcile loop with the SyncJob CRD
  registered against `envtest`'s apiserver. This would pin the
  full chain Console → CR writer → admission → controller
  reconcile → metric emission. ADR-082, when written, must
  weigh envtest's binary overhead against the coverage.
- **Update mutation**: add a focused single-package test that
  pins `update` carries the same label translation. Today's
  test covers `create`; the production code uses the same
  translation logic for `update` (manager.go line 392), so this
  test is a redundant safety-net rather than a new contract.
