package io.astrasync.connector.api.source;

import static org.assertj.core.api.Assertions.assertThat;
import static org.assertj.core.api.Assertions.assertThatThrownBy;

import java.util.HashMap;
import java.util.List;
import java.util.Map;
import org.junit.jupiter.api.Test;

class SplitContractsTest {
    @Test
    void positionsAreSortedAndImmutable() {
        Map<String, String> offsets = new HashMap<>();
        offsets.put("z", "3");
        offsets.put("a", "1");

        SplitPosition position = new SplitPosition(offsets);

        assertThat(position.offsets()).containsExactly(Map.entry("a", "1"), Map.entry("z", "3"));
        assertThatThrownBy(() -> position.offsets().put("b", "2")).isInstanceOf(UnsupportedOperationException.class);
        assertThat(SplitPosition.unbounded().isUnbounded()).isTrue();
        assertThat(position.isUnbounded()).isFalse();
    }

    @Test
    void positionsAndSplitsRejectInvalidValues() {
        Map<String, String> nullValue = new HashMap<>();
        nullValue.put("id", null);

        assertThatThrownBy(() -> new SplitPosition(nullValue)).isInstanceOf(NullPointerException.class);
        assertThatThrownBy(() -> new SplitPosition(Map.of(" ", "1"))).isInstanceOf(IllegalArgumentException.class);
        assertThatThrownBy(
                        () -> new SourceSplit("", "jdbc:records", SplitPosition.unbounded(), SplitPosition.unbounded()))
                .isInstanceOf(IllegalArgumentException.class);
        assertThatThrownBy(() -> new SourceSplit("split-1", "", SplitPosition.unbounded(), SplitPosition.unbounded()))
                .isInstanceOf(IllegalArgumentException.class);
        assertThatThrownBy(() -> new SourceSplit("split-1", "jdbc:records", null, SplitPosition.unbounded()))
                .isInstanceOf(NullPointerException.class);
    }

    @Test
    void enumeratorContractReturnsSplitDescriptors() {
        SourceSplit split = new SourceSplit(
                "split-1", "jdbc:records", new SplitPosition(Map.of("id", "1")), SplitPosition.unbounded());
        SplitEnumerator enumerator = () -> List.of(split);

        assertThat(enumerator.enumerate()).containsExactly(split);
        assertThat(split.start().offsets()).containsEntry("id", "1");
        assertThat(split.end().isUnbounded()).isTrue();
    }
}
