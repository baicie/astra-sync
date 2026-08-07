package io.astrasync.engine.runtime;

import io.astrasync.engine.jobspec.SpillSpec;
import java.nio.file.Path;
import java.util.Objects;

/** Process-local spill settings resolved by the Worker that owns an exchange. */
public record SpillPolicy(boolean enabled, Path root, long maxBytes, int maxFiles) {
    public SpillPolicy {
        if (enabled) {
            root = Objects.requireNonNull(root, "spill root must not be null")
                    .toAbsolutePath()
                    .normalize();
            if (maxBytes <= 0) {
                throw new IllegalArgumentException("spill maxBytes must be positive");
            }
            if (maxFiles <= 0) {
                throw new IllegalArgumentException("spill maxFiles must be positive");
            }
        } else if (root != null || maxBytes != 0 || maxFiles != 0) {
            throw new IllegalArgumentException("disabled spill policy must not carry a root or bounds");
        }
    }

    public static SpillPolicy disabled() {
        return new SpillPolicy(false, null, 0, 0);
    }

    public static SpillPolicy enabled(Path root, SpillSpec spec) {
        SpillSpec checked = Objects.requireNonNull(spec, "spill spec must not be null");
        if (!checked.enabled()) {
            throw new IllegalArgumentException("spill spec must be enabled");
        }
        return new SpillPolicy(true, root, checked.maxBytes(), checked.maxFiles());
    }

    public static SpillPolicy descriptor(SpillSpec spec) {
        SpillSpec checked = Objects.requireNonNull(spec, "spill spec must not be null");
        return checked.enabled()
                ? new SpillPolicy(true, Path.of("."), checked.maxBytes(), checked.maxFiles())
                : disabled();
    }

    /** Compares portable policy fields and intentionally ignores the process-local root. */
    public boolean compatibleWith(SpillPolicy other) {
        return other != null && enabled == other.enabled && maxBytes == other.maxBytes && maxFiles == other.maxFiles;
    }
}
