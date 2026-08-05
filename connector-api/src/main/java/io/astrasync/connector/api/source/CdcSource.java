package io.astrasync.connector.api.source;

import io.astrasync.connector.api.data.CdcBatch;
import java.time.Duration;
import java.util.Optional;

/** Unbounded change source with ordered, checkpoint-coupled acknowledgements. */
public interface CdcSource extends AutoCloseable {
    void openAt(SplitPosition resumePosition);

    Optional<CdcBatch> poll(Duration timeout);

    SplitPosition acknowledge(CdcBatch batch);

    @Override
    void close();
}
