package io.astrasync.engine.kernel;

import static org.assertj.core.api.Assertions.assertThat;
import static org.assertj.core.api.Assertions.assertThatThrownBy;

import java.util.ArrayList;
import java.util.List;
import java.util.concurrent.atomic.AtomicInteger;
import org.junit.jupiter.api.Test;

class SingleNodeSyncJobTest {
    @Test
    void runsMultipleBoundedBatchesThroughTransformsInOrder() {
        List<SyncRecord> written = new ArrayList<>();
        List<Integer> requestedLimits = new ArrayList<>();
        AtomicInteger poll = new AtomicInteger();
        RecordSource source = maxRecords -> {
            requestedLimits.add(maxRecords);
            return switch (poll.getAndIncrement()) {
                case 0 -> SyncBatch.data(List.of(SyncRecord.of("name", "ada"), SyncRecord.of("name", "grace")));
                case 1 -> SyncBatch.last(List.of(SyncRecord.of("name", "linus")));
                default -> throw new AssertionError("source polled after end of input");
            };
        };

        SyncResult result = SingleNodeSyncJob.builder()
                .source(source)
                .transform(record ->
                        record.with("name", record.get("name").toString().toUpperCase()))
                .transform(record -> record.with("name", "[" + record.get("name") + "]"))
                .sink(written::add)
                .maxBatchRecords(2)
                .build()
                .run();

        assertThat(requestedLimits).containsExactly(2, 2);
        assertThat(written).extracting(record -> record.get("name")).containsExactly("[ADA]", "[GRACE]", "[LINUS]");
        assertThat(result.readCount()).isEqualTo(3);
        assertThat(result.writtenCount()).isEqualTo(3);
        assertThat(result.batchCount()).isEqualTo(2);
        assertThat(result.maxObservedBatchSize()).isEqualTo(2);
        assertThat(result.elapsedNanos()).isNotNegative();
    }

    @Test
    void doesNotPollNextBatchUntilCurrentBatchIsWritten() {
        List<SyncRecord> written = new ArrayList<>();
        AtomicInteger poll = new AtomicInteger();
        RecordSource source = maxRecords -> switch (poll.getAndIncrement()) {
            case 0 -> {
                assertThat(written).isEmpty();
                yield SyncBatch.data(List.of(SyncRecord.of("id", 1), SyncRecord.of("id", 2)));
            }
            case 1 -> {
                assertThat(written).hasSize(2);
                yield SyncBatch.last(List.of(SyncRecord.of("id", 3)));
            }
            default -> throw new AssertionError("source polled after end of input");
        };

        SingleNodeSyncJob.builder()
                .source(source)
                .sink(written::add)
                .maxBatchRecords(2)
                .build()
                .run();

        assertThat(written).extracting(record -> record.get("id")).containsExactly(1, 2, 3);
    }

    @Test
    void rejectsABatchThatExceedsTheRequestedLimit() {
        List<SyncRecord> written = new ArrayList<>();
        SingleNodeSyncJob job = SingleNodeSyncJob.builder()
                .source(limit -> SyncBatch.last(List.of(SyncRecord.of("id", 1), SyncRecord.of("id", 2))))
                .sink(written::add)
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
        LifecycleSource source = new LifecycleSource(SyncBatch.last(List.of(SyncRecord.of("id", 1))));
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
        List<SyncRecord> written = new ArrayList<>();
        AtomicInteger poll = new AtomicInteger();
        RecordSource source = maxRecords -> {
            if (poll.getAndIncrement() == 0) {
                return SyncBatch.data(List.of(SyncRecord.of("id", 1)));
            }
            throw new IllegalStateException("source read");
        };

        assertThatThrownBy(() -> SingleNodeSyncJob.builder()
                        .source(source)
                        .sink(written::add)
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
        LifecycleSource source = new LifecycleSource(SyncBatch.end());
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
        RecordSource source = new RecordSource() {
            @Override
            public SyncBatch readBatch(int maxRecords) {
                return SyncBatch.last(List.of(SyncRecord.of("id", 1)));
            }

            @Override
            public void close() {
                throw new IllegalStateException("source close");
            }
        };
        RecordSink sink = new RecordSink() {
            @Override
            public void write(SyncRecord record) {
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
        RecordSource source = new RecordSource() {
            @Override
            public SyncBatch readBatch(int maxRecords) {
                return SyncBatch.last(List.of(SyncRecord.of("id", 1)));
            }

            @Override
            public void close() {
                throw new IllegalStateException("source close");
            }
        };
        RecordSink sink = new RecordSink() {
            @Override
            public void write(SyncRecord record) {}

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
        LifecycleSource source = new LifecycleSource(SyncBatch.end());
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
                        .source(limit -> SyncBatch.end())
                        .build())
                .isInstanceOf(IllegalStateException.class)
                .hasMessage("source and sink are required");
    }

    private static final class LifecycleSource implements RecordSource {
        private final SyncBatch batch;
        private int openCount;
        private int closeCount;
        private RuntimeException openFailure;

        private LifecycleSource(SyncBatch batch) {
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
        public SyncBatch readBatch(int maxRecords) {
            return batch;
        }

        @Override
        public void close() {
            closeCount++;
        }
    }

    private static final class LifecycleSink implements RecordSink {
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
        public void write(SyncRecord record) {}

        @Override
        public void close() {
            closeCount++;
        }
    }
}
