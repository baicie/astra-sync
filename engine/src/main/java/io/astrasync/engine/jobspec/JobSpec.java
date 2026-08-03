package io.astrasync.engine.jobspec;

import java.util.Objects;

public record JobSpec(String apiVersion, String kind, JobMetadata metadata, JobConfiguration spec) {
    public static final String API_VERSION = "sync.astrasync.io/v1";
    public static final String KIND = "SyncJob";

    public JobSpec {
        if (!API_VERSION.equals(apiVersion)) {
            throw new IllegalArgumentException("unsupported apiVersion: " + apiVersion);
        }
        if (!KIND.equals(kind)) {
            throw new IllegalArgumentException("unsupported kind: " + kind);
        }
        metadata = Objects.requireNonNull(metadata, "metadata must not be null");
        spec = Objects.requireNonNull(spec, "spec must not be null");
    }
}
