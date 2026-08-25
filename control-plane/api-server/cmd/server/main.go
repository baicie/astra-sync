package main

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/base64"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"net/netip"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/google/uuid"
	"github.com/grpc-ecosystem/grpc-gateway/v2/runtime"
	"go.uber.org/zap"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/reflection"
	"google.golang.org/protobuf/proto"

	controlv1 "io.astrasync/control-plane/api-server/gen/go/v1"
	"io.astrasync/control-plane/api-server/internal/authn"
	"io.astrasync/control-plane/api-server/internal/catalogproto"
	"io.astrasync/control-plane/api-server/internal/compilerclient"
	"io.astrasync/control-plane/api-server/internal/metrics"
	"io.astrasync/control-plane/api-server/internal/replication"
	"io.astrasync/control-plane/api-server/internal/service"
	"io.astrasync/control-plane/auth"
	authpostgres "io.astrasync/control-plane/auth/postgres"
	"io.astrasync/control-plane/auth/transport"
	"io.astrasync/control-plane/catalog"
	catalogpostgres "io.astrasync/control-plane/catalog/postgres"
	"io.astrasync/control-plane/connection"
	connectionpostgres "io.astrasync/control-plane/connection/postgres"
	jobpostgres "io.astrasync/control-plane/job/postgres"
	replicationadapters "io.astrasync/control-plane/replication/adapters"
	replicationmetrics "io.astrasync/control-plane/replication/metrics"
	replicationobjectstore "io.astrasync/control-plane/replication/objectstore"
	replicationruntime "io.astrasync/control-plane/replication/runtime"
)

const shutdownTimeout = 10 * time.Second

type config struct {
	databaseURL                string
	grpcListen                 string
	grpcEndpoint               string
	httpListen                 string
	metricsListen              string
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
	mtlsClientCAFile           string
	mtlsRequireClientCert      bool
	trustedProxyCIDRs          string
	connectionTestDeadline     time.Duration
	connectionTestPolicies     map[string]connection.TestEgressPolicy
	connectionMutationsEnabled bool
	connectionTestsEnabled     bool
	connectionRuntimeEnabled   bool
	region                     string
	regionRole                 string
	peerRegion                 string
	peerEndpoint               string
	replicationRuntimeEnabled  bool
	replicationObjectStoreRoot string
	replicationWALPrefix       string
	replicationWALResumeFrom   int64
}

func main() {
	logger := newComponentLogger("apiserver", os.Stdout, os.Getenv("LOG_LEVEL"))
	slog.SetDefault(logger)

	configuration, err := loadConfig(os.Getenv)
	if err != nil {
		logger.Error("failed to load configuration", "error", err.Error())
		os.Exit(1)
	}
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	multiRegionMetrics, err := replicationmetrics.NewBundle()
	if err != nil {
		logger.Error("multi-region metrics failed to initialize", "error", err.Error())
		os.Exit(1)
	}
	if _, err := metricsServer(ctx, logger, configuration.metricsListen, multiRegionMetrics.Registry); err != nil {
		logger.Error("metrics listener failed to start", "error", err.Error())
		os.Exit(1)
	}
	if err := run(ctx, configuration, multiRegionMetrics); err != nil {
		logger.Error("api-server terminated with error", "error", err.Error())
		os.Exit(1)
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
	mtlsClientCAFile := strings.TrimSpace(getenv("MTLS_CLIENT_CA_FILE"))
	mtlsRequireClientCert := strings.ToLower(valueOrDefault(getenv("MTLS_REQUIRE_CLIENT_CERT"), "true")) == "true"
	if environment == "production" && mtlsClientCAFile == "" {
		return config{}, fmt.Errorf("production requires MTLS_CLIENT_CA_FILE")
	}
	if environment == "production" && !mtlsRequireClientCert {
		return config{}, fmt.Errorf("production requires MTLS_REQUIRE_CLIENT_CERT=true")
	}
	if mtlsClientCAFile != "" {
		if _, err := os.Stat(mtlsClientCAFile); err != nil {
			return config{}, fmt.Errorf("MTLS_CLIENT_CA_FILE: %w", err)
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
	region := strings.TrimSpace(valueOrDefault(getenv("ASTRA_REGION"), "local"))
	if region == "" {
		return config{}, fmt.Errorf("ASTRA_REGION must not be blank")
	}
	regionRole := strings.ToLower(strings.TrimSpace(valueOrDefault(getenv("ASTRA_ROLE"), "primary")))
	if regionRole != "primary" && regionRole != "standby" && regionRole != "secondary" {
		return config{}, fmt.Errorf("ASTRA_ROLE must be primary, standby, or secondary")
	}
	peerRegion := strings.TrimSpace(getenv("ASTRA_PEER_REGION"))
	peerEndpoint := strings.TrimSpace(getenv("ASTRA_PEER_ENDPOINT"))
	replicationRuntimeEnabled, err := booleanSetting(getenv("ASTRA_REPLICATION_RUNTIME_ENABLED"), "ASTRA_REPLICATION_RUNTIME_ENABLED", false)
	if err != nil {
		return config{}, err
	}
	objectStoreRoot := strings.TrimSpace(getenv("ASTRA_REPLICATION_OBJECT_STORE_ROOT"))
	walPrefix := strings.Trim(strings.TrimSpace(getenv("ASTRA_REPLICATION_WAL_PREFIX")), "/")
	walResumeValue := valueOrDefault(getenv("ASTRA_REPLICATION_WAL_RESUME_FROM"), "0")
	walResumeFrom, err := strconv.ParseInt(walResumeValue, 10, 64)
	if err != nil || walResumeFrom < 0 {
		return config{}, fmt.Errorf("ASTRA_REPLICATION_WAL_RESUME_FROM must be a non-negative integer")
	}
	if replicationRuntimeEnabled {
		if peerRegion == "" || peerEndpoint == "" || objectStoreRoot == "" || walPrefix == "" {
			return config{}, fmt.Errorf("replication runtime requires ASTRA_PEER_REGION, ASTRA_PEER_ENDPOINT, ASTRA_REPLICATION_OBJECT_STORE_ROOT, and ASTRA_REPLICATION_WAL_PREFIX")
		}
	}
	return config{
		databaseURL: databaseURL, grpcListen: valueOrDefault(getenv("GRPC_LISTEN_ADDRESS"), ":50051"),
		grpcEndpoint:  valueOrDefault(getenv("GRPC_GATEWAY_ENDPOINT"), "127.0.0.1:50051"),
		httpListen:    valueOrDefault(getenv("HTTP_LISTEN_ADDRESS"), ":8080"),
		metricsListen: strings.TrimSpace(getenv("METRICS_LISTEN_ADDRESS")),
		environment:   environment, authMode: authMode,
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
		mtlsClientCAFile:           mtlsClientCAFile,
		mtlsRequireClientCert:      mtlsRequireClientCert,
		trustedProxyCIDRs:          trustedProxyCIDRs,
		connectionTestDeadline:     connectionTestDeadline,
		connectionTestPolicies:     connectionTestPolicies,
		connectionMutationsEnabled: connectionMutationsEnabled,
		connectionTestsEnabled:     connectionTestsEnabled,
		connectionRuntimeEnabled:   connectionRuntimeEnabled,
		region:                     region,
		regionRole:                 regionRole,
		peerRegion:                 strings.TrimSpace(getenv("ASTRA_PEER_REGION")),
		peerEndpoint:               strings.TrimSpace(getenv("ASTRA_PEER_ENDPOINT")),
		replicationRuntimeEnabled:  replicationRuntimeEnabled,
		replicationObjectStoreRoot: objectStoreRoot,
		replicationWALPrefix:       walPrefix,
		replicationWALResumeFrom:   walResumeFrom,
	}, nil
}

func run(ctx context.Context, configuration config, multiRegionMetrics *replicationmetrics.Bundle) error {
	if multiRegionMetrics == nil || multiRegionMetrics.Recorder == nil {
		return fmt.Errorf("multi-region metrics bundle is required")
	}
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
	metricRecorder := metrics.DefaultRecorder()
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
		controlv1.RegionTopologyService_ServiceDesc,
		controlv1.ReplicationService_ServiceDesc,
		controlv1.RegionPromotionService_ServiceDesc,
		controlv1.RegionRecoveryService_ServiceDesc,
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
			Metrics: metricRecorder, Clock: time.Now, EventID: uuid.NewString,
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
		service.WithAuditQueryMetrics(metricRecorder),
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
	regionRole := controlv1.RegionRole_REGION_ROLE_PRIMARY
	if configuration.regionRole == "standby" || configuration.regionRole == "secondary" {
		regionRole = controlv1.RegionRole_REGION_ROLE_STANDBY
	}
	regions := []replication.TopologyRegion{{
		Name: configuration.region, Role: regionRole,
		APIServerEndpoint: configuration.grpcEndpoint,
	}}
	if configuration.peerRegion != "" && configuration.peerEndpoint != "" {
		peerRole := controlv1.RegionRole_REGION_ROLE_STANDBY
		if regionRole == controlv1.RegionRole_REGION_ROLE_STANDBY {
			peerRole = controlv1.RegionRole_REGION_ROLE_PRIMARY
		}
		regions = append(regions, replication.TopologyRegion{
			Name: configuration.peerRegion, Role: peerRole,
			APIServerEndpoint: configuration.peerEndpoint,
		})
	}
	postgresStore, err := replicationadapters.NewPostgreSQLStore(jobRepository.DB())
	if err != nil {
		return fmt.Errorf("create replication PostgreSQL adapter: %w", err)
	}
	if err := postgresStore.Migrate(ctx); err != nil {
		return err
	}
	replicationService := replication.NewService(regions, nil,
		replication.WithMetrics(multiRegionMetrics.Recorder),
		replication.WithCheckpointDeduplicator(postgresStore),
	)
	var replicationRuntime *replicationruntime.Runtime
	if configuration.replicationRuntimeEnabled {
		store, err := replicationobjectstore.NewFileStore(configuration.replicationObjectStoreRoot)
		if err != nil {
			return fmt.Errorf("create replication object store: %w", err)
		}
		eventBridge, err := replication.NewEventHandlerBridge(replicationService)
		if err != nil {
			return fmt.Errorf("create replication event handler: %w", err)
		}
		walReader, err := replicationadapters.NewWALReader(ctx, store, zap.NewNop(), configuration.region, configuration.replicationWALPrefix, configuration.replicationWALResumeFrom)
		if err != nil {
			return fmt.Errorf("create replication WAL reader: %w", err)
		}
		stateRestorer, err := replicationadapters.NewFileStateRestorer(store)
		if err != nil {
			return fmt.Errorf("create replication state restorer: %w", err)
		}
		auditor, err := replicationadapters.NewZapAuditLogger(zap.NewNop())
		if err != nil {
			return fmt.Errorf("create replication audit logger: %w", err)
		}
		remoteRecovery := replication.NewLazyRemoteRecoveryClient(func() *grpc.ClientConn {
			if replicationRuntime == nil {
				return nil
			}
			return replicationRuntime.Connection()
		})
		replicationRuntime, err = replicationruntime.New(zap.NewNop(), replicationruntime.Config{
			Region: configuration.region, PeerRegion: configuration.peerRegion, PeerEndpoint: configuration.peerEndpoint,
			Metrics: multiRegionMetrics, ReplicationResumeFrom: configuration.replicationWALResumeFrom,
			ReplicatorConfig: replicationruntime.ReplicatorConfig{BatchSize: 128, PollInterval: time.Second, RetryInitial: 100 * time.Millisecond, RetryMax: 5 * time.Second},
			EnableTLS:        configuration.tlsCertificateFile != "",
			CACertPath:       configuration.tlsCertificateFile, ClientCertPath: configuration.tlsCertificateFile,
			ClientKeyPath: configuration.tlsPrivateKeyFile, ServerName: configuration.tlsServerName,
		}, replicationruntime.Dependencies{
			EventHandler: eventBridge, EventSenderFactory: replication.EventSenderFactory{}, EventEncoder: replication.NewCheckpointEventEncoder(configuration.peerRegion),
			PromotionStore: postgresStore, EpochAssigner: postgresStore, JobReader: postgresStore, JobWriter: postgresStore,
			ProgressStore: postgresStore,
			Fencer:        postgresStore, Topology: configuredReplicationTopology{current: configuration.region, standby: configuration.peerRegion}, RemoteRecovery: remoteRecovery,
			WALEntryReader: walReader, ObjectStorage: store, ManifestParser: replicationadapters.JSONManifestParser{},
			Validator: replicationadapters.CheckpointValidator{}, StateRestorer: stateRestorer, Auditor: auditor,
		})
		if err != nil {
			return fmt.Errorf("create replication runtime: %w", err)
		}
		backend, err := replication.NewPromotionManagerBackend(replicationRuntime.Promotion())
		if err != nil {
			_ = replicationRuntime.Close()
			return fmt.Errorf("create promotion backend: %w", err)
		}
		replicationService.SetPromotionBackend(backend)
		recoveryBackend, err := replication.NewRecoveryManagerBackend(replicationRuntime.Recovery())
		if err != nil {
			_ = replicationRuntime.Close()
			return fmt.Errorf("create recovery backend: %w", err)
		}
		replicationService.SetRecoveryBackend(recoveryBackend)
		if err := replicationRuntime.Start(ctx); err != nil {
			_ = replicationRuntime.Close()
			return fmt.Errorf("start replication runtime: %w", err)
		}
		defer replicationRuntime.Close()
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
		serverTLSConfig, err := transport.ServerTLSConfig(transport.ServerTLSConfigInput{
			CertificateFile:   configuration.tlsCertificateFile,
			PrivateKeyFile:    configuration.tlsPrivateKeyFile,
			ClientCAPath:      configuration.mtlsClientCAFile,
			RequireClientCert: configuration.mtlsRequireClientCert && configuration.environment == "production",
		})
		if err != nil {
			return fmt.Errorf("load gRPC TLS identity: %w", err)
		}
		grpcOptions = append(grpcOptions, grpc.Creds(credentials.NewTLS(serverTLSConfig)))
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
	controlv1.RegisterRegionTopologyServiceServer(grpcServer, replicationService)
	controlv1.RegisterReplicationServiceServer(grpcServer, replicationService)
	controlv1.RegisterRegionPromotionServiceServer(grpcServer, replicationService)
	controlv1.RegisterRegionRecoveryServiceServer(grpcServer, replicationService)
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
		"RegionTopologyService":   controlv1.RegisterRegionTopologyServiceHandlerFromEndpoint,
		"ReplicationService":      controlv1.RegisterReplicationServiceHandlerFromEndpoint,
		"RegionPromotionService":  controlv1.RegisterRegionPromotionServiceHandlerFromEndpoint,
		"RegionRecoveryService":   controlv1.RegisterRegionRecoveryServiceHandlerFromEndpoint,
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
				if err != nil {
					return err
				}
				if configuration.replicationRuntimeEnabled && (replicationRuntime == nil || !replicationRuntime.Ready()) {
					return errors.New("replication runtime is not ready")
				}
				return nil
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
			slog.Default().Warn("compiler inventory publisher unavailable; serving last verified snapshot")
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
			slog.Default().Warn("connector inventory publisher unavailable; serving last verified snapshot")
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
