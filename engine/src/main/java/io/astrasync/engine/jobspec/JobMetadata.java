package io.astrasync.engine.jobspec;

import java.util.Objects;
import java.util.regex.Pattern;

public record JobMetadata(String name) {
    private static final Pattern NAME_PATTERN = Pattern.compile("[a-z0-9](?:[a-z0-9-]{0,61}[a-z0-9])?");

    public JobMetadata {
        name = Objects.requireNonNull(name, "name must not be null");
        if (!NAME_PATTERN.matcher(name).matches()) {
            throw new IllegalArgumentException("name must be a lowercase DNS label of at most 63 characters");
        }
    }
}
