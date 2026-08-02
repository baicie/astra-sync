package io.astrasync.connector.api.data;

import static org.assertj.core.api.Assertions.assertThat;
import static org.assertj.core.api.Assertions.assertThatThrownBy;

import java.util.ArrayList;
import java.util.LinkedHashMap;
import java.util.List;
import java.util.Map;
import org.junit.jupiter.api.Test;

class RowTest {
    @Test
    void preservesInsertionOrderAndNullValues() {
        LinkedHashMap<String, Object> values = new LinkedHashMap<>();
        values.put("id", 7);
        values.put("name", null);

        Row row = Row.of(values);

        assertThat(row.asMap().keySet()).containsExactly("id", "name");
        assertThat(row.get("id")).isEqualTo(7);
        assertThat(row.get("name")).isNull();
        assertThat(row.contains("name")).isTrue();
        assertThat(row.size()).isEqualTo(2);
    }

    @Test
    void copiesTheMapStructureAndExposesAnUnmodifiableView() {
        LinkedHashMap<String, Object> source = new LinkedHashMap<>();
        source.put("id", 1);

        Row row = Row.of(source);
        source.put("late", 2);

        assertThat(row.asMap()).containsOnlyKeys("id");
        assertThatThrownBy(() -> row.asMap().put("blocked", 3)).isInstanceOf(UnsupportedOperationException.class);
    }

    @Test
    void structurallyCopiesButDoesNotDeepCloneValues() {
        List<String> mutableValue = new ArrayList<>();
        Row row = Row.of("items", mutableValue);

        mutableValue.add("visible");

        assertThat(row.get("items")).isSameAs(mutableValue);
        assertThat(row.get("items")).isEqualTo(List.of("visible"));
    }

    @Test
    void withCreatesANewRowWithoutChangingExistingColumnOrder() {
        LinkedHashMap<String, Object> values = new LinkedHashMap<>();
        values.put("first", 1);
        values.put("second", 2);
        Row original = Row.of(values);

        Row updated = original.with("first", 3).with("third", null);

        assertThat(original.get("first")).isEqualTo(1);
        assertThat(updated.asMap().keySet()).containsExactly("first", "second", "third");
        assertThat(updated.get("first")).isEqualTo(3);
        assertThat(updated.get("third")).isNull();
    }

    @Test
    void hasValueEqualityAndStableEmptyInstance() {
        Row first = Row.of(Map.of("id", 1));
        Row second = Row.of(Map.of("id", 1));

        assertThat(first).isEqualTo(second).hasSameHashCodeAs(second);
        assertThat(first.toString()).contains("id=1");
        assertThat(Row.empty()).isSameAs(Row.of(Map.of()));
    }

    @Test
    void rejectsNullOrBlankColumnNames() {
        LinkedHashMap<String, Object> withNullName = new LinkedHashMap<>();
        withNullName.put(null, "value");

        assertThatThrownBy(() -> Row.of((Map<String, Object>) null))
                .isInstanceOf(NullPointerException.class)
                .hasMessageContaining("values");
        assertThatThrownBy(() -> Row.of(withNullName))
                .isInstanceOf(NullPointerException.class)
                .hasMessageContaining("columnName");
        assertThatThrownBy(() -> Row.of("  ", "value"))
                .isInstanceOf(IllegalArgumentException.class)
                .hasMessageContaining("columnName");
        assertThatThrownBy(() -> Row.empty().with("", "value"))
                .isInstanceOf(IllegalArgumentException.class)
                .hasMessageContaining("columnName");
    }
}
