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

	"github.com/google/uuid"
	"github.com/grpc-ecosystem/grpc-gateway/v2/runtime"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/reflection"

	jobv1 "io.astrasync/control-plane/api-server/gen/go/v1"
	"io.astrasync/control-plane/api-server/internal/service"
	jobpostgres "io.astrasync/control-plane/job/postgres"
)

const shutdownTimeout = 10 * time.Second

type config struct {
	databaseURL  string
	grpcListen   string
	grpcEndpoint string
	httpListen   string
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
	return config{
		databaseURL:  databaseURL,
		grpcListen:   valueOrDefault(getenv("GRPC_LISTEN_ADDRESS"), ":50051"),
		grpcEndpoint: valueOrDefault(getenv("GRPC_GATEWAY_ENDPOINT"), "127.0.0.1:50051"),
		httpListen:   valueOrDefault(getenv("HTTP_LISTEN_ADDRESS"), ":8080"),
	}, nil
}

func run(ctx context.Context, configuration config) error {
	repository, err := jobpostgres.Open(ctx, configuration.databaseURL)
	if err != nil {
		return err
	}
	defer repository.Close()
	if err := repository.Migrate(ctx); err != nil {
		return err
	}

	jobService, err := service.NewJobService(repository, time.Now, uuid.NewString)
	if err != nil {
		return fmt.Errorf("create job service: %w", err)
	}
	grpcListener, err := net.Listen("tcp", configuration.grpcListen)
	if err != nil {
		return fmt.Errorf("listen for gRPC: %w", err)
	}
	grpcServer := grpc.NewServer()
	jobv1.RegisterJobServiceServer(grpcServer, jobService)
	reflection.Register(grpcServer)

	gateway := runtime.NewServeMux()
	if err := jobv1.RegisterJobServiceHandlerFromEndpoint(
		ctx,
		gateway,
		configuration.grpcEndpoint,
		[]grpc.DialOption{grpc.WithTransportCredentials(insecure.NewCredentials())},
	); err != nil {
		grpcListener.Close()
		return fmt.Errorf("register REST gateway: %w", err)
	}
	httpServer := &http.Server{
		Addr:              configuration.httpListen,
		Handler:           apiHandler(gateway, repository.Ping),
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
		if serveErr := httpServer.ListenAndServe(); serveErr != nil && !errors.Is(serveErr, http.ErrServerClosed) {
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
