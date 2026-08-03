package io.astrasync.engine.jobspec;

import java.util.Objects;

public record DeliverySpec(DeliveryGuarantee guarantee) {
    public DeliverySpec {
        guarantee = Objects.requireNonNull(guarantee, "guarantee must not be null");
    }
}
