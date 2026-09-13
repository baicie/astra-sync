#!/usr/bin/env python3
"""Resolve envtest tool versions from the controller Go module."""

from __future__ import annotations

import argparse
import json
import re
import subprocess
import sys
from pathlib import Path
from typing import Dict


CONTROLLER_RUNTIME_MODULE = "sigs.k8s.io/controller-runtime"
KUBERNETES_MODULE = "k8s.io/apimachinery"
SEMVER = re.compile(r"^v(\d+)\.(\d+)\.\d+(?:[-+][0-9A-Za-z.-]+)?$")


def parse_module_versions(document: str) -> Dict[str, str]:
    """Parse concatenated ``go list -m -json`` documents."""
    versions: Dict[str, str] = {}
    decoder = json.JSONDecoder()
    offset = 0
    while offset < len(document):
        while offset < len(document) and document[offset].isspace():
            offset += 1
        if offset >= len(document):
            break
        value, offset = decoder.raw_decode(document, offset)
        if not isinstance(value, dict):
            raise ValueError("go list output must contain JSON objects")
        path = value.get("Path")
        version = value.get("Version")
        if isinstance(path, str) and isinstance(version, str):
            versions[path] = version
    return versions


def envtest_versions(module_versions: Dict[str, str]) -> Dict[str, str]:
    """Return setup-envtest and Kubernetes test-binary selectors."""
    try:
        controller_runtime = module_versions[CONTROLLER_RUNTIME_MODULE]
        kubernetes = module_versions[KUBERNETES_MODULE]
    except KeyError as error:
        raise ValueError(f"missing module version: {error.args[0]}") from error

    kubernetes_match = SEMVER.fullmatch(kubernetes)
    if SEMVER.fullmatch(controller_runtime) is None or kubernetes_match is None:
        raise ValueError(
            "controller-runtime and k8s.io/apimachinery versions must be stable "
            "semantic versions"
        )
    if kubernetes_match.group(1) not in {"0", "1"}:
        raise ValueError("k8s.io/apimachinery major version is not supported")
    return {
        "SETUP_ENVTEST_VERSION": controller_runtime,
        "ENVTEST_K8S_VERSION": f"1.{kubernetes_match.group(2)}.x",
    }


def resolve_versions(module_dir: Path) -> Dict[str, str]:
    """Read module versions through the Go toolchain."""
    completed = subprocess.run(
        [
            "go",
            "list",
            "-m",
            "-json",
            CONTROLLER_RUNTIME_MODULE,
            KUBERNETES_MODULE,
        ],
        cwd=module_dir,
        capture_output=True,
        text=True,
        check=False,
    )
    if completed.returncode != 0:
        sys.stderr.write(completed.stderr)
        raise SystemExit(
            f"go list failed for controller module: {module_dir}"
        )
    return envtest_versions(parse_module_versions(completed.stdout))


def main(argv: list[str] | None = None) -> int:
    parser = argparse.ArgumentParser(description=__doc__.splitlines()[0])
    parser.add_argument(
        "--module-dir",
        type=Path,
        default=Path(__file__).resolve().parent.parent
        / "control-plane"
        / "controller",
        help="Controller Go module directory.",
    )
    args = parser.parse_args(argv)
    for name, value in resolve_versions(args.module_dir.resolve()).items():
        print(f"{name}={value}")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
