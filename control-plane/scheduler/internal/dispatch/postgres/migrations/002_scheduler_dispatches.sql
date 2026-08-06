CREATE TABLE IF NOT EXISTS astrasync_scheduler_dispatches (
    job_uid UUID NOT NULL REFERENCES astrasync_control_jobs(uid) ON DELETE CASCADE,
    execution_epoch BIGINT NOT NULL CHECK (execution_epoch > 0),
    namespace VARCHAR(63) NOT NULL,
    name VARCHAR(63) NOT NULL,
    owner_id VARCHAR(255) NOT NULL,
    phase VARCHAR(16) NOT NULL CHECK (
        phase IN ('CLAIMED', 'STARTING', 'RUNNING', 'STOPPING', 'SUCCEEDED', 'FAILED', 'CANCELED')
    ),
    lease_expires_at TIMESTAMPTZ NOT NULL,
    last_heartbeat_at TIMESTAMPTZ,
    heartbeat_token UUID NOT NULL DEFAULT gen_random_uuid(),
    attempt INTEGER NOT NULL CHECK (attempt > 0),
    last_error TEXT NOT NULL DEFAULT '',
    created_at TIMESTAMPTZ NOT NULL,
    updated_at TIMESTAMPTZ NOT NULL,
    PRIMARY KEY (job_uid, execution_epoch),
    UNIQUE (namespace, name, execution_epoch),
    FOREIGN KEY (namespace, name) REFERENCES astrasync_control_jobs(namespace, name) ON DELETE CASCADE
);

ALTER TABLE astrasync_scheduler_dispatches
    ADD COLUMN IF NOT EXISTS last_heartbeat_at TIMESTAMPTZ;

ALTER TABLE astrasync_scheduler_dispatches
    ADD COLUMN IF NOT EXISTS heartbeat_token UUID;

ALTER TABLE astrasync_scheduler_dispatches
    ALTER COLUMN heartbeat_token SET DEFAULT gen_random_uuid();

UPDATE astrasync_scheduler_dispatches
   SET heartbeat_token = gen_random_uuid()
 WHERE heartbeat_token IS NULL;

ALTER TABLE astrasync_scheduler_dispatches
    ALTER COLUMN heartbeat_token SET NOT NULL;

UPDATE astrasync_scheduler_dispatches
   SET last_heartbeat_at = updated_at
 WHERE last_heartbeat_at IS NULL
   AND phase IN ('CLAIMED', 'STARTING', 'RUNNING', 'STOPPING');

CREATE INDEX IF NOT EXISTS astrasync_scheduler_dispatches_active_lease_idx
    ON astrasync_scheduler_dispatches (lease_expires_at, updated_at)
    WHERE phase IN ('CLAIMED', 'STARTING', 'RUNNING', 'STOPPING');

CREATE INDEX IF NOT EXISTS astrasync_scheduler_dispatches_active_heartbeat_idx
    ON astrasync_scheduler_dispatches (last_heartbeat_at)
    WHERE phase IN ('CLAIMED', 'STARTING', 'RUNNING', 'STOPPING')
      AND last_heartbeat_at IS NOT NULL;
