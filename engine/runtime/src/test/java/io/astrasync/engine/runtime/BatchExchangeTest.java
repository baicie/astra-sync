package io.astrasync.engine.runtime;

import static org.assertj.core.api.Assertions.assertThat;
import static org.assertj.core.api.Assertions.assertThatThrownBy;

import io.astrasync.connector.api.data.Row;
import io.astrasync.connector.api.data.RowBatch;
import java.util.List;
import java.util.concurrent.ExecutionException;
import java.util.concurrent.ExecutorService;
import java.util.concurrent.Executors;
import java.util.concurrent.Future;
import java.util.concurrent.TimeUnit;
import org.junit.jupiter.api.Test;

class BatchExchangeTest {
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
}
