#!/usr/bin/env python3
"""Unit tests for ``scripts/check-runbook-templates.py``.

Run with::

    python3 scripts/test_check_runbook_templates.py
"""

from __future__ import annotations

import sys
import tempfile
import unittest
from pathlib import Path

# Load the script under test with an explicit module spec so the test works
# regardless of whether the runner adds ``scripts/`` to ``sys.path``.
import importlib.util

_SCRIPT_PATH = Path(__file__).resolve().parent / "check-runbook-templates.py"
_spec = importlib.util.spec_from_file_location("_check_runbook_templates", _SCRIPT_PATH)
_module = importlib.util.module_from_spec(_spec)
_spec.loader.exec_module(_module)

check = _module.check
DEFAULT_ROOT = _module.DEFAULT_ROOT
SCAN_ROOTS = _module.SCAN_ROOTS
file_contains_placeholder = _module.file_contains_placeholder
file_contains_prod_hostname = _module.file_contains_prod_hostname
iter_markdown_files = _module.iter_markdown_files
check_all = _module.check_all
PLACEHOLDER_RE = _module.PLACEHOLDER_RE
PROD_HOSTNAME_PATTERNS = _module.PROD_HOSTNAME_PATTERNS


class PlaceholderRegexTest(unittest.TestCase):
    def test_matches_placeholder(self):
        self.assertIsNotNone(PLACEHOLDER_RE.search("<vault-path-prefix>"))

    def test_matches_single_word(self):
        self.assertIsNotNone(PLACEHOLDER_RE.search("<idp>"))

    def test_rejects_unclosed(self):
        self.assertIsNone(PLACEHOLDER_RE.search("<vault-path-prefix"))

    def test_rejects_empty_brackets(self):
        self.assertIsNone(PLACEHOLDER_RE.search("<>"))

    def test_rejects_text_only(self):
        self.assertIsNone(PLACEHOLDER_RE.search("vault-path-prefix"))


class ProdHostnamePatternsTest(unittest.TestCase):
    def test_patterns_are_non_empty(self):
        self.assertGreater(len(PROD_HOSTNAME_PATTERNS), 0)
        for pattern in PROD_HOSTNAME_PATTERNS:
            self.assertTrue(pattern)


class CheckEndToEndTest(unittest.TestCase):
    def setUp(self):
        self.tmp = tempfile.TemporaryDirectory()
        self.root = Path(self.tmp.name)

    def tearDown(self):
        self.tmp.cleanup()

    def _write(self, name: str, body: str) -> Path:
        path = self.root / name
        path.write_text(body, encoding="utf-8")
        return path

    def test_empty_directory_passes(self):
        self.assertEqual(check(self.root), [])

    def test_template_with_placeholder_passes(self):
        self._write(
            "idp-registration.md",
            "# IdP\n\nUse `<vault-path-prefix>` for the secret path.\n",
        )
        self.assertEqual(check(self.root), [])

    def test_missing_placeholder_fails(self):
        path = self._write("populated.md", "# Runbook\n\nNo placeholders here.\n")
        failures = check(self.root)
        self.assertEqual(len(failures), 1)
        self.assertIn(str(path), failures[0])
        self.assertIn("missing <placeholder>", failures[0])

    def test_prod_hostname_fails(self):
        path = self._write(
            "populated.md",
            "# Runbook\n\nConnect to `<astra-prod>` to apply the change.\n",
        )
        failures = check(self.root)
        self.assertEqual(len(failures), 1)
        self.assertIn(str(path), failures[0])
        self.assertIn("production hostname", failures[0])

    def test_both_failures_reported(self):
        path = self._write(
            "populated.md",
            "# Runbook\n\nConnect to astra-prod to apply the change.\n",
        )
        failures = check(self.root)
        # Both the missing-placeholder and the prod-hostname assertions
        # fire on a fully populated file; the script reports both.
        self.assertEqual(len(failures), 2)
        joined = "\n".join(failures)
        self.assertIn(str(path), joined)
        self.assertIn("missing <placeholder>", joined)
        self.assertIn("production hostname", joined)

    def test_default_root_is_repo_runbooks(self):
        # The default root should resolve to ``<repo>/docs/runbooks``.
        expected = (Path(__file__).resolve().parent.parent / "docs" / "runbooks").resolve()
        self.assertEqual(DEFAULT_ROOT.resolve(), expected)


class FileContainsPlaceholderTest(unittest.TestCase):
    def setUp(self):
        self.tmp = tempfile.TemporaryDirectory()
        self.path = Path(self.tmp.name) / "doc.md"

    def tearDown(self):
        self.tmp.cleanup()

    def test_returns_true_when_present(self):
        self.path.write_text("Use <vault-path-prefix> for the secret path.\n", encoding="utf-8")
        self.assertTrue(file_contains_placeholder(self.path))

    def test_returns_false_when_absent(self):
        self.path.write_text("No placeholders here.\n", encoding="utf-8")
        self.assertFalse(file_contains_placeholder(self.path))


class FileContainsProdHostnameTest(unittest.TestCase):
    def setUp(self):
        self.tmp = tempfile.TemporaryDirectory()
        self.path = Path(self.tmp.name) / "doc.md"

    def tearDown(self):
        self.tmp.cleanup()

    def test_returns_matching_patterns(self):
        self.path.write_text(
            "Connect to <astra-prod> via <prod-control-plane>.",
            encoding="utf-8",
        )
        self.assertEqual(
            file_contains_prod_hostname(self.path),
            ["astra-prod", "prod-control-plane"],
        )

    def test_returns_empty_when_clean(self):
        self.path.write_text("Connect to <vault-path-prefix> for the secret.\n", encoding="utf-8")
        self.assertEqual(file_contains_prod_hostname(self.path), [])


class IterMarkdownFilesTest(unittest.TestCase):
    def setUp(self):
        self.tmp = tempfile.TemporaryDirectory()
        self.root = Path(self.tmp.name)

    def tearDown(self):
        self.tmp.cleanup()

    def test_walks_nested(self):
        (self.root / "a.md").write_text("# A\n", encoding="utf-8")
        (self.root / "sub").mkdir()
        (self.root / "sub" / "b.md").write_text("# B\n", encoding="utf-8")
        (self.root / "ignored.txt").write_text("ignored", encoding="utf-8")
        files = iter_markdown_files(self.root)
        self.assertEqual(len(files), 2)
        self.assertTrue(any(f.name == "a.md" for f in files))
        self.assertTrue(any(f.name == "b.md" for f in files))

    def test_missing_directory(self):
        self.assertEqual(iter_markdown_files(self.root / "absent"), [])

    def test_handles_single_file(self):
        path = self.root / "single.md"
        path.write_text("# single\n", encoding="utf-8")
        self.assertEqual(iter_markdown_files(path), [path])


class CheckModeTest(unittest.TestCase):
    """Test that template mode requires a placeholder and doc mode does not."""

    def setUp(self):
        self.tmp = tempfile.TemporaryDirectory()
        self.root = Path(self.tmp.name)

    def tearDown(self):
        self.tmp.cleanup()

    def _write(self, name: str, body: str) -> Path:
        path = self.root / name
        path.write_text(body, encoding="utf-8")
        return path

    def test_template_mode_requires_placeholder(self):
        path = self._write("guide.md", "# Operator guide\n\nNo placeholders here.\n")
        failures = check(self.root, mode="template")
        self.assertTrue(any("missing <placeholder>" in f for f in failures))

    def test_doc_mode_skips_placeholder_check(self):
        path = self._write("guide.md", "# Operator guide\n\nNo placeholders here.\n")
        failures = check(self.root, mode="doc")
        # No prod hostname, no placeholder requirement → no failures.
        self.assertEqual(failures, [])

    def test_doc_mode_still_catches_prod_hostname(self):
        path = self._write(
            "guide.md",
            "# Operator guide\n\nConnect to <astra-prod> for the secret.\n",
        )
        failures = check(self.root, mode="doc")
        # Prod hostname must be flagged regardless of mode.
        self.assertTrue(any("production hostname" in f for f in failures))
        # But the placeholder inside the angle-bracket is preserved —
        # a single file must not lose its placeholders just because the
        # mode is doc.
        self.assertFalse(any("missing <placeholder>" in f for f in failures))


class ScanRootsRegistrationTest(unittest.TestCase):
    """The default CI scan must include the Phase 14 and Phase 15 surfaces."""

    def test_phase14_argocd_root_registered(self):
        # The ArgoCD operator-onboarding README is treated as a template;
        # the rest of the directory is treated as doc mode.
        argocd_root = next(
            (root for root, _mode in SCAN_ROOTS if root.name == "README.md" and "argocd" in str(root)),
            None,
        )
        self.assertIsNotNone(argocd_root, "ArgoCD onboarding README not registered for scan")
        self.assertTrue(str(argocd_root).replace("\\", "/").endswith("deployment/argocd/README.md"))
        # And the directory itself must also be registered (doc mode).
        argocd_dir = next(
            (root for root, _mode in SCAN_ROOTS if root.name == "argocd"),
            None,
        )
        self.assertIsNotNone(argocd_dir, "ArgoCD directory not registered for doc-mode scan")

    def test_phase15_catalog_authoring_registered(self):
        catalog_doc = next(
            (root for root, _mode in SCAN_ROOTS if root.name == "catalog-authoring.md"),
            None,
        )
        self.assertIsNotNone(catalog_doc, "catalog-authoring.md not registered for scan")
        self.assertTrue(str(catalog_doc).replace("\\", "/").endswith("docs/catalog-authoring.md"))

    def test_catalog_authoring_uses_doc_mode(self):
        # The authoring guide is operator documentation, not a template;
        # it must use doc mode so a placeholder check is skipped.
        catalog_doc = next(
            root for root, _mode in SCAN_ROOTS if root.name == "catalog-authoring.md"
        )
        mode = next(mode for root, mode in SCAN_ROOTS if root == catalog_doc)
        self.assertEqual(mode, "doc")


class CheckAllEndToEndTest(unittest.TestCase):
    """Smoke-test that check_all runs without crashing on the real repo layout."""

    def test_check_all_runs_against_real_repo(self):
        # We don't assert pass/fail — we just verify the function can
        # walk every registered root and return a list of strings (possibly
        # empty). This guards against typos in SCAN_ROOTS at import time.
        result = check_all()
        self.assertIsInstance(result, list)
        for entry in result:
            self.assertIsInstance(entry, str)


if __name__ == "__main__":
    unittest.main()