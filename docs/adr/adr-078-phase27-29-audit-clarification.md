# ADR-078: Phase 27 / 29 Audit Clarification — Real Diffs in Working-Tree Files

## Status

Proposed

## Context

After committing the eight backfill slices from ADR-077
(commits `eb4930e..d963408`), six files remain in
`git status` as "modified":

| File | `git diff --shortstat` |
| --- | --- |
| `control-plane/api-server/internal/authn/interceptor_test.go` | +127 / -0 |
| `control-plane/api-server/internal/service/job_mutation_service_test.go` | +118 / -5 |
| `control-plane/api-server/internal/service/job_validation_service.go` | +17 / -0 |
| `control-plane/job/postgres/mutation_repository.go` | +7 / -3 |
| `control-plane/job/postgres/repository.go` | +15 / -2 |
| `deployment/operator/config/crd/bases/sync.astrasync.io_syncjobs.yaml` | +4 / -0 |

ADR-077 §Context claims these files are "phantom modified" (the
result of `core.fileMode` / line-ending metadata drift) and
excludes them from the backfill. Inspection of the diff contents
shows this claim is **incorrect**: each file has substantive
additions tied to Phase 27 / Phase 29 design ADRs.

The error in ADR-077's audit methodology was a single `git ls-tree`
comparison at the blob level:

```
git ls-tree HEAD -- <path>
git ls-tree origin/main -- <path>
```

These return the **committed** blob SHA. `git ls-tree HEAD`
returns the SHA of the file as of HEAD (i.e. before the backfill
modifications were made to the working tree); `git ls-tree
origin/main` returns the SHA of the file as of `origin/main`. The
fact that the two blob SHAs are identical at HEAD merely confirms
that HEAD's committed version of the file matches `origin/main`;
it does **not** say anything about whether the working tree's
uncommitted modifications differ from HEAD.

A correct audit would have used `git diff -- <path>` against
HEAD (or, equivalently, `git diff --cached` if staged) to
inspect the uncommitted delta.

The actual uncommitted changes in these six files are summarized
below. All changes match the Phase 27 / Phase 29 design ADRs and
do **not** introduce new behaviour beyond what ADR-071 / ADR-074
already specify.

## Decision

### 1. Phase 27 / 29 follow-up backfill

The six remaining files are committed as three additional
backfill slices, each pinning one Phase:

1. `test(api-server): phase29 interceptor tenant-id coverage` —
   `interceptor_test.go` (+127 lines). Adds
   `TestInterceptorAttachesVerifiedTenantID` and three sibling
   cases covering the ADR-074 §3 ordering rule
   (metadata-declared tenant-id wins over membership-derived
   tenant-id).
2. `feat(api-server,postgres): phase29 mutation tenant-id
   persistence` — `job_validation_service.go` (+17 lines,
   `resolvedTenantIDForMutation`), `mutation_repository.go`
   (+7 / -3, INSERT now writes `tenant_id` and the UPDATE
   retains `tenant_id` on tombstone), `repository.go` (+15 /
   -2, embeds migration 003 and surfaces the tenant-id path in
   `migrations`), and `job_mutation_service_test.go` (+118 /
   -5, covers the new `tenant_id` redaction path).
3. `feat(controller): phase27 syncjob crd tenant-id label
   validation` — `sync.astrasync.io_syncjobs.yaml` (+4 lines,
   CEL validation rule requiring the
   `astrasync.io/tenant-id` label to be a canonical lowercase
   UUID).

Commits are appended to `phase13/kubernetes-production-hardening`
after the eight ADR-077 commits, preserving the linear history
and the per-slice bisect surface.

### 2. ADR-077 §Context is amended by reference

ADR-077 is not modified in place; this ADR (ADR-078) supersedes
the §Context claim that the six files are phantom-modified. The
`docs/adr/README.md` index gains an ADR-078 row marked Proposed.

The four "phantom modified" files in ADR-077 §Follow-ups that
were never real (`interceptor_test.go`, `job_mutation_service_test.go`,
`job_validation_service.go`, `mutation_repository.go`,
`repository.go`, `sync.astrasync.io_syncjobs.yaml`) are reduced
to zero after the three commits in §1 land.

### 3. Audit methodology correction

The corrected audit methodology, recorded here for future audits:

```bash
# 1. Compare committed blobs at HEAD vs origin/main.
git ls-tree HEAD -- <path>
git ls-tree origin/main -- <path>

# 2. Always also inspect uncommitted working-tree delta.
git diff -- <path>
git diff --shortstat -- <path>
```

If step 2 is skipped, files whose HEAD-vs-origin-main blobs match
will be misclassified as "phantom modified" when the working
tree holds real uncommitted diffs. This is the failure mode that
produced ADR-077 §Context.

### 4. CHANGELOG

A new "Unreleased / Added" entry is added: "Phase 27 / 29 audit
clarification (ADR-078). Three follow-up commits re-categorize
six files previously claimed to be phantom-modified in ADR-077
§Context: real diffs of +287 lines (+21 / -10) of Phase 27 CRD
validation and Phase 29 interceptor / mutation / persistence
work, all of which already conform to ADR-071 and ADR-074."

## Consequences

### Positive

- Branch tip is fully self-contained: a fresh clone reproduces
  the working tree behaviour without uncommitted modifications.
- ADR-077's audit error is recorded as a concrete
  methodological correction (the `git diff` step), preventing
  the same mistake in future audits.
- The three follow-up commits each pin a single Phase and a
  single concern, preserving the per-slice bisect surface.

### Negative

- The total audit PR now spans **eleven commits** rather than
  the eight recorded in ADR-077 §Decision.1. ADR-077's
  architectural intent (per-slice commits) is preserved; only
  the count changes.
- A future reader of ADR-077 §Context who does not also read
  ADR-078 may be confused by the §Context phantom-modified
  claim. The CHANGELOG note mitigates this.

## Follow-ups (out of scope for this ADR)

- **Normalize working-tree metadata**: if a future audit turns
  up file-mode / line-ending metadata drift that produces
  spurious diffs, normalize `.gitattributes` and re-clone.
- **CI gate for future audits**: add a CI job that runs
  `git diff --stat origin/main..HEAD` against expected file
  lists and rejects PRs whose untracked-file list exceeds a
  declared baseline. Out of scope for this ADR.
