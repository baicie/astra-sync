package io.astrasync.connector.api.data;

import java.util.List;
import java.util.Objects;

/** An immutable batch of rows that may also mark the end of input. */
public final class RowBatch {
    private static final RowBatch END = new RowBatch(List.of(), true);

    private final List<Row> rows;
    private final boolean endOfInput;

    private RowBatch(List<Row> rows, boolean endOfInput) {
        Objects.requireNonNull(rows, "rows must not be null");
        this.rows = rows.stream()
                .map(row -> Objects.requireNonNull(row, "rows must not contain null"))
                .toList();
        if (this.rows.isEmpty() && !endOfInput) {
            throw new IllegalArgumentException("an empty batch must end the input");
        }
        this.endOfInput = endOfInput;
    }

    public static RowBatch data(List<Row> rows) {
        return new RowBatch(rows, false);
    }

    public static RowBatch last(List<Row> rows) {
        Objects.requireNonNull(rows, "rows must not be null");
        return rows.isEmpty() ? END : new RowBatch(rows, true);
    }

    public static RowBatch end() {
        return END;
    }

    public List<Row> rows() {
        return rows;
    }

    public int size() {
        return rows.size();
    }

    public boolean endOfInput() {
        return endOfInput;
    }

    @Override
    public boolean equals(Object other) {
        return this == other
                || other instanceof RowBatch batch && endOfInput == batch.endOfInput && rows.equals(batch.rows);
    }

    @Override
    public int hashCode() {
        return Objects.hash(rows, endOfInput);
    }

    @Override
    public String toString() {
        return "RowBatch{rows=" + rows + ", endOfInput=" + endOfInput + '}';
    }
}
