package io.astrasync.connector.api.data;

import static org.assertj.core.api.Assertions.assertThat;
import static org.assertj.core.api.Assertions.assertThatThrownBy;

import io.astrasync.connector.api.RecordKey;
import io.astrasync.connector.api.SourcePosition;
import io.astrasync.connector.api.TraceContext;
import java.util.Map;
import org.junit.jupiter.api.Test;

class CdcContractsTest {
    @Test
    void keepsStructuredKeysAndPositionsImmutable() {
        RecordKey key = RecordKey.of(Map.of("id", 7));
        SourcePosition first =
                SourcePosition.of("position-1", "mysql-a", "shop", "shop.orders", Map.of("pos", "10"), 100, "tx-1", 1);
        SourcePosition second =
                SourcePosition.of("position-2", "mysql-a", "shop", "shop.orders", Map.of("pos", "11"), 101, "tx-1", 2);

        assertThat(key.values()).containsEntry("id", 7);
        assertThat(key.toBytes()).isNotEmpty();
        assertThat(first.isBefore(second)).isTrue();
        assertThat(first.laterThan(second)).contains(second);
        assertThat(first.earlierThan(second)).contains(first);
        assertThat(first.earlierThan(
                        SourcePosition.of("other", "mysql-b", "shop", "shop.orders", Map.of("pos", "1"), 1, "", 1)))
                .isEmpty();
    }

    @Test
    void validatesAndCopiesCdcBatches() {
        DataEvent event = event("event-1");
        CdcBatch batch = new CdcBatch(1, java.util.List.of(event), CdcPhase.SNAPSHOT, true);

        assertThat(batch.size()).isEqualTo(1);
        assertThat(batch.events()).containsExactly(event);
        assertThatThrownBy(() -> new CdcBatch(0, java.util.List.of(event), CdcPhase.STREAMING, false))
                .isInstanceOf(IllegalArgumentException.class);
        assertThatThrownBy(() -> new CdcBatch(1, java.util.List.of(), CdcPhase.STREAMING, false))
                .isInstanceOf(IllegalArgumentException.class);
    }

    private static DataEvent event(String id) {
        return new ImmutableDataEvent(
                id,
                SourcePosition.of("position", "source", "db", "db.table", Map.of("pos", "1"), 1, "", 1),
                DataEvent.Operation.SNAPSHOT,
                1,
                2,
                "schema",
                "db.table",
                RecordKey.of(Map.of("id", 1)),
                Row.empty(),
                Row.of(Map.of("id", 1)),
                Map.of("source.snapshot", "last"),
                TraceContext.root());
    }
}
