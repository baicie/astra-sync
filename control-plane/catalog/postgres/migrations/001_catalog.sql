CREATE TABLE IF NOT EXISTS astrasync_connector_descriptor_snapshots (
    descriptor_revision VARCHAR(71) PRIMARY KEY,
    connector_name VARCHAR(128) NOT NULL,
    artifact_version VARCHAR(128) NOT NULL,
    payload BYTEA NOT NULL,
    created_at TIMESTAMPTZ NOT NULL
);

CREATE UNIQUE INDEX IF NOT EXISTS astrasync_connector_name_version_idx
    ON astrasync_connector_descriptor_snapshots (connector_name, artifact_version);

CREATE TABLE IF NOT EXISTS astrasync_connector_inventories (
    inventory_revision VARCHAR(71) PRIMARY KEY,
    compiler_revision VARCHAR(71) NOT NULL,
    execution_profile VARCHAR(128) NOT NULL,
    payload BYTEA NOT NULL,
    activated_at TIMESTAMPTZ NOT NULL
);

CREATE TABLE IF NOT EXISTS astrasync_connector_inventory_descriptors (
    inventory_revision VARCHAR(71) NOT NULL REFERENCES astrasync_connector_inventories(inventory_revision),
    position INTEGER NOT NULL CHECK (position >= 0),
    descriptor_revision VARCHAR(71) NOT NULL REFERENCES astrasync_connector_descriptor_snapshots(descriptor_revision),
    PRIMARY KEY (inventory_revision, position),
    UNIQUE (inventory_revision, descriptor_revision)
);

CREATE TABLE IF NOT EXISTS astrasync_connector_inventory_activation (
    execution_profile VARCHAR(128) PRIMARY KEY,
    inventory_revision VARCHAR(71) NOT NULL REFERENCES astrasync_connector_inventories(inventory_revision),
    activated_at TIMESTAMPTZ NOT NULL
);

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
