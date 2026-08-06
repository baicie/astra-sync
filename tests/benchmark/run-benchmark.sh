#!/usr/bin/env bash
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
cd "${repo_root}"

mvn -B -ntp -pl tests/benchmark -am package -DskipTests
exec java --add-opens=java.base/java.nio=ALL-UNNAMED \
  -jar tests/benchmark/target/astrasync-benchmarks.jar "$@"
