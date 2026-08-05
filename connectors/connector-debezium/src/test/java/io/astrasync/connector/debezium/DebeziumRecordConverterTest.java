package io.astrasync.connector.debezium;

import static org.assertj.core.api.Assertions.assertThat;

import io.astrasync.connector.api.data.DataEvent;
import java.time.Clock;
import java.time.Instant;
import java.time.ZoneOffset;
import java.util.Map;
import org.apache.kafka.connect.data.Schema;
import org.apache.kafka.connect.data.SchemaBuilder;
import org.apache.kafka.connect.data.Struct;
import org.apache.kafka.connect.source.SourceRecord;
import org.junit.jupiter.api.Test;

class DebeziumRecordConverterTest {
    private static final Schema KEY_SCHEMA = SchemaBuilder.struct()
            .name("shop.orders.Key")
            .field("id", Schema.INT64_SCHEMA)
            .build();
    private static final Schema ROW_SCHEMA = SchemaBuilder.struct()
            .name("shop.orders.Value")
            .optional()
            .field("id", Schema.INT64_SCHEMA)
            .field("status", Schema.STRING_SCHEMA)
            .build();
    private static final Schema SOURCE_SCHEMA = SchemaBuilder.struct()
            .name("io.debezium.connector.mysql.Source")
            .field("connector", Schema.STRING_SCHEMA)
            .field("version", Schema.STRING_SCHEMA)
            .field("name", Schema.STRING_SCHEMA)
            .field("db", Schema.STRING_SCHEMA)
            .field("table", Schema.STRING_SCHEMA)
            .field("ts_ms", Schema.INT64_SCHEMA)
            .field("snapshot", SchemaBuilder.string().optional().build())
            .field("txId", SchemaBuilder.int64().optional().build())
            .build();
    private static final Schema ENVELOPE_SCHEMA = SchemaBuilder.struct()
            .name("shop.orders.Envelope")
            .field("before", ROW_SCHEMA)
            .field("after", ROW_SCHEMA)
            .field("source", SOURCE_SCHEMA)
            .field("op", Schema.STRING_SCHEMA)
            .field(
                    "transaction",
                    SchemaBuilder.struct()
                            .optional()
                            .field("id", Schema.STRING_SCHEMA)
                            .build())
            .build();

    private final DebeziumRecordConverter converter =
            new DebeziumRecordConverter(Clock.fixed(Instant.ofEpochMilli(2_000), ZoneOffset.UTC));

    @Test
    void convertsSnapshotAndUpdateEnvelopes() {
        DataEvent snapshot =
                converter.convert(record("r", null, row("NEW"), "last", 10)).orElseThrow();
        DataEvent update = converter
                .convert(record("u", row("NEW"), row("PAID"), "false", 11))
                .orElseThrow();

        assertThat(snapshot.getOperation()).isEqualTo(DataEvent.Operation.SNAPSHOT);
        assertThat(snapshot.getTableId()).isEqualTo("shop.orders");
        assertThat(snapshot.getKey().values()).containsEntry("id", 7L);
        assertThat(snapshot.getAfter().asMap()).containsEntry("status", "NEW");
        assertThat(snapshot.getHeaders()).containsEntry("source.snapshot", "last");
        assertThat(snapshot.getSourcePosition().getOffset()).containsEntry("pos", "10");
        assertThat(update.getOperation()).isEqualTo(DataEvent.Operation.UPDATE);
        assertThat(update.getBefore().asMap()).containsEntry("status", "NEW");
        assertThat(update.getAfter().asMap()).containsEntry("status", "PAID");
        assertThat(update.getIngestTime()).isEqualTo(2_000);
    }

    @Test
    void ignoresTombstonesAndHeartbeatRecords() {
        SourceRecord tombstone =
                new SourceRecord(Map.of("server", "shop"), Map.of("pos", 12L), "shop.orders", null, null);
        Schema heartbeatSchema = SchemaBuilder.struct()
                .name("heartbeat")
                .field("ts_ms", Schema.INT64_SCHEMA)
                .build();
        SourceRecord heartbeat = new SourceRecord(
                Map.of("server", "shop"),
                Map.of("pos", 13L),
                "__debezium-heartbeat.shop",
                heartbeatSchema,
                new Struct(heartbeatSchema).put("ts_ms", 1L));

        assertThat(converter.convert(tombstone)).isEmpty();
        assertThat(converter.convert(heartbeat)).isEmpty();
    }

    private static SourceRecord record(String operation, Struct before, Struct after, String snapshot, long position) {
        Struct source = new Struct(SOURCE_SCHEMA)
                .put("connector", "mysql")
                .put("version", "2.5.4.Final")
                .put("name", "shop-source")
                .put("db", "shop")
                .put("table", "orders")
                .put("ts_ms", 1_000L)
                .put("snapshot", snapshot)
                .put("txId", 42L);
        Struct envelope = new Struct(ENVELOPE_SCHEMA)
                .put("before", before)
                .put("after", after)
                .put("source", source)
                .put("op", operation)
                .put("transaction", null);
        Struct key = new Struct(KEY_SCHEMA).put("id", 7L);
        return new SourceRecord(
                Map.of("server", "shop-source"),
                Map.of("file", "mysql-bin.000001", "pos", position),
                "shop.shop.orders",
                KEY_SCHEMA,
                key,
                ENVELOPE_SCHEMA,
                envelope);
    }

    private static Struct row(String status) {
        return new Struct(ROW_SCHEMA).put("id", 7L).put("status", status);
    }
}
