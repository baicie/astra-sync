package io.astrasync.engine.local;

import io.astrasync.connector.api.ConnectorRole;
import java.util.Objects;

/** Reports a connector creation failure that happened after compilation but before resource open. */
public final class JobMaterializationException extends IllegalArgumentException {
    private static final long serialVersionUID = 1L;

    private final ConnectorRole role;
    private final String connector;

    public JobMaterializationException(ConnectorRole role, String connector, String message, Throwable cause) {
        super(message, cause);
        this.role = Objects.requireNonNull(role, "role must not be null");
        this.connector = Objects.requireNonNull(connector, "connector must not be null");
    }

    public ConnectorRole role() {
        return role;
    }

    public String connector() {
        return connector;
    }
}
