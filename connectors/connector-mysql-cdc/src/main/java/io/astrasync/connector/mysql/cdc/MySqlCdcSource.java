package io.astrasync.connector.mysql.cdc;

import io.astrasync.connector.api.data.CdcBatch;
import io.astrasync.connector.api.source.CdcSource;
import io.astrasync.connector.api.source.SplitPosition;
import io.astrasync.connector.debezium.DebeziumCdcSource;
import java.io.IOException;
import java.nio.file.Files;
import java.nio.file.Path;
import java.time.Duration;
import java.util.Optional;

final class MySqlCdcSource implements CdcSource {
    private final MySqlCdcConnectorOptions options;
    private final DebeziumCdcSource delegate;

    MySqlCdcSource(MySqlCdcConnectorOptions options) {
        this.options = options;
        this.delegate = new DebeziumCdcSource(
                options.identity(), options.properties(), options.queuedBatches(), options.commitTimeout());
    }

    @Override
    public void openAt(SplitPosition resumePosition) {
        Path parent = options.schemaHistoryFile().getParent();
        try {
            Files.createDirectories(parent);
        } catch (IOException exception) {
            throw new IllegalStateException("failed to create MySQL schema history directory " + parent, exception);
        }
        delegate.openAt(resumePosition);
    }

    @Override
    public Optional<CdcBatch> poll(Duration timeout) {
        return delegate.poll(timeout);
    }

    @Override
    public SplitPosition acknowledge(CdcBatch batch) {
        return delegate.acknowledge(batch);
    }

    @Override
    public void close() {
        delegate.close();
    }
}
