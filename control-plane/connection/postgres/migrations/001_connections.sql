CREATE TABLE IF NOT EXISTS astrasync_connections (
    tenant_id UUID NOT NULL REFERENCES astrasync_auth_tenants(tenant_id) ON DELETE RESTRICT,
    name VARCHAR(63) NOT NULL,
    uid UUID NOT NULL UNIQUE,
    connector VARCHAR(128) NOT NULL,
    version BIGINT NOT NULL CHECK (version > 0),
    current_generation BIGINT NOT NULL CHECK (current_generation > 0),
    state VARCHAR(16) NOT NULL CHECK (state IN ('DISABLED', 'ACTIVE')),
    display_name VARCHAR(256) NOT NULL,
    description VARCHAR(2048) NOT NULL,
    created_at TIMESTAMPTZ NOT NULL,
    updated_at TIMESTAMPTZ NOT NULL,
    PRIMARY KEY (tenant_id, name),
    UNIQUE (tenant_id, uid)
);

CREATE TABLE IF NOT EXISTS astrasync_connection_generations (
    connection_uid UUID NOT NULL REFERENCES astrasync_connections(uid) ON DELETE CASCADE,
    generation BIGINT NOT NULL CHECK (generation > 0),
    descriptor_revision VARCHAR(71) NOT NULL,
    connection_schema_revision VARCHAR(71) NOT NULL,
    settings JSONB NOT NULL,
    provider_kind VARCHAR(64),
    restricted_locator JSONB,
    created_at TIMESTAMPTZ NOT NULL,
    PRIMARY KEY (connection_uid, generation),
    CHECK ((provider_kind IS NULL) = (restricted_locator IS NULL))
);

CREATE TABLE IF NOT EXISTS astrasync_connection_list_revisions (
    tenant_id UUID PRIMARY KEY REFERENCES astrasync_auth_tenants(tenant_id) ON DELETE RESTRICT,
    revision BIGINT NOT NULL CHECK (revision > 0)
);

CREATE TABLE IF NOT EXISTS astrasync_job_connection_bindings (
    job_uid UUID NOT NULL REFERENCES astrasync_control_jobs(uid) ON DELETE CASCADE,
    tenant_id UUID NOT NULL REFERENCES astrasync_auth_tenants(tenant_id) ON DELETE RESTRICT,
    role VARCHAR(16) NOT NULL CHECK (role IN ('SOURCE', 'SINK')),
    connection_uid UUID NOT NULL REFERENCES astrasync_connections(uid) ON DELETE RESTRICT,
    connector VARCHAR(128) NOT NULL,
    created_at TIMESTAMPTZ NOT NULL,
    PRIMARY KEY (job_uid, role),
    UNIQUE (tenant_id, job_uid, role)
);

CREATE INDEX IF NOT EXISTS astrasync_job_connection_bindings_connection_idx
    ON astrasync_job_connection_bindings (connection_uid, job_uid, role);

CREATE TABLE IF NOT EXISTS astrasync_execution_connection_bindings (
    job_uid UUID NOT NULL REFERENCES astrasync_control_jobs(uid) ON DELETE RESTRICT,
    epoch BIGINT NOT NULL CHECK (epoch > 0),
    tenant_id UUID NOT NULL REFERENCES astrasync_auth_tenants(tenant_id) ON DELETE RESTRICT,
    role VARCHAR(16) NOT NULL CHECK (role IN ('SOURCE', 'SINK')),
    connection_uid UUID NOT NULL,
    generation BIGINT NOT NULL,
    descriptor_revision VARCHAR(71) NOT NULL,
    compiler_revision VARCHAR(71) NOT NULL,
    created_at TIMESTAMPTZ NOT NULL,
    PRIMARY KEY (job_uid, epoch, role),
    FOREIGN KEY (connection_uid, generation)
        REFERENCES astrasync_connection_generations(connection_uid, generation) ON DELETE RESTRICT
);

CREATE INDEX IF NOT EXISTS astrasync_execution_connection_bindings_connection_idx
    ON astrasync_execution_connection_bindings (connection_uid, generation, job_uid, epoch);

CREATE TABLE IF NOT EXISTS astrasync_connection_tests (
    operation_id UUID PRIMARY KEY,
    tenant_id UUID NOT NULL REFERENCES astrasync_auth_tenants(tenant_id) ON DELETE RESTRICT,
    connection_uid UUID NOT NULL,
    generation BIGINT NOT NULL,
    descriptor_revision VARCHAR(71) NOT NULL,
    state VARCHAR(32) NOT NULL CHECK (state IN ('QUEUED', 'RUNNING', 'SUCCEEDED', 'FAILED', 'TIMED_OUT', 'CANCELED', 'EXPIRED')),
    phase VARCHAR(32),
    result_code VARCHAR(64),
    success BOOLEAN NOT NULL DEFAULT FALSE,
    remediation_key VARCHAR(128) NOT NULL DEFAULT '',
    created_at TIMESTAMPTZ NOT NULL,
    started_at TIMESTAMPTZ,
    completed_at TIMESTAMPTZ,
    expires_at TIMESTAMPTZ NOT NULL,
    FOREIGN KEY (connection_uid, generation)
        REFERENCES astrasync_connection_generations(connection_uid, generation) ON DELETE RESTRICT
);

CREATE INDEX IF NOT EXISTS astrasync_connection_tests_tenant_time_idx
    ON astrasync_connection_tests (tenant_id, created_at DESC, operation_id);

CREATE TABLE IF NOT EXISTS astrasync_connection_materialization_receipts (
    tenant_id UUID NOT NULL REFERENCES astrasync_auth_tenants(tenant_id) ON DELETE RESTRICT,
    job_uid UUID NOT NULL,
    epoch BIGINT NOT NULL CHECK (epoch > 0),
    role VARCHAR(16) NOT NULL CHECK (role IN ('SOURCE', 'SINK')),
    connection_uid UUID NOT NULL,
    generation BIGINT NOT NULL,
    descriptor_revision VARCHAR(71) NOT NULL,
    provider_kind VARCHAR(64) NOT NULL,
    provider_object_uid VARCHAR(256) NOT NULL,
    provider_version_token VARCHAR(256) NOT NULL,
    generated_secret_name VARCHAR(253) NOT NULL,
    generated_secret_uid UUID NOT NULL,
    generated_resource_version VARCHAR(256) NOT NULL,
    created_at TIMESTAMPTZ NOT NULL,
    PRIMARY KEY (job_uid, epoch, role),
    FOREIGN KEY (connection_uid, generation)
        REFERENCES astrasync_connection_generations(connection_uid, generation) ON DELETE RESTRICT
);

ALTER TABLE astrasync_connection_materialization_receipts
    ADD COLUMN IF NOT EXISTS provider_object_uid VARCHAR(256);

UPDATE astrasync_connection_materialization_receipts
   SET provider_object_uid = provider_version_token
 WHERE provider_object_uid IS NULL;

ALTER TABLE astrasync_connection_materialization_receipts
    ALTER COLUMN provider_object_uid SET NOT NULL;

CREATE TABLE IF NOT EXISTS astrasync_connection_cleanup_obligations (
    obligation_id UUID PRIMARY KEY,
    tenant_id UUID NOT NULL REFERENCES astrasync_auth_tenants(tenant_id) ON DELETE RESTRICT,
    connection_uid UUID NOT NULL REFERENCES astrasync_connections(uid) ON DELETE RESTRICT,
    generation BIGINT NOT NULL,
    job_uid UUID NOT NULL,
    epoch BIGINT NOT NULL CHECK (epoch > 0),
    state VARCHAR(16) NOT NULL CHECK (state IN ('PENDING', 'COMPLETE')),
    created_at TIMESTAMPTZ NOT NULL,
    updated_at TIMESTAMPTZ NOT NULL
);

CREATE UNIQUE INDEX IF NOT EXISTS astrasync_connection_cleanup_execution_idx
    ON astrasync_connection_cleanup_obligations (connection_uid, job_uid, epoch);

CREATE TABLE IF NOT EXISTS astrasync_connection_idempotency (
    tenant_id UUID NOT NULL REFERENCES astrasync_auth_tenants(tenant_id) ON DELETE RESTRICT,
    actor_id VARCHAR(256) NOT NULL,
    method VARCHAR(256) NOT NULL,
    key_fingerprint VARCHAR(71) NOT NULL,
    request_digest VARCHAR(71) NOT NULL,
    status VARCHAR(16) NOT NULL CHECK (status IN ('IN_PROGRESS', 'COMPLETE')),
    result_kind VARCHAR(32),
    result_projection JSONB,
    audit_event_id VARCHAR(128),
    created_at TIMESTAMPTZ NOT NULL,
    completed_at TIMESTAMPTZ,
    expires_at TIMESTAMPTZ NOT NULL,
    PRIMARY KEY (tenant_id, actor_id, method, key_fingerprint)
);

CREATE INDEX IF NOT EXISTS astrasync_connection_idempotency_expiry_idx
    ON astrasync_connection_idempotency (expires_at);

CREATE TABLE IF NOT EXISTS astrasync_connection_tombstones (
    tenant_id UUID NOT NULL REFERENCES astrasync_auth_tenants(tenant_id) ON DELETE RESTRICT,
    name VARCHAR(63) NOT NULL,
    uid UUID NOT NULL,
    final_version BIGINT NOT NULL,
    final_generation BIGINT NOT NULL,
    deleted_at TIMESTAMPTZ NOT NULL,
    expires_at TIMESTAMPTZ NOT NULL,
    PRIMARY KEY (tenant_id, uid)
);

CREATE INDEX IF NOT EXISTS astrasync_connection_tombstones_expiry_idx
    ON astrasync_connection_tombstones (expires_at);

CREATE TABLE IF NOT EXISTS astrasync_security_audit_events (
    event_id VARCHAR(128) PRIMARY KEY,
    event_type VARCHAR(128) NOT NULL,
    actor_id VARCHAR(256) NOT NULL,
    tenant_id UUID,
    request_id VARCHAR(128) NOT NULL,
    outcome VARCHAR(32) NOT NULL,
    attributes JSONB NOT NULL,
    occurred_at TIMESTAMPTZ NOT NULL
);
