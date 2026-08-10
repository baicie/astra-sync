package io.astrasync.connector.api;

/** Declares whether a connector role accepts a tenant Connection reference. */
public enum ConnectionRequirement {
    NONE,
    OPTIONAL,
    REQUIRED
}
