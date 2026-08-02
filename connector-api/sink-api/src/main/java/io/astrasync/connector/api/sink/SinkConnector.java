package io.astrasync.connector.api.sink;

import io.astrasync.connector.api.*;
import io.astrasync.connector.api.data.RecordBatch;
import io.astrasync.connector.api.metadata.Schema;

import java.util.Collection;
import java.util.List;

public interface SinkConnector extends Configurable {

    SinkCapabilities capabilities();

    Schema getSchema(String tableId);

    SinkWriter createWriter(WriterContext context);

    Optional<GlobalCommitter> createCommitter();
}

public interface SinkWriter extends AutoCloseable {

    void open();

    void write(RecordBatch batch, Context context);

    List<CommitHandle> prepareCommit(long checkpointId);

    WriterState snapshotState(long checkpointId);

    void abort(long checkpointId);

    void close();
}

public interface GlobalCommitter extends AutoCloseable {

    void open();

    void commit(long checkpointId, long epoch, Collection<CommitHandle> handles);

    void abort(long checkpointId, long epoch, Collection<CommitHandle> handles);

    CommitterState snapshotState(long checkpointId);

    void recover(List<CommitterState> pendingCommits);

    void close();
}

public interface WriterContext {

    String getJobId();

    String getTaskId();

    String getTargetTable();

    SinkConfig getConfig();

    Schema getSchema();
}

public interface CommitHandle {

    String getCommitterId();

    byte[] getCommitData();

    String getTargetPath();
}

public interface WriterState extends SerializableState {

    long getCheckpointId();

    long getEpoch();

    String getTargetTable();
}
