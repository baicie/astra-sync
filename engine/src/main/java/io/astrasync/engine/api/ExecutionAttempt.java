package io.astrasync.engine.api;

public interface ExecutionAttempt {

    String getExecutionAttemptId();

    String getJobId();

    long getJobVersion();

    long getEpoch();

    int getAttemptNumber();

    long getStartTimestamp();

    ExecutionState getState();
}

public enum ExecutionState {
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

public interface JobExecutionResult {

    String getJobId();

    long getJobVersion();

    JobStatus getStatus();

    Optional<String> getErrorMessage();

    Optional<String> getSavepointPath();

    Map<String, Long> getAccumulators();
}

public interface JobStatus {

    JobState getState();

    long getStartTime();

    long getEndTime();

    long getDurationMillis();

    Optional<JobFailureInfo> getFailureInfo();
}

public enum JobState {
    INITIALIZING,
    RUNNING,
    PAUSED,
    FAILING,
    FAILED,
    CANCELING,
    CANCELED,
    FINISHED
}

public interface JobFailureInfo {

    String getReason();

    String getRootCause();

    String getStackTrace();

    long getTimestamp();

    String getHost();
}
