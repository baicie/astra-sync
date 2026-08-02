package io.astrasync.engine.api;

public interface Checkpoint {

    long getCheckpointId();

    String getJobId();

    long getJobVersion();

    long getTimestamp();

    long getDuration();

    CheckpointType getType();

    CheckpointStatus getStatus();

    CheckpointStatistics getStatistics();

    String getExternalPath();
}

public enum CheckpointType {
    FULL,
    INCREMENTAL,
    UNALIGNED
}

public enum CheckpointStatus {
    COMPLETED,
    FAILED,
    EXPIRED,
    IN_PROGRESS
}

public interface CheckpointStatistics {

    long getStateSize();

    int getAlignmentBuffered();

    int getProcessedRecords();

    int getAcknowledgedSubtasks();

    int getTotalSubtasks();
}

public interface Savepoint {

    String getPath();

    long getCheckpointId();

    String getJobId();

    long getJobVersion();

    long getTimestamp();

    SavepointFormat getFormat();
}

public enum SavepointFormat {
    CANONICAL,
    NATIVE
}
