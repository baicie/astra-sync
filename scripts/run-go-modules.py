#!/usr/bin/env python3
"""Run one Go command across the repository's Go modules."""

from __future__ import annotations

import subprocess
import sys
from pathlib import Path


REPOSITORY_ROOT = Path(__file__).resolve().parent.parent
GO_MODULES = (
    "control-plane",
    "control-plane/api-server",
    "control-plane/controller",
    "control-plane/scheduler",
    "control-plane/catalog",
    "control-plane/auth",
    "console",
)


def main(argv: list[str]) -> int:
    if len(argv) != 2 or argv[1] not in {"build", "test", "vet", "fmt"}:
        print(f"usage: {Path(argv[0]).name} <build|test|vet|fmt>", file=sys.stderr)
        return 2

    command = {
        "build": ["go", "build", "./..."],
        "test": ["go", "test", "./..."],
        "vet": ["go", "vet", "./..."],
        "fmt": ["go", "fmt", "./..."],
    }[argv[1]]

    for module in GO_MODULES:
        module_path = REPOSITORY_ROOT / module
        print(f"Running {argv[1]} for {module}...")
        completed = subprocess.run(command, cwd=module_path, check=False)
        if completed.returncode != 0:
            return completed.returncode
    return 0


if __name__ == "__main__":
    raise SystemExit(main(sys.argv))
