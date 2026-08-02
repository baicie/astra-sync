package io.astrasync.connector.api;

import static org.assertj.core.api.Assertions.assertThat;
import static org.assertj.core.api.Assertions.assertThatThrownBy;
import static org.assertj.core.api.Assertions.entry;

import java.util.LinkedHashMap;
import java.util.Map;
import org.junit.jupiter.api.Test;

class ConnectorConfigurationTest {
    @Test
    void copiesSortsAndProtectsOptions() {
        LinkedHashMap<String, String> source = new LinkedHashMap<>();
        source.put("z", "last");
        source.put("a", "first");

        ConnectorConfiguration configuration = ConnectorConfiguration.of(source);
        source.put("late", "ignored");

        assertThat(configuration.asMap()).containsOnly(entry("a", "first"), entry("z", "last"));
        assertThat(configuration.asMap().keySet()).containsExactly("a", "z");
        assertThatThrownBy(() -> configuration.asMap().put("blocked", "value"))
                .isInstanceOf(UnsupportedOperationException.class);
    }

    @Test
    void exposesRequiredAndOptionalStrings() {
        ConnectorConfiguration configuration = ConnectorConfiguration.of(Map.of("path", "", "format", "csv"));

        assertThat(configuration.required("path")).isEmpty();
        assertThat(configuration.optional("format")).contains("csv");
        assertThat(configuration.optional("missing")).isEmpty();
        assertThat(configuration.contains("path")).isTrue();
    }

    @Test
    void missingRequiredOptionIdentifiesTheKey() {
        assertThatThrownBy(() -> ConnectorConfiguration.empty().required("path"))
                .isInstanceOf(IllegalArgumentException.class)
                .hasMessageContaining("path");
    }

    @Test
    void parsesIntegersExactlyAndUsesDefaultOnlyWhenAbsent() {
        ConnectorConfiguration configuration = ConnectorConfiguration.of(Map.of("batch", "1024", "spaced", " 7"));

        assertThat(configuration.getInt("batch")).isEqualTo(1024);
        assertThat(configuration.getInt("missing", 23)).isEqualTo(23);
        assertThatThrownBy(() -> configuration.getInt("spaced", 23))
                .isInstanceOf(IllegalArgumentException.class)
                .hasMessageContaining("spaced")
                .hasMessageNotContaining(" 7");
        assertThatThrownBy(() -> configuration.getInt("missing"))
                .isInstanceOf(IllegalArgumentException.class)
                .hasMessageContaining("missing");
    }

    @Test
    void parsesOnlyLowercaseBooleanLiterals() {
        ConnectorConfiguration configuration =
                ConnectorConfiguration.of(Map.of("enabled", "true", "disabled", "false", "lenient", "TRUE"));

        assertThat(configuration.getBoolean("enabled")).isTrue();
        assertThat(configuration.getBoolean("disabled")).isFalse();
        assertThat(configuration.getBoolean("missing", true)).isTrue();
        assertThatThrownBy(() -> configuration.getBoolean("lenient"))
                .isInstanceOf(IllegalArgumentException.class)
                .hasMessageContaining("lenient")
                .hasMessageNotContaining("TRUE");
    }

    @Test
    void rejectsInvalidOptionMapsAndKeys() {
        LinkedHashMap<String, String> nullValue = new LinkedHashMap<>();
        nullValue.put("path", null);

        assertThatThrownBy(() -> ConnectorConfiguration.of(null))
                .isInstanceOf(NullPointerException.class)
                .hasMessageContaining("options");
        assertThatThrownBy(() -> ConnectorConfiguration.of(nullValue))
                .isInstanceOf(NullPointerException.class)
                .hasMessageContaining("path");
        assertThatThrownBy(() -> ConnectorConfiguration.of(Map.of(" ", "value")))
                .isInstanceOf(IllegalArgumentException.class)
                .hasMessageContaining("key");
        assertThatThrownBy(() -> ConnectorConfiguration.empty().optional(""))
                .isInstanceOf(IllegalArgumentException.class)
                .hasMessageContaining("key");
    }

    @Test
    void hasValueEqualityAndStableEmptyInstance() {
        ConnectorConfiguration first = ConnectorConfiguration.of(Map.of("path", "input.csv"));
        ConnectorConfiguration second = ConnectorConfiguration.of(Map.of("path", "input.csv"));

        assertThat(first).isEqualTo(second).hasSameHashCodeAs(second);
        assertThat(first.toString()).contains("path").doesNotContain("input.csv");
        assertThat(ConnectorConfiguration.empty()).isSameAs(ConnectorConfiguration.of(Map.of()));
    }
}
