"""Run the Docker Compose multi-region acceptance test with cleanup."""

from __future__ import annotations

import shutil
import subprocess
import sys
from pathlib import Path


ROOT = Path(__file__).resolve().parents[1]
COMPOSE_FILE = ROOT / "tests" / "integration" / "multi-region" / "docker-compose.yaml"
LOG_DIR = ROOT / "tests" / "integration" / "multi-region" / "test-logs"


def run(
    *args: str,
    check: bool = True,
    cwd: Path = ROOT,
) -> subprocess.CompletedProcess[str]:
    return subprocess.run(args, cwd=cwd, check=check, text=True)


def main() -> int:
    if shutil.which("docker") is None:
        print("docker executable is required", file=sys.stderr)
        return 2
    LOG_DIR.mkdir(parents=True, exist_ok=True)
    compose = ("docker", "compose", "-f", str(COMPOSE_FILE))
    try:
        run(*compose, "build")
        return run(
            "go",
            "test",
            "-tags=integration",
            "-v",
            "./multi-region/acceptance",
            cwd=ROOT / "tests" / "integration",
        ).returncode
    finally:
        logs = LOG_DIR / "compose.log"
        with logs.open("w", encoding="utf-8") as output:
            subprocess.run((*compose, "logs", "--no-color"), cwd=ROOT, stdout=output, stderr=subprocess.STDOUT, check=False)
        run(*compose, "down", "-v", "--remove-orphans", check=False)


if __name__ == "__main__":
    raise SystemExit(main())
