#!/usr/bin/env python3
"""Check that two files have identical binary contents.

Usage::

    python3 scripts/check-files-identical.py EXPECTED ACTUAL

Exit code 0 means both files exist and are byte-for-byte identical. Exit code 1
means a file is missing or the contents differ.
"""

from __future__ import annotations

import argparse
import filecmp
import sys
from pathlib import Path


def files_are_identical(expected: Path, actual: Path) -> bool:
    """Return whether both paths exist as files with identical bytes."""
    return expected.is_file() and actual.is_file() and filecmp.cmp(expected, actual, shallow=False)


def main(argv: list[str] | None = None) -> int:
    parser = argparse.ArgumentParser(description=__doc__.splitlines()[0])
    parser.add_argument("expected", type=Path)
    parser.add_argument("actual", type=Path)
    args = parser.parse_args(argv)

    if files_are_identical(args.expected, args.actual):
        return 0

    print(f"files differ or are missing: {args.expected} {args.actual}", file=sys.stderr)
    return 1


if __name__ == "__main__":
    raise SystemExit(main())
