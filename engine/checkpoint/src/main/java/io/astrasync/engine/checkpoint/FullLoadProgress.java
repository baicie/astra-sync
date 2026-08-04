package io.astrasync.engine.checkpoint;

import com.fasterxml.jackson.annotation.JsonIgnore;
import io.astrasync.connector.api.source.SourceSplit;
import io.astrasync.engine.runtime.WorkerResult;
import java.util.Collections;
import java.util.Map;
import java.util.Objects;
import java.util.Optional;
import java.util.TreeMap;
import java.util.regex.Pattern;

/** Durable split-level progress for one single-active-Coordinator full-load run. */
public record FullLoadProgress(
        int formatVersion, String jobId, SplitPlan plan, Map<String, CompletedSplit> completedSplits) {
    public static final int CURRENT_FORMAT_VERSION = 1;
    private static final Pattern JOB_ID = Pattern.compile("[a-z0-9](?:[a-z0-9-]{0,61}[a-z0-9])?");

    public FullLoadProgress {
        if (formatVersion != CURRENT_FORMAT_VERSION) {
            throw new IllegalArgumentException("unsupported full-load progress format: " + formatVersion);
        }
        jobId = requireJobId(jobId);
        SplitPlan checkedPlan = Objects.requireNonNull(plan, "plan must not be null");
        TreeMap<String, CompletedSplit> ordered = new TreeMap<>();
        Objects.requireNonNull(completedSplits, "completedSplits must not be null")
                .forEach((splitId, completed) -> {
                    String expectedFingerprint = checkedPlan.splitFingerprints().get(splitId);
                    CompletedSplit checked = Objects.requireNonNull(completed, "completed split must not be null");
                    if (expectedFingerprint == null) {
                        throw new IllegalArgumentException("completed split is not part of the plan: " + splitId);
                    }
                    if (!expectedFingerprint.equals(checked.splitFingerprint())) {
                        throw new IllegalArgumentException("completed split fingerprint changed: " + splitId);
                    }
                    ordered.put(splitId, checked);
                });
        plan = checkedPlan;
        completedSplits = Collections.unmodifiableMap(ordered);
    }

    public static FullLoadProgress create(String jobId, SplitPlan plan) {
        return new FullLoadProgress(CURRENT_FORMAT_VERSION, jobId, plan, Map.of());
    }

    @JsonIgnore
    public int completedCount() {
        return completedSplits.size();
    }

    @JsonIgnore
    public boolean isComplete() {
        return completedSplits.size() == plan.splitFingerprints().size();
    }

    @JsonIgnore
    public Optional<WorkerResult> completedResult(String splitId) {
        CompletedSplit completed = completedSplits.get(Objects.requireNonNull(splitId, "splitId must not be null"));
        return completed == null
                ? Optional.empty()
                : Optional.of(new WorkerResult(completed.workerId(), splitId, completed.metrics()));
    }

    public FullLoadProgress withCompletion(SourceSplit split, WorkerResult result) {
        SourceSplit checkedSplit = Objects.requireNonNull(split, "split must not be null");
        WorkerResult checkedResult = Objects.requireNonNull(result, "result must not be null");
        if (!checkedSplit.splitId().equals(checkedResult.taskId())) {
            throw new IllegalArgumentException("Worker result changed split identity: " + checkedResult.taskId());
        }
        String splitFingerprint = plan.requireMatchingSplit(checkedSplit);
        if (completedSplits.containsKey(checkedSplit.splitId())) {
            return this;
        }
        TreeMap<String, CompletedSplit> updated = new TreeMap<>(completedSplits);
        updated.put(
                checkedSplit.splitId(),
                new CompletedSplit(checkedResult.workerId(), splitFingerprint, checkedResult.metrics()));
        return new FullLoadProgress(formatVersion, jobId, plan, updated);
    }

    static String requireJobId(String value) {
        Objects.requireNonNull(value, "jobId must not be null");
        if (!JOB_ID.matcher(value).matches()) {
            throw new IllegalArgumentException("jobId must be a lowercase DNS label of at most 63 characters");
        }
        return value;
    }
}
