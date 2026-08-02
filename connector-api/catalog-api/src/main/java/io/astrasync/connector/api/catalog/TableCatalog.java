package io.astrasync.connector.api.catalog;

import java.util.List;
import java.util.Optional;

public interface TableCatalog {

    Optional<TableInfo> getTable(String tableId);

    List<TableInfo> listTables(String database);

    boolean tableExists(String tableId);

    List<ColumnInfo> getColumns(String tableId);

    PrimaryKeyInfo getPrimaryKey(String tableId);

    SchemaInfo getSchemaInfo(String tableId);
}

public interface TableInfo {

    String getTableId();

    String getCatalog();

    String getDatabase();

    String getSchema();

    String getTableName();

    TableType getTableType();

    List<String> getPartitionColumns();

    long getRowCount();

    long getDataSizeBytes();

    long getLastModifiedTime();

    boolean isStreamTable();
}

public interface ColumnInfo {

    int getColumnId();

    String getColumnName();

    String getDataType();

    int getPosition();

    boolean isNullable();

    boolean isPrimaryKey();

    boolean isPartitionKey();

    String getDefaultValue();

    String getComment();

    String getExpression();
}

public interface PrimaryKeyInfo {

    List<String> getColumns();

    String getConstraintName();
}

public interface SchemaInfo {

    String getSchemaId();

    int getVersion();

    List<ColumnInfo> getColumns();

    String getSerializedSchema();
}

public enum TableType {
    TABLE,
    VIEW,
    MATERIALIZED_VIEW,
    EXTERNAL_TABLE,
    TEMPORARY_TABLE
}
