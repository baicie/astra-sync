package io.astrasync.connector.debezium;

import static org.assertj.core.api.Assertions.assertThat;
import static org.assertj.core.api.Assertions.assertThatThrownBy;

import java.util.LinkedHashMap;
import java.util.Map;
import org.junit.jupiter.api.Test;

class DebeziumIdentitiesTest {
    @Test
    void createsStableOrderIndependentSecretFreeIdentities() {
        Map<String, String> first = new LinkedHashMap<>();
        first.put("table", "shop.orders");
        first.put("database", "shop");
        Map<String, String> second = new LinkedHashMap<>();
        second.put("database", "shop");
        second.put("table", "shop.orders");

        String firstIdentity = DebeziumIdentities.forConfiguration("mysql-cdc", first);
        String secondIdentity = DebeziumIdentities.forConfiguration("mysql-cdc", second);
        String changedIdentity = DebeziumIdentities.forConfiguration("mysql-cdc", Map.of("database", "other"));

        assertThat(firstIdentity)
                .isEqualTo(secondIdentity)
                .startsWith("mysql-cdc:v1:")
                .doesNotContain("shop", "orders");
        assertThat(changedIdentity).isNotEqualTo(firstIdentity);
    }

    @Test
    void rejectsMissingOrBlankIdentityFields() {
        assertThatThrownBy(() -> DebeziumIdentities.forConfiguration("mysql-cdc", null))
                .isInstanceOf(NullPointerException.class)
                .hasMessageContaining("identityFields");
        assertThatThrownBy(() -> DebeziumIdentities.forConfiguration(" ", Map.of("database", "shop")))
                .isInstanceOf(IllegalArgumentException.class)
                .hasMessageContaining("connectorType");
        assertThatThrownBy(() -> DebeziumIdentities.forConfiguration("mysql-cdc", Map.of(" ", "shop")))
                .isInstanceOf(IllegalArgumentException.class)
                .hasMessageContaining("identity field");
        assertThatThrownBy(() -> DebeziumIdentities.forConfiguration("mysql-cdc", Map.of("database", " ")))
                .isInstanceOf(IllegalArgumentException.class)
                .hasMessageContaining("identity value");
    }
}
