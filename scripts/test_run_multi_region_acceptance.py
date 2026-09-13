#!/usr/bin/env python3
"""Unit tests for the multi-region acceptance runner cleanup contract."""

from __future__ import annotations

import importlib.util
import subprocess
import tempfile
import unittest
from pathlib import Path
from unittest import mock


SCRIPT_PATH = Path(__file__).resolve().parent / "run-multi-region-acceptance.py"
SPEC = importlib.util.spec_from_file_location("_run_multi_region_acceptance", SCRIPT_PATH)
MODULE = importlib.util.module_from_spec(SPEC)
SPEC.loader.exec_module(MODULE)


class MultiRegionAcceptanceRunnerTest(unittest.TestCase):
    def test_main_collects_logs_and_tears_down_when_acceptance_fails(self):
        with tempfile.TemporaryDirectory() as temporary_directory:
            log_directory = Path(temporary_directory)
            completed = subprocess.CompletedProcess(args=("go", "test"), returncode=1)
            with (
                mock.patch.object(MODULE.shutil, "which", return_value="docker"),
                mock.patch.object(MODULE, "LOG_DIR", log_directory),
                mock.patch.object(MODULE, "COMPOSE_FILE", log_directory / "compose.yaml"),
                mock.patch.object(MODULE, "run", side_effect=[None, completed, None]) as run,
                mock.patch.object(MODULE.subprocess, "run") as subprocess_run,
            ):
                self.assertEqual(MODULE.main(), 1)

            self.assertEqual(run.call_args_list[0].args[-1], "build")
            self.assertEqual(run.call_args_list[1].args[0], "go")
            self.assertEqual(run.call_args_list[2].args[-3:], ("down", "-v", "--remove-orphans"))
            subprocess_run.assert_called_once()
            self.assertEqual(subprocess_run.call_args.args[0][-2:], ("logs", "--no-color"))
            self.assertTrue((log_directory / "compose.log").exists())

    def test_log_write_failure_still_tears_down(self):
        compose = ("docker", "compose", "-f", "compose.yaml")
        with (
            mock.patch.object(MODULE, "LOG_DIR", Path("Z:/unavailable/astrasync-logs")),
            mock.patch.object(MODULE, "run") as run,
        ):
            MODULE.collect_compose_logs(compose)
            MODULE.teardown_compose(compose)

        run.assert_called_once_with(*compose, "down", "-v", "--remove-orphans", check=False)

    def test_teardown_process_error_is_best_effort(self):
        compose = ("docker", "compose", "-f", "compose.yaml")
        with mock.patch.object(MODULE, "run", side_effect=OSError("docker unavailable")):
            MODULE.teardown_compose(compose)


if __name__ == "__main__":
    unittest.main()
