package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"

	connectionpostgres "io.astrasync/control-plane/connection/postgres"
	"io.astrasync/control-plane/observability"
	"io.astrasync/control-plane/scheduler/internal/connectiontest"
	"io.astrasync/control-plane/scheduler/internal/materialization"
)

const shutdownTimeout = 10 * time.Second

func main() {
	logger := observability.NewComponentLogger("connection-test-executor")
	configuration, err := loadConfig(os.Getenv)
	if err != nil {
		logger.Error("invalid Connection test executor configuration", "error", err)
		os.Exit(1)
	}
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	if err := run(ctx, configuration, logger); err != nil {
		logger.Error("Connection test executor stopped", "error", err)
		os.Exit(1)
	}
}

func run(ctx context.Context, configuration applicationConfig, logger *slog.Logger) error {
	repository, err := connectionpostgres.Open(ctx, configuration.databaseURL)
	if err != nil {
		return err
	}
	defer repository.Close()
	if err := repository.Migrate(ctx); err != nil {
		return err
	}
	restConfiguration, err := kubernetesConfig(configuration.kubeconfig)
	if err != nil {
		return err
	}
	restConfiguration.Timeout = configuration.kubernetesTimeout
	kubernetesClient, err := kubernetes.NewForConfig(restConfiguration)
	if err != nil {
		return fmt.Errorf("create Connection test Kubernetes client: %w", err)
	}
	provider, err := materialization.NewKubernetesSecretProvider(kubernetesClient)
	if err != nil {
		return err
	}
	resolver, err := connectionTestResolver(configuration.dnsServer, configuration.dialTimeout)
	if err != nil {
		return err
	}
	guard, err := connectiontest.NewEgressGuard(
		resolver, configuration.protectedCIDRs,
		configuration.maximumDNSAnswers, configuration.dialTimeout,
	)
	if err != nil {
		return err
	}
	registry, err := connectiontest.DefaultRegistry()
	if err != nil {
		return err
	}
	executor, err := connectiontest.NewExecutor(
		repository, provider, registry, guard, configuration.executor, time.Now,
	)
	if err != nil {
		return err
	}
	healthServer := &http.Server{
		Addr: configuration.healthAddress, Handler: healthHandler(repository, kubernetesClient),
		ReadHeaderTimeout: 5 * time.Second, IdleTimeout: 30 * time.Second,
	}
	errorsChannel := make(chan error, 2)
	go func() {
		if runErr := executor.Run(ctx); runErr != nil {
			errorsChannel <- runErr
		}
	}()
	go func() {
		if serveErr := healthServer.ListenAndServe(); serveErr != nil &&
			!errors.Is(serveErr, http.ErrServerClosed) {
			errorsChannel <- fmt.Errorf("serve Connection test health endpoint: %w", serveErr)
		}
	}()
	logger.Info("Connection test executor started", "executor_id", configuration.executor.ExecutorID)
	select {
	case <-ctx.Done():
	case runErr := <-errorsChannel:
		_ = healthServer.Close()
		return runErr
	}
	shutdownContext, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
	defer cancel()
	if err := healthServer.Shutdown(shutdownContext); err != nil {
		return fmt.Errorf("shut down Connection test health endpoint: %w", err)
	}
	return nil
}

func connectionTestResolver(server string, timeout time.Duration) (*connectiontest.SystemDNSResolver, error) {
	resolver := net.DefaultResolver
	if server != "" {
		resolver = &net.Resolver{
			PreferGo: true,
			Dial: func(ctx context.Context, _, _ string) (net.Conn, error) {
				dialer := net.Dialer{Timeout: timeout, KeepAlive: -1}
				return dialer.DialContext(ctx, "udp", server)
			},
		}
	}
	return connectiontest.NewSystemDNSResolver(resolver)
}

func kubernetesConfig(kubeconfig string) (*rest.Config, error) {
	if kubeconfig != "" {
		configuration, err := clientcmd.BuildConfigFromFlags("", kubeconfig)
		if err != nil {
			return nil, fmt.Errorf("load Connection test kubeconfig: %w", err)
		}
		return configuration, nil
	}
	configuration, err := rest.InClusterConfig()
	if err != nil {
		return nil, fmt.Errorf("load in-cluster Kubernetes configuration: %w", err)
	}
	return configuration, nil
}

type readinessDatabase interface {
	Ping(context.Context) error
}

func healthHandler(database readinessDatabase, client kubernetes.Interface) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(response http.ResponseWriter, _ *http.Request) {
		response.Header().Set("Content-Type", "text/plain; charset=utf-8")
		_, _ = response.Write([]byte("ok\n"))
	})
	mux.HandleFunc("GET /readyz", func(response http.ResponseWriter, request *http.Request) {
		readyContext, cancel := context.WithTimeout(request.Context(), 2*time.Second)
		defer cancel()
		if err := database.Ping(readyContext); err != nil {
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
