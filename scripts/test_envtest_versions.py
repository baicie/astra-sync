#!/usr/bin/env python3
"""Unit tests for scripts/envtest-versions.py."""

from __future__ import annotations

import importlib.util
import sys
import unittest
from pathlib import Path


SCRIPTS_DIR = Path(__file__).resolve().parent
SPEC = importlib.util.spec_from_file_location(
    "envtest_versions_under_test",
    SCRIPTS_DIR / "envtest-versions.py",
)
_envtest_versions = importlib.util.module_from_spec(SPEC)
sys.modules[SPEC.name] = _envtest_versions
SPEC.loader.exec_module(_envtest_versions)

envtest_versions = _envtest_versions.envtest_versions
parse_module_versions = _envtest_versions.parse_module_versions


class EnvtestVersionsTest(unittest.TestCase):
    def test_parses_concatenated_go_list_documents(self):
        versions = parse_module_versions(
            '{"Path":"sigs.k8s.io/controller-runtime","Version":"v0.24.1"}\n'
            '{"Path":"k8s.io/apimachinery","Version":"v0.36.3"}\n'
        )
        self.assertEqual(
            envtest_versions(versions),
            {
                "SETUP_ENVTEST_VERSION": "v0.24.1",
                "ENVTEST_K8S_VERSION": "1.36.x",
            },
        )

    def test_rejects_missing_module(self):
        with self.assertRaisesRegex(ValueError, "missing module version"):
            envtest_versions(
                {"sigs.k8s.io/controller-runtime": "v0.24.1"}
            )

    def test_rejects_non_semantic_module_version(self):
        with self.assertRaisesRegex(ValueError, "stable semantic versions"):
            envtest_versions(
                {
                    "sigs.k8s.io/controller-runtime": "latest",
                    "k8s.io/apimachinery": "v0.36.3",
                }
            )


if __name__ == "__main__":
    unittest.main()
