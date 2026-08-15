CREATE INDEX IF NOT EXISTS astrasync_security_audit_tenant_keyset_idx
    ON astrasync_security_audit_events (tenant_id, occurred_at DESC, event_id DESC)
    WHERE tenant_id IS NOT NULL;
