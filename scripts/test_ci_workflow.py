#!/usr/bin/env python3
"""Regression tests for the GitHub Actions CI workflow contract.

These tests parse ``.github/workflows/ci.yml`` and assert that the
load-bearing CI steps and job scopes recorded by the Phase 12 multi-region
CI work, Phase 13 production hardening, Phase 14 ArgoCD GitOps, and
Phase 15 connector catalog lifecycle automation are all present and
correctly wired. Removing or weakening any of these assertions is treated
as a regression.
"""

from __future__ import annotations

import re
import unittest
from pathlib import Path


WORKFLOW = Path(__file__).resolve().parent.parent / ".github" / "workflows" / "ci.yml"
ACTION_RE = re.compile(r"^\s*uses:\s+([^\s#]+)", re.MULTILINE)
SHA_RE = re.compile(r"^[0-9a-f]{40}$")


def _read_workflow() -> str:
    return WORKFLOW.read_text(encoding="utf-8")


def _step_body(workflow: str, name: str) -> str:
    """Return the YAML body of a step whose name matches ``name``.

    ``picocli`` / GitHub Actions step names are unique within a job; we
    scan the workflow linearly and return the text between the matched
    ``name:`` line and the next sibling step.
    """
    pattern = re.compile(
        r"-\s+name:\s+"
        + re.escape(name)
        + r"\b[^#\n]*\n((?:\s{6,}[^\n]*\n)+)",
        re.MULTILINE,
    )
    match = pattern.search(workflow)
    if match is None:
        raise AssertionError(f"CI step not found: {name}")
    return match.group(1)


def _step_names(workflow: str) -> list[str]:
    """Return every distinct CI step name found in the workflow."""
    return re.findall(r"-\s+name:\s+([^\n]+)", workflow)


class CIWorkflowContractTest(unittest.TestCase):
    @classmethod
    def setUpClass(cls):
        cls.workflow = _read_workflow()

    def test_all_third_party_actions_are_pinned_to_commit_sha(self):
        actions = ACTION_RE.findall(self.workflow)
        self.assertTrue(actions)
        for action in actions:
            self.assertRegex(action, r"^[^/@\s]+/[^/@\s]+@[0-9a-f]{40}$")
            self.assertRegex(action.rsplit("@", 1)[1], SHA_RE)

    def test_change_detection_exports_required_scopes(self):
        for output in ("go", "java", "helm", "docs", "multi_region"):
            self.assertIn(f"{output}: ${{{{ steps.filter.outputs.{output} }}}}", self.workflow)

        self.assertIn("tests/integration", self.workflow)
        self.assertIn("scripts/run-multi-region-acceptance.py", self.workflow)
        self.assertIn("deployment/docker", self.workflow)


class CIWorkflowMultiRegionJobTest(unittest.TestCase):
    @classmethod
    def setUpClass(cls):
        cls.workflow = _read_workflow()

    def setUp(self):
        start = self.workflow.index("  multi-region-acceptance:")
        end = self.workflow.index("  protocols:", start)
        self.job = self.workflow[start:end]

    def test_runs_and_cleans_up_on_failure(self):
        self.assertIn("needs.changes.outputs.multi_region == 'true'", self.job)
        self.assertIn("run: make test-integration-multi-region", self.job)
        self.assertIn("name: Collect Compose logs", self.job)
        self.assertIn("name: Upload Compose logs", self.job)
        self.assertIn("name: Ensure Compose resources are removed", self.job)
        self.assertEqual(self.job.count("if: always()"), 3)
        self.assertLess(self.job.index("Collect Compose logs"), self.job.index("Upload Compose logs"))
        self.assertLess(self.job.index("Upload Compose logs"), self.job.index("Ensure Compose resources"))


class CIWorkflowPhase13ProductionProfileTest(unittest.TestCase):
    """Phase 13 Slice 30 follow-up: production profile CI step."""

    @classmethod
    def setUpClass(cls):
        cls.workflow = _read_workflow()
        cls.body = _step_body(cls.workflow, "Lint and render production profile")

    def test_step_asserts_production_marker_reaches_manifest(self):
        self.assertIn("values-production.yaml", self.body)
        self.assertIn("environment: production", self.body)
        self.assertIn("apiServer.replicas=3", self.body)

    def test_step_asserts_production_safety_nets(self):
        # HPA + PDB + NetworkPolicy + mtls annotations must all be present
        # in the production render; their absence would silently regress
        # the production profile.
        for needle in (
            "kind: HorizontalPodAutoscaler",
            "kind: PodDisruptionBudget",
            "kind: NetworkPolicy",
            "astrasync-api-server-mtls",
        ):
            self.assertIn(needle, self.body, f"missing production-safety assertion: {needle}")

    def test_step_only_runs_on_helm_changes(self):
        # The production profile step must be gated on the helm scope so
        # it does not run on unrelated changes.
        self.assertIn(
            "if: needs.changes.outputs.helm == 'true'",
            self.body,
        )


class CIWorkflowPhase14ArgoCDTest(unittest.TestCase):
    """Phase 14 Slice 40 follow-up: staging profile + ArgoCD CR schema steps."""

    @classmethod
    def setUpClass(cls):
        cls.workflow = _read_workflow()

    def test_staging_profile_step_present(self):
        body = _step_body(self.workflow, "Lint and render staging profile")
        # Staging must render 3 PDBs and 0 HPAs / 0 NetworkPolicies.
        self.assertIn("values-staging.yaml", body)
        self.assertIn("expected 3 PDBs from staging", body)
        self.assertIn("expected 0 HPAs from staging", body)
        self.assertIn("expected 0 NetworkPolicies from staging", body)
        # Staging-only markers must propagate.
        self.assertIn("environment: staging", body)
        self.assertIn("DEBUG log level did not propagate", body)

    def test_argocd_schema_step_present(self):
        body = _step_body(self.workflow, "Validate ArgoCD CR schema")
        # All three ArgoCD manifests must be parsed.
        self.assertIn("deployment/argocd/application.yaml", body)
        self.assertIn("deployment/argocd/applicationset.yaml", body)
        self.assertIn("deployment/argocd/namespace.yaml", body)
        # The Role must not grant secrets access.
        self.assertIn("Role must not grant secrets access", body)
        # Matrix generator (clusters + git) must be asserted.
        self.assertIn("expected cluster generator", body)
        self.assertIn("expected git generator", body)


class CIWorkflowPhase15CatalogTest(unittest.TestCase):
    """Phase 15 Slice 41: catalog-check with diff-catalog fallback."""

    @classmethod
    def setUpClass(cls):
        cls.workflow = _read_workflow()
        cls.body = _step_body(cls.workflow, "Check deployment connector inventory")

    def test_step_uses_dynamic_build_version(self):
        self.assertIn("CATALOG_BUILD_VERSION: ${{ github.sha }}", self.workflow)
        self.assertIn("--compiler-build $CATALOG_BUILD_VERSION", self.body)

    def test_step_falls_back_to_diff_catalog(self):
        # On failure, the step must invoke scripts/diff-catalog.py so the
        # developer sees the actual descriptor drift in the CI log.
        self.assertIn("scripts/diff-catalog.py", self.body)
        self.assertIn("catalog differs from freshly-exported catalog", self.body)
        self.assertIn("Running diff-catalog for diagnostics", self.body)


class CIWorkflowStepInventoryTest(unittest.TestCase):
    """All expected load-bearing CI steps must be present.

    Removing or renaming any of these is a regression. This test exists
    so that a refactor of the workflow file cannot silently drop a step.
    """

    EXPECTED_STEPS = (
        "Check deployment connector inventory",
        "Lint and render production profile",
        "Lint and render staging profile",
        "Validate ArgoCD CR schema",
    )

    def test_expected_steps_all_present(self):
        workflow = _read_workflow()
        step_names = _step_names(workflow)
        for expected in self.EXPECTED_STEPS:
            self.assertIn(expected, step_names, f"missing load-bearing CI step: {expected}")


if __name__ == "__main__":
    unittest.main()
