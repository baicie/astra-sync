CREATE TABLE IF NOT EXISTS astrasync_auth_principals (
    principal_id UUID PRIMARY KEY,
    issuer VARCHAR(2048) NOT NULL,
    subject VARCHAR(512) NOT NULL,
    display_name VARCHAR(256) NOT NULL DEFAULT '',
    status VARCHAR(16) NOT NULL CHECK (status IN ('ACTIVE', 'DISABLED')),
    created_at TIMESTAMPTZ NOT NULL,
    updated_at TIMESTAMPTZ NOT NULL,
    UNIQUE (issuer, subject)
);

CREATE TABLE IF NOT EXISTS astrasync_auth_tenants (
    tenant_id UUID PRIMARY KEY,
    namespace VARCHAR(63) NOT NULL UNIQUE,
    display_name VARCHAR(256) NOT NULL DEFAULT '',
    status VARCHAR(16) NOT NULL CHECK (status IN ('ACTIVE', 'SUSPENDED')),
    authz_revision BIGINT NOT NULL CHECK (authz_revision > 0),
    created_at TIMESTAMPTZ NOT NULL,
    updated_at TIMESTAMPTZ NOT NULL
);

CREATE TABLE IF NOT EXISTS astrasync_auth_memberships (
    tenant_id UUID NOT NULL REFERENCES astrasync_auth_tenants(tenant_id) ON DELETE RESTRICT,
    principal_id UUID NOT NULL REFERENCES astrasync_auth_principals(principal_id) ON DELETE RESTRICT,
    role_id VARCHAR(64) NOT NULL CHECK (role_id IN ('tenant_viewer', 'tenant_operator', 'tenant_auditor', 'tenant_admin')),
    status VARCHAR(16) NOT NULL CHECK (status IN ('ACTIVE', 'DISABLED')),
    granted_by VARCHAR(256) NOT NULL,
    granted_at TIMESTAMPTZ NOT NULL,
    updated_at TIMESTAMPTZ NOT NULL,
    PRIMARY KEY (tenant_id, principal_id)
);

CREATE TABLE IF NOT EXISTS astrasync_auth_platform_roles (
    principal_id UUID NOT NULL REFERENCES astrasync_auth_principals(principal_id) ON DELETE RESTRICT,
    role_id VARCHAR(64) NOT NULL CHECK (role_id = 'platform_admin'),
    status VARCHAR(16) NOT NULL CHECK (status IN ('ACTIVE', 'DISABLED')),
    granted_by VARCHAR(256) NOT NULL,
    granted_at TIMESTAMPTZ NOT NULL,
    PRIMARY KEY (principal_id, role_id)
);

CREATE TABLE IF NOT EXISTS astrasync_auth_sessions (
    session_hash BYTEA PRIMARY KEY,
    principal_id UUID NOT NULL REFERENCES astrasync_auth_principals(principal_id) ON DELETE RESTRICT,
    encrypted_tokens BYTEA NOT NULL,
    csrf_hash BYTEA NOT NULL,
    revision BIGINT NOT NULL CHECK (revision > 0),
    idle_expires_at TIMESTAMPTZ NOT NULL,
    absolute_expires_at TIMESTAMPTZ NOT NULL,
    revoked_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL,
    updated_at TIMESTAMPTZ NOT NULL
);

CREATE INDEX IF NOT EXISTS astrasync_auth_memberships_principal_idx
    ON astrasync_auth_memberships (principal_id, tenant_id);

CREATE INDEX IF NOT EXISTS astrasync_auth_sessions_principal_idx
    ON astrasync_auth_sessions (principal_id, absolute_expires_at);

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

CREATE INDEX IF NOT EXISTS astrasync_security_audit_tenant_time_idx
    ON astrasync_security_audit_events (tenant_id, occurred_at DESC, event_id);
