package io.astrasync.engine.jobspec;

import java.util.List;
import java.util.Objects;

public record JobConfiguration(
        ConnectorSpec source,
        List<TransformSpec> transforms,
        ConnectorSpec sink,
        DeliverySpec delivery,
        RuntimeSpec runtime) {
    public JobConfiguration {
        source = Objects.requireNonNull(source, "source must not be null");
        transforms = List.copyOf(Objects.requireNonNull(transforms, "transforms must not be null"));
        sink = Objects.requireNonNull(sink, "sink must not be null");
        delivery = Objects.requireNonNull(delivery, "delivery must not be null");
        runtime = Objects.requireNonNull(runtime, "runtime must not be null");
    }
}
