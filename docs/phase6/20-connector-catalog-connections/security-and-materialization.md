# Phase 6 Slice 20 Security and Credential Materialization

## Status

Implementation-ready security design. The current Scheduler rejects `connectionRef`; no Secret
provider or materialization path is implemented.

## Protected Assets

The primary protected assets are external credential bytes, private keys, certificates, access
tokens, endpoint metadata, Secret locators, tenant identity, and the ability to cause a data-plane
workload to use a credential. A Connection name and UID are not credentials, but they remain
tenant-scoped operational metadata and are excluded from ordinary logs.

The design addresses these threats:

- cross-tenant reference substitution or Secret namespace traversal;
- raw secret persistence in JobSpec, PostgreSQL, browser state, audit, logs, or traces;
- descriptor fields that misclassify a sensitive option as Job owned;
- rotation races that silently change an accepted execution;
- mutable provider aliases that make retry non-deterministic;
- Scheduler retry overwriting another epoch's credentials;
- provider errors or connector exceptions echoing secret or endpoint values;
- connection testing becoming an SSRF proxy or performing external writes;
- stale materialized Secrets surviving execution cleanup; and
- service identities gaining broad read access unrelated to scheduled tenant work.

## Trust Boundaries

```text
untrusted browser / gRPC client
        |
        | no secret bytes; write-only locator input
        v
API Server + PostgreSQL metadata
        |
        | stable UID/generation only
        v
Scheduler lease owner
        |
        | scoped provider request over authenticated workload identity
        v
Secret provider adapter ----> external Secret bytes
        |
        | in-memory bounded material
        v
immutable epoch Secret ----> epoch data-plane pods ----> connector factory
```

The API Server never receives provider bytes. The provider adapter/materializer is a separate
internal component or tightly isolated package with a dedicated service account and no public
listener. The Scheduler supplies a database-proven tenant, Connection UID, generation, and epoch;
the materializer does not accept a caller-selected namespace or arbitrary provider path.

## Provider Contract

The internal provider SPI has a deliberately small contract:

| Operation | Behavior |
|---|---|
| `validateLocatorShape` | Pure validation of provider kind, bounded fields, and descriptor-required keys |
| `materialize` | Resolve one exact immutable provider object and return required byte fields in a closeable buffer |
| `describeReceipt` | Return safe provider kind, object UID/version token, and field-presence metadata |

There is no list-all-Secrets, reveal, export, arbitrary-key read, update, rotate, or delete method in
the runtime SPI. Administrative metadata selection, if later exposed, uses another API and service
identity that returns no Secret data.

Materialization input includes authenticated tenant UUID, server-owned provider policy ID,
restricted locator, expected connector/Connection schema revision, required logical secret fields,
Job UID, epoch, and deadline. The adapter rejects unknown fields and returns only the descriptor-
declared logical fields. Buffers are bounded, never converted to diagnostic strings, and are
zeroed/closed on best effort immediately after Kubernetes Secret creation.

## Initial Kubernetes Secret Provider

The first provider is `KUBERNETES_SECRET_V1`. The external Secret is provisioned outside AstraSync
and must satisfy all of these conditions:

- it is in the Kubernetes namespace mapped server-side from tenant UUID;
- it has an AstraSync tenant ownership label matching that mapping;
- `immutable: true` is set;
- its name and UID match the restricted locator captured in the Connection generation;
- its data keys match the bounded descriptor-to-provider key mapping; and
- the materializer service account is authorized to `get` only Secrets in permitted tenant
  namespaces, with admission/network controls narrowing effective access.

The public request never supplies a Kubernetes namespace. The write-only locator contains Secret
name, Secret UID, and logical-field-to-data-key mapping. Pinning UID plus requiring immutability
prevents delete/recreate or in-place data changes from silently altering a generation. Rotation
points to a newly provisioned immutable Secret object and creates a new Connection generation.

Kubernetes RBAC cannot generally restrict `get` by resource name. Deployments therefore separate
tenant namespaces, validate ownership labels and UID after retrieval, use a dedicated materializer
service account, and apply audit policy. Higher-assurance multi-tenant installations should use
provider-specific workload identity or per-tenant materializer identities. A cluster-wide Secret
reader is not an acceptable default.

Vault and cloud Secret managers are future adapters. They must pin an immutable version, use
tenant-scoped workload identity, and meet the same no-list/no-write/redaction contract before they
can be enabled.

## Materialization Sequence

For each accepted Job epoch:

1. Scheduler acquires/renews the existing dispatch lease and verifies Job UID, epoch, tenant, and
   desired state.
2. It loads source/sink `execution_connection_binding` rows; it never resolves by human name or
   current generation.
3. It verifies the descriptor and compiler revisions against the selected runtime profile.
4. It submits exact immutable locator requests to the materializer with a short deadline and
   cancellation bound to the dispatch lease.
5. The provider authenticates through workload identity, reads only the pinned object, verifies
   tenant label, UID, immutability, key set, and size, and returns bounded in-memory fields.
6. The materializer creates one deterministic immutable Kubernetes Secret for the Job UID/epoch.
   Logical source/sink fields use generated data keys; external names and key names are not copied
   into labels or annotations.
7. It stores a redacted receipt with generated Secret UID/resource version and the service audit
   outcome, then closes in-memory buffers.
8. Scheduler creates epoch data-plane resources that mount the Secret as read-only files. The
   JobSpec Secret remains credential free.
9. Runtime bootstrap merges descriptor defaults, captured Connection data, and Job-owned options,
   then invokes the connector factory under the existing artifact version fence.

If source or sink has no Connection, no empty credential file is synthesized. If both roles refer
to the same Connection generation, the materializer can read once but still exposes logical fields
under role-scoped paths. The initial generic Worker pool may require both role paths; later
role-specific pools should narrow mounts without changing the epoch contract.

The Coordinator receives only references to mounted files and safe generation metadata. Secret
values are not arguments or environment variables because those are frequently exposed through
process inspection, crash reports, pod specs, and diagnostics.

## Runtime Configuration Envelope

The mounted Secret contains a versioned binary or strict properties envelope understood only by
runtime bootstrap. It carries:

- envelope schema version;
- Job UID and epoch;
- role;
- Connection UID and generation;
- descriptor revision;
- logical option key/value bytes; and
- provider receipt identity needed for local consistency checks.

The envelope is never accepted from JobSpec or the browser. Runtime verifies identity fields
against its trusted launch parameters and rejects unknown schema/keys, duplicate ownership,
missing required secret fields, or mismatched epoch. Connector configuration objects redact values
from `toString`, errors, and metrics.

No raw value hash is persisted as an identity or audit field. Low-entropy passwords are vulnerable
to offline guessing even when hashed. Idempotency comes from immutable provider UID/version and
Connection generation, not from publishing a digest of bytes.

## Deterministic Kubernetes Resources

The credential Secret name is derived from stable Job UID and epoch using the same bounded naming
rules as other Scheduler resources. It is labeled only with safe resource type, Job UID hash/prefix,
and epoch selectors needed for reconciliation. It is `immutable: true` and is not shared across
epochs or tenants.

Scheduler retry behaves as follows:

- absent Secret: materialize the captured generations and create it;
- existing immutable Secret whose UID/resource version matches a durable UID/epoch/generation
  receipt: reuse it;
- existing Secret after an uncertain create but before receipt commit: resolve the same pinned
  provider generation again and compare envelope bytes only in bounded memory before committing a
  receipt;
- existing Secret with another identity or mismatched bytes: fail closed as an ownership conflict;
- partially created later resources: continue normal deterministic reconciliation; and
- expired/canceled lease: stop provider work and do not launch data-plane resources.

The materializer does not overwrite or patch Secret data. An uncertain create outcome is recovered
with an exact `Get`, identity checks, and the bounded in-memory comparison above; no comparison hash
is persisted or logged. A provider retry always requests the same captured immutable object.

Because the credential Secret is consumed by multiple deterministic epoch resources, cleanup is
explicitly owned by Scheduler reconciliation rather than a fragile single Kubernetes owner
reference. Cancellation and terminal cleanup remove data-plane resources before the credential
Secret. An orphan sweeper selects only validated AstraSync labels, cross-checks PostgreSQL epoch
state, and uses a minimum age before deletion.

## Rotation and Revocation Semantics

Rotation is prospective at the Start transaction boundary:

```text
Start epoch 7 captures generation 3
Rotate commits generation 4
Scheduler dispatches epoch 7 -> generation 3
Stop / later Start epoch 8 -> generation 4
```

This guarantees deterministic retry and avoids mutating credentials beneath a running connector.
It also means rotation is not emergency revocation. For suspected compromise, an administrator
must disable the Connection, Stop affected Jobs, revoke the provider credential according to the
external system's policy, rotate to a new immutable Secret, and explicitly restart. Provider-side
revocation may cause running epochs to fail before Stop; AstraSync reports that failure without
substituting another generation.

Disabling after a Start commit does not cancel that accepted epoch. Audit timestamps and epoch
bindings make the ordering visible. A future force-revocation workflow would require an explicit
cross-Job impact and fencing design.

## Connection Test Isolation

Connection tests use the same provider adapter and configuration envelope but a separate service
account, queue, namespace, network policy, resource quota, and short-lived pod/process from Job
execution. The API tier never opens the target connection.

The test executor enforces:

- tenant/actor/Connection rate, concurrency, and daily budgets;
- server-selected DNS resolver and egress policy;
- resolution pinning for the duration of one attempt to reduce DNS rebinding;
- bounded addresses, redirects, sockets, TLS handshakes, bytes, and wall time;
- connector-owned read-only probe implementation with no caller command/query input;
- no Kubernetes API token beyond the exact provider/runtime needs; and
- deletion of test credential Secrets and pods on every terminal path.

Private destinations are allowed only when tenant egress policy explicitly permits them; blocking
all private address space would make the product unusable for its primary database workloads.
Tests cannot reach control-plane metadata endpoints or Kubernetes service ranges unless explicitly
allowlisted for that tenant.

Probe error adapters map vendor exceptions to stable phases such as `DNS`, `NETWORK`, `TLS`,
`AUTHENTICATION`, `PROTOCOL`, `TIMEOUT`, and `POLICY`. They discard vendor messages before crossing
the executor boundary.

## Redaction and Data Handling

| Surface | Allowed | Prohibited |
|---|---|---|
| PostgreSQL Connection row | Non-secret/restricted settings, provider kind, restricted immutable locator | Secret bytes |
| Public Connection response | Safe descriptor fields, configured marker, generation, state | Locator, key mapping, secret value |
| JobSpec / Job response | Tenant-local opaque `connection_ref` | Resolved UID/generation in public spec, secret value |
| Descriptor | Option sensitivity and schema | Defaults/examples containing credentials, deployment path |
| Validation | Presence, compatibility, safe issue code/path | Provider lookup, locator, rejected value |
| Kubernetes JobSpec Secret | JobSpec and safe launch metadata | Connection credentials |
| Epoch credential Secret | Strict runtime envelope | Human-readable external locator labels/annotations |
| Application log/trace/metric | Request/test IDs, counts, stable code, latency | Options, refs, locators, endpoint, provider/vendor text |
| Audit | Resource UID, version/generation, descriptor revision, outcome | Name when unnecessary, locator, endpoint, bytes, request body |
| Idempotency | Digest, resource/result ID, bounded status | Raw request, Secret binding, Connection values |
| Browser | Configured marker and write-only replacement field | Existing locator or materialized value |

Generic protobuf/JSON reflection, request-body logging, map logging, and exception serialization are
not permitted on Connection/materializer paths. Every boundary uses an allowlisted projection.
Tests inject unique password, locator, endpoint, and delimiter sentinels and scan every listed
surface, including Kubernetes events and terminated pod logs.

## Failure Handling

Provider and Kubernetes calls occur outside the Job Start transaction. Start acceptance means the
epoch and generation binding are durable; it does not mean credentials have materialized. A
materialization failure transitions dispatch/Job status through existing fenced service-actor
paths with a sanitized failure code and request ID.

The system never:

- retry-resolves `current` generation;
- falls back to environment variables or raw Job options;
- reuse a prior epoch Secret;
- mark a Connection disabled solely because a transient provider call failed;
- return provider authorization detail to an end user; or
- launch a Coordinator after required credential materialization is incomplete.

If audit/receipt persistence fails after creating an epoch Secret, Scheduler does not launch the
execution and reconciliation removes the orphan. If cleanup fails, it records a bounded retryable
obligation and deletion of the Connection remains blocked until the obligation clears.

## Legacy Migration

Before enabling public Job mutation or runtime references for a tenant:

1. inventory persisted Jobs using the active descriptor sensitivity schema;
2. block the tenant gate if unknown options or raw sensitive values exist;
3. have an authorized operator provision immutable external Secrets through the provider's normal
   process;
4. create disabled Connections with no secret bytes crossing the AstraSync API;
5. update Jobs with expected versions so raw Connection-owned options are removed and stable
   bindings are created;
6. enable and optionally test Connections, then run canonical validation in shadow mode; and
7. prove database, backup, audit, log, trace, and browser projections contain no sentinels before
   enabling Starts.

Migration tooling must not print or store old raw values in command history or temporary files.
Automatic extraction of credentials from PostgreSQL into new Secrets is not part of this slice;
that would widen the control plane's secret authority and requires a separately reviewed tool.

The historical Connection CRD is not imported automatically. Its `username`, `secretRef`, and
free-form properties lack stable tenant, UID, version, descriptor, and provider semantics.

## Security Verification Gates

- Cross every public and internal method with another tenant's name, UID, generation, page token,
  Secret name, and Kubernetes namespace; prove uniform denial.
- Attempt raw secret fields, masked placeholders, unknown pass-through keys, duplicate ownership,
  oversized values, and log delimiters; prove fail-closed redaction.
- Rotate and disable concurrently with Start across API replicas; prove epoch generation ordering.
- Delete/recreate the external Secret name with another UID; prove materialization rejects it.
- Crash after provider read, Secret create, receipt write, pod create, cancellation, and terminal
  cleanup; prove no cross-epoch reuse and eventual cleanup.
- Remove provider permission and verify sanitized failure with no fallback.
- Exercise test SSRF targets, DNS rebinding, redirects, metadata endpoints, slow handshakes, large
  errors, and write-capable probes; prove policy and bounds.
- Scan PostgreSQL/backup fixtures, API/browser output, logs, traces, metrics, audit, idempotency,
  Kubernetes YAML/events, and crash output for all sentinel values.
