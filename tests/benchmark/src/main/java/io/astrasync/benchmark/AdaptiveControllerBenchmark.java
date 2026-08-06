package io.astrasync.benchmark;

import io.astrasync.engine.runtime.AdaptiveBatchController;
import io.astrasync.engine.runtime.AdaptiveBatchPolicy;
import io.astrasync.engine.runtime.AdaptiveBatchSample;
import io.astrasync.engine.runtime.AdaptiveParallelismController;
import io.astrasync.engine.runtime.AdaptiveParallelismPolicy;
import io.astrasync.engine.runtime.AdaptiveParallelismSample;
import java.util.concurrent.TimeUnit;
import org.openjdk.jmh.annotations.Benchmark;
import org.openjdk.jmh.annotations.BenchmarkMode;
import org.openjdk.jmh.annotations.Fork;
import org.openjdk.jmh.annotations.Mode;
import org.openjdk.jmh.annotations.OutputTimeUnit;
import org.openjdk.jmh.annotations.Scope;
import org.openjdk.jmh.annotations.Setup;
import org.openjdk.jmh.annotations.State;

@BenchmarkMode(Mode.Throughput)
@OutputTimeUnit(TimeUnit.SECONDS)
@Fork(
        value = 1,
        jvmArgsAppend = {"-Xms256m", "-Xmx256m"})
@State(Scope.Thread)
public class AdaptiveControllerBenchmark {
    private AdaptiveBatchController batchController;
    private AdaptiveParallelismController parallelismController;

    @Setup
    public void setUp() {
        batchController = new AdaptiveBatchController(AdaptiveBatchPolicy.adaptive(16, 256, 1_000_000, 2), 4_096);
        parallelismController =
                new AdaptiveParallelismController(AdaptiveParallelismPolicy.adaptive(1, 4, 16, 5_000_000, 2), 16);
    }

    @Benchmark
    public int batchDecision() {
        batchController.observe(new AdaptiveBatchSample(256, 750_000, 0, 0, 8));
        return batchController.currentBatchRecords();
    }

    @Benchmark
    public int parallelismDecision() {
        parallelismController.observe(new AdaptiveParallelismSample(3_000_000, 8, 4));
        return parallelismController.currentParallelism();
    }
}
