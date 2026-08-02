package io.astrasync.engine.jobspec;

import java.util.Arrays;

public enum DeliveryGuarantee {
    AT_MOST_ONCE("at-most-once"),
    AT_LEAST_ONCE("at-least-once"),
    EXACTLY_ONCE("exactly-once");

    private final String externalName;

    DeliveryGuarantee(String externalName) {
        this.externalName = externalName;
    }

    public String externalName() {
        return externalName;
    }

    public static DeliveryGuarantee fromExternalName(String value) {
        return Arrays.stream(values())
                .filter(guarantee -> guarantee.externalName.equals(value))
                .findFirst()
                .orElseThrow(() -> new IllegalArgumentException("unsupported delivery guarantee: " + value));
    }
}
