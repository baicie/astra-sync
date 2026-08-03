package io.astrasync.engine.api;

import java.util.Map;
import java.util.Optional;

public interface ExecutionAttempt {

    String getExecutionAttemptId();

    String getJobId();

    long getJobVersion();

    long getEpoch();

    int getAttemptNumber();

    long getStartTimestamp();

    ExecutionState getState();
}

enum ExecutionState {
    CREATED,
    SCHEDULING,
    DEPLOYING,
    RUNNING,
    FAILING,
    FAILED,
    CANCELING,
    CANCELED,
    SUSPENDED,
    RECONCILING,
    RESTARTING
}

interface JobExecutionResult {

    String getJobId();

    long getJobVersion();

    JobStatus getStatus();

    Optional<String> getErrorMessage();

    Optional<String> getSavepointPath();

    Map<String, Long> getAccumulators();
}

interface JobStatus {

    JobState getState();

    long getStartTime();

    long getEndTime();

    long getDurationMillis();

    Optional<JobFailureInfo> getFailureInfo();
}

enum JobState {
    INITIALIZING,
    RUNNING,
    PAUSED,
    FAILING,
    FAILED,
    CANCELING,
    CANCELED,
    FINISHED
}

interface JobFailureInfo {

    String getReason();

    String getRootCause();

    String getStackTrace();

    long getTimestamp();

    String getHost();
}
