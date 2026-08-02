package io.astrasync.connector.api.metadata;

import java.util.List;

public interface TableCatalog {

    String getCatalogId();

    String getDatabase();

    List<TableDescriptor> listTables();

    TableDescriptor getTable(String tableId);

    boolean tableExists(String tableId);

    List<ColumnDescriptor> getColumns(String tableId);

    PrimaryKey getPrimaryKey(String tableId);

    List<IndexDescriptor> getIndexes(String tableId);
}

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

public interface ColumnDescriptor {

    int getColumnId();

    String getColumnName();

    LogicalType getDataType();

    boolean isNullable();

    boolean isPrimaryKey();

    String getComment();

    String getDefaultValue();
}

public interface PrimaryKey {

    List<String> getColumns();

    String getConstraintName();
}

public interface IndexDescriptor {

    String getIndexName();

    List<String> getColumns();

    boolean isUnique();

    IndexType getIndexType();
}

public enum TableType {
    TABLE,
    VIEW,
    MATERIALIZED_VIEW,
    EXTERNAL_TABLE,
    TEMPORARY_TABLE
}

public enum IndexType {
    PRIMARY_KEY,
    UNIQUE,
    NON_UNIQUE,
    FULL_TEXT,
    SPATIAL,
    BITMAP
}
