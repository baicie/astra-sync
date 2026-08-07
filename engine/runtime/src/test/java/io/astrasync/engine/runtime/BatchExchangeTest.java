package io.astrasync.engine.runtime;

import static org.assertj.core.api.Assertions.assertThat;
import static org.assertj.core.api.Assertions.assertThatThrownBy;

import io.astrasync.connector.api.data.Row;
import io.astrasync.connector.api.data.RowBatch;
import java.math.BigDecimal;
import java.nio.file.Path;
import java.time.LocalDate;
import java.time.LocalDateTime;
import java.time.LocalTime;
import java.time.OffsetDateTime;
import java.time.ZoneOffset;
import java.util.LinkedHashMap;
import java.util.List;
import java.util.concurrent.ExecutionException;
import java.util.concurrent.ExecutorService;
import java.util.concurrent.Executors;
import java.util.concurrent.Future;
import java.util.concurrent.TimeUnit;
import org.junit.jupiter.api.Test;
import org.junit.jupiter.api.io.TempDir;

class BatchExchangeTest {
    @TempDir
    Path spillRoot;

    @Test
    void publishBlocksWhenCapacityIsExhaustedUntilReceiverConsumes() throws Exception {
        BatchExchange exchange = new BatchExchange(1);
        exchange.publish(RowBatch.data(List.of(Row.of("id", 1))));
        ExecutorService executor = Executors.newSingleThreadExecutor();
        try {
            Future<?> pending = executor.submit(() -> exchange.publish(RowBatch.last(List.of(Row.of("id", 2)))));
            assertThat(pending.isDone()).isFalse();
            assertThat(exchange.receive().rows()).containsExactly(Row.of("id", 1));
            pending.get(1, TimeUnit.SECONDS);
            assertThat(exchange.receive().endOfInput()).isTrue();
        } finally {
            executor.shutdownNow();
        }
    }

    @Test
    void failureReleasesAWaitingPublisherAndReceiver() throws Exception {
        BatchExchange exchange = new BatchExchange(1);
        exchange.publish(RowBatch.data(List.of(Row.of("id", 1))));
        ExecutorService executor = Executors.newSingleThreadExecutor();
        try {
            Future<?> publisher = executor.submit(() -> exchange.publish(RowBatch.data(List.of(Row.of("id", 2)))));
            exchange.fail(new IllegalStateException("sink failed"));
            assertThatThrownBy(() -> publisher.get(1, TimeUnit.SECONDS))
                    .isInstanceOf(ExecutionException.class)
                    .hasCauseInstanceOf(ExchangeFailureException.class);
        } finally {
            executor.shutdownNow();
        }

        BatchExchange emptyExchange = new BatchExchange(1);
        executor = Executors.newSingleThreadExecutor();
        try {
            Future<?> receiver = executor.submit(emptyExchange::receive);
            emptyExchange.fail(new IllegalStateException("source failed"));
            assertThatThrownBy(() -> receiver.get(1, TimeUnit.SECONDS))
                    .isInstanceOf(ExecutionException.class)
                    .hasCauseInstanceOf(ExchangeFailureException.class);
        } finally {
            executor.shutdownNow();
        }
    }

    @Test
    void spillRoundTripPreservesRowsOrderingAndEndOfInput() {
        SpillPolicy policy = new SpillPolicy(true, spillRoot, 16 * 1024, 4);
        RowBatch original = RowBatch.last(List.of(Row.of(values())));

        try (BatchExchange exchange = new BatchExchange(2, policy)) {
            exchange.publish(original);

            assertThat(exchange.size()).isEqualTo(1);
            RowBatch received = exchange.receive();
            assertThat(received.endOfInput()).isTrue();
            assertThat(received.rows()).hasSize(1);
            assertThat(received.rows().get(0).asMap().keySet()).containsExactlyElementsOf(values().keySet());
            values().forEach((name, expected) -> {
                Object actual = received.rows().get(0).get(name);
                if (expected instanceof byte[] expectedBytes) {
                    assertThat((byte[]) actual).containsExactly(expectedBytes);
                } else {
                    assertThat(actual).isEqualTo(expected);
                }
            });
        }

        assertThat(listEntries(spillRoot)).isEmpty();
    }

    @Test
    void spillCapacityUsesTheSmallerTaskAndFileBound() throws Exception {
        try (BatchExchange exchange = new BatchExchange(3, new SpillPolicy(true, spillRoot, 4096, 1))) {
            assertThat(exchange.capacity()).isEqualTo(1);
            exchange.publish(RowBatch.data(List.of(Row.of("id", 1))));

            ExecutorService executor = Executors.newSingleThreadExecutor();
            try {
                Future<?> pending = executor.submit(() -> exchange.publish(RowBatch.end()));
                assertThat(pending.isDone()).isFalse();
                assertThat(exchange.receive()).isEqualTo(RowBatch.data(List.of(Row.of("id", 1))));
                pending.get(1, TimeUnit.SECONDS);
                assertThat(exchange.receive()).isEqualTo(RowBatch.end());
            } finally {
                executor.shutdownNow();
            }
        }
    }

    @Test
    void spillRejectsUnsupportedValuesAndRemovesItsTemporaryDirectory() {
        BatchExchange exchange = new BatchExchange(1, new SpillPolicy(true, spillRoot, 4096, 1));
        try {
            assertThatThrownBy(() -> exchange.publish(RowBatch.data(List.of(Row.of("value", new Object())))))
                    .isInstanceOf(ExchangeFailureException.class)
                    .hasMessageContaining("failed to spill batch");
        } finally {
            exchange.close();
        }
        assertThat(listEntries(spillRoot)).isEmpty();
    }

    private static LinkedHashMap<String, Object> values() {
        LinkedHashMap<String, Object> values = new LinkedHashMap<>();
        values.put("string", "value");
        values.put("boolean", true);
        values.put("byte", (byte) 1);
        values.put("short", (short) 2);
        values.put("integer", 3);
        values.put("long", 4L);
        values.put("float", 5.5f);
        values.put("double", 6.5d);
        values.put("decimal", new BigDecimal("7.50"));
        values.put("binary", new byte[] {8, 9});
        values.put("date", LocalDate.of(2024, 1, 2));
        values.put("time", LocalTime.of(3, 4, 5, 6));
        values.put("localDateTime", LocalDateTime.of(2024, 1, 2, 3, 4, 5, 6));
        values.put("offsetDateTime", OffsetDateTime.of(2024, 1, 2, 3, 4, 5, 6, ZoneOffset.ofHours(8)));
        return values;
    }

    private static List<Path> listEntries(Path directory) {
        try (var entries = java.nio.file.Files.list(directory)) {
            return entries.toList();
        } catch (java.io.IOException exception) {
            throw new AssertionError(exception);
        }
    }
}
