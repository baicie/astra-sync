package io.astrasync.connector.jdbc;

import java.sql.Connection;
import java.sql.DriverManager;
import java.sql.SQLException;
import java.util.UUID;

final class JdbcTestSupport {
    private JdbcTestSupport() {}

    static String url() {
        return "jdbc:h2:mem:astrasync_" + UUID.randomUUID().toString().replace('-', '_')
                + ";MODE=PostgreSQL;DB_CLOSE_DELAY=-1";
    }

    static Connection connect(String url) throws SQLException {
        return DriverManager.getConnection(url);
    }
}
