# Phase 6 Slice 20 Verification

## Status

Implementation complete; production rollout is operator-controlled. Repository closeout evidence
was collected on 2026-08-10 with every rollout gate at its default disabled value.

## Delivery Traceability

| Requirement | Authoritative implementation evidence | Verification evidence |
|---|---|---|
| Deployment-owned connector catalog | Java descriptor SPI and inventory export, protobuf contracts, PostgreSQL catalog activation, Catalog list/get API | Connector schema tests, deterministic inventory comparison, catalog repository integration |
| Tenant Connection lifecycle | Typed domain/repository, ten RPCs, CAS versions, immutable generations, idempotency, tombstones | Memory/service suites and PostgreSQL atomic lifecycle/concurrent executor tests |
| OIDC, RBAC, and audit | OIDC validator/resolver, generated-method registry, tenant authorizer, transactional audit, Console BFF sessions/CSRF | Auth unit tests, PostgreSQL identity integration, API/BFF tampering and redaction tests |
| Stable Job bindings | Stable Job UID plus atomic Job-to-Connection rows | Job mutation service and PostgreSQL binding replacement/foreign-key tests |
| Epoch generation snapshot | Start transaction captures immutable source/sink generations and compiler revision | PostgreSQL Job lifecycle/generation-fencing race integration |
| External Secret boundary | Restricted locator types and exact immutable Kubernetes Secret provider | Provider policy, UID recreation, exact-key, size, and sentinel tests |
| Scheduler materialization | Lease-fenced deterministic execution Secret, durable receipts, runtime mounts, cleanup | Materializer/dispatcher tests and PostgreSQL receipt/cleanup integration |
| Java runtime merge | Strict mounted credential envelope and `RuntimeCredentialLoader` | Runtime loader validation, duplicate ownership, identity, and zeroization-oriented tests |
| Isolated Connection test | Database queue, fenced claims, dedicated executor, DNS/egress/SSRF controls | Admission limits, executor fencing, resolver/policy/probe, timeout, and sanitized result tests |
| Console workflows | OIDC BFF routes, catalog/Connection/Job forms, CAS/conflict and polling behavior | Go BFF and same-origin return-path tests, JavaScript syntax check, browser closeout below |
| Migration and rollback | Additive migrations, default-closed gates, Helm dependency checks, operator runbook | Helm default/invalid/full render matrix and runbook query review |

## Public RPC Coverage

| Service | Methods covered |
|---|---|
| `ConnectorCatalogService` | `ListConnectorDescriptors`, `GetConnectorDescriptor` |
| `ConnectionService` | `CreateConnection`, `GetConnection`, `ListConnections`, `UpdateConnection`, `RotateConnection`, `EnableConnection`, `DisableConnection`, `DeleteConnection`, `TestConnection`, `GetConnectionTest` |
| `JobValidationService` | `ValidateJobSpec` with descriptor, tenant, Connection, and runtime-gate validation |
| `JobService` | All eight lifecycle methods through deny-by-default authorization; mutations use CAS/idempotency/audit and stable binding rules |

Generated-service registry tests fail when a public method lacks an explicit permission mapping.
Catalog and Connection projections are allowlisted; Secret locators, provider values, OIDC tokens,
request bodies, and credential sentinels are excluded from public response, audit, log, and
idempotency surfaces.

## Lifecycle and Race Evidence

| Boundary | Required outcome | Covered by |
|---|---|---|
| Job Update vs Connection Delete | Stable foreign key prevents a dangling or recaptured name reference | Job mutation and Connection PostgreSQL integration |
| Start vs Rotate | Epoch captures exactly one committed generation | Atomic Job mutation generation-fencing integration |
| Start vs Disable | Transaction winner determines admission; accepted epoch is immutable | Service/repository race tests |
| Catalog rollout vs Start | Active descriptor/compiler revisions match or Start fails closed | Catalog/validation service suites |
| Scheduler retry vs rotation | Retry reads epoch binding, never current Connection generation | Materialization store and dispatcher suites |
| Secret delete/recreate | Exact pinned Kubernetes UID mismatch is rejected | Kubernetes provider tests |
| Lease loss vs materialization | Stale owner cannot launch Coordinator or commit receipts | Dispatcher/materializer/store tests |
| Terminal/Stop cleanup | Data-plane resources precede execution credential Secret cleanup; external Secret survives | Dispatcher cleanup/orphan tests |
| Test executor retry | Lease/executor fencing prevents stale completion | Connection executor PostgreSQL integration |

## Automated Closeout

| Gate | Command or scope | Result |
|---|---|---|
| Go formatting, vet, unit tests | Seven modules listed in `Makefile` | PASS |
| PostgreSQL integration | Job, auth, catalog, Connection, materialization, Scheduler dispatch, Controller convergence | PASS |
| Java reactor and coverage | `mvn -B -ntp verify -DskipITs` | PASS |
| Java formatting | `mvn -B -ntp spotless:check` | PASS |
| Connector inventory | Export deployed inventory and byte/hash compare with `deployment/catalog/connector-inventory.pb` | PASS |
| Protobuf | Buf 1.34.0 lint plus deterministic Go generation and clean generated diff | PASS |
| Helm | Lint; default closed; reject runtime/test half-enable; render full enable | PASS |
| Linux Go build | `GOOS=linux GOARCH=amd64 go build ./...` in all seven modules | PASS |
| Console JavaScript | `node --check console/internal/server/web/app.js` | PASS |
| Container images | API, compiler-validation, Controller, Scheduler, test executor, Console, Coordinator, Worker | PASS |
| Console browser | Authenticated workflow layout and interaction at desktop/mobile viewports | ENVIRONMENT LIMITED; evidence boundary below |
| Whitespace and links | `git diff --check` and relative Markdown link validation | PASS |

The in-app Browser could not initialize in this execution environment because its runtime rejected
the required `node:process` import. This is a verification-tool limitation, not an application
failure. The built Console image served `GET /health` with HTTP 200 at `http://127.0.0.1:18090`;
the BFF/session/security suites, same-origin return-path regression tests, JavaScript syntax check,
responsive CSS review, and CSP review passed. No real OIDC provider or control-plane backend was
attached to that local container, so an authenticated browser workflow is not claimed here.

The PostgreSQL closeout uses PostgreSQL 16 and includes:

```text
TestRepositoryPersistsLifecycleAcrossConnections
TestAtomicJobMutationsFenceConnectionGenerations
TestRepositoryAtomicallyActivatesCatalog
TestRepositoryMaterializesOIDCIdentityAndTenantMembership
TestRepositoryPersistsAtomicConnectionLifecycle
TestRepositoryFencesConcurrentConnectionTestExecutors
TestStorePersistsLeaseFencedMaterializationReceiptsAndCleanup
TestStoreSerializesCapacityAndFencesLeaseAndHeartbeatTakeover
TestReconcilerConvergesSyncJobWithPostgres
```

## Rollout Evidence Boundary

The repository proves implementation, default-closed packaging, and local/CI behavior. It does not
claim that a real identity provider, tenant Secret namespace, staging cluster, or production tenant
has been enabled. Operators must execute
[`migration-and-rollback.md`](migration-and-rollback.md), retain environment-specific evidence, and
approve each expansion. Production rollout is not a prerequisite for merging code whose shipped
behavior remains disabled, but it is a prerequisite for claiming production availability.
