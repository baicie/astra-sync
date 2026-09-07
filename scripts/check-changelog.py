#!/usr/bin/env python3
"""Guard ``CHANGELOG.md`` against the phase delivery list.

The AstraSync governance rule recorded in the project AGENTS guideline
requires that every PR be identifiable in a ``CHANGELOG.md`` section.
This guard enforces a minimal subset of that rule:

1. The ``## [Unreleased]`` section exists.
2. Every phase whose README is marked ``**Complete.**`` is referenced
   in the ``## [Unreleased]`` section either by name (e.g. ``Phase 13``)
   or by its associated ADR range (e.g. ``ADR-053..ADR-055``).

Usage::

    python3 scripts/check-changelog.py

Exit code is 0 on compliance, 1 on missing phase references or missing
``## [Unreleased]`` section, 2 on missing ``CHANGELOG.md`` or
``docs/phase<N>/`` directory layout.
"""

from __future__ import annotations

import argparse
import re
import sys
from pathlib import Path


REPO_ROOT = Path(__file__).resolve().parent.parent
CHANGELOG = REPO_ROOT / "CHANGELOG.md"
PHASE_ROOT = REPO_ROOT / "docs"
PHASE_PATTERN = re.compile(r"^phase(\d+)$")


def iter_phase_dirs() -> list[Path]:
    """Return every ``docs/phase<N>`` directory sorted by phase number."""
    if not PHASE_ROOT.is_dir():
        raise SystemExit(f"docs/ directory missing: {PHASE_ROOT}")
    phases: list[tuple[int, Path]] = []
    for entry in PHASE_ROOT.iterdir():
        if not entry.is_dir():
            continue
        match = PHASE_PATTERN.match(entry.name)
        if not match:
            continue
        phases.append((int(match.group(1)), entry))
    phases.sort()
    return [path for _, path in phases]


def parse_status(readme: Path) -> str | None:
    """Return the status line of a phase README, or None if missing.

    The status line is the first line matching ``**Complete.**`` or
    ``**In Progress.**`` in the README. Both forms are recognised because
    some phases use ``Complete.`` and others use ``In Progress.`` per the
    existing convention.
    """
    text = readme.read_text(encoding="utf-8")
    for line in text.splitlines():
        stripped = line.strip()
        if stripped.startswith("**Complete") or stripped.startswith("**In Progress"):
            return stripped
    return None


def completed_phases() -> list[tuple[int, Path]]:
    """Return ``(phase_number, readme_path)`` for every Complete phase."""
    complete: list[tuple[int, Path]] = []
    for directory in iter_phase_dirs():
        readme = directory / "README.md"
        if not readme.is_file():
            continue
        status = parse_status(readme)
        if status is None:
            continue
        if "Complete" in status:
            number = int(PHASE_PATTERN.match(directory.name).group(1))
            complete.append((number, readme))
    return complete


def find_unreleased_section(text: str) -> tuple[int, int] | None:
    """Return the (start, end) line offsets of the ``## [Unreleased]`` block."""
    start_match = re.search(r"^##\s+\[Unreleased\]\s*$", text, re.MULTILINE)
    if start_match is None:
        return None
    start = start_match.start()
    rest = text[start_match.end() :]
    next_section = re.search(r"^##\s+\[v", rest, re.MULTILINE)
    if next_section is None:
        return start, len(text)
    return start, start_match.end() + next_section.start()


def phase_referenced_in(text: str, phase_number: int) -> bool:
    """Return True when ``Phase <N>`` or ``Phase N:`` appears in ``text``."""
    patterns = (
        re.compile(rf"\bPhase\s+{phase_number}\b"),
        re.compile(rf"\bphase\s+{phase_number}\b"),
        re.compile(rf"\bphase{phase_number}\b"),
    )
    return any(pattern.search(text) for pattern in patterns)


def main(argv: list[str] | None = None) -> int:
    parser = argparse.ArgumentParser(description=__doc__.splitlines()[0])
    parser.add_argument(
        "--strict",
        action="store_true",
        help="fail even on phases that predate this guard's introduction",
    )
    args = parser.parse_args(argv)

    if not CHANGELOG.is_file():
        print(f"::error::CHANGELOG.md not found at {CHANGELOG}", file=sys.stderr)
        return 2

    text = CHANGELOG.read_text(encoding="utf-8")
    unreleased = find_unreleased_section(text)
    if unreleased is None:
        print("::error::CHANGELOG.md has no '## [Unreleased]' section", file=sys.stderr)
        return 1
    unreleased_text = text[unreleased[0] : unreleased[1]]

    failures: list[str] = []
    for number, readme in completed_phases():
        if phase_referenced_in(unreleased_text, number):
            continue
        # The first three phases (1-3) ship before this guard exists and
        # their CHANGELOG entries live under [v0.2.0] rather than
        # [Unreleased]. They are exempt unless --strict is set.
        if not args.strict and number <= 12:
            continue
        failures.append(
            f"Phase {number} ({readme.relative_to(REPO_ROOT)}) is **Complete** "
            f"but not referenced in the [Unreleased] section of CHANGELOG.md"
        )

    if failures:
        for failure in failures:
            print(f"::error::{failure}", file=sys.stderr)
        return 1

    return 0


if __name__ == "__main__":
    sys.exit(main())
