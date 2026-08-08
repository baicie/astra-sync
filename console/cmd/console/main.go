package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"

	"io.astrasync/console/internal/server"
	jobv1 "io.astrasync/control-plane/api-server/gen/go/v1"
)

const shutdownTimeout = 10 * time.Second

type config struct {
	grpcEndpoint string
	httpListen   string
	namespace    string
}

type grpcJobReader struct {
	client jobv1.JobServiceClient
}

func (r grpcJobReader) ListJobs(ctx context.Context, request *jobv1.ListJobsRequest) (*jobv1.ListJobsResponse, error) {
	return r.client.ListJobs(ctx, request)
}

func (r grpcJobReader) GetJob(ctx context.Context, request *jobv1.GetJobRequest) (*jobv1.Job, error) {
	return r.client.GetJob(ctx, request)
}

func (r grpcJobReader) GetJobStatus(ctx context.Context, request *jobv1.GetJobStatusRequest) (*jobv1.JobStatus, error) {
	return r.client.GetJobStatus(ctx, request)
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
	return config{
		grpcEndpoint: valueOrDefault(getenv("ASTRASYNC_API_GRPC_ENDPOINT"), "127.0.0.1:50051"),
		httpListen:   valueOrDefault(getenv("CONSOLE_LISTEN_ADDRESS"), ":8090"),
		namespace:    valueOrDefault(getenv("CONSOLE_NAMESPACE"), "default"),
	}, nil
}

func run(ctx context.Context, configuration config) error {
	connection, err := grpc.NewClient(
		configuration.grpcEndpoint,
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	)
	if err != nil {
		return fmt.Errorf("dial control-plane gRPC: %w", err)
	}
	defer connection.Close()

	console, err := server.New(grpcJobReader{client: jobv1.NewJobServiceClient(connection)}, configuration.namespace)
	if err != nil {
		return fmt.Errorf("create Console server: %w", err)
	}
	listener, err := net.Listen("tcp", configuration.httpListen)
	if err != nil {
		return fmt.Errorf("listen for Console HTTP: %w", err)
	}
	httpServer := &http.Server{
		Handler:           console.Handler(),
		ReadHeaderTimeout: 5 * time.Second,
		IdleTimeout:       60 * time.Second,
	}
	errorsChannel := make(chan error, 1)
	go func() {
		if serveErr := httpServer.Serve(listener); serveErr != nil && !errors.Is(serveErr, http.ErrServerClosed) {
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

func valueOrDefault(value, defaultValue string) string {
	if value == "" {
		return defaultValue
	}
	return value
}
