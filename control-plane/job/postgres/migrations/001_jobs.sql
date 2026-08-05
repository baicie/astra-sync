CREATE TABLE IF NOT EXISTS astrasync_control_jobs (
    namespace VARCHAR(63) NOT NULL,
    name VARCHAR(63) NOT NULL,
    uid UUID NOT NULL UNIQUE,
    version BIGINT NOT NULL CHECK (version > 0),
    spec JSONB NOT NULL,
    status JSONB NOT NULL,
    created_at TIMESTAMPTZ NOT NULL,
    updated_at TIMESTAMPTZ NOT NULL,
    PRIMARY KEY (namespace, name)
);

CREATE INDEX IF NOT EXISTS astrasync_control_jobs_namespace_name_idx
    ON astrasync_control_jobs (namespace, name);
