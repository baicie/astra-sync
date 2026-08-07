package io.astrasync.engine.jobspec;

/** Optional, bounded disk spill settings for a Worker exchange. */
public record SpillSpec(boolean enabled, long maxBytes, int maxFiles) {
    public SpillSpec {
        if (enabled) {
            if (maxBytes <= 0) {
                throw new IllegalArgumentException("enabled spill maxBytes must be positive");
            }
            if (maxFiles <= 0) {
                throw new IllegalArgumentException("enabled spill maxFiles must be positive");
            }
        } else if (maxBytes != 0 || maxFiles != 0) {
            throw new IllegalArgumentException("disabled spill must use zero bounds");
        }
    }

    public static SpillSpec disabled() {
        return new SpillSpec(false, 0, 0);
    }
}
