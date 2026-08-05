package main

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"

	jobpostgres "io.astrasync/control-plane/job/postgres"
	dispatchpostgres "io.astrasync/control-plane/scheduler/internal/dispatch/postgres"
	dispatchkube "io.astrasync/control-plane/scheduler/internal/kubernetes"
	schedulerinternal "io.astrasync/control-plane/scheduler/internal/scheduler"
)

const shutdownTimeout = 10 * time.Second

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	slog.SetDefault(logger)
	configuration, err := loadConfig(os.Getenv)
	if err != nil {
		logger.Error("invalid scheduler configuration", "error", err)
		os.Exit(1)
	}
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	if err := run(ctx, configuration, logger); err != nil {
		logger.Error("scheduler stopped", "error", err)
		os.Exit(1)
	}
}

func run(ctx context.Context, configuration applicationConfig, logger *slog.Logger) error {
	db, err := sql.Open("pgx", configuration.databaseURL)
	if err != nil {
		return fmt.Errorf("open scheduler PostgreSQL: %w", err)
	}
	defer db.Close()
	db.SetMaxOpenConns(configuration.scheduler.MaximumActive + 5)
	if err := db.PingContext(ctx); err != nil {
		return fmt.Errorf("connect scheduler PostgreSQL: %w", err)
	}
	jobs := jobpostgres.New(db)
	if err := jobs.Migrate(ctx); err != nil {
		return err
	}
	dispatches := dispatchpostgres.New(db)
	if err := dispatches.Migrate(ctx); err != nil {
		return err
	}

	restConfig, err := kubernetesConfig(configuration.kubeconfig)
	if err != nil {
		return err
	}
	restConfig.Timeout = configuration.scheduler.OperationTimeout
	kubernetesClient, err := kubernetes.NewForConfig(restConfig)
	if err != nil {
		return fmt.Errorf("create Kubernetes client: %w", err)
	}
	dispatcher, err := dispatchkube.New(kubernetesClient, configuration.dispatcher)
	if err != nil {
		return err
	}
	reconciler, err := schedulerinternal.New(
		configuration.scheduler, dispatches, jobs, dispatcher, time.Now, logger)
	if err != nil {
		return err
	}

	healthServer := &http.Server{
		Addr:              configuration.healthAddress,
		Handler:           healthHandler(db, kubernetesClient),
		ReadHeaderTimeout: 5 * time.Second,
		IdleTimeout:       30 * time.Second,
	}
	errorsChannel := make(chan error, 2)
	go func() {
		if runErr := reconciler.Run(ctx); runErr != nil {
			errorsChannel <- runErr
		}
	}()
	go func() {
		if serveErr := healthServer.ListenAndServe(); serveErr != nil && !errors.Is(serveErr, http.ErrServerClosed) {
			errorsChannel <- fmt.Errorf("serve scheduler health endpoint: %w", serveErr)
		}
	}()

	select {
	case <-ctx.Done():
	case runErr := <-errorsChannel:
		_ = healthServer.Close()
		return runErr
	}
	shutdownContext, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
	defer cancel()
	if err := healthServer.Shutdown(shutdownContext); err != nil {
		return fmt.Errorf("shut down scheduler health endpoint: %w", err)
	}
	return nil
}

func kubernetesConfig(kubeconfig string) (*rest.Config, error) {
	if kubeconfig != "" {
		configuration, err := clientcmd.BuildConfigFromFlags("", kubeconfig)
		if err != nil {
			return nil, fmt.Errorf("load kubeconfig: %w", err)
		}
		return configuration, nil
	}
	configuration, err := rest.InClusterConfig()
	if err != nil {
		return nil, fmt.Errorf("load in-cluster Kubernetes configuration: %w", err)
	}
	return configuration, nil
}

type databasePinger interface {
	PingContext(context.Context) error
}

func healthHandler(database databasePinger, client kubernetes.Interface) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(response http.ResponseWriter, _ *http.Request) {
		response.Header().Set("Content-Type", "text/plain; charset=utf-8")
		_, _ = response.Write([]byte("ok\n"))
	})
	mux.HandleFunc("GET /readyz", func(response http.ResponseWriter, request *http.Request) {
		readyContext, cancel := context.WithTimeout(request.Context(), 2*time.Second)
		defer cancel()
		if err := database.PingContext(readyContext); err != nil {
			http.Error(response, "PostgreSQL is not ready", http.StatusServiceUnavailable)
			return
		}
		if _, err := client.Discovery().ServerVersion(); err != nil {
			http.Error(response, "Kubernetes API is not ready", http.StatusServiceUnavailable)
			return
		}
		response.Header().Set("Content-Type", "text/plain; charset=utf-8")
		_, _ = response.Write([]byte("ready\n"))
	})
	return mux
}
