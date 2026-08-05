package io.astrasync.connector.api.data;

/** Position of a CDC batch relative to the consistent snapshot handoff. */
public enum CdcPhase {
    SNAPSHOT,
    HANDOFF,
    STREAMING
}
