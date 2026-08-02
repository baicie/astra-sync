package io.astrasync.engine.plan;

import java.util.Objects;

public final class JobCompilationException extends IllegalArgumentException {
    private static final long serialVersionUID = 1L;

    private final CompilationErrorCode code;

    public JobCompilationException(CompilationErrorCode code, String message) {
        super(message);
        this.code = Objects.requireNonNull(code, "code must not be null");
    }

    public CompilationErrorCode code() {
        return code;
    }
}
