package io.astrasync.engine.kernel;

import static org.assertj.core.api.Assertions.assertThat;
import static org.assertj.core.api.Assertions.assertThatThrownBy;

import java.util.LinkedHashMap;
import java.util.Map;
import org.junit.jupiter.api.Test;

class SyncRecordTest {
    @Test
    void keepsNullValuesAndDefensivelyCopiesInput() {
        Map<String, Object> input = new LinkedHashMap<>();
        input.put("id", 7);
        input.put("note", null);

        SyncRecord record = SyncRecord.of(input);
        input.put("id", 8);

        assertThat(record.asMap()).containsEntry("id", 7).containsEntry("note", null);
        assertThatThrownBy(() -> record.asMap().put("extra", true)).isInstanceOf(UnsupportedOperationException.class);
    }

    @Test
    void withReturnsANewRecordAndAllowsNullValues() {
        SyncRecord original = SyncRecord.of("id", 7);
        SyncRecord updated = original.with("note", null);
        SyncRecord nullRecord = SyncRecord.of("note", null);

        assertThat(original.asMap()).containsExactly(Map.entry("id", 7));
        assertThat(updated.asMap()).containsEntry("id", 7).containsEntry("note", null);
        assertThat(nullRecord.asMap()).containsEntry("note", null);
    }

    @Test
    void rejectsNullKeys() {
        Map<String, Object> values = new LinkedHashMap<>();
        values.put(null, "value");

        assertThatThrownBy(() -> SyncRecord.of(values))
                .isInstanceOf(NullPointerException.class)
                .hasMessage("record key must not be null");
    }
}
