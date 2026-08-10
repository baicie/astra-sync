CREATE TABLE IF NOT EXISTS astrasync_auth_login_transactions (
    state_hash BYTEA PRIMARY KEY CHECK (octet_length(state_hash) = 32),
    browser_hash BYTEA NOT NULL CHECK (octet_length(browser_hash) = 32),
    encrypted_payload BYTEA NOT NULL,
    expires_at TIMESTAMPTZ NOT NULL,
    created_at TIMESTAMPTZ NOT NULL
);

CREATE INDEX IF NOT EXISTS astrasync_auth_login_transactions_expiry_idx
    ON astrasync_auth_login_transactions (expires_at);
