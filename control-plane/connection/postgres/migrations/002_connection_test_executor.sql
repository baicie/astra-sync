ALTER TABLE astrasync_connection_tests
    ADD COLUMN IF NOT EXISTS executor_id VARCHAR(128),
    ADD COLUMN IF NOT EXISTS lease_expires_at TIMESTAMPTZ,
    ADD COLUMN IF NOT EXISTS attempt INTEGER NOT NULL DEFAULT 0 CHECK (attempt >= 0),
    ADD COLUMN IF NOT EXISTS actor_id VARCHAR(256),
    ADD COLUMN IF NOT EXISTS egress_policy_revision VARCHAR(71),
    ADD COLUMN IF NOT EXISTS allowed_cidrs JSONB,
    ADD COLUMN IF NOT EXISTS deadline_at TIMESTAMPTZ;

UPDATE astrasync_connection_tests
   SET actor_id = COALESCE(actor_id, 'legacy'),
       egress_policy_revision = COALESCE(
           egress_policy_revision,
           'sha256:4ef29d4c72587541d7b6dd7556b595153b4f5f144f77c37ca66517f4a600e91a'),
       allowed_cidrs = COALESCE(allowed_cidrs, '[]'::jsonb),
       deadline_at = COALESCE(deadline_at, created_at + INTERVAL '30 seconds')
 WHERE actor_id IS NULL OR egress_policy_revision IS NULL
    OR allowed_cidrs IS NULL OR deadline_at IS NULL;

ALTER TABLE astrasync_connection_tests
    ALTER COLUMN actor_id SET NOT NULL,
    ALTER COLUMN egress_policy_revision SET NOT NULL,
    ALTER COLUMN allowed_cidrs SET NOT NULL,
    ALTER COLUMN deadline_at SET NOT NULL;

CREATE INDEX IF NOT EXISTS astrasync_connection_tests_claim_v2_idx
    ON astrasync_connection_tests (state, deadline_at, lease_expires_at, created_at, operation_id)
    WHERE state IN ('QUEUED', 'RUNNING');

CREATE INDEX IF NOT EXISTS astrasync_connection_tests_admission_idx
    ON astrasync_connection_tests (tenant_id, actor_id, created_at DESC, state);
