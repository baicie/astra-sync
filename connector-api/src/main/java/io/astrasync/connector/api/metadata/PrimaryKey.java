package io.astrasync.connector.api.metadata;

import java.util.List;

public interface PrimaryKey {
    List<String> getColumns();

    String getConstraintName();
}
