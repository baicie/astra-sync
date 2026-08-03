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
