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
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/google/uuid"
	"github.com/grpc-ecosystem/grpc-gateway/v2/runtime"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/reflection"
	"google.golang.org/protobuf/proto"

	controlv1 "io.astrasync/control-plane/api-server/gen/go/v1"
	"io.astrasync/control-plane/api-server/internal/authn"
	"io.astrasync/control-plane/api-server/internal/catalogproto"
	"io.astrasync/control-plane/api-server/internal/compilerclient"
	"io.astrasync/control-plane/api-server/internal/service"
	"io.astrasync/control-plane/auth"
	authpostgres "io.astrasync/control-plane/auth/postgres"
	"io.astrasync/control-plane/auth/transport"
	"io.astrasync/control-plane/catalog"
	catalogpostgres "io.astrasync/control-plane/catalog/postgres"
	"io.astrasync/control-plane/connection"
	connectionpostgres "io.astrasync/control-plane/connection/postgres"
	jobpostgres "io.astrasync/control-plane/job/postgres"
)

const shutdownTimeout = 10 * time.Second

type config struct {
	databaseURL                string
	grpcListen                 string
	grpcEndpoint               string
	httpListen                 string
	environment                string
	authMode                   string
	oidcIssuer                 string
	oidcAudience               string
	catalogPath                string
	executionProfile           string
	catalogTokenKey            []byte
	compilerEndpoint           string
	compilerTimeout            time.Duration
	compilerCertFile           string
	compilerKeyFile            string
	compilerCAFile             string
	compilerServerName         string
	tlsCertificateFile         string
	tlsPrivateKeyFile          string
	tlsServerName              string
	trustedProxyCIDRs          string
	connectionTestDeadline     time.Duration
	connectionTestPolicies     map[string]connection.TestEgressPolicy
	connectionMutationsEnabled bool
	connectionTestsEnabled     bool
	connectionRuntimeEnabled   bool
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
	databaseURL := getenv("DATABASE_URL")
	if databaseURL == "" {
		return config{}, fmt.Errorf("DATABASE_URL must be configured")
	}
	environment := strings.ToLower(valueOrDefault(getenv("APP_ENV"), "development"))
	if environment != "development" && environment != "test" && environment != "production" {
		return config{}, fmt.Errorf("APP_ENV must be development, test, or production")
	}
	authMode := strings.ToLower(valueOrDefault(getenv("AUTH_MODE"), "disabled"))
	if authMode != "disabled" && authMode != "oidc" {
		return config{}, fmt.Errorf("AUTH_MODE must be disabled or oidc")
	}
	tokenKeyValue := getenv("CATALOG_TOKEN_KEY")
	tokenKey, err := decodeSecretKey(tokenKeyValue)
	if err != nil {
		return config{}, err
	}
	certificateFile := getenv("TLS_CERTIFICATE_FILE")
	privateKeyFile := getenv("TLS_PRIVATE_KEY_FILE")
	if (certificateFile == "") != (privateKeyFile == "") {
		return config{}, fmt.Errorf("TLS certificate and private key must be configured together")
	}
	if authMode == "oidc" && (getenv("OIDC_ISSUER") == "" || getenv("OIDC_AUDIENCE") == "") {
		return config{}, fmt.Errorf("OIDC_ISSUER and OIDC_AUDIENCE are required in oidc mode")
	}
	trustedProxyCIDRs := strings.TrimSpace(getenv("TRUSTED_PROXY_CIDRS"))
	if environment == "production" && trustedProxyCIDRs == "" {
		return config{}, fmt.Errorf("production requires TRUSTED_PROXY_CIDRS")
	}
	if environment == "production" {
		if authMode != "oidc" {
			return config{}, fmt.Errorf("production requires AUTH_MODE=oidc")
		}
		if certificateFile == "" {
			return config{}, fmt.Errorf("production requires TLS certificate and private key")
		}
		if tokenKeyValue == "" {
			return config{}, fmt.Errorf("production requires CATALOG_TOKEN_KEY")
		}
	}
	if trustedProxyCIDRs != "" {
		if _, err := transport.ParseCIDRList(trustedProxyCIDRs); err != nil {
			return config{}, fmt.Errorf("TRUSTED_PROXY_CIDRS: %w", err)
		}
	}
	compilerTimeout, err := boundedDuration(
		valueOrDefault(getenv("COMPILER_VALIDATION_TIMEOUT"), "3s"),
		"COMPILER_VALIDATION_TIMEOUT", 100*time.Millisecond, 30*time.Second,
	)
	if err != nil {
		return config{}, err
	}
	connectionTestDeadline, err := boundedDuration(
		valueOrDefault(getenv("CONNECTION_TEST_DEADLINE"), "30s"),
		"CONNECTION_TEST_DEADLINE", time.Second, 2*time.Minute,
	)
	if err != nil {
		return config{}, err
	}
	connectionTestPolicies, err := parseConnectionTestPolicies(
		getenv("CONNECTION_TEST_TENANT_EGRESS_POLICIES"),
	)
	if err != nil {
		return config{}, err
	}
	connectionMutationsEnabled, err := booleanSetting(
		getenv("CONNECTION_MUTATIONS_ENABLED"), "CONNECTION_MUTATIONS_ENABLED", false,
	)
	if err != nil {
		return config{}, err
	}
	connectionTestsEnabled, err := booleanSetting(
		getenv("CONNECTION_TESTS_ENABLED"), "CONNECTION_TESTS_ENABLED", false,
	)
	if err != nil {
		return config{}, err
	}
	connectionRuntimeEnabled, err := booleanSetting(
		getenv("CONNECTION_RUNTIME_ENABLED"), "CONNECTION_RUNTIME_ENABLED", false,
	)
	if err != nil {
		return config{}, err
	}
	compilerCertFile := getenv("COMPILER_VALIDATION_TLS_CERTIFICATE_FILE")
	compilerKeyFile := getenv("COMPILER_VALIDATION_TLS_PRIVATE_KEY_FILE")
	compilerCAFile := getenv("COMPILER_VALIDATION_TLS_CA_FILE")
	compilerTLSFields := 0
	for _, value := range []string{compilerCertFile, compilerKeyFile, compilerCAFile} {
		if value != "" {
			compilerTLSFields++
		}
	}
	if compilerTLSFields != 0 && compilerTLSFields != 3 {
		return config{}, fmt.Errorf("compiler validation TLS certificate, private key, and CA must be configured together")
	}
	if environment == "production" && compilerTLSFields != 3 {
		return config{}, fmt.Errorf("production requires mutual TLS for compiler validation")
	}
	return config{
		databaseURL: databaseURL, grpcListen: valueOrDefault(getenv("GRPC_LISTEN_ADDRESS"), ":50051"),
		grpcEndpoint: valueOrDefault(getenv("GRPC_GATEWAY_ENDPOINT"), "127.0.0.1:50051"),
		httpListen:   valueOrDefault(getenv("HTTP_LISTEN_ADDRESS"), ":8080"),
		environment:  environment, authMode: authMode,
		oidcIssuer: getenv("OIDC_ISSUER"), oidcAudience: getenv("OIDC_AUDIENCE"),
		catalogPath:      valueOrDefault(getenv("CONNECTOR_INVENTORY_PATH"), defaultCatalogPath()),
		executionProfile: valueOrDefault(getenv("CONNECTOR_EXECUTION_PROFILE"), "standard"),
		catalogTokenKey:  tokenKey, tlsCertificateFile: certificateFile,
		compilerEndpoint: valueOrDefault(getenv("COMPILER_VALIDATION_ENDPOINT"), "127.0.0.1:50052"),
		compilerTimeout:  compilerTimeout, compilerCertFile: compilerCertFile,
		compilerKeyFile: compilerKeyFile, compilerCAFile: compilerCAFile,
		compilerServerName:         valueOrDefault(getenv("COMPILER_VALIDATION_TLS_SERVER_NAME"), "compiler-validation"),
		tlsPrivateKeyFile:          privateKeyFile,
		tlsServerName:              valueOrDefault(getenv("TLS_SERVER_NAME"), "localhost"),
		trustedProxyCIDRs:          trustedProxyCIDRs,
		connectionTestDeadline:     connectionTestDeadline,
		connectionTestPolicies:     connectionTestPolicies,
		connectionMutationsEnabled: connectionMutationsEnabled,
		connectionTestsEnabled:     connectionTestsEnabled,
		connectionRuntimeEnabled:   connectionRuntimeEnabled,
	}, nil
}

func run(ctx context.Context, configuration config) error {
	jobRepository, err := jobpostgres.Open(ctx, configuration.databaseURL)
	if err != nil {
		return err
	}
	defer jobRepository.Close()
	if err := jobRepository.Migrate(ctx); err != nil {
		return err
	}
	authRepository, err := authpostgres.Open(ctx, configuration.databaseURL)
	if err != nil {
		return err
	}
	defer authRepository.Close()
	if err := authRepository.Migrate(ctx); err != nil {
		return err
	}
	catalogRepository, err := catalogpostgres.Open(ctx, configuration.databaseURL)
	if err != nil {
		return err
	}
	defer catalogRepository.Close()
	if err := catalogRepository.Migrate(ctx); err != nil {
		return err
	}
	connectionRepository, err := connectionpostgres.Open(ctx, configuration.databaseURL)
	if err != nil {
		return err
	}
	defer connectionRepository.Close()
	if err := connectionRepository.Migrate(ctx); err != nil {
		return err
	}
	if err := jobRepository.MigrateMutations(ctx); err != nil {
		return err
	}
	compilerDialOptions, err := compilerClientDialOptions(configuration)
	if err != nil {
		return err
	}
	compilerConnection, err := grpc.NewClient(configuration.compilerEndpoint, compilerDialOptions...)
	if err != nil {
		return fmt.Errorf("connect to compiler validation service: %w", err)
	}
	defer compilerConnection.Close()
	compilerClient, err := compilerclient.New(compilerConnection, configuration.compilerTimeout)
	if err != nil {
		return fmt.Errorf("create compiler validation client: %w", err)
	}
	if err := reconcileCompilerCatalog(ctx, configuration, catalogRepository, compilerClient); err != nil {
		return err
	}

	var authorizer auth.Authorizer = auth.DevelopmentAuthorizer{}
	grpcOptions := make([]grpc.ServerOption, 0, 2)
	registry := authn.NewRegistry()
	if err := registry.ValidateServices(
		controlv1.JobService_ServiceDesc,
		controlv1.JobValidationService_ServiceDesc,
		controlv1.ConnectorCatalogService_ServiceDesc,
		controlv1.ConnectionService_ServiceDesc,
		controlv1.AuditService_ServiceDesc,
		controlv1.IdentityService_ServiceDesc,
		controlv1.AccessService_ServiceDesc,
	); err != nil {
		return fmt.Errorf("validate API authorization registry: %w", err)
	}
	if configuration.authMode == "oidc" {
		validator, err := auth.NewOIDCValidator(auth.OIDCConfig{
			Issuer: configuration.oidcIssuer, Audience: configuration.oidcAudience,
			AcceptedTokenTypes: []string{"JWT", "at+jwt", "application/at+jwt"},
		})
		if err != nil {
			return fmt.Errorf("configure OIDC validation: %w", err)
		}
		contextAuthorizer := auth.ContextAuthorizer{CurrentPolicyRevision: authRepository.CurrentPolicyRevision}
		authorizer = contextAuthorizer
		interceptor := authn.Interceptor{
			Authenticator: auth.BearerAuthenticator{Validator: validator, Resolver: authRepository},
			Authorizer:    contextAuthorizer, AuditWriter: authRepository, Registry: registry,
			Clock: time.Now, EventID: uuid.NewString,
		}
		if err := interceptor.Validate(); err != nil {
			return err
		}
		grpcOptions = append(grpcOptions, grpc.UnaryInterceptor(interceptor.Unary()))
	}
	catalogService, err := service.NewConnectorCatalogService(
		catalogRepository, authorizer, configuration.executionProfile,
		configuration.catalogTokenKey, time.Now,
	)
	if err != nil {
		return fmt.Errorf("create connector catalog service: %w", err)
	}
	connectionService, err := service.NewConnectionService(
		connectionRepository, catalogRepository, authorizer, configuration.executionProfile,
		configuration.catalogTokenKey, time.Now, uuid.NewString,
		service.WithConnectionTestDeadline(configuration.connectionTestDeadline),
		service.WithConnectionMutationsEnabled(configuration.connectionMutationsEnabled),
		service.WithConnectionTestsEnabled(configuration.connectionTestsEnabled),
		service.WithConnectionTestPolicyResolver(
			newStaticConnectionTestPolicies(configuration.connectionTestPolicies),
		),
	)
	if err != nil {
		return fmt.Errorf("create Connection service: %w", err)
	}
	jobValidationService, err := service.NewJobValidationService(
		jobRepository, connectionRepository, catalogRepository, authorizer, compilerClient,
		configuration.executionProfile, uuid.NewString,
		service.WithConnectionRuntimeEnabled(configuration.connectionRuntimeEnabled),
	)
	if err != nil {
		return fmt.Errorf("create Job validation service: %w", err)
	}
	jobService, err := service.NewTransactionalJobService(
		jobRepository, jobValidationService, authorizer, configuration.catalogTokenKey,
		time.Now, uuid.NewString,
	)
	if err != nil {
		return fmt.Errorf("create transactional Job service: %w", err)
	}
	auditService, err := service.NewAuditService(
		authRepository, authorizer, configuration.catalogTokenKey, time.Now, uuid.NewString,
	)
	if err != nil {
		return fmt.Errorf("create audit service: %w", err)
	}
	identityService, err := service.NewIdentityService(authRepository, authorizer,
		service.WithIdentityClock(time.Now),
		service.WithIdentityUIDSource(uuid.NewString),
	)
	if err != nil {
		return fmt.Errorf("create identity service: %w", err)
	}
	accessService, err := service.NewAccessService(authRepository, authorizer,
		service.WithAccessClock(time.Now),
		service.WithAccessUIDSource(uuid.NewString),
	)
	if err != nil {
		return fmt.Errorf("create access service: %w", err)
	}
	trustedProxyPrefixes, err := loadTrustedProxyPrefixes(configuration)
	if err != nil {
		return fmt.Errorf("configure trusted-proxy boundary: %w", err)
	}
	if configuration.tlsCertificateFile == "" {
		if configuration.environment == "production" {
			return fmt.Errorf("production requires TLS certificate and private key for the gRPC listener")
		}
	} else {
		serverCredentials, err := credentials.NewServerTLSFromFile(
			configuration.tlsCertificateFile, configuration.tlsPrivateKeyFile,
		)
		if err != nil {
			return fmt.Errorf("load gRPC TLS identity: %w", err)
		}
		grpcOptions = append(grpcOptions, grpc.Creds(serverCredentials))
	}
	grpcListener, err := net.Listen("tcp", configuration.grpcListen)
	if err != nil {
		return fmt.Errorf("listen for gRPC: %w", err)
	}
	grpcServer := grpc.NewServer(grpcOptions...)
	controlv1.RegisterJobServiceServer(grpcServer, jobService)
	controlv1.RegisterJobValidationServiceServer(grpcServer, jobValidationService)
	controlv1.RegisterConnectorCatalogServiceServer(grpcServer, catalogService)
	controlv1.RegisterConnectionServiceServer(grpcServer, connectionService)
	controlv1.RegisterAuditServiceServer(grpcServer, auditService)
	controlv1.RegisterIdentityServiceServer(grpcServer, identityService)
	controlv1.RegisterAccessServiceServer(grpcServer, accessService)
	if configuration.environment != "production" {
		reflection.Register(grpcServer)
	}

	gateway := runtime.NewServeMux()
	dialOptions, err := gatewayDialOptions(configuration)
	if err != nil {
		grpcListener.Close()
		return err
	}
	for name, register := range map[string]func(context.Context, *runtime.ServeMux, string, []grpc.DialOption) error{
		"JobService":              controlv1.RegisterJobServiceHandlerFromEndpoint,
		"JobValidationService":    controlv1.RegisterJobValidationServiceHandlerFromEndpoint,
		"ConnectorCatalogService": controlv1.RegisterConnectorCatalogServiceHandlerFromEndpoint,
		"ConnectionService":       controlv1.RegisterConnectionServiceHandlerFromEndpoint,
		"AuditService":            controlv1.RegisterAuditServiceHandlerFromEndpoint,
	} {
		if err := register(ctx, gateway, configuration.grpcEndpoint, dialOptions); err != nil {
			grpcListener.Close()
			return fmt.Errorf("register %s REST gateway: %w", name, err)
		}
	}
	httpServer := &http.Server{
		Addr: configuration.httpListen,
		Handler: apiHandler(
			transport.TrustedProxyMiddleware(trustedProxyPrefixes)(
				transport.SecurityHeaders()(gateway),
			),
			func(ctx context.Context) error {
				for _, check := range []func(context.Context) error{
					jobRepository.Ping, authRepository.Ping, catalogRepository.Ping, connectionRepository.Ping,
				} {
					if err := check(ctx); err != nil {
						return err
					}
				}
				_, err := catalogRepository.Current(ctx, configuration.executionProfile)
				return err
			},
		),
		ReadHeaderTimeout: 5 * time.Second,
		IdleTimeout:       60 * time.Second,
	}

	errorsChannel := make(chan error, 2)
	go func() {
		if serveErr := grpcServer.Serve(grpcListener); serveErr != nil {
			errorsChannel <- fmt.Errorf("serve gRPC: %w", serveErr)
		}
	}()
	go func() {
		var serveErr error
		if configuration.tlsCertificateFile != "" {
			serveErr = httpServer.ListenAndServeTLS(configuration.tlsCertificateFile, configuration.tlsPrivateKeyFile)
		} else {
			serveErr = httpServer.ListenAndServe()
		}
		if serveErr != nil && !errors.Is(serveErr, http.ErrServerClosed) {
			errorsChannel <- fmt.Errorf("serve HTTP: %w", serveErr)
		}
	}()

	select {
	case <-ctx.Done():
	case serveErr := <-errorsChannel:
		grpcServer.Stop()
		_ = httpServer.Close()
		return serveErr
	}

	shutdownContext, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
	defer cancel()
	grpcServer.GracefulStop()
	if err := httpServer.Shutdown(shutdownContext); err != nil {
		return fmt.Errorf("shut down HTTP server: %w", err)
	}
	return nil
}

func apiHandler(gateway http.Handler, ping func(context.Context) error) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /health", func(response http.ResponseWriter, _ *http.Request) {
		response.Header().Set("Content-Type", "text/plain; charset=utf-8")
		response.WriteHeader(http.StatusOK)
		_, _ = response.Write([]byte("ok\n"))
	})
	mux.HandleFunc("GET /ready", func(response http.ResponseWriter, request *http.Request) {
		ctx, cancel := context.WithTimeout(request.Context(), time.Second)
		defer cancel()
		if err := ping(ctx); err != nil {
			http.Error(response, "not ready", http.StatusServiceUnavailable)
			return
		}
		response.Header().Set("Content-Type", "text/plain; charset=utf-8")
		response.WriteHeader(http.StatusOK)
		_, _ = response.Write([]byte("ready\n"))
	})
	mux.Handle("/", gateway)
	return mux
}

func valueOrDefault(value, defaultValue string) string {
	if value == "" {
		return defaultValue
	}
	return value
}

func booleanSetting(value, name string, defaultValue bool) (bool, error) {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "":
		return defaultValue, nil
	case "true":
		return true, nil
	case "false":
		return false, nil
	default:
		return false, fmt.Errorf("%s must be true or false", name)
	}
}

func decodeSecretKey(value string) ([]byte, error) {
	if value == "" {
		return []byte("development-only-catalog-token-key"), nil
	}
	for _, encoding := range []*base64.Encoding{base64.RawStdEncoding, base64.StdEncoding, base64.RawURLEncoding} {
		decoded, err := encoding.DecodeString(value)
		if err == nil && len(decoded) >= 32 {
			return decoded, nil
		}
	}
	if len(value) >= 32 {
		return []byte(value), nil
	}
	return nil, fmt.Errorf("CATALOG_TOKEN_KEY must contain at least 32 bytes")
}

func defaultCatalogPath() string {
	for _, candidate := range []string{
		"deployment/catalog/connector-inventory.pb",
		"../../../deployment/catalog/connector-inventory.pb",
		"/app/catalog/connector-inventory.pb",
	} {
		if _, err := os.Stat(candidate); err == nil {
			return candidate
		}
	}
	return "deployment/catalog/connector-inventory.pb"
}

type compilerInventoryClient interface {
	Inventory(context.Context, string) (*controlv1.ConnectorInventory, error)
}

func reconcileCompilerCatalog(
	ctx context.Context,
	configuration config,
	repository catalog.Repository,
	compiler compilerInventoryClient,
) error {
	inventory, err := compiler.Inventory(ctx, configuration.executionProfile)
	if err != nil {
		if _, retainedErr := repository.Current(ctx, configuration.executionProfile); retainedErr == nil {
			log.Printf("compiler inventory publisher unavailable; serving last verified snapshot")
			return nil
		}
		return fmt.Errorf("read deployment connector inventory from compiler: %w", err)
	}
	if inventory.GetExecutionProfile() != configuration.executionProfile {
		return fmt.Errorf("compiler inventory execution profile does not match deployment")
	}
	payload, err := (proto.MarshalOptions{Deterministic: true}).Marshal(inventory)
	if err != nil {
		return fmt.Errorf("encode compiler connector inventory: %w", err)
	}
	expected, readErr := readInventoryArtifact(configuration.catalogPath)
	if readErr == nil {
		if !proto.Equal(inventory, expected) {
			return fmt.Errorf("compiler inventory differs from the deployment inventory artifact")
		}
	} else if !errors.Is(readErr, os.ErrNotExist) {
		return fmt.Errorf("read deployment inventory artifact: %w", readErr)
	}
	reconciler, err := catalog.NewReconciler(repository, catalogproto.Validator{}, time.Now, uuid.NewString)
	if err != nil {
		return fmt.Errorf("create connector catalog reconciler: %w", err)
	}
	if _, _, err := reconciler.Reconcile(
		ctx, payload, "service:compiler-inventory", "startup-"+uuid.NewString(),
	); err != nil {
		return fmt.Errorf("reconcile compiler connector inventory: %w", err)
	}
	return nil
}

func readInventoryArtifact(path string) (*controlv1.ConnectorInventory, error) {
	payload, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	inventory := &controlv1.ConnectorInventory{}
	if err := proto.Unmarshal(payload, inventory); err != nil {
		return nil, fmt.Errorf("decode protobuf inventory: %w", err)
	}
	return inventory, nil
}

func verifyCompilerCatalog(
	ctx context.Context,
	compiler compilerInventoryClient,
	repository catalog.Repository,
	executionProfile string,
) error {
	inventory, err := compiler.Inventory(ctx, executionProfile)
	if err != nil {
		return fmt.Errorf("compiler inventory unavailable: %w", err)
	}
	snapshot, err := repository.Current(ctx, executionProfile)
	if err != nil {
		return err
	}
	if inventory.GetInventoryRevision() != snapshot.InventoryRevision ||
		inventory.GetCompilerRevision() != snapshot.CompilerRevision ||
		inventory.GetExecutionProfile() != snapshot.ExecutionProfile {
		return fmt.Errorf("compiler inventory does not match active catalog")
	}
	return nil
}

func reconcileDeploymentCatalog(
	ctx context.Context, configuration config, repository catalog.Repository,
) error {
	payload, err := os.ReadFile(configuration.catalogPath)
	if err != nil {
		if _, retainedErr := repository.Current(ctx, configuration.executionProfile); retainedErr == nil {
			log.Printf("connector inventory publisher unavailable; serving last verified snapshot")
			return nil
		}
		return fmt.Errorf("read deployment connector inventory: %w", err)
	}
	reconciler, err := catalog.NewReconciler(
		repository, catalogproto.Validator{}, time.Now, uuid.NewString,
	)
	if err != nil {
		return fmt.Errorf("create connector catalog reconciler: %w", err)
	}
	if _, _, err := reconciler.Reconcile(
		ctx, payload, "service:catalog-reconciler", "startup-"+uuid.NewString(),
	); err != nil {
		return fmt.Errorf("reconcile deployment connector inventory: %w", err)
	}
	return nil
}

func loadTrustedProxyPrefixes(configuration config) ([]netip.Prefix, error) {
	if configuration.trustedProxyCIDRs == "" {
		return nil, nil
	}
	prefixes, err := transport.ParseCIDRList(configuration.trustedProxyCIDRs)
	if err != nil {
		return nil, err
	}
	return prefixes, nil
}

func gatewayDialOptions(configuration config) ([]grpc.DialOption, error) {
	if configuration.tlsCertificateFile == "" {
		return []grpc.DialOption{grpc.WithTransportCredentials(insecure.NewCredentials())}, nil
	}
	pem, err := os.ReadFile(configuration.tlsCertificateFile)
	if err != nil {
		return nil, fmt.Errorf("read gateway TLS trust certificate: %w", err)
	}
	pool := x509.NewCertPool()
	if !pool.AppendCertsFromPEM(pem) {
		return nil, fmt.Errorf("gateway TLS trust certificate is invalid")
	}
	return []grpc.DialOption{grpc.WithTransportCredentials(credentials.NewTLS(&tls.Config{
		MinVersion: tls.VersionTLS12, RootCAs: pool, ServerName: configuration.tlsServerName,
	}))}, nil
}

func compilerClientDialOptions(configuration config) ([]grpc.DialOption, error) {
	if configuration.compilerCertFile == "" {
		return []grpc.DialOption{grpc.WithTransportCredentials(insecure.NewCredentials())}, nil
	}
	certificate, err := tls.LoadX509KeyPair(configuration.compilerCertFile, configuration.compilerKeyFile)
	if err != nil {
		return nil, fmt.Errorf("load compiler validation client identity: %w", err)
	}
	pem, err := os.ReadFile(configuration.compilerCAFile)
	if err != nil {
		return nil, fmt.Errorf("read compiler validation CA: %w", err)
	}
	pool := x509.NewCertPool()
	if !pool.AppendCertsFromPEM(pem) {
		return nil, fmt.Errorf("compiler validation CA is invalid")
	}
	return []grpc.DialOption{grpc.WithTransportCredentials(credentials.NewTLS(&tls.Config{
		MinVersion: tls.VersionTLS12, RootCAs: pool, ServerName: configuration.compilerServerName,
		Certificates: []tls.Certificate{certificate},
	}))}, nil
}

func boundedDuration(value, label string, minimum, maximum time.Duration) (time.Duration, error) {
	parsed, err := time.ParseDuration(value)
	if err != nil || parsed < minimum || parsed > maximum {
		return 0, fmt.Errorf("%s must be between %s and %s", label, minimum, maximum)
	}
	return parsed, nil
}
