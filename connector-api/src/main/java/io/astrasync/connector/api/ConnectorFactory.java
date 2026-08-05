package io.astrasync.connector.api;

import io.astrasync.connector.api.sink.BatchSink;
import io.astrasync.connector.api.sink.CdcSink;
import io.astrasync.connector.api.source.BatchSource;
import io.astrasync.connector.api.source.CdcSource;

/** Creates resource-free connector instances after successful planning. */
public interface ConnectorFactory {
    ConnectorDescriptor descriptor();

    default BatchSource createSource(ConnectorConfiguration configuration) {
        throw new UnsupportedOperationException(
                "connector '" + descriptor().name() + "' does not support the SOURCE role");
    }

    default BatchSink createSink(ConnectorConfiguration configuration) {
        throw new UnsupportedOperationException(
                "connector '" + descriptor().name() + "' does not support the SINK role");
    }

    default CdcSource createCdcSource(ConnectorConfiguration configuration) {
        throw new UnsupportedOperationException(
                "connector '" + descriptor().name() + "' does not support change data capture");
    }

    default CdcSink createCdcSink(ConnectorConfiguration configuration) {
        throw new UnsupportedOperationException(
                "connector '" + descriptor().name() + "' does not support change event writes");
    }
}
