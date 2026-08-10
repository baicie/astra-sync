CREATE TABLE IF NOT EXISTS astrasync_job_idempotency (
    tenant_id UUID NOT NULL REFERENCES astrasync_auth_tenants(tenant_id) ON DELETE RESTRICT,
    actor_id VARCHAR(256) NOT NULL,
    method VARCHAR(256) NOT NULL,
    key_fingerprint VARCHAR(71) NOT NULL,
    request_digest VARCHAR(71) NOT NULL,
    status VARCHAR(16) NOT NULL CHECK (status IN ('IN_PROGRESS', 'COMPLETE')),
    result_kind VARCHAR(16),
    result_job_uid UUID,
    result_name VARCHAR(63),
    result_version BIGINT,
    result_outcome VARCHAR(16),
    audit_event_id VARCHAR(128),
    created_at TIMESTAMPTZ NOT NULL,
    completed_at TIMESTAMPTZ,
    expires_at TIMESTAMPTZ NOT NULL,
    PRIMARY KEY (tenant_id, actor_id, method, key_fingerprint)
);

CREATE INDEX IF NOT EXISTS astrasync_job_idempotency_expiry_idx
    ON astrasync_job_idempotency (expires_at);

CREATE TABLE IF NOT EXISTS astrasync_job_tombstones (
    tenant_id UUID NOT NULL REFERENCES astrasync_auth_tenants(tenant_id) ON DELETE RESTRICT,
    namespace VARCHAR(63) NOT NULL,
    name VARCHAR(63) NOT NULL,
    uid UUID NOT NULL,
    final_version BIGINT NOT NULL CHECK (final_version > 0),
    deleted_at TIMESTAMPTZ NOT NULL,
    expires_at TIMESTAMPTZ NOT NULL,
    PRIMARY KEY (tenant_id, uid)
);

CREATE INDEX IF NOT EXISTS astrasync_job_tombstones_expiry_idx
    ON astrasync_job_tombstones (expires_at);

-- Execution bindings are retained independently of a deleted public Job row.
-- The immutable Job UID remains the historical correlation key.
ALTER TABLE astrasync_execution_connection_bindings
    DROP CONSTRAINT IF EXISTS astrasync_execution_connection_bindings_job_uid_fkey;
