package connectiontest

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"database/sql"
	"errors"
	"fmt"
	"net"
	"net/url"
	"strconv"
	"strings"
	"sync/atomic"
	"time"

	"github.com/go-sql-driver/mysql"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"io.astrasync/control-plane/connection"
)

type jdbcKind uint8

const (
	jdbcGeneric jdbcKind = iota
	jdbcMySQL
	jdbcPostgreSQL
)

var mysqlDialSequence atomic.Uint64

type JDBCProbe struct {
	kind jdbcKind
}

type databaseTarget struct {
	driver   jdbcKind
	host     string
	port     uint16
	database string
	user     string
	password string
	sslMode  string
}

func (p JDBCProbe) Execute(
	ctx context.Context,
	configuration *Configuration,
	guard *EgressGuard,
	policy connection.TestEgressPolicy,
) ProbeResult {
	target, ok := p.target(configuration)
	if !ok {
		return FailedProbe(
			connection.TestPhaseHandshake, connection.TestResultHandshakeFailed,
			"connection.test.configuration",
		)
	}
	endpoint, err := guard.Resolve(ctx, target.host, target.port, policy)
	if err != nil {
		if ctx.Err() != nil {
			return TimedOutProbe(connection.TestPhaseDNS)
		}
		if errors.Is(err, ErrPolicyDenied) {
			return FailedProbe(
				connection.TestPhasePolicy, connection.TestResultPolicyDenied,
				"connection.test.egress_policy",
			)
		}
		return FailedProbe(
			connection.TestPhaseDNS, connection.TestResultDNSFailed,
			"connection.test.dns",
		)
	}
	if target.driver == jdbcPostgreSQL {
		err = probePostgreSQL(ctx, target, endpoint)
	} else {
		err = probeMySQL(ctx, target, endpoint)
	}
	if err == nil {
		return SuccessfulProbe()
	}
	return classifyProbeError(ctx, err)
}

func (p JDBCProbe) target(configuration *Configuration) (databaseTarget, bool) {
	switch p.kind {
	case jdbcGeneric:
		return parseJDBCURL(configuration)
	case jdbcMySQL:
		return targetFromFields(configuration, jdbcMySQL, 3306)
	case jdbcPostgreSQL:
		return targetFromFields(configuration, jdbcPostgreSQL, 5432)
	default:
		return databaseTarget{}, false
	}
}

func targetFromFields(
	configuration *Configuration, driver jdbcKind, defaultPort uint16,
) (databaseTarget, bool) {
	host, hostFound := configuration.required("hostname")
	database, databaseFound := configuration.required("database")
	user, userFound := configuration.required("username")
	password, passwordFound := configuration.required("password")
	if !hostFound || !databaseFound || !userFound || !passwordFound ||
		len(database) > 128 || strings.ContainsAny(database, "\x00\r\n") {
		return databaseTarget{}, false
	}
	port := defaultPort
	if configuredPort, found := configuration.value("port"); found {
		parsed, err := strconv.ParseUint(configuredPort, 10, 16)
		if err != nil || parsed == 0 {
			return databaseTarget{}, false
		}
		port = uint16(parsed)
	}
	sslMode, _ := configuration.value("sslMode")
	return databaseTarget{
		driver: driver, host: host, port: port, database: database,
		user: user, password: password, sslMode: sslMode,
	}, true
}

func parseJDBCURL(configuration *Configuration) (databaseTarget, bool) {
	jdbcURL, found := configuration.required("url")
	if !found || len(jdbcURL) > 4096 || !strings.HasPrefix(jdbcURL, "jdbc:") {
		return databaseTarget{}, false
	}
	parsed, err := url.Parse(strings.TrimPrefix(jdbcURL, "jdbc:"))
	if err != nil || parsed.User != nil || parsed.Fragment != "" || parsed.Hostname() == "" {
		return databaseTarget{}, false
	}
	driver := jdbcKind(255)
	defaultPort := uint16(0)
	switch strings.ToLower(parsed.Scheme) {
	case "postgresql", "postgres":
		driver, defaultPort = jdbcPostgreSQL, 5432
	case "mysql":
		driver, defaultPort = jdbcMySQL, 3306
	default:
		return databaseTarget{}, false
	}
	port := defaultPort
	if parsed.Port() != "" {
		value, parseErr := strconv.ParseUint(parsed.Port(), 10, 16)
		if parseErr != nil || value == 0 {
			return databaseTarget{}, false
		}
		port = uint16(value)
	}
	database, err := url.PathUnescape(strings.TrimPrefix(parsed.EscapedPath(), "/"))
	if err != nil || database == "" || len(database) > 128 || strings.ContainsAny(database, "/\x00\r\n") {
		return databaseTarget{}, false
	}
	sslMode := ""
	for key, values := range parsed.Query() {
		if (driver != jdbcPostgreSQL || !strings.EqualFold(key, "sslmode")) &&
			(driver != jdbcMySQL || !strings.EqualFold(key, "sslMode")) || len(values) != 1 {
			return databaseTarget{}, false
		}
		sslMode = values[0]
	}
	user, _ := configuration.value("user")
	password, _ := configuration.value("password")
	return databaseTarget{
		driver: driver, host: parsed.Hostname(), port: port, database: database,
		user: user, password: password, sslMode: sslMode,
	}, true
}

func probePostgreSQL(ctx context.Context, target databaseTarget, endpoint PinnedEndpoint) error {
	sslMode, ok := postgresSSLMode(target.sslMode)
	if !ok {
		return errInvalidProbeConfiguration
	}
	dsn := &url.URL{
		Scheme: "postgres", User: url.UserPassword(target.user, target.password),
		Host: net.JoinHostPort(target.host, strconv.Itoa(int(target.port))), Path: target.database,
	}
	query := dsn.Query()
	query.Set("sslmode", sslMode)
	dsn.RawQuery = query.Encode()
	configuration, err := pgx.ParseConfig(dsn.String())
	if err != nil {
		return errInvalidProbeConfiguration
	}
	configuration.LookupFunc = func(context.Context, string) ([]string, error) {
		addresses := make([]string, len(endpoint.addresses))
		for index, address := range endpoint.addresses {
			addresses[index] = address.String()
		}
		return addresses, nil
	}
	configuration.DialFunc = endpoint.DialContext
	configuration.RuntimeParams["application_name"] = "astrasync-connection-test"
	configuration.RuntimeParams["default_transaction_read_only"] = "on"
	configuration.RuntimeParams["statement_timeout"] = "1000"
	connectionValue, err := pgx.ConnectConfig(ctx, configuration)
	if err != nil {
		return err
	}
	closeContext, cancel := context.WithTimeout(context.WithoutCancel(ctx), time.Second)
	defer cancel()
	return connectionValue.Close(closeContext)
}

func probeMySQL(ctx context.Context, target databaseTarget, endpoint PinnedEndpoint) error {
	tlsConfig, allowFallback, ok := mysqlTLSMode(target.sslMode, endpoint.ServerName())
	if !ok {
		return errInvalidProbeConfiguration
	}
	network := fmt.Sprintf("astrasync-test-%d", mysqlDialSequence.Add(1))
	mysql.RegisterDialContext(network, func(ctx context.Context, address string) (net.Conn, error) {
		return endpoint.DialContext(ctx, "tcp", address)
	})
	defer mysql.DeregisterDialContext(network)
	configuration := mysql.NewConfig()
	configuration.User = target.user
	configuration.Passwd = target.password
	configuration.Net = network
	configuration.Addr = net.JoinHostPort(target.host, strconv.Itoa(int(target.port)))
	configuration.DBName = target.database
	configuration.Timeout = endpoint.dialTimeout
	configuration.ReadTimeout = endpoint.dialTimeout
	configuration.WriteTimeout = endpoint.dialTimeout
	configuration.TLS = tlsConfig
	configuration.AllowFallbackToPlaintext = allowFallback
	configuration.AllowAllFiles = false
	configuration.AllowCleartextPasswords = false
	configuration.AllowOldPasswords = false
	configuration.MultiStatements = false
	connector, err := mysql.NewConnector(configuration)
	if err != nil {
		return errInvalidProbeConfiguration
	}
	database := sql.OpenDB(connector)
	database.SetMaxOpenConns(1)
	database.SetMaxIdleConns(0)
	defer database.Close()
	return database.PingContext(ctx)
}

var errInvalidProbeConfiguration = errors.New("Connection test probe configuration is invalid")

func postgresSSLMode(value string) (string, bool) {
	value = strings.ToLower(strings.TrimSpace(value))
	if value == "" {
		return "prefer", true
	}
	switch value {
	case "disable", "allow", "prefer", "require", "verify-ca", "verify-full":
		return value, true
	default:
		return "", false
	}
}

func mysqlTLSMode(value, serverName string) (*tls.Config, bool, bool) {
	value = strings.ToLower(strings.ReplaceAll(strings.TrimSpace(value), "_", "-"))
	switch value {
	case "", "preferred":
		return &tls.Config{MinVersion: tls.VersionTLS12, InsecureSkipVerify: true}, true, true
	case "disabled", "disable":
		return nil, false, true
	case "required", "require":
		return &tls.Config{MinVersion: tls.VersionTLS12, InsecureSkipVerify: true}, false, true
	case "verify-ca", "verify-identity", "verify-full":
		return &tls.Config{MinVersion: tls.VersionTLS12, ServerName: serverName}, false, true
	default:
		return nil, false, false
	}
}

func classifyProbeError(ctx context.Context, err error) ProbeResult {
	if ctx.Err() != nil || errors.Is(err, context.DeadlineExceeded) {
		return TimedOutProbe(connection.TestPhaseTransport)
	}
	if errors.Is(err, ErrPolicyDenied) {
		return FailedProbe(
			connection.TestPhasePolicy, connection.TestResultPolicyDenied,
			"connection.test.egress_policy",
		)
	}
	if errors.Is(err, ErrDialFailed) {
		return FailedProbe(
			connection.TestPhaseTransport, connection.TestResultTransportFailed,
			"connection.test.transport",
		)
	}
	var unknownAuthority x509.UnknownAuthorityError
	var certificateInvalid x509.CertificateInvalidError
	var recordHeader tls.RecordHeaderError
	if errors.As(err, &unknownAuthority) || errors.As(err, &certificateInvalid) || errors.As(err, &recordHeader) {
		return FailedProbe(
			connection.TestPhaseTLS, connection.TestResultTLSFailed,
			"connection.test.tls",
		)
	}
	var postgresError *pgconn.PgError
	if errors.As(err, &postgresError) && (postgresError.Code == "28P01" || postgresError.Code == "28000") {
		return FailedProbe(
			connection.TestPhaseAuthentication, connection.TestResultAuthenticationFailed,
			"connection.test.authentication",
		)
	}
	var mysqlError *mysql.MySQLError
	if errors.As(err, &mysqlError) && (mysqlError.Number == 1044 || mysqlError.Number == 1045 || mysqlError.Number == 1698) {
		return FailedProbe(
			connection.TestPhaseAuthentication, connection.TestResultAuthenticationFailed,
			"connection.test.authentication",
		)
	}
	if errors.Is(err, errInvalidProbeConfiguration) {
		return FailedProbe(
			connection.TestPhaseHandshake, connection.TestResultHandshakeFailed,
			"connection.test.configuration",
		)
	}
	return FailedProbe(
		connection.TestPhaseHandshake, connection.TestResultHandshakeFailed,
		"connection.test.handshake",
	)
}
