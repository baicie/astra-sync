package io.astrasync.engine.checkpoint;

/** Signals that a persisted full-load run no longer matches its enumerated split plan. */
public final class SplitPlanMismatchException extends IllegalStateException {
    private static final long serialVersionUID = 1L;

    public SplitPlanMismatchException(String message) {
        super(message);
    }
}
