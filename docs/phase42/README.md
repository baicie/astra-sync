# Phase 42 - Envtest Tool Version Automation

## Status

**Complete.**

Phase 42 closes the Phase 36 version-automation follow-up by deriving the
`setup-envtest` tool and Kubernetes test-binary versions from the controller
module.

ADR: [ADR-092](../adr/adr-092-envtest-tool-version-automation.md)

---

## Goal

Keep the integration toolchain aligned with:

```text
control-plane/controller/go.mod
├── sigs.k8s.io/controller-runtime
└── k8s.io/apimachinery
```

No workflow edit should be required after a dependency version bump.

---

## Delivered Files

```text
.github/workflows/
└── control-plane-integration.yml

scripts/
├── envtest-versions.py
├── test_envtest_versions.py
└── test_ci_workflow.py

docs/adr/
└── adr-092-envtest-tool-version-automation.md

docs/phase42/
└── README.md
```

---

## Resolution Contract

For:

```text
sigs.k8s.io/controller-runtime v0.24.1
k8s.io/apimachinery             v0.36.3
```

the script emits:

```text
SETUP_ENVTEST_VERSION=v0.24.1
ENVTEST_K8S_VERSION=1.36.x
```

The workflow writes these values to `$GITHUB_ENV` before installing
`setup-envtest` or selecting assets.

---

## Verification

- `scripts/test_envtest_versions.py` tests parsing, validation, and mapping.
- `scripts/test_ci_workflow.py` rejects hard-coded versions in the setup step.
- Running `python scripts/envtest-versions.py` against the current module
  produces the expected values.

---

## Acceptance Criteria

- [x] `setup-envtest` version follows controller-runtime.
- [x] Kubernetes envtest assets follow the Kubernetes module minor.
- [x] CI exports resolved values through `$GITHUB_ENV`.
- [x] The workflow setup step contains no hard-coded tool versions.
- [x] Parser and workflow regression tests are present.
- [x] `make test-scripts` includes the new tests.
- [x] ADR-092 is indexed in `docs/adr/README.md`.
- [x] CHANGELOG includes the Phase 42 entry.

---

## Non-Goals

- No change to controller-runtime or Kubernetes module versions.
- No envtest behavior or controller production change.
- No new Python package dependency.
- No change to the integration workflow's path filters.

---

## Rollback

Remove the resolver step and scripts, then restore literal versions in the
workflow. This returns toolchain alignment to manual maintenance.
