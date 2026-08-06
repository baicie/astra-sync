package io.astrasync.engine.kernel;

import static org.assertj.core.api.Assertions.assertThat;
import static org.assertj.core.api.Assertions.assertThatThrownBy;

import io.astrasync.connector.api.data.Row;
import io.astrasync.connector.api.data.RowBatch;
import io.astrasync.connector.api.sink.BatchSink;
import io.astrasync.connector.api.source.BatchSource;
import io.astrasync.engine.runtime.AdaptiveBatchPolicy;
import java.util.ArrayList;
import java.util.List;
import java.util.concurrent.atomic.AtomicInteger;
import java.util.function.Consumer;
import java.util.function.IntFunction;
import org.junit.jupiter.api.Test;

class SingleNodeSyncJobTest {
    @Test
    void cancellationBeforeOpenLeavesBothResourcesUntouched() {
        LifecycleSource source = new LifecycleSource(RowBatch.end());
        LifecycleSink sink = new LifecycleSink();

        assertThatThrownBy(() -> SingleNodeSyncJob.builder()
                        .source(source)
                        .sink(sink)
                        .cancellationToken(() -> true)
                        .build()
                        .run())
                .isInstanceOfSatisfying(SyncJobException.class, exception -> {
                    assertThat(exception.stage()).isEqualTo(SyncStage.CANCELLED);
                    assertThat(exception.partialResult().readCount()).isZero();
                    assertThat(exception.partialResult().writtenCount()).isZero();
                });
        assertThat(source.openCount).isZero();
        assertThat(source.closeCount).isZero();
        assertThat(sink.openCount).isZero();
        assertThat(sink.closeCount).isZero();
    }

    @Test
    void cancellationAfterSourceOpenClosesOnlyTheOpenedSource() {
        LifecycleSource source = new LifecycleSource(RowBatch.end());
        LifecycleSink sink = new LifecycleSink();
        AtomicInteger checks = new AtomicInteger();

        assertThatThrownBy(() -> SingleNodeSyncJob.builder()
                        .source(source)
                        .sink(sink)
                        .cancellationToken(() -> checks.incrementAndGet() >= 2)
                        .build()
                        .run())
                .isInstanceOfSatisfying(SyncJobException.class, exception -> assertThat(exception.stage())
                        .isEqualTo(SyncStage.CANCELLED));
        assertThat(source.openCount).isEqualTo(1);
        assertThat(source.closeCount).isEqualTo(1);
        assertThat(sink.openCount).isZero();
        assertThat(sink.closeCount).isZero();
    }

    @Test
    void cancellationBeforeWriteReportsPartialReadAndClosesInReverseOrder() {
        LifecycleSource source = new LifecycleSource(RowBatch.last(List.of(Row.of("id", 1), Row.of("id", 2))));
        LifecycleSink sink = new LifecycleSink();
        AtomicInteger checks = new AtomicInteger();

        assertThatThrownBy(() -> SingleNodeSyncJob.builder()
                        .source(source)
                        .sink(sink)
                        .cancellationToken(() -> checks.incrementAndGet() >= 4)
                        .build()
                        .run())
                .isInstanceOfSatisfying(SyncJobException.class, exception -> {
                    assertThat(exception.stage()).isEqualTo(SyncStage.CANCELLED);
                    assertThat(exception.partialResult().readCount()).isEqualTo(2);
                    assertThat(exception.partialResult().writtenCount()).isZero();
                });
        assertThat(source.closeCount).isEqualTo(1);
        assertThat(sink.closeCount).isEqualTo(1);
    }

    @Test
    void cancellationCheckFailurePreservesPartialMetricsAndCloseFailures() {
        BatchSource source = new BatchSource() {
            @Override
            public void open() {}

            @Override
            public RowBatch readBatch(int maxRows) {
                return RowBatch.last(List.of(Row.of("id", 1), Row.of("id", 2)));
            }

            @Override
            public void close() {
                throw new IllegalStateException("source close");
            }
        };
        BatchSink sink = new BatchSink() {
            @Override
            public void open() {}

            @Override
            public void writeBatch(RowBatch batch) {
                throw new AssertionError("sink must not receive a batch after the token fails");
            }

            @Override
            public void close() {
                throw new IllegalStateException("sink close");
            }
        };
        AtomicInteger checks = new AtomicInteger();

        assertThatThrownBy(() -> SingleNodeSyncJob.builder()
                        .source(source)
                        .sink(sink)
                        .cancellationToken(() -> {
                            if (checks.incrementAndGet() == 4) {
                                throw new IllegalStateException("token boom");
                            }
                            return false;
                        })
                        .build()
                        .run())
                .isInstanceOfSatisfying(SyncJobException.class, exception -> {
                    assertThat(exception.stage()).isEqualTo(SyncStage.CANCELLATION_CHECK);
                    assertThat(exception.getCause()).hasMessage("token boom");
                    assertThat(exception.partialResult().readCount()).isEqualTo(2);
                    assertThat(exception.partialResult().writtenCount()).isZero();
                    assertThat(exception.getSuppressed())
                            .extracting(Throwable::getMessage)
                            .containsExactly("sink close", "source close");
                });
    }

    @Test
    void runsMultipleBoundedBatchesThroughTransformsInOrder() {
        List<Row> written = new ArrayList<>();
        List<Integer> writtenBatchSizes = new ArrayList<>();
        List<Boolean> writtenEndFlags = new ArrayList<>();
        List<Integer> requestedLimits = new ArrayList<>();
        AtomicInteger poll = new AtomicInteger();
        BatchSource source = batchSource(maxRecords -> {
            requestedLimits.add(maxRecords);
            return switch (poll.getAndIncrement()) {
                case 0 -> RowBatch.data(List.of(Row.of("name", "ada"), Row.of("name", "grace")));
                case 1 -> RowBatch.last(List.of(Row.of("name", "linus")));
                default -> throw new AssertionError("source polled after end of input");
            };
        });

        SyncResult result = SingleNodeSyncJob.builder()
                .source(source)
                .transform(record ->
                        record.with("name", record.get("name").toString().toUpperCase()))
                .transform(record -> record.with("name", "[" + record.get("name") + "]"))
                .sink(batchSink(batch -> {
                    writtenBatchSizes.add(batch.size());
                    writtenEndFlags.add(batch.endOfInput());
                    written.addAll(batch.rows());
                }))
                .maxBatchRecords(2)
                .build()
                .run();

        assertThat(requestedLimits).containsExactly(2, 2);
        assertThat(writtenBatchSizes).containsExactly(2, 1);
        assertThat(writtenEndFlags).containsExactly(false, true);
        assertThat(written).extracting(record -> record.get("name")).containsExactly("[ADA]", "[GRACE]", "[LINUS]");
        assertThat(result.readCount()).isEqualTo(3);
        assertThat(result.writtenCount()).isEqualTo(3);
        assertThat(result.batchCount()).isEqualTo(2);
        assertThat(result.maxObservedBatchSize()).isEqualTo(2);
        assertThat(result.elapsedNanos()).isNotNegative();
    }

    @Test
    void adaptsTheReadLimitAfterACompletedBatch() {
        List<Integer> requestedLimits = new ArrayList<>();
        AtomicInteger poll = new AtomicInteger();
        BatchSource source = batchSource(maxRecords -> {
            requestedLimits.add(maxRecords);
            return poll.getAndIncrement() == 0
                    ? RowBatch.data(List.of(Row.of("id", 1)))
                    : RowBatch.last(List.of(Row.of("id", 2)));
        });

        SingleNodeSyncJob.builder()
                .source(source)
                .sink(batchSink(ignored -> {}))
                .maxBatchRecords(4)
                .adaptiveBatchPolicy(AdaptiveBatchPolicy.adaptive(1, 2, 1_000_000_000L, 0))
                .build()
                .run();

        assertThat(requestedLimits).containsExactly(2, 4);
    }

    @Test
    void doesNotPollNextBatchUntilCurrentBatchIsWritten() {
        List<Row> written = new ArrayList<>();
        AtomicInteger poll = new AtomicInteger();
        BatchSource source = batchSource(maxRecords -> switch (poll.getAndIncrement()) {
            case 0 -> {
                assertThat(written).isEmpty();
                yield RowBatch.data(List.of(Row.of("id", 1), Row.of("id", 2)));
            }
            case 1 -> {
                assertThat(written).hasSize(2);
                yield RowBatch.last(List.of(Row.of("id", 3)));
            }
            default -> throw new AssertionError("source polled after end of input");
        });

        SingleNodeSyncJob.builder()
                .source(source)
                .sink(batchSink(batch -> written.addAll(batch.rows())))
                .maxBatchRecords(2)
                .build()
                .run();

        assertThat(written).extracting(record -> record.get("id")).containsExactly(1, 2, 3);
    }

    @Test
    void rejectsABatchThatExceedsTheRequestedLimit() {
        List<Row> written = new ArrayList<>();
        SingleNodeSyncJob job = SingleNodeSyncJob.builder()
                .source(batchSource(limit -> RowBatch.last(List.of(Row.of("id", 1), Row.of("id", 2)))))
                .sink(batchSink(batch -> written.addAll(batch.rows())))
                .maxBatchRecords(1)
                .build();

        assertThatThrownBy(job::run).isInstanceOfSatisfying(SyncJobException.class, exception -> {
            assertThat(exception.stage()).isEqualTo(SyncStage.SOURCE_READ);
            assertThat(exception.partialResult().readCount()).isZero();
            assertThat(exception.partialResult().writtenCount()).isZero();
            assertThat(exception.getCause()).hasMessage("source returned 2 records, limit is 1");
        });
        assertThat(written).isEmpty();
    }

    @Test
    void reportsPartialMetricsAndClosesResourcesAfterTransformFailure() {
        LifecycleSource source = new LifecycleSource(RowBatch.last(List.of(Row.of("id", 1))));
        LifecycleSink sink = new LifecycleSink();
        SingleNodeSyncJob job = SingleNodeSyncJob.builder()
                .source(source)
                .transform(record -> {
                    throw new IllegalArgumentException("bad record");
                })
                .sink(sink)
                .build();

        assertThatThrownBy(job::run).isInstanceOfSatisfying(SyncJobException.class, exception -> {
            assertThat(exception.stage()).isEqualTo(SyncStage.TRANSFORM);
            assertThat(exception.partialResult().readCount()).isEqualTo(1);
            assertThat(exception.partialResult().writtenCount()).isZero();
            assertThat(exception.getCause()).hasMessage("bad record");
        });
        assertThat(source.openCount).isEqualTo(1);
        assertThat(source.closeCount).isEqualTo(1);
        assertThat(sink.openCount).isEqualTo(1);
        assertThat(sink.closeCount).isEqualTo(1);
    }

    @Test
    void reportsSourceReadFailureAfterCompletedWork() {
        List<Row> written = new ArrayList<>();
        AtomicInteger poll = new AtomicInteger();
        BatchSource source = batchSource(maxRecords -> {
            if (poll.getAndIncrement() == 0) {
                return RowBatch.data(List.of(Row.of("id", 1)));
            }
            throw new IllegalStateException("source read");
        });

        assertThatThrownBy(() -> SingleNodeSyncJob.builder()
                        .source(source)
                        .sink(batchSink(batch -> written.addAll(batch.rows())))
                        .build()
                        .run())
                .isInstanceOfSatisfying(SyncJobException.class, exception -> {
                    assertThat(exception.stage()).isEqualTo(SyncStage.SOURCE_READ);
                    assertThat(exception.partialResult().readCount()).isEqualTo(1);
                    assertThat(exception.partialResult().writtenCount()).isEqualTo(1);
                    assertThat(exception.partialResult().batchCount()).isEqualTo(1);
                    assertThat(exception.getCause()).hasMessage("source read");
                });
        assertThat(written).hasSize(1);
    }

    @Test
    void closesTheSourceWhenSinkOpenFails() {
        LifecycleSource source = new LifecycleSource(RowBatch.end());
        LifecycleSink sink = new LifecycleSink();
        sink.openFailure = new IllegalStateException("sink open");

        assertThatThrownBy(() -> SingleNodeSyncJob.builder()
                        .source(source)
                        .sink(sink)
                        .build()
                        .run())
                .isInstanceOfSatisfying(SyncJobException.class, exception -> {
                    assertThat(exception.stage()).isEqualTo(SyncStage.SINK_OPEN);
                    assertThat(exception.partialResult().readCount()).isZero();
                    assertThat(exception.getCause()).hasMessage("sink open");
                });
        assertThat(source.openCount).isEqualTo(1);
        assertThat(source.closeCount).isEqualTo(1);
        assertThat(sink.openCount).isEqualTo(1);
        assertThat(sink.closeCount).isZero();
    }

    @Test
    void preservesPrimaryFailureAndSuppressesAllCloseFailures() {
        BatchSource source = new BatchSource() {
            @Override
            public void open() {}

            @Override
            public RowBatch readBatch(int maxRecords) {
                return RowBatch.last(List.of(Row.of("id", 1)));
            }

            @Override
            public void close() {
                throw new IllegalStateException("source close");
            }
        };
        BatchSink sink = new BatchSink() {
            @Override
            public void open() {}

            @Override
            public void writeBatch(RowBatch batch) {
                throw new IllegalArgumentException("sink write");
            }

            @Override
            public void close() {
                throw new IllegalStateException("sink close");
            }
        };

        assertThatThrownBy(() -> SingleNodeSyncJob.builder()
                        .source(source)
                        .sink(sink)
                        .build()
                        .run())
                .isInstanceOfSatisfying(SyncJobException.class, exception -> {
                    assertThat(exception.stage()).isEqualTo(SyncStage.SINK_WRITE);
                    assertThat(exception.getCause()).hasMessage("sink write");
                    assertThat(exception.getSuppressed())
                            .extracting(Throwable::getMessage)
                            .containsExactly("sink close", "source close");
                });
    }

    @Test
    void reportsTheFirstCloseFailureAndSuppressesTheSecond() {
        BatchSource source = new BatchSource() {
            @Override
            public void open() {}

            @Override
            public RowBatch readBatch(int maxRecords) {
                return RowBatch.last(List.of(Row.of("id", 1)));
            }

            @Override
            public void close() {
                throw new IllegalStateException("source close");
            }
        };
        BatchSink sink = new BatchSink() {
            @Override
            public void open() {}

            @Override
            public void writeBatch(RowBatch batch) {}

            @Override
            public void close() {
                throw new IllegalStateException("sink close");
            }
        };

        assertThatThrownBy(() -> SingleNodeSyncJob.builder()
                        .source(source)
                        .sink(sink)
                        .build()
                        .run())
                .isInstanceOfSatisfying(SyncJobException.class, exception -> {
                    assertThat(exception.stage()).isEqualTo(SyncStage.CLOSE);
                    assertThat(exception.partialResult().writtenCount()).isEqualTo(1);
                    assertThat(exception.getCause()).hasMessage("sink close");
                    assertThat(exception.getSuppressed())
                            .extracting(Throwable::getMessage)
                            .containsExactly("source close");
                });
    }

    @Test
    void doesNotCloseASourceWhoseOpenFailed() {
        LifecycleSource source = new LifecycleSource(RowBatch.end());
        source.openFailure = new IllegalStateException("source open");
        LifecycleSink sink = new LifecycleSink();

        assertThatThrownBy(() -> SingleNodeSyncJob.builder()
                        .source(source)
                        .sink(sink)
                        .build()
                        .run())
                .isInstanceOfSatisfying(SyncJobException.class, exception -> assertThat(exception.stage())
                        .isEqualTo(SyncStage.SOURCE_OPEN));
        assertThat(source.closeCount).isZero();
        assertThat(sink.openCount).isZero();
        assertThat(sink.closeCount).isZero();
    }

    @Test
    void rejectsInvalidBuilderConfiguration() {
        assertThatThrownBy(() -> SingleNodeSyncJob.builder().maxBatchRecords(0))
                .isInstanceOf(IllegalArgumentException.class)
                .hasMessage("maxBatchRecords must be positive");
        assertThatThrownBy(() -> SingleNodeSyncJob.builder()
                        .source(batchSource(limit -> RowBatch.end()))
                        .build())
                .isInstanceOf(IllegalStateException.class)
                .hasMessage("source and sink are required");
    }

    private static BatchSource batchSource(IntFunction<RowBatch> reader) {
        return new BatchSource() {
            @Override
            public void open() {}

            @Override
            public RowBatch readBatch(int maxRows) {
                return reader.apply(maxRows);
            }

            @Override
            public void close() {}
        };
    }

    private static BatchSink batchSink(Consumer<RowBatch> writer) {
        return new BatchSink() {
            @Override
            public void open() {}

            @Override
            public void writeBatch(RowBatch batch) {
                writer.accept(batch);
            }

            @Override
            public void close() {}
        };
    }

    private static final class LifecycleSource implements BatchSource {
        private final RowBatch batch;
        private int openCount;
        private int closeCount;
        private RuntimeException openFailure;

        private LifecycleSource(RowBatch batch) {
            this.batch = batch;
        }

        @Override
        public void open() {
            openCount++;
            if (openFailure != null) {
                throw openFailure;
            }
        }

        @Override
        public RowBatch readBatch(int maxRecords) {
            return batch;
        }

        @Override
        public void close() {
            closeCount++;
        }
    }

    private static final class LifecycleSink implements BatchSink {
        private int openCount;
        private int closeCount;
        private RuntimeException openFailure;

        @Override
        public void open() {
            openCount++;
            if (openFailure != null) {
                throw openFailure;
            }
        }

        @Override
        public void writeBatch(RowBatch batch) {}

        @Override
        public void close() {
            closeCount++;
        }
    }
}
