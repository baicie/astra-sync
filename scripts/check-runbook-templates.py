#!/usr/bin/env python3
"""Guard that every Markdown file under ``docs/runbooks/`` is a template.

The guard enforces two invariants recorded by ADR-046:

1. Every runbook template in the repository must contain at least one
   ``<placeholder>`` token. A populated runbook that ships to the
   repository will not contain placeholders because the operator
   replaced them with environment-specific values.
2. Every runbook template must not contain a known production
   hostname pattern. The list is intentionally short; the guard is
   best-effort and a review-side check, not an exhaustive filter.

Usage::

    python3 scripts/check-runbook-templates.py [--root PATH]

Exit code 0 means every template passes. Exit code 1 means at least
one file failed an assertion; the script prints the failing file and
the failing assertion to stderr.
"""

from __future__ import annotations

import argparse
import re
import sys
from pathlib import Path

# Repository-relative path to the runbook templates directory.
DEFAULT_ROOT = Path(__file__).resolve().parent.parent / "docs" / "runbooks"

# Token shape: angle-bracket-wrapped identifier. Matches
# ``<placeholder>``, ``<vault-path-prefix>``, etc.
PLACEHOLDER_RE = re.compile(r"<[A-Za-z][A-Za-z0-9_-]*>")

# Known production hostname patterns. The list is short on purpose;
# the guard cannot enumerate every possible production hostname and
# the review-side check covers the rest.
PROD_HOSTNAME_PATTERNS = (
    "astra-prod",
    "prod-control-plane",
    "prod.astrasync",
    "astrasync-prod",
)


def iter_markdown_files(root: Path) -> list[Path]:
    """Return every ``*.md`` file under ``root`` sorted by path."""
    if not root.exists():
        return []
    return sorted(p for p in root.rglob("*.md") if p.is_file())


def file_contains_placeholder(path: Path) -> bool:
    """Return True when ``path`` contains at least one placeholder."""
    text = path.read_text(encoding="utf-8")
    return PLACEHOLDER_RE.search(text) is not None


def file_contains_prod_hostname(path: Path) -> list[str]:
    """Return the list of prod hostname patterns found in ``path``."""
    text = path.read_text(encoding="utf-8")
    return [pattern for pattern in PROD_HOSTNAME_PATTERNS if pattern in text]


def check(root: Path) -> list[str]:
    """Return a list of failure messages. Empty list means the gate passed."""
    failures: list[str] = []
    for path in iter_markdown_files(root):
        if not file_contains_placeholder(path):
            failures.append(
                f"{path}: missing <placeholder> token; populated runbooks "
                f"belong in the deployment-side store, not in the repository"
            )
        prod_patterns = file_contains_prod_hostname(path)
        if prod_patterns:
            failures.append(
                f"{path}: contains production hostname pattern(s) "
                f"{prod_patterns!r}; replace with a <placeholder>"
            )
    return failures


def main(argv: list[str] | None = None) -> int:
    parser = argparse.ArgumentParser(description=__doc__.splitlines()[0])
    parser.add_argument(
        "--root",
        type=Path,
        default=DEFAULT_ROOT,
        help=f"path to the runbooks directory (default: {DEFAULT_ROOT})",
    )
    args = parser.parse_args(argv)

    failures = check(args.root)
    if failures:
        for message in failures:
            print(message, file=sys.stderr)
        return 1
    return 0


if __name__ == "__main__":
    raise SystemExit(main())