package io.astrasync.benchmark;

import io.astrasync.connector.api.data.Row;
import io.astrasync.connector.api.data.RowBatch;
import io.astrasync.format.arrow.ArrowBatch;
import io.astrasync.format.arrow.ArrowBatchCodec;
import io.astrasync.format.arrow.ArrowIpcCodec;
import java.math.BigDecimal;
import java.time.LocalDateTime;
import java.util.ArrayList;
import java.util.LinkedHashMap;
import java.util.List;
import java.util.concurrent.TimeUnit;
import org.apache.arrow.memory.RootAllocator;
import org.apache.arrow.vector.BigIntVector;
import org.apache.arrow.vector.types.pojo.Schema;
import org.openjdk.jmh.annotations.Benchmark;
import org.openjdk.jmh.annotations.BenchmarkMode;
import org.openjdk.jmh.annotations.Fork;
import org.openjdk.jmh.annotations.Level;
import org.openjdk.jmh.annotations.Measurement;
import org.openjdk.jmh.annotations.Mode;
import org.openjdk.jmh.annotations.OutputTimeUnit;
import org.openjdk.jmh.annotations.Param;
import org.openjdk.jmh.annotations.Scope;
import org.openjdk.jmh.annotations.Setup;
import org.openjdk.jmh.annotations.State;
import org.openjdk.jmh.annotations.TearDown;
import org.openjdk.jmh.annotations.Warmup;

@BenchmarkMode(Mode.Throughput)
@OutputTimeUnit(TimeUnit.SECONDS)
@Warmup(iterations = 3, time = 1)
@Measurement(iterations = 5, time = 1)
@Fork(
        value = 2,
        jvmArgsAppend = {"--add-opens=java.base/java.nio=ALL-UNNAMED", "-Xms512m", "-Xmx512m"})
@State(Scope.Thread)
public class ArrowBatchBenchmark {
    private static final long ALLOCATION_LIMIT = 256L * 1024 * 1024;
    private static final long PAYLOAD_LIMIT = 256L * 1024 * 1024;

    @Param({"128", "2048"})
    private int rowCount;

    private RootAllocator allocator;
    private RowBatch rows;
    private Schema schema;
    private ArrowBatch arrow;
    private byte[] ipc;

    @Setup(Level.Trial)
    public void setUp() {
        allocator = new RootAllocator(512L * 1024 * 1024);
        rows = RowBatch.last(rows(rowCount));
        schema = ArrowBatchCodec.inferSchema(rows);
        arrow = ArrowBatchCodec.encode(allocator, ALLOCATION_LIMIT, schema, rows);
        ipc = ArrowIpcCodec.encode(arrow, PAYLOAD_LIMIT);
    }

    @TearDown(Level.Trial)
    public void tearDown() {
        arrow.close();
        allocator.close();
    }

    @Benchmark
    public long rowScan() {
        long checksum = 0;
        for (Row row : rows.rows()) {
            checksum += (Long) row.get("id");
        }
        return checksum;
    }

    @Benchmark
    public long arrowVectorScan() {
        BigIntVector ids = (BigIntVector) arrow.root().getVector("id");
        long checksum = 0;
        for (int index = 0; index < arrow.size(); index++) {
            checksum += ids.get(index);
        }
        return checksum;
    }

    @Benchmark
    public long rowToArrow() {
        try (ArrowBatch converted = ArrowBatchCodec.encode(allocator, ALLOCATION_LIMIT, schema, rows)) {
            return converted.allocatedBytes();
        }
    }

    @Benchmark
    public long ipcRoundTrip() {
        try (ArrowBatch decoded = ArrowIpcCodec.decode(allocator, ALLOCATION_LIMIT, PAYLOAD_LIMIT, ipc)) {
            return decoded.size() + decoded.allocatedBytes();
        }
    }

    private static List<Row> rows(int count) {
        List<Row> rows = new ArrayList<>(count);
        LocalDateTime base = LocalDateTime.of(2026, 1, 1, 0, 0);
        for (int index = 0; index < count; index++) {
            LinkedHashMap<String, Object> values = new LinkedHashMap<>();
            values.put("id", (long) index);
            values.put("name", "record-" + index);
            values.put("amount", BigDecimal.valueOf(index * 17L, 2));
            values.put("active", (index & 1) == 0);
            values.put("event_time", base.plusNanos(index));
            rows.add(Row.of(values));
        }
        return rows;
    }
}
