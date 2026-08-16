package main

import (
	"context"
	"log/slog"
	"net"
	"net/http"
	"time"

	consolemetrics "io.astrasync/console/observability"
)

// metricsServer hosts the /metrics endpoint on a dedicated port that is
// independent of the Console's HTTP listener. The deployment defaults
// to :9090; the Prometheus scrape configuration in
// deployment/helm/astrasync matches.
func metricsServer(ctx context.Context, logger *slog.Logger, listenAddress string) (*http.Server, error) {
	if listenAddress == "" {
		listenAddress = ":9090"
	}
	listener, err := net.Listen("tcp", listenAddress)
	if err != nil {
		return nil, err
	}
	server := &http.Server{
		Handler:           consolemetrics.Handler(),
		ReadHeaderTimeout: 5 * time.Second,
	}
	go func() {
		logger.Info("metrics listener started", "address", listener.Addr().String())
		if serveErr := server.Serve(listener); serveErr != nil && serveErr != http.ErrServerClosed {
			logger.Error("metrics listener failed", "error", serveErr.Error())
		}
	}()
	go func() {
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if shutdownErr := server.Shutdown(shutdownCtx); shutdownErr != nil {
			logger.Error("metrics listener shutdown failed", "error", shutdownErr.Error())
		}
	}()
	return server, nil
}
