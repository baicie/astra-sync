package io.astrasync.engine.coordinator;

import io.astrasync.engine.plan.CompiledJobPlan;
import java.util.Objects;

/** Compiled plan and stopped-run state for a local CDC job. */
public record CdcJobRunResult(CompiledJobPlan plan, CdcRunResult runResult) {
    public CdcJobRunResult {
        plan = Objects.requireNonNull(plan, "plan must not be null");
        runResult = Objects.requireNonNull(runResult, "runResult must not be null");
    }
}
