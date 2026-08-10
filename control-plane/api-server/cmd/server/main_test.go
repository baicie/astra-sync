package main

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"errors"
	"fmt"
	"io"
	"math/big"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	gatewayruntime "github.com/grpc-ecosystem/grpc-gateway/v2/runtime"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/test/bufconn"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"

	jobv1 "io.astrasync/control-plane/api-server/gen/go/v1"
	"io.astrasync/control-plane/api-server/internal/service"
	catalogmemory "io.astrasync/control-plane/catalog/memory"
	"io.astrasync/control-plane/job/memory"
)

func TestLoadConfigRequiresDatabaseAndAppliesNetworkDefaults(t *testing.T) {
	if _, err := loadConfig(func(string) string { return "" }); err == nil {
		t.Fatal("expected missing database URL failure")
	}
	configuration, err := loadConfig(func(key string) string {
		if key == "DATABASE_URL" {
			return "postgresql://example/astrasync"
		}
		return ""
	})
	if err != nil {
		t.Fatalf("load config: %v", err)
	}
	if configuration.grpcListen != ":50051" || configuration.grpcEndpoint != "127.0.0.1:50051" ||
		configuration.httpListen != ":8080" {
		t.Fatalf("unexpected defaults: %+v", configuration)
	}
	if configuration.connectionMutationsEnabled || configuration.connectionTestsEnabled ||
		configuration.connectionRuntimeEnabled {
		t.Fatalf("Connection rollout gates must default closed: %+v", configuration)
	}
}

func TestLoadConfigRequiresExplicitConnectionRolloutGates(t *testing.T) {
	values := map[string]string{
		"DATABASE_URL":                 "postgresql://example/astrasync",
		"CONNECTION_MUTATIONS_ENABLED": "true",
		"CONNECTION_TESTS_ENABLED":     "true",
		"CONNECTION_RUNTIME_ENABLED":   "true",
	}
	configuration, err := loadConfig(func(key string) string { return values[key] })
	if err != nil || !configuration.connectionMutationsEnabled || !configuration.connectionTestsEnabled ||
		!configuration.connectionRuntimeEnabled {
		t.Fatalf("load explicit Connection rollout gates: config=%+v err=%v", configuration, err)
	}
	values["CONNECTION_TESTS_ENABLED"] = "enabled"
	if _, err := loadConfig(func(key string) string { return values[key] }); err == nil {
		t.Fatal("expected invalid Connection rollout gate rejection")
	}
}

func TestLoadConfigEnforcesProductionAndCompilerMTLSGates(t *testing.T) {
	values := map[string]string{
		"DATABASE_URL": "postgresql://example/astrasync",
		"APP_ENV":      "production",
	}
	if _, err := loadConfig(func(key string) string { return values[key] }); err == nil {
		t.Fatal("expected production authentication gate")
	}
	values["AUTH_MODE"] = "oidc"
	values["OIDC_ISSUER"] = "https://issuer.example"
	values["OIDC_AUDIENCE"] = "astrasync"
	values["TLS_CERTIFICATE_FILE"] = "server.crt"
	values["TLS_PRIVATE_KEY_FILE"] = "server.key"
	values["CATALOG_TOKEN_KEY"] = "0123456789abcdef0123456789abcdef"
	if _, err := loadConfig(func(key string) string { return values[key] }); err == nil {
		t.Fatal("expected production compiler mTLS gate")
	}
	values["COMPILER_VALIDATION_TLS_CERTIFICATE_FILE"] = "client.crt"
	values["COMPILER_VALIDATION_TLS_PRIVATE_KEY_FILE"] = "client.key"
	values["COMPILER_VALIDATION_TLS_CA_FILE"] = "ca.crt"
	configuration, err := loadConfig(func(key string) string { return values[key] })
	if err != nil || configuration.compilerTimeout != 3*time.Second {
		t.Fatalf("load gated production config: config=%+v err=%v", configuration, err)
	}

	delete(values, "COMPILER_VALIDATION_TLS_PRIVATE_KEY_FILE")
	if _, err := loadConfig(func(key string) string { return values[key] }); err == nil {
		t.Fatal("expected partial compiler TLS configuration rejection")
	}
}

func TestCompilerClientDialOptionsLoadMutualTLSIdentity(t *testing.T) {
	certificateFile, keyFile := writeTestCertificate(t)
	options, err := compilerClientDialOptions(config{
		compilerCertFile: certificateFile, compilerKeyFile: keyFile,
		compilerCAFile: certificateFile, compilerServerName: "compiler-validation",
	})
	if err != nil || len(options) != 1 {
		t.Fatalf("load compiler mutual TLS options: options=%d err=%v", len(options), err)
	}
}

func TestReconcileCompilerCatalogUsesCompilerAuthorityAndRetainsLastSnapshot(t *testing.T) {
	artifactPath := filepath.Join("..", "..", "..", "..", "deployment", "catalog", "connector-inventory.pb")
	inventory, err := readInventoryArtifact(artifactPath)
	if err != nil {
		t.Fatalf("read deployment inventory fixture: %v", err)
	}
	repository := catalogmemory.New()
	compiler := staticCompilerInventory{inventory: inventory}
	configuration := config{
		executionProfile: inventory.GetExecutionProfile(), catalogPath: artifactPath,
	}
	if err := reconcileCompilerCatalog(context.Background(), configuration, repository, compiler); err != nil {
		t.Fatalf("reconcile compiler catalog: %v", err)
	}
	snapshot, err := repository.Current(context.Background(), inventory.GetExecutionProfile())
	if err != nil || snapshot.InventoryRevision != inventory.GetInventoryRevision() ||
		snapshot.CompilerRevision != inventory.GetCompilerRevision() {
		t.Fatalf("unexpected active compiler snapshot: snapshot=%+v err=%v", snapshot, err)
	}
	if err := reconcileCompilerCatalog(
		context.Background(), configuration, repository,
		staticCompilerInventory{err: errors.New("compiler unavailable")},
	); err != nil {
		t.Fatalf("last verified catalog was not retained: %v", err)
	}

	drifted := proto.Clone(inventory).(*jobv1.ConnectorInventory)
	drifted.CompilerRevision = "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	if err := reconcileCompilerCatalog(
		context.Background(), configuration, repository, staticCompilerInventory{inventory: drifted},
	); err == nil {
		t.Fatal("expected deployment/compiler inventory drift rejection")
	}
}

type staticCompilerInventory struct {
	inventory *jobv1.ConnectorInventory
	err       error
}

func (c staticCompilerInventory) Inventory(context.Context, string) (*jobv1.ConnectorInventory, error) {
	if c.err != nil {
		return nil, c.err
	}
	return c.inventory, nil
}

func writeTestCertificate(t *testing.T) (string, string) {
	t.Helper()
	privateKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("generate test private key: %v", err)
	}
	template := &x509.Certificate{
		SerialNumber: big.NewInt(1), Subject: pkix.Name{CommonName: "compiler-validation"},
		NotBefore: time.Now().Add(-time.Minute), NotAfter: time.Now().Add(time.Hour),
		IsCA: true, BasicConstraintsValid: true,
		KeyUsage:    x509.KeyUsageDigitalSignature | x509.KeyUsageCertSign,
		ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth, x509.ExtKeyUsageServerAuth},
	}
	der, err := x509.CreateCertificate(rand.Reader, template, template, &privateKey.PublicKey, privateKey)
	if err != nil {
		t.Fatalf("create test certificate: %v", err)
	}
	directory := t.TempDir()
	certificateFile := filepath.Join(directory, "client.crt")
	keyFile := filepath.Join(directory, "client.key")
	if err := os.WriteFile(certificateFile, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}), 0o600); err != nil {
		t.Fatalf("write test certificate: %v", err)
	}
	privateDER, err := x509.MarshalPKCS8PrivateKey(privateKey)
	if err != nil {
		t.Fatalf("marshal test private key: %v", err)
	}
	if err := os.WriteFile(keyFile, pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: privateDER}), 0o600); err != nil {
		t.Fatalf("write test private key: %v", err)
	}
	return certificateFile, keyFile
}

func TestAPIHealthAndReadiness(t *testing.T) {
	gateway := http.HandlerFunc(func(response http.ResponseWriter, _ *http.Request) {
		response.WriteHeader(http.StatusTeapot)
	})
	ready := apiHandler(gateway, func(context.Context) error { return nil })
	for path, expected := range map[string]int{
		"/health": http.StatusOK,
		"/ready":  http.StatusOK,
		"/jobs":   http.StatusTeapot,
	} {
		request := httptest.NewRequest(http.MethodGet, path, nil)
		response := httptest.NewRecorder()
		ready.ServeHTTP(response, request)
		if response.Code != expected {
			t.Fatalf("%s: expected %d, got %d", path, expected, response.Code)
		}
	}

	notReady := apiHandler(gateway, func(context.Context) error { return errors.New("database unavailable") })
	request := httptest.NewRequest(http.MethodGet, "/ready", nil)
	response := httptest.NewRecorder()
	notReady.ServeHTTP(response, request)
	if response.Code != http.StatusServiceUnavailable {
		t.Fatalf("expected unavailable readiness, got %d", response.Code)
	}
}

func TestRESTGatewayRunsIdempotentJobLifecycle(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	repository := memory.New()
	jobService, err := service.NewJobService(
		repository,
		func() time.Time { return time.Date(2026, 8, 5, 5, 0, 0, 0, time.UTC) },
		func() string { return "9c256122-a311-4625-96ad-b7e893ce7bb1" },
	)
	if err != nil {
		t.Fatalf("create service: %v", err)
	}

	listener := bufconn.Listen(1024 * 1024)
	grpcServer := grpc.NewServer()
	jobv1.RegisterJobServiceServer(grpcServer, jobService)
	go func() { _ = grpcServer.Serve(listener) }()
	t.Cleanup(func() {
		grpcServer.Stop()
		_ = listener.Close()
	})

	connection, err := grpc.DialContext(
		ctx,
		"passthrough:///bufnet",
		grpc.WithContextDialer(func(context.Context, string) (net.Conn, error) { return listener.Dial() }),
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithBlock(),
	)
	if err != nil {
		t.Fatalf("dial in-memory gRPC server: %v", err)
	}
	t.Cleanup(func() { _ = connection.Close() })

	gateway := gatewayruntime.NewServeMux()
	if err := jobv1.RegisterJobServiceHandler(ctx, gateway, connection); err != nil {
		t.Fatalf("register gateway: %v", err)
	}
	handler := apiHandler(gateway, func(context.Context) error { return nil })

	created := &jobv1.Job{}
	postGateway(t, handler, "/astra.control.v1.JobService/CreateJob", `{
		"namespace":"default",
		"name":"orders",
		"spec":{
			"source":{"connector":"mysql-cdc","options":{"table":"shop.orders"}},
			"sink":{"connector":"jdbc","options":{"table":"orders"}},
			"delivery":{"guarantee":"DELIVERY_GUARANTEE_EXACTLY_ONCE"},
			"runtime":{"maxBatchRecords":128}
		}
	}`, created)
	if created.GetVersion() != 1 || created.GetStatus().GetState() != jobv1.JobState_JOB_STATE_CREATED {
		t.Fatalf("unexpected REST create response: %+v", created)
	}

	started := &jobv1.Job{}
	startRequest := `{"namespace":"default","name":"orders","expectedVersion":"1"}`
	postGateway(t, handler, "/astra.control.v1.JobService/StartJob", startRequest, started)
	if started.GetVersion() != 2 || started.GetStatus().GetEpoch() != 1 ||
		started.GetStatus().GetState() != jobv1.JobState_JOB_STATE_INITIALIZING {
		t.Fatalf("unexpected REST start response: %+v", started)
	}

	retried := &jobv1.Job{}
	postGateway(t, handler, "/astra.control.v1.JobService/StartJob", startRequest, retried)
	if retried.GetVersion() != 2 || retried.GetStatus().GetEpoch() != 1 {
		t.Fatalf("REST start retry was not idempotent: %+v", retried)
	}
}

func postGateway(t *testing.T, handler http.Handler, path, payload string, target *jobv1.Job) {
	t.Helper()
	request := httptest.NewRequest(http.MethodPost, path, bytes.NewBufferString(payload))
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("POST %s returned %d: %s", path, response.Code, response.Body.String())
	}
	body, err := io.ReadAll(response.Result().Body)
	if err != nil {
		t.Fatalf("read POST %s response: %v", path, err)
	}
	if err := protojson.Unmarshal(body, target); err != nil {
		t.Fatalf("decode POST %s response (%s): %v", path, fmt.Sprintf("%q", body), err)
	}
}
