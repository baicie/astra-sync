#!/usr/bin/env python3
"""Guard repository runbook and multi-region templates.

The guard enforces two invariants recorded by ADR-046 (and extended by
ADR-056 to cover Phase 14 ArgoCD onboarding and Phase 15 connector
authoring guides):

1. **Template mode.** Every repository-side template (runbooks, Helm
   multi-region profiles, ArgoCD README) must contain at least one
   ``<placeholder>`` token. A populated template will not contain
   placeholders because the operator replaced them with
   environment-specific values.
2. **Doc mode.** Every repository-side operator guide
   (``docs/catalog-authoring.md``, ``deployment/argocd/README.md``) must
   not contain a known production hostname pattern. The guard is
   best-effort and a review-side check, not an exhaustive filter.
3. **Both modes.** No file may contain a known production hostname
   pattern.

Each registered root declares its mode:

- ``runbooks`` (default) — template mode
- ``docs/observability`` — template mode
- ``deployment/helm/astrasync/templates/multi-region`` — template mode
- ``deployment/argocd`` — template mode (the README is operator onboarding)
- ``docs/catalog-authoring.md`` — doc mode

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


# Repository root.
REPO_ROOT = Path(__file__).resolve().parent.parent

# Repository-relative path to the runbook templates directory.
DEFAULT_ROOT = REPO_ROOT / "docs" / "runbooks"

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


# Roots that the guard scans as part of ``check-all``. Each entry is a
# (path, mode) tuple where mode is either ``template`` (must contain a
# <placeholder>) or ``doc`` (placeholder check skipped; only prod
# hostname check applied).
#
# Notes on individual roots:
#
# - ``deployment/argocd``: only the operator-onboarding README is a
#   template; the ArgoCD Application / ApplicationSet / namespace / values
#   manifests are real Kubernetes configs whose identifiers must be
#   literal. We scan the README in template mode and the rest of the
#   directory in doc mode (production-hostname check only).
# - ``docs/catalog-authoring.md``: operator guide, doc mode.
SCAN_ROOTS: tuple[tuple[Path, str], ...] = (
    (REPO_ROOT / "docs" / "runbooks", "template"),
    (REPO_ROOT / "docs" / "observability", "template"),
    (REPO_ROOT / "deployment" / "helm" / "astrasync" / "templates" / "multi-region", "template"),
    (REPO_ROOT / "deployment" / "argocd" / "README.md", "template"),
    (REPO_ROOT / "deployment" / "argocd", "doc"),
    (REPO_ROOT / "docs" / "catalog-authoring.md", "doc"),
)


def iter_markdown_files(root: Path) -> list[Path]:
    """Return supported template files under ``root`` sorted by path.

    When ``root`` is a single file, return ``[root]`` if it exists.
    """
    if root.is_file():
        return [root] if root.exists() else []
    if not root.exists():
        return []
    suffixes = {".md", ".yaml", ".yml"}
    return sorted(p for p in root.rglob("*") if p.is_file() and p.suffix in suffixes)


def file_contains_placeholder(path: Path) -> bool:
    """Return True when ``path`` contains at least one placeholder."""
    text = path.read_text(encoding="utf-8")
    return PLACEHOLDER_RE.search(text) is not None


def file_contains_prod_hostname(path: Path) -> list[str]:
    """Return the list of prod hostname patterns found in ``path``.

    YAML files are intentionally skipped in doc mode because they are
    runtime Kubernetes configs whose hostname-like identifiers (e.g.
    ArgoCD ApplicationSet comments using ``astrasync-prod-production`` as
    a naming example) are not authoritative environment substitutions.
    """
    if path.suffix in {".yaml", ".yml"}:
        # Skip YAML: runbook-hostname assertions target the human-facing
        # operator docs (.md), not the runtime Kubernetes config.
        return []
    text = path.read_text(encoding="utf-8")
    return [pattern for pattern in PROD_HOSTNAME_PATTERNS if pattern in text]


def check(root: Path, mode: str = "template") -> list[str]:
    """Return a list of failure messages. Empty list means the gate passed."""
    failures: list[str] = []
    for path in iter_markdown_files(root):
        if mode == "template" and not file_contains_placeholder(path):
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


def check_all() -> list[str]:
    """Run the gate against every registered root and aggregate failures."""
    failures: list[str] = []
    for root, mode in SCAN_ROOTS:
        failures.extend(check(root, mode))
    return failures


def main(argv: list[str] | None = None) -> int:
    parser = argparse.ArgumentParser(description=__doc__.splitlines()[0])
    parser.add_argument(
        "--root",
        type=Path,
        default=DEFAULT_ROOT,
        help=f"path to the runbooks directory (default: {DEFAULT_ROOT})",
    )
    parser.add_argument(
        "--mode",
        choices=("template", "doc"),
        default="template",
        help="placeholder enforcement mode (default: template)",
    )
    parser.add_argument(
        "--all",
        action="store_true",
        help="scan every registered root (used by the CI check-docs gate)",
    )
    args = parser.parse_args(argv)

    if args.all:
        failures = check_all()
    else:
        failures = check(args.root, args.mode)
    if failures:
        for message in failures:
            print(message, file=sys.stderr)
        return 1
    return 0


if __name__ == "__main__":
    sys.exit(main())
