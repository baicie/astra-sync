-- Phase 29 — server-side tenant-id consumption (ADR-074).
--
-- Adds a nullable `tenant_id` column to `astrasync_control_jobs`. The column
-- is intentionally nullable so existing rows (which pre-date Phase 29) keep
-- their schema-compatible shape. A follow-up backfill migration (see
-- docs/phase29/README.md) will join against `astrasync_auth_tenants` to
-- populate existing rows and then tighten the constraint to NOT NULL.
--
-- Forward-compatible write path:
--   * MutationRepository writes `mutation.TenantID` into this column on
--     Create, Update, and tombstone rows (see ADR-074 §5).
--   * Direct `Repository.Update` callers (lifecycle controller status
--     transitions) will continue to leave the column NULL until the
--     backfill lands; this is acceptable because the column is nullable.
--
-- The column is keyed to a UUID domain. No CHECK constraint is added in
-- this migration so existing NULL rows satisfy the schema unchanged.
ALTER TABLE astrasync_control_jobs
    ADD COLUMN IF NOT EXISTS tenant_id UUID;

CREATE INDEX IF NOT EXISTS astrasync_control_jobs_tenant_id_idx
    ON astrasync_control_jobs (tenant_id);

-- The list path is scoped by namespace today; this index is a forward-
-- looking accelerator for queries that join against astrasync_auth_tenants
-- during the backfill and for future tenant-scoped job list endpoints.
-- Cardinality is bounded by the tenant count and is safe to keep.