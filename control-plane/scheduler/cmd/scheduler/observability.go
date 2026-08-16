package main

import (
	"context"
	"io"
	"log/slog"
	"net"
	"net/http"
	"strings"
	"time"

	"io.astrasync/control-plane/scheduler/internal/metrics"
)

func newComponentLogger(component string, output io.Writer, levelText string) *slog.Logger {
	level := slog.LevelInfo
	if err := level.UnmarshalText([]byte(strings.TrimSpace(levelText))); err != nil {
		level = slog.LevelInfo
	}
	return slog.New(slog.NewJSONHandler(output, &slog.HandlerOptions{Level: level})).With("component", component)
}

// metricsServer hosts the /metrics endpoint on a dedicated port that is
// independent of the Scheduler's gRPC and HTTP listeners. An empty address
// disables the listener so the Helm monitoring toggle remains fail-closed.
func metricsServer(ctx context.Context, logger *slog.Logger, listenAddress string) (*http.Server, error) {
	listenAddress = strings.TrimSpace(listenAddress)
	if listenAddress == "" {
		return nil, nil
	}
	listener, err := net.Listen("tcp", listenAddress)
	if err != nil {
		return nil, err
	}
	server := &http.Server{
		Handler:           metrics.Handler(),
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
