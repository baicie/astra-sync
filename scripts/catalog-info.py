#!/usr/bin/env python3
"""Print a deployment connector inventory in human-readable form.

Usage::

    python3 scripts/catalog-info.py [INVENTORY.pb]

Reads the inventory protobuf at the given path (defaults to
``deployment/catalog/connector-inventory.pb``) by invoking the AstraSync CLI's
``catalog-print`` subcommand and reformats the output as a tabular summary.

Exit code is 0 on success, non-zero on read or parse failure.
"""

from __future__ import annotations

import argparse
import os
import subprocess
import sys
from pathlib import Path
from typing import Dict, List, Tuple


def find_cli_jar(workspace: Path) -> Path:
    """Locate the built CLI all-deps jar.

    The Makefile builds ``cli/target/astrasync-cli-<version>-all.jar``; the
    version may differ across releases. Pick the highest ``-all.jar`` present.
    """
    cli_target = workspace / "cli" / "target"
    if not cli_target.is_dir():
        raise SystemExit(
            f"cli/target/ is missing; run `make build-java` or "
            f"`mvn -pl cli -am package -DskipTests` first"
        )
    candidates = sorted(cli_target.glob("astrasync-cli-*-all.jar"))
    if not candidates:
        raise SystemExit(
            f"no astrasync-cli-*-all.jar under {cli_target}; "
            f"run `mvn -pl cli -am package -DskipTests` first"
        )
    return candidates[-1]


def run_catalog_print(jar: Path, inventory: Path) -> str:
    """Invoke the CLI and return the stdout as a string."""
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
    """Parse ``key=value`` lines into header dict and descriptor map.

    The format is deterministic line-oriented; ``header.*`` keys become the
    header dict, ``descriptor.<name>.<field>=value`` become per-descriptor
    entries (one dict per descriptor name).
    """
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
    return header, [descriptors[name] for name in sorted(descriptors)]


def format_summary(
    header: Dict[str, str], descriptors: Dict[str, Dict[str, str]]
) -> str:
    lines: List[str] = []
    lines.append("Connector Inventory")
    lines.append("===================")
    lines.append("")
    lines.append(f"  inventory_schema_version: {header.get('inventory_schema_version', '?')}")
    lines.append(f"  inventory_revision:       {header.get('inventory_revision', '?')}")
    lines.append(f"  compiler_revision:        {header.get('compiler_revision', '?')}")
    lines.append(f"  compiler_build:           {header.get('compiler_build', '?')}")
    lines.append(f"  execution_profile:        {header.get('execution_profile', '?')}")
    lines.append(f"  job_spec_schema_revision: {header.get('job_spec_schema_revision', '?')}")
    lines.append(f"  descriptor_count:         {header.get('descriptor_count', '?')}")
    lines.append("")
    lines.append("Descriptors")
    lines.append("-----------")
    if not descriptors:
        lines.append("  (none)")
    for name in sorted(descriptors):
        descriptor = descriptors[name]
        lines.append(f"  {name}")
        lines.append(f"    artifact_version:       {descriptor.get('artifact_version', '?')}")
        lines.append(f"    descriptor_revision:    {descriptor.get('descriptor_revision', '?')}")
        lines.append(f"    descriptor_schema_version: {descriptor.get('descriptor_schema_version', '?')}")
        lines.append(f"    capabilities:           {descriptor.get('capabilities', '')}")
        lines.append(f"    delivery_constraints:   {descriptor.get('delivery_constraints', '')}")
        lines.append(f"    execution_modes:        {descriptor.get('execution_modes', '')}")
        lines.append(f"    option_count:           {descriptor.get('option_count', '?')}")
        lines.append(f"    role_count:             {descriptor.get('role_count', '?')}")
    return os.linesep.join(lines) + os.linesep


def main() -> int:
    parser = argparse.ArgumentParser(description=__doc__.splitlines()[0])
    parser.add_argument(
        "inventory",
        nargs="?",
        default="deployment/catalog/connector-inventory.pb",
        help="Path to a ConnectorInventory protobuf file.",
    )
    parser.add_argument(
        "--jar",
        help="Override the CLI jar path (defaults to cli/target/astrasync-cli-*-all.jar).",
    )
    args = parser.parse_args()

    workspace = Path(__file__).resolve().parent.parent
    inventory_path = Path(args.inventory).resolve()
    if not inventory_path.is_file():
        raise SystemExit(f"inventory not found: {inventory_path}")

    jar_path = Path(args.jar).resolve() if args.jar else find_cli_jar(workspace)

    output = run_catalog_print(jar_path, inventory_path)
    header, descriptors = parse_lines(output)
    sys.stdout.write(format_summary(header, descriptors))
    return 0


if __name__ == "__main__":
    sys.exit(main())
