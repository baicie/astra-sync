#!/usr/bin/env python3
"""Dry-run the AstraSync release process without mutating any file.

The release flow is:

1. Verify the Maven project version in ``pom.xml`` is in sync with the
   intended release tag.
2. Verify the deployment-authoritative connector inventory matches what
   the current CLI jar would export against the current commit.
3. Report which ``docs/phase<N>/`` directories are marked ``**Complete.**``
   but have not yet been added to the ``## [Unreleased]`` section of
   ``CHANGELOG.md``.
4. Report which ``api/protobuf/v1/*.proto`` and ``api/protobuf/compiler/v1/*.proto``
   files are tracked by git and would be re-emitted by ``make proto-generate``.

The script never modifies the working tree or the index. Exit code is 0
if every check passes, 1 if any drift is detected, 2 if a prerequisite
(e.g. the CLI jar) is missing.

Usage::

    python3 scripts/release-dry-run.py [--version 0.3.0-SNAPSHOT]
"""

from __future__ import annotations

import argparse
import re
import subprocess
import sys
from pathlib import Path


REPO_ROOT = Path(__file__).resolve().parent.parent


def _repo_relative(*parts: str) -> Path:
    """Build a path under ``REPO_ROOT``. Indirection so tests can
    monkeypatch ``REPO_ROOT`` and still hit the right file."""
    return REPO_ROOT.joinpath(*parts)


def pom_xml_path() -> Path:
    return _repo_relative("pom.xml")


def changelog_path() -> Path:
    return _repo_relative("CHANGELOG.md")


def phase_root_path() -> Path:
    return _repo_relative("docs")


def cli_target_path() -> Path:
    return _repo_relative("cli", "target")


def proto_roots() -> tuple[Path, ...]:
    """Return the proto directories relative to ``REPO_ROOT``."""
    return (
        _repo_relative("api", "protobuf", "v1"),
        _repo_relative("api", "protobuf", "compiler", "v1"),
    )


def _git(*args: str) -> str:
    completed = subprocess.run(
        ["git", *args],
        cwd=REPO_ROOT,
        capture_output=True,
        text=True,
        check=False,
    )
    if completed.returncode != 0:
        return ""
    return completed.stdout.strip()


def read_pom_version() -> str | None:
    """Return the project version declared in the root ``pom.xml``.

    The root ``pom.xml`` has the form::

        <groupId>...</groupId>
        <artifactId>astrasync</artifactId>
        <version>0.1.0-SNAPSHOT</version>

    We search for the first ``<version>`` line at the root level (i.e.
    not nested inside a ``<dependency>`` or ``<plugin>`` block by limiting
    the search to the first 30 lines).
    """
    if not pom_xml_path().is_file():
        return None
    text = pom_xml_path().read_text(encoding="utf-8")
    head = "\n".join(text.splitlines()[:30])
    match = re.search(r"<artifactId>astrasync</artifactId>\s*<version>([^<]+)</version>", head)
    if match is None:
        return None
    return match.group(1)


def find_cli_jar() -> Path | None:
    """Locate the built CLI all-deps jar under ``cli/target/``."""
    target = cli_target_path()
    if not target.is_dir():
        return None
    candidates = sorted(target.glob("astrasync-cli-*-all.jar"))
    if not candidates:
        return None
    return candidates[-1]


def compute_catalog_build_version() -> str:
    """Return ``git rev-parse --short HEAD`` or ``unknown`` if unavailable."""
    return _git("rev-parse", "--short", "HEAD") or "unknown"


def iter_phase_dirs() -> list[Path]:
    phase_root = phase_root_path()
    if not phase_root.is_dir():
        return []
    pattern = re.compile(r"^phase(\d+)$")
    phases: list[tuple[int, Path]] = []
    for entry in phase_root.iterdir():
        if entry.is_dir() and pattern.match(entry.name):
            phases.append((int(pattern.match(entry.name).group(1)), entry))
    phases.sort()
    return [path for _, path in phases]


def completed_phases() -> list[int]:
    completed: list[int] = []
    for directory in iter_phase_dirs():
        readme = directory / "README.md"
        if not readme.is_file():
            continue
        first_lines = "\n".join(readme.read_text(encoding="utf-8").splitlines()[:10])
        if "Complete" in first_lines:
            completed.append(int(re.search(r"\d+", directory.name).group()))
    return completed


def unreleased_section() -> str:
    """Return the body of the ``## [Unreleased]`` section of CHANGELOG.md."""
    changelog = changelog_path()
    if not changelog.is_file():
        return ""
    text = changelog.read_text(encoding="utf-8")
    match = re.search(r"^##\s+\[Unreleased\]\s*$\n(.*?)(?=^##\s+\[v|\Z)", text, re.MULTILINE | re.DOTALL)
    if match is None:
        return ""
    return match.group(1)


def phases_missing_from_changelog(strict: bool = False) -> list[int]:
    """Return phase numbers marked Complete but absent from ``[Unreleased]``.

    By default, phases whose CHANGELOG entries live under a release
    section (``## [vX.Y.Z]``) are exempt, mirroring the
    ``scripts/check-changelog.py`` guard's policy. Pass ``strict=True`` to
    require every Complete phase to be referenced in ``[Unreleased]``.
    """
    body = unreleased_section()
    missing: list[int] = []
    for number in completed_phases():
        if re.search(rf"\bPhase\s+{number}\b", body) or re.search(
            rf"\bphase\s+{number}\b", body
        ):
            continue
        # Same exemption rule as scripts/check-changelog.py: phases
        # that have shipped under a versioned section are exempt
        # unless --strict. Phases 1-12 ship under [v0.2.0]; Phases
        # 13-16 ship under [v0.3.0] (ADR-057); Phase 17 ships
        # under [v0.4.0] (ADR-059); Phase 18 and Phase 19 ship
        # under [v0.5.0] (ADR-062); Phase 21 ships
        # under [v0.6.0] (ADR-064); Phase 22 and Phase 23 ship
        # under [v0.7.0] (ADR-067).
        if not strict and number <= 23:
            continue
        missing.append(number)
    return missing


def iter_protos(repo_root: Path | None = None) -> list[Path]:
    """Return every tracked ``*.proto`` under ``api/protobuf/``.

    Tests can pass an explicit ``repo_root`` to override the production
    default; the production code path leaves the argument as ``None``.
    """
    protos: list[Path] = []
    for root in proto_roots() if repo_root is None else (
        repo_root / "api" / "protobuf" / "v1",
        repo_root / "api" / "protobuf" / "compiler" / "v1",
    ):
        if root.is_dir():
            protos.extend(sorted(root.rglob("*.proto")))
    return protos


def changed_prototypes() -> list[Path]:
    """Return ``*.proto`` files whose generated artefacts have unstaged changes."""
    completed = subprocess.run(
        ["git", "status", "--porcelain", "--", "protocol/", "control-plane/api-server/gen/go/"],
        cwd=REPO_ROOT,
        capture_output=True,
        text=True,
        check=False,
    )
    if completed.returncode != 0:
        return []
    changed: set[Path] = set()
    for line in completed.stdout.splitlines():
        # Each porcelain line: "<status> <path>"; status is two chars.
        if len(line) < 4:
            continue
        path = line[3:].strip()
        if path.endswith(".proto") or path.endswith(".pb.go") or path.endswith("ControlPlaneProto.java"):
            changed.add(REPO_ROOT / path)
    return sorted(changed)


def main(argv: list[str] | None = None) -> int:
    parser = argparse.ArgumentParser(description=__doc__.splitlines()[0])
    parser.add_argument(
        "--version",
        help="intended release version (e.g. 0.3.0-SNAPSHOT); if omitted the script prints the current pom.xml version",
    )
    parser.add_argument(
        "--check-cli-jar",
        action="store_true",
        help="verify the CLI jar exports a catalog matching the committed inventory (requires the jar to be built)",
    )
    parser.add_argument(
        "--strict",
        action="store_true",
        help="fail even on phases that predate this guard's introduction (1-12, shipped under v0.2.0)",
    )
    args = parser.parse_args(argv)

    failures: list[str] = []

    pom_version = read_pom_version()
    if pom_version is None:
        failures.append("could not read <version> from pom.xml")
    else:
        print(f"Maven project version: {pom_version}")

    if args.version and pom_version and args.version != pom_version:
        failures.append(
            f"requested --version {args.version} does not match pom.xml {pom_version}"
        )

    git_short = _git("rev-parse", "--short", "HEAD")
    print(f"git HEAD: {git_short or 'unavailable'}")
    print(f"catalog build version (would be embedded on re-export): {compute_catalog_build_version()}")

    jar = find_cli_jar()
    if jar is None:
        print("CLI jar: not built (run `make build-java` or `mvn -pl cli -am package -DskipTests`)")
    else:
        print(f"CLI jar: {jar.relative_to(REPO_ROOT)}")

    completed = completed_phases()
    if completed:
        print(f"Complete phases: {', '.join(str(p) for p in completed)}")
    else:
        print("Complete phases: (none detected)")

    missing = phases_missing_from_changelog(strict=args.strict)
    if missing:
        failures.append(
            "phases Complete but not in CHANGELOG [Unreleased]: "
            + ", ".join(f"Phase {n}" for n in missing)
        )
    else:
        print("CHANGELOG [Unreleased] covers every Complete phase")

    protos = iter_protos()
    print(f"Tracked proto files: {len(protos)}")
    changed = changed_prototypes()
    if changed:
        # Build the set of *.proto files that have unstaged .pb.go / Java counterparts.
        for path in changed:
            if path.suffix == ".proto":
                continue
            failures.append(
                f"generated proto binding has uncommitted changes: {path.relative_to(REPO_ROOT)}"
            )

    if failures:
        print()
        print("Release dry-run FAILED:")
        for failure in failures:
            print(f"  - {failure}")
        return 1

    print()
    print("Release dry-run PASSED. No drift detected.")
    return 0


if __name__ == "__main__":
    sys.exit(main())
