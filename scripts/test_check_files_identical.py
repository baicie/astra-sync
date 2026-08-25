#!/usr/bin/env python3
"""Unit tests for ``scripts/check-files-identical.py``."""

from __future__ import annotations

import importlib.util
import tempfile
import unittest
from pathlib import Path

_SCRIPT_PATH = Path(__file__).resolve().parent / "check-files-identical.py"
_spec = importlib.util.spec_from_file_location("_check_files_identical", _SCRIPT_PATH)
_module = importlib.util.module_from_spec(_spec)
_spec.loader.exec_module(_module)

files_are_identical = _module.files_are_identical


class FilesAreIdenticalTest(unittest.TestCase):
    def setUp(self):
        self.tmp = tempfile.TemporaryDirectory()
        self.root = Path(self.tmp.name)
        self.expected = self.root / "expected.pb"
        self.actual = self.root / "actual.pb"

    def tearDown(self):
        self.tmp.cleanup()

    def test_identical_binary_files_pass(self):
        content = bytes([0, 1, 127, 128, 255])
        self.expected.write_bytes(content)
        self.actual.write_bytes(content)
        self.assertTrue(files_are_identical(self.expected, self.actual))

    def test_different_binary_files_fail(self):
        self.expected.write_bytes(b"expected")
        self.actual.write_bytes(b"actual")
        self.assertFalse(files_are_identical(self.expected, self.actual))

    def test_missing_file_fails(self):
        self.expected.write_bytes(b"expected")
        self.assertFalse(files_are_identical(self.expected, self.actual))


if __name__ == "__main__":
    unittest.main()
