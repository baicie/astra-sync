package main

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	gatewayruntime "github.com/grpc-ecosystem/grpc-gateway/v2/runtime"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/test/bufconn"
	"google.golang.org/protobuf/encoding/protojson"

	jobv1 "io.astrasync/control-plane/api-server/gen/go/v1"
	"io.astrasync/control-plane/api-server/internal/service"
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
