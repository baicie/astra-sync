#!/usr/bin/env python3
"""Regression tests for the multi-region CI workflow contract."""

from __future__ import annotations

import re
import unittest
from pathlib import Path


WORKFLOW = Path(__file__).resolve().parent.parent / ".github" / "workflows" / "ci.yml"
ACTION_RE = re.compile(r"^\s*uses:\s+([^\s#]+)", re.MULTILINE)
SHA_RE = re.compile(r"^[0-9a-f]{40}$")


class CIWorkflowContractTest(unittest.TestCase):
    @classmethod
    def setUpClass(cls):
        cls.workflow = WORKFLOW.read_text(encoding="utf-8")

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

    def test_multi_region_job_runs_and_cleans_up_on_failure(self):
        start = self.workflow.index("  multi-region-acceptance:")
        end = self.workflow.index("  protocols:", start)
        job = self.workflow[start:end]

        self.assertIn("needs.changes.outputs.multi_region == 'true'", job)
        self.assertIn("run: make test-integration-multi-region", job)
        self.assertIn("name: Collect Compose logs", job)
        self.assertIn("name: Upload Compose logs", job)
        self.assertIn("name: Ensure Compose resources are removed", job)
        self.assertEqual(job.count("if: always()"), 3)
        self.assertLess(job.index("Collect Compose logs"), job.index("Upload Compose logs"))
        self.assertLess(job.index("Upload Compose logs"), job.index("Ensure Compose resources"))


if __name__ == "__main__":
    unittest.main()
