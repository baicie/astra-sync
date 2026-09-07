#!/usr/bin/env python3
"""Diff two deployment connector inventories and explain the difference.

Usage::

    python3 scripts/diff-catalog.py [--quiet] EXPECTED ACTUAL

Both paths must be ConnectorInventory protobuf files. The script invokes the
AstraSync CLI's ``catalog-print`` subcommand twice, parses the line-oriented
output, and prints a unified diff. Exit code is 0 if the inventories are
equivalent in descriptor set and metadata, 1 otherwise, 2 on tool failure.

The script never reads the binary protobuf directly; it relies on the CLI
itself to decode the file so the diff format stays in lockstep with whatever
the CLI exposes.

Two inventories are considered equivalent when:

- every ``header.*`` key matches (compiler_build, compiler_revision,
  execution_profile, inventory_revision, inventory_schema_version,
  job_spec_schema_revision, descriptor_count); AND
- the set of descriptor names matches (no additions or removals); AND
- for each descriptor name present in both, every per-descriptor field
  matches (artifact_version, descriptor_revision, descriptor_schema_version,
  capabilities, delivery_constraints, execution_modes, option_count,
  role_count).

Build-version-only differences (``compiler_build`` or ``compiler_revision``
changing while descriptor set is identical) are reported as a separate
"build version drift" line and the script still exits 0; this lets a release
re-bake the catalog against the new commit without flagging it as a real
catalog regression.
"""

from __future__ import annotations

import argparse
import difflib
import os
import subprocess
import sys
from pathlib import Path
from typing import Dict, List, Tuple


def find_cli_jar(workspace: Path) -> Path:
    cli_target = workspace / "cli" / "target"
    if not cli_target.is_dir():
        raise SystemExit(
            f"cli/target/ is missing; run `mvn -pl cli -am package -DskipTests` first"
        )
    candidates = sorted(cli_target.glob("astrasync-cli-*-all.jar"))
    if not candidates:
        raise SystemExit(
            f"no astrasync-cli-*-all.jar under {cli_target}; "
            f"run `mvn -pl cli -am package -DskipTests` first"
        )
    return candidates[-1]


def run_catalog_print(jar: Path, inventory: Path) -> str:
    completed = subprocess.run(
        ["java", "-jar", str(jar), "catalog-print", str(inventory)],
        capture_output=True,
        text=True,
        check=False,
    )
    if completed.returncode != 0:
        sys.stderr.write(completed.stderr)
        raise SystemExit(
            f"catalog-print exited with {completed.returncode}; "
            f"inventory may be invalid: {inventory}"
        )
    return completed.stdout


def parse_lines(output: str) -> Tuple[Dict[str, str], Dict[str, Dict[str, str]]]:
    header: Dict[str, str] = {}
    descriptors: Dict[str, Dict[str, str]] = {}
    for raw_line in output.splitlines():
        line = raw_line.strip()
        if not line or "=" not in line:
            continue
        key, value = line.split("=", 1)
        if key.startswith("header."):
            header[key[len("header.") :]] = value
        elif key.startswith("descriptor."):
            parts = key.split(".", 2)
            if len(parts) != 3:
                continue
            _, name, field = parts
            descriptors.setdefault(name, {})[field] = value
    return header, descriptors


def categorise_diff(
    expected_header: Dict[str, str],
    expected_descriptors: Dict[str, Dict[str, str]],
    actual_header: Dict[str, str],
    actual_descriptors: Dict[str, Dict[str, str]],
) -> Tuple[List[str], List[str], bool]:
    """Return (errors, build_version_drift_notes, is_build_only_drift).

    ``errors`` are lines describing semantic drift (descriptor additions,
    removals, option set changes). ``build_version_drift_notes`` describes
    header-level changes that are expected to flip on every commit even when
    the descriptor set is unchanged.
    """
    errors: List[str] = []
    build_drift: List[str] = []

    # Header drift
    header_keys = set(expected_header) | set(actual_header)
    for key in sorted(header_keys):
        expected_value = expected_header.get(key, "<missing>")
        actual_value = actual_header.get(key, "<missing>")
        if expected_value == actual_value:
            continue
        if key in ("compiler_build", "compiler_revision"):
            build_drift.append(
                f"header.{key}: {expected_value!r} -> {actual_value!r}"
            )
        else:
            errors.append(
                f"header.{key}: {expected_value!r} -> {actual_value!r}"
            )

    # Descriptor set additions / removals
    expected_names = set(expected_descriptors)
    actual_names = set(actual_descriptors)
    for added in sorted(actual_names - expected_names):
        errors.append(f"descriptor added: {added}")
    for removed in sorted(expected_names - actual_names):
        errors.append(f"descriptor removed: {removed}")

    # Per-descriptor field changes
    common = expected_names & actual_names
    for name in sorted(common):
        expected_fields = expected_descriptors[name]
        actual_fields = actual_descriptors[name]
        for field in sorted(set(expected_fields) | set(actual_fields)):
            expected_value = expected_fields.get(field, "<missing>")
            actual_value = actual_fields.get(field, "<missing>")
            if expected_value != actual_value:
                errors.append(
                    f"descriptor.{name}.{field}: {expected_value!r} -> {actual_value!r}"
                )

    is_build_only = not errors and bool(build_drift)
    return errors, build_drift, is_build_only


def print_unified_diff(
    expected_path: Path,
    expected_output: str,
    actual_path: Path,
    actual_output: str,
) -> None:
    expected_lines = expected_output.splitlines(keepends=True)
    actual_lines = actual_output.splitlines(keepends=True)
    diff = difflib.unified_diff(
        expected_lines,
        actual_lines,
        fromfile=str(expected_path),
        tofile=str(actual_path),
        n=3,
    )
    sys.stdout.write("".join(diff))


def main() -> int:
    parser = argparse.ArgumentParser(description=__doc__.splitlines()[0])
    parser.add_argument("expected", help="Expected (committed) catalog protobuf.")
    parser.add_argument("actual", help="Actual (freshly-exported) catalog protobuf.")
    parser.add_argument(
        "--jar",
        help="Override the CLI jar path (defaults to cli/target/astrasync-cli-*-all.jar).",
    )
    parser.add_argument(
        "--quiet",
        action="store_true",
        help="Suppress the unified diff; print only the categorised summary.",
    )
    args = parser.parse_args()

    workspace = Path(__file__).resolve().parent.parent
    expected_path = Path(args.expected).resolve()
    actual_path = Path(args.actual).resolve()
    if not expected_path.is_file():
        raise SystemExit(f"expected inventory not found: {expected_path}")
    if not actual_path.is_file():
        raise SystemExit(f"actual inventory not found: {actual_path}")

    jar_path = Path(args.jar).resolve() if args.jar else find_cli_jar(workspace)

    expected_output = run_catalog_print(jar_path, expected_path)
    actual_output = run_catalog_print(jar_path, actual_path)

    expected_header, expected_descriptors = parse_lines(expected_output)
    actual_header, actual_descriptors = parse_lines(actual_output)

    errors, build_drift, is_build_only = categorise_diff(
        expected_header,
        expected_descriptors,
        actual_header,
        actual_descriptors,
    )

    if not errors and not build_drift:
        return 0

    sys.stdout.write(f"Comparing {expected_path} (expected)" + os.linesep)
    sys.stdout.write(f"     with {actual_path} (actual)" + os.linesep)
    sys.stdout.write(os.linesep)

    if build_drift:
        sys.stdout.write("Build-version drift (expected to flip per commit):" + os.linesep)
        for line in build_drift:
            sys.stdout.write(f"  - {line}" + os.linesep)
        sys.stdout.write(os.linesep)

    if errors:
        sys.stdout.write("Catalog drift:" + os.linesep)
        for line in errors:
            sys.stdout.write(f"  - {line}" + os.linesep)
        sys.stdout.write(os.linesep)
        if not args.quiet:
            sys.stdout.write("Unified diff of catalog-print output:" + os.linesep)
            print_unified_diff(expected_path, expected_output, actual_path, actual_output)

    if is_build_only:
        # Descriptor set matches; only header-level build fields differ.
        # This is expected during a release re-bake and not a regression.
        sys.stdout.write(
            "Verdict: build-version-only drift; descriptor set is identical. "
            "Re-bake the committed catalog against the current commit." + os.linesep
        )
        return 0

    sys.stdout.write(
        "Verdict: catalog drift detected; the committed inventory must be "
        "regenerated via `make catalog-export` and the new bytes committed." + os.linesep
    )
    return 1


if __name__ == "__main__":
    sys.exit(main())
