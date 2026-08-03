package io.astrasync.engine.jobspec;

import java.util.Objects;

public final class JobSpecParseException extends IllegalArgumentException {
    private static final long serialVersionUID = 1L;

    private final String path;

    public JobSpecParseException(String path, String message) {
        this(path, message, null);
    }

    public JobSpecParseException(String path, String message, Throwable cause) {
        super(Objects.requireNonNull(path, "path must not be null") + ": " + message, cause);
        this.path = path;
    }

    public String path() {
        return path;
    }
}
