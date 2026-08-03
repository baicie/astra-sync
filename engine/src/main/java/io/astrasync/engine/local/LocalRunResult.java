package io.astrasync.engine.local;

import io.astrasync.engine.kernel.SyncResult;
import io.astrasync.engine.plan.CompiledJobPlan;
import java.util.Objects;

/** The immutable plan and terminal metrics from one local execution. */
public record LocalRunResult(CompiledJobPlan plan, SyncResult metrics) {
    public LocalRunResult {
        plan = Objects.requireNonNull(plan, "plan must not be null");
        metrics = Objects.requireNonNull(metrics, "metrics must not be null");
    }
}
