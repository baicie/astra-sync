package connectiontest

import (
	"context"
	"errors"
	"testing"

	"github.com/go-sql-driver/mysql"
	"github.com/jackc/pgx/v5/pgconn"

	"io.astrasync/control-plane/connection"
)

func TestJDBCProbeAcceptsOnlyBoundedDatabaseURLs(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		name   string
		url    string
		valid  bool
		driver jdbcKind
	}{
		{name: "postgres", url: "jdbc:postgresql://db.example:5433/orders?sslmode=verify-full", valid: true, driver: jdbcPostgreSQL},
		{name: "mysql", url: "jdbc:mysql://db.example/orders?sslMode=required", valid: true, driver: jdbcMySQL},
		{name: "caller query parameter", url: "jdbc:postgresql://db.example/orders?options=-c%20search_path%3Dprivate"},
		{name: "embedded credentials", url: "jdbc:postgresql://admin:secret@db.example/orders"},
		{name: "multiple path segments", url: "jdbc:mysql://db.example/orders/private"},
		{name: "unsupported driver", url: "jdbc:h2:mem:test"},
	} {
		t.Run(test.name, func(t *testing.T) {
			configuration, err := NewConfiguration(
				[]connection.Setting{{Key: "url", Value: test.url, Sensitivity: connection.SensitivityRestricted}}, nil,
			)
			if err != nil {
				t.Fatalf("construct configuration: %v", err)
			}
			defer configuration.Close()
			target, valid := parseJDBCURL(configuration)
			if valid != test.valid || valid && target.driver != test.driver {
				t.Fatalf("unexpected parse result: target=%+v valid=%v", target, valid)
			}
		})
	}
}

func TestProbeErrorClassificationNeverReturnsVendorText(t *testing.T) {
	t.Parallel()
	postgresResult := classifyProbeError(context.Background(), &pgconn.PgError{
		Code: "28P01", Message: "password-sentinel",
	})
	if postgresResult.Phase != connection.TestPhaseAuthentication ||
		postgresResult.ResultCode != connection.TestResultAuthenticationFailed {
		t.Fatalf("unexpected PostgreSQL classification: %+v", postgresResult)
	}
	mysqlResult := classifyProbeError(context.Background(), &mysql.MySQLError{
		Number: 1045, Message: "password-sentinel",
	})
	if mysqlResult.Phase != connection.TestPhaseAuthentication ||
		mysqlResult.ResultCode != connection.TestResultAuthenticationFailed {
		t.Fatalf("unexpected MySQL classification: %+v", mysqlResult)
	}
	transport := classifyProbeError(context.Background(), errors.Join(ErrDialFailed, errors.New("endpoint-sentinel")))
	if transport.Phase != connection.TestPhaseTransport ||
		transport.RemediationKey != "connection.test.transport" {
		t.Fatalf("unexpected transport classification: %+v", transport)
	}
}
