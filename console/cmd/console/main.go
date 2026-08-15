package main

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/base64"
	"errors"
	"fmt"
	"log"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/protobuf/types/known/emptypb"

	"io.astrasync/console/internal/authflow"
	"io.astrasync/console/internal/oidc"
	"io.astrasync/console/internal/server"
	jobv1 "io.astrasync/control-plane/api-server/gen/go/v1"
	"io.astrasync/control-plane/auth"
	authpostgres "io.astrasync/control-plane/auth/postgres"
	"io.astrasync/control-plane/auth/transport"
)

const shutdownTimeout = 10 * time.Second

type config struct {
	grpcEndpoint       string
	httpListen         string
	environment        string
	authMode           string
	namespace          string
	developmentTenant  string
	databaseURL        string
	publicOrigin       string
	oidcIssuer         string
	oidcAudience       string
	oidcClientID       string
	oidcClientSecret   string
	oidcScopes         []string
	sessionKey         []byte
	sessionIdleTTL     time.Duration
	sessionAbsoluteTTL time.Duration
	loginTTL           time.Duration
	apiTLSCAFile       string
	apiTLSServerName   string
	tlsCertificateFile string
	tlsPrivateKeyFile  string
	trustedProxyCIDRs  string
}

type grpcBackend struct {
	jobs        jobv1.JobServiceClient
	validation  jobv1.JobValidationServiceClient
	catalog     jobv1.ConnectorCatalogServiceClient
	connections jobv1.ConnectionServiceClient
	audit       jobv1.AuditServiceClient
}

func (b grpcBackend) ListJobs(ctx context.Context, request *jobv1.ListJobsRequest) (*jobv1.ListJobsResponse, error) {
	return b.jobs.ListJobs(ctx, request)
}
func (b grpcBackend) GetJob(ctx context.Context, request *jobv1.GetJobRequest) (*jobv1.Job, error) {
	return b.jobs.GetJob(ctx, request)
}
func (b grpcBackend) GetJobStatus(ctx context.Context, request *jobv1.GetJobStatusRequest) (*jobv1.JobStatus, error) {
	return b.jobs.GetJobStatus(ctx, request)
}
func (b grpcBackend) CreateJob(ctx context.Context, request *jobv1.CreateJobRequest) (*jobv1.Job, error) {
	return b.jobs.CreateJob(ctx, request)
}
func (b grpcBackend) UpdateJob(ctx context.Context, request *jobv1.UpdateJobRequest) (*jobv1.Job, error) {
	return b.jobs.UpdateJob(ctx, request)
}
func (b grpcBackend) DeleteJob(ctx context.Context, request *jobv1.DeleteJobRequest) (*emptypb.Empty, error) {
	return b.jobs.DeleteJob(ctx, request)
}
func (b grpcBackend) StartJob(ctx context.Context, request *jobv1.StartJobRequest) (*jobv1.Job, error) {
	return b.jobs.StartJob(ctx, request)
}
func (b grpcBackend) StopJob(ctx context.Context, request *jobv1.StopJobRequest) (*jobv1.Job, error) {
	return b.jobs.StopJob(ctx, request)
}
func (b grpcBackend) ValidateJobSpec(ctx context.Context, request *jobv1.ValidateJobSpecRequest) (*jobv1.JobValidationResult, error) {
	return b.validation.ValidateJobSpec(ctx, request)
}
func (b grpcBackend) ListConnectorDescriptors(ctx context.Context, request *jobv1.ListConnectorDescriptorsRequest) (*jobv1.ListConnectorDescriptorsResponse, error) {
	return b.catalog.ListConnectorDescriptors(ctx, request)
}
func (b grpcBackend) GetConnectorDescriptor(ctx context.Context, request *jobv1.GetConnectorDescriptorRequest) (*jobv1.GetConnectorDescriptorResponse, error) {
	return b.catalog.GetConnectorDescriptor(ctx, request)
}
func (b grpcBackend) CreateConnection(ctx context.Context, request *jobv1.CreateConnectionRequest) (*jobv1.Connection, error) {
	return b.connections.CreateConnection(ctx, request)
}
func (b grpcBackend) GetConnection(ctx context.Context, request *jobv1.GetConnectionRequest) (*jobv1.Connection, error) {
	return b.connections.GetConnection(ctx, request)
}
func (b grpcBackend) ListConnections(ctx context.Context, request *jobv1.ListConnectionsRequest) (*jobv1.ListConnectionsResponse, error) {
	return b.connections.ListConnections(ctx, request)
}
func (b grpcBackend) UpdateConnection(ctx context.Context, request *jobv1.UpdateConnectionRequest) (*jobv1.Connection, error) {
	return b.connections.UpdateConnection(ctx, request)
}
func (b grpcBackend) RotateConnection(ctx context.Context, request *jobv1.RotateConnectionRequest) (*jobv1.Connection, error) {
	return b.connections.RotateConnection(ctx, request)
}
func (b grpcBackend) EnableConnection(ctx context.Context, request *jobv1.EnableConnectionRequest) (*jobv1.Connection, error) {
	return b.connections.EnableConnection(ctx, request)
}
func (b grpcBackend) DisableConnection(ctx context.Context, request *jobv1.DisableConnectionRequest) (*jobv1.Connection, error) {
	return b.connections.DisableConnection(ctx, request)
}
func (b grpcBackend) DeleteConnection(ctx context.Context, request *jobv1.DeleteConnectionRequest) (*jobv1.DeleteConnectionResponse, error) {
	return b.connections.DeleteConnection(ctx, request)
}
func (b grpcBackend) TestConnection(ctx context.Context, request *jobv1.TestConnectionRequest) (*jobv1.ConnectionTest, error) {
	return b.connections.TestConnection(ctx, request)
}
func (b grpcBackend) GetConnectionTest(ctx context.Context, request *jobv1.GetConnectionTestRequest) (*jobv1.ConnectionTest, error) {
	return b.connections.GetConnectionTest(ctx, request)
}
func (b grpcBackend) ListAuditEvents(ctx context.Context, request *jobv1.ListAuditEventsRequest) (*jobv1.ListAuditEventsResponse, error) {
	return b.audit.ListAuditEvents(ctx, request)
}

func main() {
	configuration, err := loadConfig(os.Getenv)
	if err != nil {
		log.Fatal(err)
	}
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	if err := run(ctx, configuration); err != nil {
		log.Fatal(err)
	}
}

func loadConfig(getenv func(string) string) (config, error) {
	environment := strings.ToLower(valueOrDefault(getenv("APP_ENV"), "development"))
	if environment != "development" && environment != "test" && environment != "production" {
		return config{}, fmt.Errorf("APP_ENV must be development, test, or production")
	}
	authMode := strings.ToLower(valueOrDefault(getenv("CONSOLE_AUTH_MODE"), "disabled"))
	if authMode != "disabled" && authMode != "oidc" {
		return config{}, fmt.Errorf("CONSOLE_AUTH_MODE must be disabled or oidc")
	}
	namespace := strings.TrimSpace(getenv("CONSOLE_NAMESPACE"))
	if authMode == "disabled" && namespace == "" {
		namespace = "default"
	}
	configuration := config{
		grpcEndpoint: valueOrDefault(getenv("ASTRASYNC_API_GRPC_ENDPOINT"), "127.0.0.1:50051"),
		httpListen:   valueOrDefault(getenv("CONSOLE_LISTEN_ADDRESS"), ":8090"), environment: environment,
		authMode: authMode, namespace: namespace,
		developmentTenant: valueOrDefault(getenv("CONSOLE_TENANT_ID"), "00000000-0000-4000-8000-000000000001"),
		databaseURL:       getenv("DATABASE_URL"), oidcIssuer: getenv("OIDC_ISSUER"),
		oidcAudience: getenv("OIDC_AUDIENCE"), oidcClientID: getenv("CONSOLE_OIDC_CLIENT_ID"),
		oidcClientSecret:   getenv("CONSOLE_OIDC_CLIENT_SECRET"),
		apiTLSCAFile:       getenv("CONSOLE_API_TLS_CA_FILE"),
		apiTLSServerName:   valueOrDefault(getenv("CONSOLE_API_TLS_SERVER_NAME"), "api-server"),
		tlsCertificateFile: strings.TrimSpace(getenv("CONSOLE_TLS_CERTIFICATE_FILE")),
		tlsPrivateKeyFile:  strings.TrimSpace(getenv("CONSOLE_TLS_PRIVATE_KEY_FILE")),
		trustedProxyCIDRs:  strings.TrimSpace(getenv("TRUSTED_PROXY_CIDRS")),
	}
	configuration.oidcScopes = strings.Fields(valueOrDefault(getenv("CONSOLE_OIDC_SCOPES"), "openid profile"))
	var err error
	configuration.sessionIdleTTL, err = boundedDuration(valueOrDefault(getenv("CONSOLE_SESSION_IDLE_TTL"), "30m"), "CONSOLE_SESSION_IDLE_TTL", time.Minute, 24*time.Hour)
	if err != nil {
		return config{}, err
	}
	configuration.sessionAbsoluteTTL, err = boundedDuration(valueOrDefault(getenv("CONSOLE_SESSION_ABSOLUTE_TTL"), "8h"), "CONSOLE_SESSION_ABSOLUTE_TTL", configuration.sessionIdleTTL, 7*24*time.Hour)
	if err != nil {
		return config{}, err
	}
	configuration.loginTTL, err = boundedDuration(valueOrDefault(getenv("CONSOLE_LOGIN_TTL"), "10m"), "CONSOLE_LOGIN_TTL", time.Minute, 30*time.Minute)
	if err != nil {
		return config{}, err
	}
	if authMode == "oidc" {
		configuration.publicOrigin, err = publicOrigin(getenv("CONSOLE_PUBLIC_URL"))
		if err != nil {
			return config{}, err
		}
		configuration.sessionKey, err = decodeSessionKey(getenv("CONSOLE_SESSION_KEY"))
		if err != nil {
			return config{}, err
		}
		if configuration.databaseURL == "" || configuration.oidcIssuer == "" || configuration.oidcAudience == "" || configuration.oidcClientID == "" {
			return config{}, fmt.Errorf("DATABASE_URL, OIDC_ISSUER, OIDC_AUDIENCE, and CONSOLE_OIDC_CLIENT_ID are required in OIDC mode")
		}
	}
	if environment == "production" {
		if authMode != "oidc" {
			return config{}, fmt.Errorf("production requires CONSOLE_AUTH_MODE=oidc")
		}
		if configuration.apiTLSCAFile == "" {
			return config{}, fmt.Errorf("production requires Console-to-API TLS")
		}
		if (configuration.tlsCertificateFile == "") != (configuration.tlsPrivateKeyFile == "") {
			return config{}, fmt.Errorf("CONSOLE_TLS_CERTIFICATE_FILE and CONSOLE_TLS_PRIVATE_KEY_FILE must be configured together")
		}
		if configuration.tlsCertificateFile == "" {
			return config{}, fmt.Errorf("production requires CONSOLE_TLS_CERTIFICATE_FILE and CONSOLE_TLS_PRIVATE_KEY_FILE")
		}
		if configuration.trustedProxyCIDRs == "" {
			return config{}, fmt.Errorf("production requires TRUSTED_PROXY_CIDRS")
		}
	}
	if configuration.trustedProxyCIDRs != "" {
		if _, err := transport.ParseCIDRList(configuration.trustedProxyCIDRs); err != nil {
			return config{}, fmt.Errorf("TRUSTED_PROXY_CIDRS: %w", err)
		}
	}
	return configuration, nil
}

func run(ctx context.Context, configuration config) error {
	dialOptions, err := apiDialOptions(configuration)
	if err != nil {
		return err
	}
	connection, err := grpc.NewClient(configuration.grpcEndpoint, dialOptions...)
	if err != nil {
		return fmt.Errorf("dial control-plane gRPC: %w", err)
	}
	defer connection.Close()
	backend := grpcBackend{jobs: jobv1.NewJobServiceClient(connection), validation: jobv1.NewJobValidationServiceClient(connection),
		catalog: jobv1.NewConnectorCatalogServiceClient(connection), connections: jobv1.NewConnectionServiceClient(connection),
		audit: jobv1.NewAuditServiceClient(connection)}

	var sessionManager server.SessionManager
	var authRepository *authpostgres.Repository
	if configuration.authMode == "oidc" {
		authRepository, err = authpostgres.Open(ctx, configuration.databaseURL)
		if err != nil {
			return err
		}
		defer authRepository.Close()
		if err := authRepository.Migrate(ctx); err != nil {
			return err
		}
		store, err := authpostgres.NewSessionStore(authRepository, configuration.sessionKey)
		if err != nil {
			return err
		}
		provider, err := oidc.New(ctx, oidc.Config{Issuer: configuration.oidcIssuer, Audience: configuration.oidcAudience,
			ClientID: configuration.oidcClientID, ClientSecret: configuration.oidcClientSecret,
			RedirectURL: configuration.publicOrigin + "/auth/callback", Scopes: configuration.oidcScopes})
		if err != nil {
			return err
		}
		sessionManager, err = authflow.New(provider, store, authRepository, authRepository, authflow.Config{
			IdleTTL: configuration.sessionIdleTTL, AbsoluteTTL: configuration.sessionAbsoluteTTL, LoginTTL: configuration.loginTTL,
		})
		if err != nil {
			return err
		}
	} else {
		sessionManager, err = authflow.NewDevelopmentManager(configuration.developmentTenant, configuration.namespace)
		if err != nil {
			return err
		}
	}

	ready := func(ctx context.Context) error {
		if authRepository != nil {
			return authRepository.Ping(ctx)
		}
		return nil
	}
	trustedProxyPrefixes, err := loadTrustedProxyPrefixes(configuration)
	if err != nil {
		return fmt.Errorf("configure trusted-proxy boundary: %w", err)
	}
	console, err := server.NewWithConfig(server.Config{Backend: backend, Sessions: sessionManager,
		Namespace: configuration.namespace, PublicOrigin: configuration.publicOrigin, AuthMode: configuration.authMode, Ready: ready})
	if err != nil {
		return fmt.Errorf("create Console server: %w", err)
	}
	listener, err := net.Listen("tcp", configuration.httpListen)
	if err != nil {
		return fmt.Errorf("listen for Console HTTP: %w", err)
	}
	handler := transport.TrustedProxyMiddleware(trustedProxyPrefixes)(
		transport.SecurityHeaders()(console.Handler()),
	)
	httpServer := &http.Server{Handler: handler, ReadHeaderTimeout: 5 * time.Second, IdleTimeout: 60 * time.Second}
	errorsChannel := make(chan error, 1)
	go func() {
		var serveErr error
		if configuration.tlsCertificateFile != "" {
			serveErr = httpServer.ServeTLS(listener, configuration.tlsCertificateFile, configuration.tlsPrivateKeyFile)
		} else {
			serveErr = httpServer.Serve(listener)
		}
		if serveErr != nil && !errors.Is(serveErr, http.ErrServerClosed) {
			errorsChannel <- fmt.Errorf("serve Console HTTP: %w", serveErr)
		}
	}()
	select {
	case <-ctx.Done():
	case serveErr := <-errorsChannel:
		_ = httpServer.Close()
		return serveErr
	}
	shutdownContext, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
	defer cancel()
	if err := httpServer.Shutdown(shutdownContext); err != nil {
		return fmt.Errorf("shut down Console HTTP: %w", err)
	}
	return nil
}

func apiDialOptions(configuration config) ([]grpc.DialOption, error) {
	if configuration.apiTLSCAFile == "" {
		return []grpc.DialOption{grpc.WithTransportCredentials(insecure.NewCredentials())}, nil
	}
	pem, err := os.ReadFile(configuration.apiTLSCAFile)
	if err != nil {
		return nil, fmt.Errorf("read Console API CA: %w", err)
	}
	pool := x509.NewCertPool()
	if !pool.AppendCertsFromPEM(pem) {
		return nil, fmt.Errorf("Console API CA is invalid")
	}
	return []grpc.DialOption{grpc.WithTransportCredentials(credentials.NewTLS(&tls.Config{
		MinVersion: tls.VersionTLS12, RootCAs: pool, ServerName: configuration.apiTLSServerName,
	}))}, nil
}

func loadTrustedProxyPrefixes(configuration config) ([]netip.Prefix, error) {
	if configuration.trustedProxyCIDRs == "" {
		return nil, nil
	}
	return transport.ParseCIDRList(configuration.trustedProxyCIDRs)
}

func publicOrigin(value string) (string, error) {
	parsed, err := url.Parse(value)
	if err != nil || parsed.Scheme != "https" || parsed.Host == "" ||
		(parsed.Path != "" && parsed.Path != "/") || parsed.RawQuery != "" || parsed.Fragment != "" {
		return "", fmt.Errorf("CONSOLE_PUBLIC_URL must be an HTTPS origin")
	}
	return strings.TrimRight(parsed.String(), "/"), nil
}

func decodeSessionKey(value string) ([]byte, error) {
	for _, encoding := range []*base64.Encoding{base64.RawStdEncoding, base64.StdEncoding, base64.RawURLEncoding} {
		decoded, err := encoding.DecodeString(value)
		if err == nil && len(decoded) >= 32 {
			return decoded, nil
		}
	}
	if len(value) >= 32 {
		return []byte(value), nil
	}
	return nil, fmt.Errorf("CONSOLE_SESSION_KEY must contain at least 32 bytes")
}

func boundedDuration(value, label string, minimum, maximum time.Duration) (time.Duration, error) {
	parsed, err := time.ParseDuration(value)
	if err != nil || parsed < minimum || parsed > maximum {
		return 0, fmt.Errorf("%s must be between %s and %s", label, minimum, maximum)
	}
	return parsed, nil
}

func valueOrDefault(value, defaultValue string) string {
	if value == "" {
		return defaultValue
	}
	return value
}

var _ auth.IdentityResolver = (*authpostgres.Repository)(nil)
var _ server.JobReader = grpcBackend{}
var _ server.JobMutationClient = grpcBackend{}
var _ server.JobValidator = grpcBackend{}
var _ server.CatalogReader = grpcBackend{}
var _ server.ConnectionClient = grpcBackend{}
var _ server.AuditReader = grpcBackend{}
