# Phase 49 - Coordinator Trusted Tenant Binding

## Status

**Complete.**

Phase 49 supplies the Coordinator trust source required by the Worker
identity transport added in Phase 48.

ADR: [ADR-099](../adr/adr-099-coordinator-trusted-tenant-binding.md)

---

## Goal

Allow a Coordinator process owner to bind one trusted tenant identity:

```text
ASTRASYNC_COORDINATOR_TENANT_ID absent/blank -> _unknown
ASTRASYNC_COORDINATOR_TENANT_ID UUID         -> validated tenant
```

The value is passed through the existing Worker protocol and appears in
Worker data-plane metric labels when the remote task executes.

---

## Delivered Files

```text
engine/coordinator/src/main/java/io/astrasync/engine/coordinator/
├── CoordinatorApplication.java
└── CoordinatorConfiguration.java

engine/coordinator/src/test/java/io/astrasync/engine/coordinator/
├── CoordinatorApplicationTest.java
└── CoordinatorConfigurationTest.java

docs/adr/
└── adr-099-coordinator-trusted-tenant-binding.md

docs/phase49/
└── README.md
```

---

## Configuration Contract

```text
ASTRASYNC_COORDINATOR_TENANT_ID
  optional
  absent or blank -> _unknown
  explicit value  -> canonical lowercase UUID
```

Invalid explicit values fail Coordinator startup. The environment value is
never read from the JobSpec, split descriptor, or task payload.

---

## Verification

- `CoordinatorConfigurationTest` verifies default, valid, and invalid tenant
  configuration.
- `CoordinatorApplicationTest` runs a real two-Worker JDBC job with a tenant
  binding and asserts the Worker registry contains the trusted tenant label.
- Existing Coordinator capacity, heartbeat, resume, and checkpoint tests keep
  their previous defaults.

---

## Acceptance Criteria

- [x] Tenant configuration is optional and defaults to `_unknown`.
- [x] Explicit tenant values must be canonical lowercase UUIDs.
- [x] Invalid tenant values fail startup.
- [x] The Coordinator passes the trusted tenant to remote tasks.
- [x] End-to-end Worker metrics use the configured tenant.
- [x] JobSpec remains untrusted input for tenant attribution.
- [x] ADR-099 is indexed in `docs/adr/README.md`.
- [x] CHANGELOG includes the Phase 49 entry.

---

## Non-Goals

- No Helm or Docker environment injection.
- No JobSpec tenant field.
- No protobuf change.
- No authentication, authorization, or Worker transport change.
- No new metric family or label name.

---

## Rollback

Remove the tenant configuration field and restore the Coordinator's
`_unknown` constant. Worker protocol behavior and metric normalization remain
unchanged.
