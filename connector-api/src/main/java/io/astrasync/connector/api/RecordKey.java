package io.astrasync.connector.api;

import java.util.LinkedHashMap;
import java.util.Map;

public interface RecordKey {

    byte[] toBytes();

    Map<String, Object> values();

    static RecordKey of(Object... values) {
        LinkedHashMap<String, Object> indexed = new LinkedHashMap<>();
        for (int index = 0; index < values.length; index++) {
            indexed.put(Integer.toString(index), values[index]);
        }
        return ImmutableRecordKey.of(indexed);
    }

    static RecordKey of(Map<String, ?> values) {
        return ImmutableRecordKey.of(values);
    }

    static RecordKey empty() {
        return ImmutableRecordKey.empty();
    }
}
