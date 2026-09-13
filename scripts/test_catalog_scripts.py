"""Unit tests for ``scripts/diff-catalog.py`` and ``scripts/catalog-info.py``.

The tests exercise the pure parsing logic by importing the helper functions
directly and feeding them synthetic ``catalog-print`` output. They do not
spawn a JVM, which keeps the test cheap and avoids requiring the CLI jar to
be built in CI's lint-only runs.
"""

from __future__ import annotations

import importlib.util
import os
import sys
import unittest
from typing import Tuple


# Allow importing the helper modules from scripts/ without packaging them.
# The scripts use dash-separated filenames (e.g. ``diff-catalog.py``), which
# Python's import machinery does not map to identifier-style module names, so
# we load them via ``importlib.util`` instead.
SCRIPTS_DIR = os.path.dirname(os.path.abspath(__file__))


def _load(module_name: str, file_name: str):
    path = os.path.join(SCRIPTS_DIR, file_name)
    spec = importlib.util.spec_from_file_location(module_name, path)
    module = importlib.util.module_from_spec(spec)
    sys.modules[module_name] = module
    spec.loader.exec_module(module)
    return module


_diff_catalog = _load("diff_catalog_script_under_test", "diff-catalog.py")
_catalog_info = _load("catalog_info_script_under_test", "catalog-info.py")

categorise_diff = _diff_catalog.categorise_diff
parse_lines = _diff_catalog.parse_lines
format_summary = _catalog_info.format_summary


SAMPLE_OUTPUT = """
header.compiler_build=a1b2c3d
header.compiler_revision=sha256:def456
header.execution_profile=standard
header.inventory_revision=sha256:abc123
header.inventory_schema_version=1
header.job_spec_schema_revision=sync.astrasync.io/v1
header.descriptor_count=2
descriptor.name=csv
descriptor.csv.artifact_version=1.0.0
descriptor.csv.descriptor_revision=sha256:csv-rev
descriptor.csv.descriptor_schema_version=1
descriptor.csv.capabilities=SNAPSHOT
descriptor.csv.delivery_constraints=AT_MOST_ONCE
descriptor.csv.execution_modes=BATCH
descriptor.csv.option_count=2
descriptor.csv.role_count=1
descriptor.name=jdbc
descriptor.jdbc.artifact_version=2.0.0
descriptor.jdbc.descriptor_revision=sha256:jdbc-rev
descriptor.jdbc.descriptor_schema_version=1
descriptor.jdbc.capabilities=SNAPSHOT
descriptor.jdbc.delivery_constraints=AT_LEAST_ONCE
descriptor.jdbc.execution_modes=BATCH,STREAMING
descriptor.jdbc.option_count=5
descriptor.jdbc.role_count=2
""".strip()


def _build_output(
    compiler_build: str = "a1b2c3d",
    compiler_revision: str = "sha256:def456",
    csv_revision: str = "sha256:csv-rev",
    csv_options: str = "2",
    jdbc_revision: str = "sha256:jdbc-rev",
    jdbc_options: str = "5",
) -> str:
    return f"""
header.compiler_build={compiler_build}
header.compiler_revision={compiler_revision}
header.execution_profile=standard
header.inventory_revision=sha256:abc123
header.inventory_schema_version=1
header.job_spec_schema_revision=sync.astrasync.io/v1
header.descriptor_count=2
descriptor.name=csv
descriptor.csv.artifact_version=1.0.0
descriptor.csv.descriptor_revision={csv_revision}
descriptor.csv.descriptor_schema_version=1
descriptor.csv.capabilities=SNAPSHOT
descriptor.csv.delivery_constraints=AT_MOST_ONCE
descriptor.csv.execution_modes=BATCH
descriptor.csv.option_count={csv_options}
descriptor.csv.role_count=1
descriptor.name=jdbc
descriptor.jdbc.artifact_version=2.0.0
descriptor.jdbc.descriptor_revision={jdbc_revision}
descriptor.jdbc.descriptor_schema_version=1
descriptor.jdbc.capabilities=SNAPSHOT
descriptor.jdbc.delivery_constraints=AT_LEAST_ONCE
descriptor.jdbc.execution_modes=BATCH,STREAMING
descriptor.jdbc.option_count={jdbc_options}
descriptor.jdbc.role_count=2
""".strip()


class ParseLinesTests(unittest.TestCase):
    def test_separates_header_and_descriptors(self):
        header, descriptors = parse_lines(SAMPLE_OUTPUT)
        self.assertEqual(header["compiler_build"], "a1b2c3d")
        self.assertEqual(header["descriptor_count"], "2")
        self.assertEqual(set(descriptors), {"csv", "jdbc"})
        self.assertEqual(descriptors["csv"]["artifact_version"], "1.0.0")
        self.assertEqual(descriptors["jdbc"]["option_count"], "5")

    def test_ignores_blank_lines_and_malformed(self):
        header, descriptors = parse_lines("\n\nmalformed\nheader.x=y\n")
        self.assertEqual(header, {"x": "y"})
        self.assertEqual(descriptors, {})

    def test_skips_descriptor_with_wrong_key_arity(self):
        # descriptor.foo (no name) is malformed; should be ignored silently.
        output = "header.x=y\ndescriptor.foo=bar\n"
        header, descriptors = parse_lines(output)
        self.assertEqual(header, {"x": "y"})
        self.assertEqual(descriptors, {})


class CategoriseDiffTests(unittest.TestCase):
    def test_identical_outputs_produce_no_drift(self):
        a = _build_output()
        b = _build_output()
        expected_header, expected_descriptors = parse_lines(a)
        actual_header, actual_descriptors = parse_lines(b)
        errors, drift, is_build_only = categorise_diff(
            expected_header,
            expected_descriptors,
            actual_header,
            actual_descriptors,
        )
        self.assertEqual(errors, [])
        self.assertEqual(drift, [])
        self.assertFalse(is_build_only)

    def test_compiler_build_drift_is_build_only(self):
        a = _build_output(compiler_build="a1b2c3d")
        b = _build_output(compiler_build="d4e5f6g")
        expected_header, expected_descriptors = parse_lines(a)
        actual_header, actual_descriptors = parse_lines(b)
        errors, drift, is_build_only = categorise_diff(
            expected_header,
            expected_descriptors,
            actual_header,
            actual_descriptors,
        )
        self.assertEqual(errors, [])
        self.assertEqual(len(drift), 1)
        self.assertIn("compiler_build", drift[0])
        self.assertTrue(is_build_only)

    def test_descriptor_added_is_an_error(self):
        a = _build_output()
        b_extra = a + "\ndescriptor.name=iceberg\ndescriptor.iceberg.artifact_version=3.0.0\ndescriptor.iceberg.descriptor_revision=sha256:ice\ndescriptor.iceberg.descriptor_schema_version=1\ndescriptor.iceberg.capabilities=SNAPSHOT\ndescriptor.iceberg.delivery_constraints=AT_LEAST_ONCE\ndescriptor.iceberg.execution_modes=BATCH\ndescriptor.iceberg.option_count=1\ndescriptor.iceberg.role_count=1\nheader.descriptor_count=3\n"
        expected_header, expected_descriptors = parse_lines(a)
        actual_header, actual_descriptors = parse_lines(b_extra)
        errors, drift, is_build_only = categorise_diff(
            expected_header,
            expected_descriptors,
            actual_header,
            actual_descriptors,
        )
        self.assertTrue(any("descriptor added: iceberg" in e for e in errors))
        self.assertFalse(is_build_only)

    def test_descriptor_option_count_change_is_an_error(self):
        a = _build_output(csv_options="2")
        b = _build_output(csv_options="3")
        expected_header, expected_descriptors = parse_lines(a)
        actual_header, actual_descriptors = parse_lines(b)
        errors, drift, is_build_only = categorise_diff(
            expected_header,
            expected_descriptors,
            actual_header,
            actual_descriptors,
        )
        self.assertTrue(any("option_count" in e for e in errors))
        self.assertFalse(is_build_only)

    def test_header_execution_profile_change_is_an_error(self):
        # Only header.* fields outside compiler_build/compiler_revision are
        # treated as semantic drift. execution_profile affects capability
        # negotiation, so it must NOT be classified as build-version drift.
        a = "header.compiler_build=a1b2c3d\nheader.compiler_revision=sha256:x\nheader.execution_profile=standard\nheader.inventory_revision=sha256:y\nheader.inventory_schema_version=1\nheader.job_spec_schema_revision=z\nheader.descriptor_count=0\n"
        b = "header.compiler_build=a1b2c3d\nheader.compiler_revision=sha256:x\nheader.execution_profile=minimal\nheader.inventory_revision=sha256:y\nheader.inventory_schema_version=1\nheader.job_spec_schema_revision=z\nheader.descriptor_count=0\n"
        expected_header, expected_descriptors = parse_lines(a)
        actual_header, actual_descriptors = parse_lines(b)
        errors, drift, is_build_only = categorise_diff(
            expected_header,
            expected_descriptors,
            actual_header,
            actual_descriptors,
        )
        self.assertEqual(drift, [])
        self.assertTrue(any("execution_profile" in e for e in errors))
        self.assertFalse(is_build_only)


class FormatSummaryTests(unittest.TestCase):
    def test_summary_contains_descriptor_names(self):
        header, descriptors = parse_lines(SAMPLE_OUTPUT)
        text = format_summary(header, descriptors)
        self.assertIn("Connector Inventory", text)
        self.assertIn("csv", text)
        self.assertIn("jdbc", text)
        self.assertIn("inventory_revision", text)

    def test_summary_handles_empty_descriptors(self):
        header, descriptors = parse_lines("header.descriptor_count=0\n")
        text = format_summary(header, descriptors)
        self.assertIn("(none)", text)


if __name__ == "__main__":
    unittest.main()
