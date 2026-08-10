# Phase 6 Slice 19 Canonical Validation and Secret Handling

## Status

Implemented boundary. The Go API resolves redacted tenant metadata and delegates canonical,
side-effect-free validation to the Java compiler-validation service.

## Problem

The Go control plane currently validates names, basic JobSpec structure, delivery enum values, and
positive runtime bounds. Connector existence, source/sink roles, execution capabilities,
transform support, and delivery feasibility are enforced by the Java `JobCompiler`. Reimplementing
those rules in browser JavaScript or Go would let accepted Jobs diverge from executable Jobs.

Slice 19 therefore needs one canonical, side-effect-free compiler boundary that is usable during
Console editing and again during mutation admission.

## Validation Ownership

```text
browser shape checks (advisory)
        |
        v
Go protobuf/domain structure checks
        |
        v
internal compiler-validation RPC
        |
        +-- Java JobSpec conversion and strict parsing
        +-- ConnectorRegistry descriptors and option schemas
        +-- JobCompiler role/capability/delivery/transform rules
        +-- deployment-owned execution profile
        |
        v
typed, redacted ValidationResult
```

The Java service owns semantic compilation because it uses the same `JobCompiler`, connector
artifacts, and descriptor versions as execution. The Go API remains responsible for protobuf
shape, tenant authorization, lifecycle, persistence, and audit. Both layers validate; neither
claims the other's authority.

The compiler endpoint is internal workload traffic, not browser reachable. It uses authenticated
service identity, strict timeouts, bounded messages, and the same release compatibility checks as
Coordinator dispatch. The public API exposes a protobuf-first `JobValidationService` and translates
between control-plane JobSpec and the internal compiler request.

## Public Validation Contract

`ValidateJobSpec` has three explicit purposes:

| Purpose | Request content | Permission | Server-selected input |
|---|---|---|---|
| `CREATE` | Proposed name and complete JobSpec | `jobs.create` | Tenant and execution profile |
| `UPDATE` | Existing name, expected version, complete replacement JobSpec | `jobs.update` | Current Job identity and state |
| `START` | Existing name and expected version | `jobs.start` | Stored JobSpec and current state |

Unknown purposes fail closed. `START` never compiles a caller-supplied alternate spec. Validation
does not reserve a name, version, connector revision, or permission and cannot replace checks in a
later mutation.

The result contains:

| Field | Meaning |
|---|---|
| `validation_id` | Random correlation ID safe for support and audit |
| `valid` | True only when no error-severity issue exists |
| `spec_digest` | SHA-256 of deterministic canonical protobuf bytes |
| `compiler_revision` | Deployment compiler and connector-inventory revision |
| `execution_mode` | Derived `BATCH` or `CDC` mode when compilation reaches that step |
| `issues` | Bounded ordered issue list |

Each issue has stable code, `ERROR` or `WARNING` severity, protobuf-style field path, safe message,
and optional descriptor documentation key. It never includes a rejected value. Results are capped
by issue count and message length; excess issues produce one `ISSUES_TRUNCATED` marker.

Initial stable error codes include:

- `STRUCTURE_INVALID`
- `CONNECTOR_NOT_FOUND`
- `ROLE_UNSUPPORTED`
- `CAPABILITY_MISSING`
- `TRANSFORM_UNSUPPORTED`
- `DELIVERY_UNSUPPORTED`
- `OPTION_UNKNOWN`
- `OPTION_REQUIRED`
- `OPTION_INVALID`
- `CONNECTION_REF_REQUIRED`
- `CONNECTION_REF_UNAVAILABLE`
- `VALIDATION_REVISION_CHANGED`

Messages are presentation hints, not a compatibility contract. Clients branch only on gRPC code,
stable issue code, severity, and field path.

## Mutation-time Validation

Create and Update invoke the canonical validator on the exact submitted spec. Start loads and
validates the durable stored spec against the current deployment execution profile. Stop and Delete
do not compile a spec.

An explicit preflight result can be cached as an optimization only when tenant, purpose, spec
digest, compiler revision, connector inventory, and authorization revision all match and the entry
is within a short bounded lifetime. Mutation admission verifies those keys; any mismatch reruns
validation. The browser cannot submit `valid=true`, choose a compiler revision, or downgrade the
execution profile.

Validation is deterministic and side-effect free:

- connector descriptors may be loaded, but connector source/sink instances are not opened;
- no database, file, queue, network, object-store, or destination operation is attempted;
- no source enumeration, schema discovery, write probe, or credential resolution occurs;
- validation time, memory, input size, option count, and issue count are bounded;
- an unavailable compiler fails Create, Update, and Start before domain mutation.

Connectivity and data-access tests require a later separately permissioned operation with explicit
rate limits and audit. A successful Slice 19 validation means the spec is structurally and
capability compatible, not that an external system is reachable.

## Connector Descriptor Contract

`ListConnectorDescriptors` returns the inventory revision and descriptors visible in one authorized
tenant. Descriptors are sorted by canonical name and version and contain:

- canonical name, version, display label, and safe description key;
- supported source/sink roles and capabilities;
- supported execution modes and delivery constraints;
- an ordered option schema: key, label key, type, required flag, default, enum choices, numeric or
  length bounds, sensitivity, advanced flag, and whether arbitrary extra keys are allowed;
- whether a tenant-scoped `connection_ref` is required or supported.

Descriptors never contain connector instances, class names needed only internally, credentials,
deployment paths, secret-reference values, or environment details. Browser forms use descriptors
for available fields and local feedback, while the server independently validates every option.
Unknown options are rejected unless a descriptor explicitly allows bounded extras.

Adding option metadata does not move semantic capability rules out of `JobCompiler`. Descriptor and
compiler contract tests must prove that every registered connector has one unambiguous canonical
name, version, role set, option schema, and side-effect-free validation path.

## Connection References

Raw passwords, access tokens, private keys, and equivalent credentials are not accepted in JobSpec
options exposed through the Console. A connector option marked sensitive requires a pre-provisioned
tenant-scoped `connection_ref`. Creating or rotating that reference is outside Slice 19.

The API treats a connection reference as an opaque identifier. Validation checks tenant access,
connector compatibility, and reference status through a metadata-only resolver. Missing and
unauthorized references return the same `CONNECTION_REF_UNAVAILABLE` issue after authorization so
the endpoint cannot become an existence oracle. The compiler receives safe capability metadata,
not secret material. Runtime service identity resolves the credential only when an execution is
dispatched.

Non-sensitive connector options may remain in JobSpec and in the authorized public projection.
Descriptor-marked sensitive values are never returned. Before mutation routes are enabled for a
tenant, existing Jobs are scanned for legacy raw sensitive options and migrated to references.
The feature gate stays closed when migration cannot prove that public read/edit projections are
secret safe.

## Redaction Rules

The following surfaces must use allowlists rather than generic object serialization:

| Surface | Allowed | Prohibited |
|---|---|---|
| Public Job response | Non-sensitive options and opaque reference ID or configured marker | Raw descriptor-sensitive option values |
| Validation issue | Code, severity, path, safe message | Rejected value, full spec, stack trace |
| Application log | Request ID, validation ID, counts, code, latency | JobSpec, options, references, tokens, request body |
| Audit event | IDs, digest, revision, issue codes, version/state transition | Spec, options, reference value, response body |
| Idempotency row | Request digest, operation result reference, bounded response fields | Raw request, JobSpec, credentials |
| Browser draft | Non-sensitive fields and selected reference ID | Resolved secret or provider token |

Even opaque connection-reference values are excluded from normal logs and audit because they can
reveal naming conventions. Validation request digests are safe only after sensitive raw options are
rejected; implementations use deterministic serialization and never log the input bytes.

Error handling sanitizes exceptions at the compiler boundary. Unknown exceptions map to
`VALIDATOR_UNAVAILABLE` or an internal request ID, while full details remain in restricted service
telemetry that also applies secret filters.

## Consistency and Availability

The API publishes one `compiler_revision` derived from compiler build, JobSpec schema version,
connector names/versions, option schema hashes, and deployment execution profile. Revision changes
invalidate validation caches. Coordinators reject a dispatch whose compiled connector versions do
not match their inventory, preserving the existing runtime fence even after API validation.

The validation service is part of Create, Update, and Start write availability. It has a short
deadline and bounded concurrency pool so a slow compiler cannot exhaust API request workers. A
dependency outage returns `Unavailable` and commits no Job, idempotency completion, or mutation
audit event. An already running Job is not stopped because validation becomes unavailable.

## Security Verification

- Send known password/token option keys and prove the API rejects rather than echoes them.
- Use an unauthorized reference and a nonexistent reference and prove externally identical results.
- Inject values containing log delimiters, HTML, and control characters and verify all surfaces
  remain encoded and redacted.
- Register a connector whose validator attempts I/O and fail its descriptor contract test.
- Change connector inventory between preflight and submit and prove mutation revalidates.
- Change a Job between Update/Start preflight and submit and prove expected-version conflict wins.
- Exhaust compiler concurrency and prove bounded `ResourceExhausted`/`Unavailable` behavior without
  bypass or partial mutation.
- Compare API validation and Coordinator compilation fixtures for every supported connector,
  delivery guarantee, execution mode, and stable error code.
