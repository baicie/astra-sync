package io.astrasync.connector.api.metadata;

import java.util.List;

public interface TableDescriptor {
    String getTableId();

    String getSchemaName();

    String getTableName();

    TableType getTableType();

    List<String> getPartitionKeys();

    long getRowCount();

    long getDataSize();

    boolean isStreaming();
}
