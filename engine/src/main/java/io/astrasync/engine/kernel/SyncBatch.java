package io.astrasync.engine.kernel;

import java.util.List;
import java.util.Objects;

public final class SyncBatch {
    private static final SyncBatch END = new SyncBatch(List.of(), true);

    private final List<SyncRecord> records;
    private final boolean endOfInput;

    private SyncBatch(List<SyncRecord> records, boolean endOfInput) {
        this.records = List.copyOf(Objects.requireNonNull(records, "records must not be null"));
        if (this.records.isEmpty() && !endOfInput) {
            throw new IllegalArgumentException("an empty batch must end the input");
        }
        this.endOfInput = endOfInput;
    }

    public static SyncBatch data(List<SyncRecord> records) {
        return new SyncBatch(records, false);
    }

    public static SyncBatch last(List<SyncRecord> records) {
        return records.isEmpty() ? END : new SyncBatch(records, true);
    }

    public static SyncBatch end() {
        return END;
    }

    public List<SyncRecord> records() {
        return records;
    }

    public int size() {
        return records.size();
    }

    public boolean endOfInput() {
        return endOfInput;
    }
}
