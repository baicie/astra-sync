"""Unit tests for ``scripts/check-changelog.py`` and ``scripts/release-dry-run.py``.

The tests build an isolated filesystem layout under ``tempfile`` so they
do not depend on the real repository's CHANGELOG or phase READMEs.
"""

from __future__ import annotations

import importlib.util
import sys
import tempfile
import unittest
from pathlib import Path


SCRIPTS_DIR = Path(__file__).resolve().parent


def _load(module_name: str, file_name: str):
    spec = importlib.util.spec_from_file_location(module_name, SCRIPTS_DIR / file_name)
    module = importlib.util.module_from_spec(spec)
    sys.modules[module_name] = module
    spec.loader.exec_module(module)
    return module


_check_changelog = _load("check_changelog_under_test", "check-changelog.py")
_release_dry_run = _load("release_dry_run_under_test", "release-dry-run.py")


# Bind the helpers we exercise directly.
find_unreleased_section = _check_changelog.find_unreleased_section
phase_referenced_in = _check_changelog.phase_referenced_in
unreleased_section = _release_dry_run.unreleased_section
phases_missing_from_changelog = _release_dry_run.phases_missing_from_changelog
completed_phases = _release_dry_run.completed_phases
read_pom_version = _release_dry_run.read_pom_version
iter_protos = _release_dry_run.iter_protos


class FindUnreleasedSectionTest(unittest.TestCase):
    def test_returns_section_when_present(self):
        text = (
            "# Changelog\n\n"
            "## [Unreleased]\n\n"
            "Phase 13.\n\n"
            "## [v0.2.0]\n\nOld.\n"
        )
        section = find_unreleased_section(text)
        self.assertIsNotNone(section)
        self.assertIn("Phase 13", text[section[0] : section[1]])
        self.assertNotIn("v0.2.0", text[section[0] : section[1]])

    def test_returns_none_when_missing(self):
        text = "# Changelog\n\n## [v0.2.0]\n\nOld.\n"
        self.assertIsNone(find_unreleased_section(text))


class PhaseReferencedInTest(unittest.TestCase):
    def test_matches_phase_number(self):
        self.assertTrue(phase_referenced_in("Phase 13 ships Helm.", 13))

    def test_matches_lowercase(self):
        self.assertTrue(phase_referenced_in("phase 13 ships Helm.", 13))

    def test_matches_directory_name(self):
        self.assertTrue(phase_referenced_in("phase13/README.md", 13))

    def test_does_not_match_unrelated(self):
        self.assertFalse(phase_referenced_in("Phase 1-2 ships.", 13))


class UnreleasedSectionHelperTest(unittest.TestCase):
    def setUp(self):
        self.tmp = tempfile.TemporaryDirectory()
        self.root = Path(self.tmp.name)
        self.changelog = self.root / "CHANGELOG.md"
        self._original_repo = _release_dry_run.REPO_ROOT
        _release_dry_run.REPO_ROOT = self.root

    def tearDown(self):
        self.tmp.cleanup()
        _release_dry_run.REPO_ROOT = self._original_repo

    def test_returns_empty_when_no_unreleased_section(self):
        self.changelog.write_text("# Changelog\n\n## [v0.2.0]\nOld\n", encoding="utf-8")
        self.assertEqual(unreleased_section(), "")

    def test_returns_section_body(self):
        self.changelog.write_text(
            "# Changelog\n\n## [Unreleased]\n\nPhase 13 ships Helm.\n\n## [v0.2.0]\nOld\n",
            encoding="utf-8",
        )
        body = unreleased_section()
        self.assertIn("Phase 13", body)
        self.assertNotIn("Old", body)


class PhasesMissingFromChangelogTest(unittest.TestCase):
    """End-to-end test that builds a self-contained repo layout."""

    def setUp(self):
        self.tmp = tempfile.TemporaryDirectory()
        self.root = Path(self.tmp.name)
        self._original_repo = _release_dry_run.REPO_ROOT
        _release_dry_run.REPO_ROOT = self.root

        docs = self.root / "docs"
        docs.mkdir()
        for number, status in [
            (1, "**Complete.**"),
            (13, "**Complete.**"),
            (14, "**Complete.**"),
            (15, "**Complete.**"),
            (16, "**In Progress.**"),
        ]:
            phase_dir = docs / f"phase{number}"
            phase_dir.mkdir()
            (phase_dir / "README.md").write_text(
                f"# Phase {number}\n\n## Status\n\n{status}\n",
                encoding="utf-8",
            )
        # CHANGELOG references only Phase 13.
        (self.root / "CHANGELOG.md").write_text(
            "# Changelog\n\n## [Unreleased]\n\nPhase 13.\n\n## [v0.2.0]\nOld.\n",
            encoding="utf-8",
        )

    def tearDown(self):
        self.tmp.cleanup()
        _release_dry_run.REPO_ROOT = self._original_repo

    def test_completed_phases_returns_only_complete(self):
        complete = completed_phases()
        self.assertIn(13, complete)
        self.assertIn(14, complete)
        self.assertIn(15, complete)
        self.assertNotIn(16, complete)

    def test_missing_phases_excludes_phases_in_unreleased(self):
        missing = phases_missing_from_changelog(strict=True)
        self.assertNotIn(13, missing)
        self.assertIn(14, missing)
        self.assertIn(15, missing)

    def test_missing_phases_default_exempts_v020(self):
        # Default (non-strict) exempts phases 1-12 because they shipped
        # under [v0.2.0] and phases 13-16 because they shipped under
        # [v0.3.0] (per ADR-057). Phase 14 and 15 are not exempt in
        # the strict test above but the default exemption only applies
        # when the corresponding versioned section exists; we exercise
        # only the default path here.
        missing = phases_missing_from_changelog(strict=False)
        self.assertNotIn(1, missing)
        # Phase 14/15 are not in [Unreleased] but are exempt under the
        # default policy (number <= 16). Use --strict to surface them.
        self.assertNotIn(14, missing)
        self.assertNotIn(15, missing)

    def test_missing_phases_strict_after_v030_exemption(self):
        # Default policy exempts phases 1-16 (Phases 1-12 -> v0.2.0,
        # Phases 13-16 -> v0.3.0). Phases 17+ would not be exempt and
        # would be reported as missing.
        # (No Phase 17 in this fixture; the assertion is that 14/15 are
        # not in the missing list under default policy.)
        missing = phases_missing_from_changelog(strict=False)
        self.assertNotIn(13, missing)
        self.assertNotIn(14, missing)
        self.assertNotIn(15, missing)


class ReadPomVersionTest(unittest.TestCase):
    def setUp(self):
        self.tmp = tempfile.TemporaryDirectory()
        self.root = Path(self.tmp.name)
        self._original_repo = _release_dry_run.REPO_ROOT
        _release_dry_run.REPO_ROOT = self.root

    def tearDown(self):
        self.tmp.cleanup()
        _release_dry_run.REPO_ROOT = self._original_repo

    def test_extracts_project_version(self):
        (self.root / "pom.xml").write_text(
            "<?xml version='1.0'?>\n"
            "<project>\n"
            "    <groupId>io.astrasync</groupId>\n"
            "    <artifactId>astrasync</artifactId>\n"
            "    <version>0.3.0-SNAPSHOT</version>\n"
            "    <dependencies>\n"
            "        <dependency>\n"
            "            <groupId>com.example</groupId>\n"
            "            <artifactId>example</artifactId>\n"
            "            <version>1.0.0</version>\n"
            "        </dependency>\n"
            "    </dependencies>\n"
            "</project>\n",
            encoding="utf-8",
        )
        self.assertEqual(read_pom_version(), "0.3.0-SNAPSHOT")

    def test_returns_none_when_artifact_id_missing(self):
        (self.root / "pom.xml").write_text(
            "<project>\n    <version>0.1.0-SNAPSHOT</version>\n</project>\n",
            encoding="utf-8",
        )
        self.assertIsNone(read_pom_version())

    def test_returns_none_when_pom_missing(self):
        self.assertIsNone(read_pom_version())


class IterProtosTest(unittest.TestCase):
    def setUp(self):
        self.tmp = tempfile.TemporaryDirectory()
        self.root = Path(self.tmp.name)
        self._original_repo = _release_dry_run.REPO_ROOT
        _release_dry_run.REPO_ROOT = self.root
        proto_root = self.root / "api" / "protobuf" / "v1"
        proto_root.mkdir(parents=True)
        (proto_root / "job.proto").write_text("// job\n", encoding="utf-8")
        (proto_root / "access.proto").write_text("// access\n", encoding="utf-8")

    def tearDown(self):
        self.tmp.cleanup()
        _release_dry_run.REPO_ROOT = self._original_repo

    def test_walks_tracked_proto_roots(self):
        protos = iter_protos()
        names = [p.name for p in protos]
        self.assertIn("job.proto", names)
        self.assertIn("access.proto", names)

    def test_handles_missing_proto_root(self):
        # Switch to a fresh root that has no api/protobuf.
        with tempfile.TemporaryDirectory() as empty:
            self.assertEqual(iter_protos(Path(empty)), [])


if __name__ == "__main__":
    unittest.main()
