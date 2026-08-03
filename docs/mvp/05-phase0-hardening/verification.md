# Slice 05 Verification

## Status

Verified on `mvp/05-phase0-hardening`.

## Commits

- Design and ADR baseline: `12a5350` (`docs(mvp): design phase zero hardening`)
- Implementation baseline: `641ac69` (`feat(mvp): harden phase zero runtime`)
- Review-fix implementation: `b7e8fad` (`fix(mvp): close phase zero review findings`)
- This verification record is committed after the checks below.

## Environment

- Windows 11 amd64
- Eclipse Temurin Java 21.0.11
- Apache Maven 3.9.16
- No external production service is required; JDBC integration uses the test-scoped H2 fixture.

## Automated Checks

| Command | Result |
|---|---|
| `mvn.cmd -B -ntp -pl cli -am clean verify -DskipITs` | PASS; clean six-module MVP path, coverage checks and shaded CLI completed |
| `mvn.cmd -B -ntp -pl cli -am verify -DskipITs` | PASS; repeat non-clean verification |
| `mvn.cmd -B -ntp verify -DskipITs` | PASS; 23 reactor modules, 19 test suites, 117 tests, 0 failures, 0 errors, 0 skipped |
| `mvn.cmd -B -ntp spotless:check` | PASS; all reactor Java sources clean |
| `helm lint --strict deployment/helm/astrasync` | PASS; chart rendered without lint errors |
| `helm template` plus parsed YAML assertions | PASS; release/name/fullname and PostgreSQL port overrides render; all 63-character override resource names remain valid and preserve component suffixes |
| PyYAML parse of `.github/workflows/ci.yml` | PASS; five jobs and their expressions load as valid YAML |
| `git diff --check` | PASS |

`actionlint` and `gitleaks` were not available locally. GitHub evaluates the workflow and runs the
full-history Gitleaks repository gate after the commits are pushed to PR #13.

The focused suites include 39 Engine tests and 16 CLI tests. Review regression coverage proves
structured cancellation-callback failures and serialization, real CSV/H2 cancellation boundaries,
parse-time JSON selection/redaction, complete unknown-runtime metrics, and populated/empty
`TIME_WITH_TIMEZONE` rejection. Existing coverage continues to prove reverse resource closure,
suppressed close failures, bounded batches, partial counters, and exit code 5.

## Packaging and Dependency Checks

```powershell
jar tf cli/target/astrasync-cli-0.1.0-SNAPSHOT-all.jar
jdeps --multi-release 21 --ignore-missing-deps cli/target/astrasync-cli-0.1.0-SNAPSHOT-all.jar
```

Both commands completed successfully. The shaded artifact is approximately 9.8 MB and its
`META-INF/services/java.sql.Driver` entry contains:

```text
com.mysql.cj.jdbc.Driver
org.postgresql.Driver
```

The JAR is intentionally self-contained, so `jdeps` lists bundled Jackson, Picocli, CSV, and JDBC
driver packages as internal to the artifact. The Maven Shade plugin still reports the expected
third-party license/manifest overlap and `module-info.class` warnings; these are packaging warnings,
not failed dependency resolution.

## CLI Evidence

The packaged command:

```powershell
java -jar cli/target/astrasync-cli-0.1.0-SNAPSHOT-all.jar run --metrics json examples/phase0/csv/job.yaml
```

returned one JSON object on stdout with `status=SUCCEEDED`, `job=csv-file-copy`,
`deliveryGuarantee=at-most-once`, and `recordsRead=recordsWritten=2`; it created the documented
create-new CSV output. The output is ignored by Git and must be absent before that example is run
again.

Packaged review regressions also passed: a missing JobSpec with `--metrics json` returned exit 2
and exactly one sanitized JSON object; an unknown option containing `super-secret-value` returned
exit 2 without emitting that value; and `run --version` returned exit 0 with
`AstraSync 0.1.0-SNAPSHOT`.

## Acceptance Summary

- Existing text CLI output remains the default and existing exit codes 0/2/3/4 remain unchanged.
- JSON reports carry bounded counters and stage evidence without serializing connector options,
  passwords, SQL, paths, stack traces, or exception causes.
- Cooperative cancellation is synchronous and observed only between bounded connector calls; it
  does not interrupt a driver call already in progress.
- Source and sink ownership remains single-threaded and reverse-ordered at close. A later failure
  or cancellation can leave already committed batches in the output; Phase 0 remains at-most-once.
- Cancellation callback defects retain partial metrics and close-failure evidence without being
  misreported as successful cancellation.
- JDBC zoned time is rejected before the first row request, including for an empty result set, so
  no offset is silently discarded.
- Helm resource names are release-scoped and honor name/fullname overrides while retaining valid
  component suffixes at the DNS label limit.
- H2 is test-only. Production JDBC jobs still require a matching driver and pre-existing schema.

## Known Limits

No signal handler, forced thread interrupt, asynchronous close, checkpoint/retry/recovery, durable
metrics store, or stronger delivery guarantee was added. Those changes require a new design and ADR.
