package io.astrasync.benchmark;

import io.astrasync.connector.api.data.Row;
import io.astrasync.connector.api.data.RowBatch;
import io.astrasync.engine.runtime.BatchExchange;
import io.astrasync.engine.runtime.SpillPolicy;
import java.io.IOException;
import java.nio.file.Files;
import java.nio.file.Path;
import java.util.List;
import java.util.concurrent.TimeUnit;
import org.openjdk.jmh.annotations.Benchmark;
import org.openjdk.jmh.annotations.BenchmarkMode;
import org.openjdk.jmh.annotations.Fork;
import org.openjdk.jmh.annotations.Level;
import org.openjdk.jmh.annotations.Mode;
import org.openjdk.jmh.annotations.OutputTimeUnit;
import org.openjdk.jmh.annotations.Scope;
import org.openjdk.jmh.annotations.Setup;
import org.openjdk.jmh.annotations.State;
import org.openjdk.jmh.annotations.TearDown;

@BenchmarkMode(Mode.Throughput)
@OutputTimeUnit(TimeUnit.SECONDS)
@Fork(
        value = 1,
        jvmArgsAppend = {"-Xms256m", "-Xmx256m"})
@State(Scope.Thread)
public class SpillExchangeBenchmark {
    private Path spillRoot;
    private BatchExchange exchange;
    private RowBatch batch;

    @Setup(Level.Iteration)
    public void setUp() throws IOException {
        spillRoot = Files.createTempDirectory("astrasync-spill-benchmark-");
        exchange = new BatchExchange(8, new SpillPolicy(true, spillRoot, 16 * 1024 * 1024, 8));
        batch = RowBatch.data(List.of(Row.of("id", 1), Row.of("payload", "benchmark")));
    }

    @TearDown(Level.Iteration)
    public void tearDown() throws IOException {
        exchange.close();
        Files.deleteIfExists(spillRoot);
    }

    @Benchmark
    public RowBatch spillPublishReceive() {
        exchange.publish(batch);
        return exchange.receive();
    }
}
