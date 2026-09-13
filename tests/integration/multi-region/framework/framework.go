// Package framework provides the multi-region end-to-end testing framework.
package framework

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"go.uber.org/zap"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/credentials/insecure"
	controlv1 "io.astrasync/control-plane/api-server/gen/go/v1"
)

// Common errors for the test framework.
var (
	ErrRegionNotReady        = errors.New("framework: region not ready")
	ErrComposeFileNotFound   = errors.New("framework: docker-compose file not found")
	ErrRegionBootstrapFailed = errors.New("framework: region bootstrap failed")
	ErrRegionTeardownFailed  = errors.New("framework: region teardown failed")
)

// Region represents a single region in the multi-region topology.
type Region struct {
	Name          string
	Role          string // "primary" or "secondary"
	APIServerURI  string
	PostgresURI   string
	ObjectStorage string
	Network       string
	HTTPURI       string
}

// Config holds the configuration for the test framework.
type Config struct {
	// ComposeFile is the path to the docker-compose file.
	ComposeFile string
	// PrimaryRegion is the name of the primary region.
	PrimaryRegion string
	// SecondaryRegion is the name of the secondary region.
	SecondaryRegion string
	// BootstrapTimeout is the time to wait for region bootstrap.
	BootstrapTimeout time.Duration
	// TeardownTimeout is the time to wait for region teardown.
	TeardownTimeout time.Duration
	// LogDirectory is the directory for test logs.
	LogDirectory  string
	AllowInsecure bool
}

// Option is a functional option for framework configuration.
type Option func(*Config)

// WithComposeFile sets the docker-compose file path.
func WithComposeFile(path string) Option {
	return func(c *Config) {
		c.ComposeFile = path
	}
}

// WithBootstrapTimeout sets the bootstrap timeout.
func WithBootstrapTimeout(d time.Duration) Option {
	return func(c *Config) {
		c.BootstrapTimeout = d
	}
}

// WithTeardownTimeout sets the teardown timeout.
func WithTeardownTimeout(d time.Duration) Option {
	return func(c *Config) {
		c.TeardownTimeout = d
	}
}

// WithLogDirectory sets the log directory.
func WithLogDirectory(dir string) Option {
	return func(c *Config) {
		c.LogDirectory = dir
	}
}

// WithInsecureTransportForTest explicitly allows insecure local transport.
func WithInsecureTransportForTest() Option {
	return func(c *Config) { c.AllowInsecure = true }
}

// defaultConfig returns the default configuration.
func defaultConfig() Config {
	return Config{
		ComposeFile:      "docker-compose.yaml",
		PrimaryRegion:    "us-east-1",
		SecondaryRegion:  "us-west-1",
		BootstrapTimeout: 5 * time.Minute,
		TeardownTimeout:  1 * time.Minute,
		LogDirectory:     "test-logs",
	}
}

// Framework is the multi-region test framework.
type Framework struct {
	t      *testing.T
	cfg    Config
	logger *zap.Logger

	mu      sync.RWMutex
	regions map[string]*Region
	conns   map[string]*grpc.ClientConn
	ready   bool
}

// New creates a new test framework.
func New(t *testing.T, opts ...Option) *Framework {
	t.Helper()

	cfg := defaultConfig()
	for _, opt := range opts {
		opt(&cfg)
	}

	logger, _ := zap.NewDevelopment()

	return &Framework{
		t:       t,
		cfg:     cfg,
		logger:  logger,
		regions: make(map[string]*Region),
		conns:   make(map[string]*grpc.ClientConn),
	}
}

// Bootstrap brings up the multi-region topology.
func (f *Framework) Bootstrap(ctx context.Context) error {
	f.mu.Lock()
	if f.ready {
		f.mu.Unlock()
		return nil
	}
	f.mu.Unlock()

	if _, err := os.Stat(f.cfg.ComposeFile); err != nil {
		return fmt.Errorf("%w: %v", ErrComposeFileNotFound, err)
	}

	// Create log directory
	if err := os.MkdirAll(f.cfg.LogDirectory, 0755); err != nil {
		return fmt.Errorf("create log directory: %w", err)
	}

	// Integration runs own the Compose project and reset its disposable databases.
	if err := f.runCompose(ctx, "down", "-v", "--remove-orphans"); err != nil {
		f.logger.Debug("compose cleanup before bootstrap failed", zap.Error(err))
	}
	if err := f.runCompose(ctx, "up", "-d", "--wait"); err != nil {
		return fmt.Errorf("%w: %v", ErrRegionBootstrapFailed, err)
	}

	// Initialize regions
	f.regions[f.cfg.PrimaryRegion] = &Region{
		Name:         f.cfg.PrimaryRegion,
		Role:         "primary",
		APIServerURI: "localhost:50051",
		PostgresURI:  "postgres://user:pass@localhost:5432/primary",
		Network:      "astrasync-primary",
		HTTPURI:      "http://localhost:8080",
	}

	f.regions[f.cfg.SecondaryRegion] = &Region{
		Name:         f.cfg.SecondaryRegion,
		Role:         "secondary",
		APIServerURI: "localhost:15061",
		PostgresURI:  "postgres://user:pass@localhost:5433/secondary",
		Network:      "astrasync-secondary",
		HTTPURI:      "http://localhost:8081",
	}

	// Wait for both regions to be healthy
	if err := f.waitForHealthy(ctx); err != nil {
		_ = f.runCompose(context.Background(), "down", "-v", "--remove-orphans")
		return fmt.Errorf("wait for healthy: %w", err)
	}

	f.mu.Lock()
	f.ready = true
	f.mu.Unlock()
	return nil
}

// Teardown shuts down the multi-region topology.
func (f *Framework) Teardown(ctx context.Context) error {
	f.mu.Lock()
	defer f.mu.Unlock()

	if !f.ready {
		return nil
	}

	// Close gRPC connections
	for _, conn := range f.conns {
		if err := conn.Close(); err != nil {
			f.logger.Warn("failed to close connection", zap.Error(err))
		}
	}

	// Stop docker-compose
	if err := f.runCompose(ctx, "down", "-v", "--remove-orphans"); err != nil {
		return fmt.Errorf("%w: %v", ErrRegionTeardownFailed, err)
	}

	f.ready = false
	return nil
}

// GetPrimaryRegion returns the primary region.
func (f *Framework) GetPrimaryRegion() *Region {
	f.mu.RLock()
	defer f.mu.RUnlock()
	return f.regions[f.cfg.PrimaryRegion]
}

// GetSecondaryRegion returns the secondary region.
func (f *Framework) GetSecondaryRegion() *Region {
	f.mu.RLock()
	defer f.mu.RUnlock()
	return f.regions[f.cfg.SecondaryRegion]
}

// GetRegion returns the region by name.
func (f *Framework) GetRegion(name string) (*Region, bool) {
	f.mu.RLock()
	defer f.mu.RUnlock()
	r, ok := f.regions[name]
	return r, ok
}

// GetConnection returns a gRPC connection to a region.
func (f *Framework) GetConnection(ctx context.Context, regionName string) (*grpc.ClientConn, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	if conn, ok := f.conns[regionName]; ok {
		return conn, nil
	}

	region, ok := f.regions[regionName]
	if !ok {
		return nil, fmt.Errorf("region not found: %s", regionName)
	}

	if !f.cfg.AllowInsecure {
		return nil, errors.New("TLS transport is required; explicitly enable insecure test transport")
	}
	conn, err := grpc.DialContext(ctx, region.APIServerURI, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		return nil, fmt.Errorf("dial %s: %w", region.APIServerURI, err)
	}

	f.conns[regionName] = conn
	return conn, nil
}

// GetTLSConnection returns a gRPC connection with TLS to a region.
func (f *Framework) GetTLSConnection(ctx context.Context, regionName, caCertPath, clientCertPath, clientKeyPath string) (*grpc.ClientConn, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	region, ok := f.regions[regionName]
	if !ok {
		return nil, fmt.Errorf("region not found: %s", regionName)
	}

	// Load CA cert
	caCert, err := os.ReadFile(caCertPath)
	if err != nil {
		return nil, fmt.Errorf("read CA cert: %w", err)
	}

	caPool := x509.NewCertPool()
	if !caPool.AppendCertsFromPEM(caCert) {
		return nil, errors.New("failed to append CA cert")
	}

	// Load client cert
	clientCert, err := tls.LoadX509KeyPair(clientCertPath, clientKeyPath)
	if err != nil {
		return nil, fmt.Errorf("load client cert: %w", err)
	}

	tlsConfig := &tls.Config{
		RootCAs:      caPool,
		Certificates: []tls.Certificate{clientCert},
		MinVersion:   tls.VersionTLS13,
	}

	conn, err := grpc.DialContext(ctx, region.APIServerURI,
		grpc.WithTransportCredentials(credentials.NewTLS(tlsConfig)))
	if err != nil {
		return nil, fmt.Errorf("dial %s: %w", region.APIServerURI, err)
	}

	f.conns[regionName] = conn
	return conn, nil
}

// waitForHealthy waits for both regions to be healthy.
func (f *Framework) waitForHealthy(ctx context.Context) error {
	timeout := time.After(f.cfg.BootstrapTimeout)
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-timeout:
			return ErrRegionNotReady
		case <-ticker.C:
			ready, err := f.checkRegionsHealth(ctx)
			if err != nil {
				f.logger.Warn("check regions health", zap.Error(err))
				continue
			}
			if ready {
				return nil
			}
		}
	}
}

// checkRegionsHealth checks if both regions are healthy.
func (f *Framework) checkRegionsHealth(ctx context.Context) (bool, error) {
	f.mu.RLock()
	regions := make([]*Region, 0, len(f.regions))
	for _, region := range f.regions {
		regions = append(regions, region)
	}
	f.mu.RUnlock()
	client := &http.Client{Timeout: 2 * time.Second}
	for _, region := range regions {
		request, err := http.NewRequestWithContext(ctx, http.MethodGet, region.HTTPURI+"/ready", nil)
		if err != nil {
			return false, err
		}
		response, err := client.Do(request)
		if err != nil {
			return false, err
		}
		_ = response.Body.Close()
		if response.StatusCode != http.StatusOK {
			return false, fmt.Errorf("region %s health returned %d", region.Name, response.StatusCode)
		}
	}
	return true, nil
}

// runCompose runs a docker-compose command.
func (f *Framework) runCompose(ctx context.Context, args ...string) error {
	f.logger.Info("running docker-compose", zap.Strings("args", args))

	composeFile, err := filepath.Abs(f.cfg.ComposeFile)
	if err != nil {
		return fmt.Errorf("resolve compose file path: %w", err)
	}
	composeDir := filepath.Dir(composeFile)

	cmd := exec.CommandContext(ctx, "docker", append([]string{"compose", "-f", composeFile}, args...)...)
	cmd.Dir = composeDir

	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("docker-compose %s: %v\n%s", strings.Join(args, " "), err, output)
	}

	f.logger.Debug("docker-compose output", zap.String("output", string(output)))
	return nil
}

// LogRegionCommand runs a command in a region container.
func (f *Framework) LogRegionCommand(regionName string, args ...string) error {
	f.mu.RLock()
	_, ok := f.regions[regionName]
	f.mu.RUnlock()

	if !ok {
		return fmt.Errorf("region not found: %s", regionName)
	}

	serviceName := "api-server-primary"
	if regionName == f.cfg.SecondaryRegion {
		serviceName = "api-server-secondary"
	}
	composeFile, err := filepath.Abs(f.cfg.ComposeFile)
	if err != nil {
		return fmt.Errorf("resolve compose file path: %w", err)
	}
	composeDir := filepath.Dir(composeFile)
	args = append([]string{"compose", "-f", composeFile, "exec", serviceName}, args...)
	cmd := exec.Command("docker", args...)
	cmd.Dir = composeDir
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("docker exec %s: %v\n%s", strings.Join(args, " "), err, output)
	}

	f.logger.Info("command executed in region",
		zap.String("region", regionName),
		zap.String("output", string(output)))
	return nil
}

// GetRegionLogs returns the logs for a region container.
func (f *Framework) GetRegionLogs(regionName string) (string, error) {
	f.mu.RLock()
	_, ok := f.regions[regionName]
	f.mu.RUnlock()

	if !ok {
		return "", fmt.Errorf("region not found: %s", regionName)
	}

	serviceName := "api-server-primary"
	if regionName == f.cfg.SecondaryRegion {
		serviceName = "api-server-secondary"
	}
	composeFile, err := filepath.Abs(f.cfg.ComposeFile)
	if err != nil {
		return "", fmt.Errorf("resolve compose file path: %w", err)
	}
	composeDir := filepath.Dir(composeFile)
	cmd := exec.Command("docker", "compose", "-f", composeFile, "logs", serviceName)
	cmd.Dir = composeDir
	output, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("docker compose logs %s: %v", serviceName, err)
	}

	return string(output), nil
}

// StartRegion starts one API Server service without rebuilding the topology.
func (f *Framework) StartRegion(ctx context.Context, regionName string) error {
	return f.runCompose(ctx, "start", f.serviceName(regionName))
}

// StopRegion stops one API Server service and leaves its persistent state intact.
func (f *Framework) StopRegion(ctx context.Context, regionName string) error {
	f.mu.Lock()
	if conn, ok := f.conns[regionName]; ok {
		_ = conn.Close()
		delete(f.conns, regionName)
	}
	f.mu.Unlock()
	return f.runCompose(ctx, "stop", f.serviceName(regionName))
}

// RestartRegion restarts one API Server service and waits for its HTTP readiness endpoint.
func (f *Framework) RestartRegion(ctx context.Context, regionName string) error {
	if err := f.runCompose(ctx, "restart", f.serviceName(regionName)); err != nil {
		return err
	}
	return f.WaitForHTTPReady(ctx, regionName)
}

// WaitForGRPCReady waits until a region accepts a gRPC connection.
func (f *Framework) WaitForGRPCReady(ctx context.Context, regionName string) error {
	return f.WaitForCondition(ctx, f.cfg.BootstrapTimeout, func() (bool, error) {
		region, ok := f.GetRegion(regionName)
		if !ok {
			return false, fmt.Errorf("region not found: %s", regionName)
		}
		probeCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
		defer cancel()
		conn, err := grpc.DialContext(probeCtx, region.APIServerURI, grpc.WithTransportCredentials(insecure.NewCredentials()), grpc.WithBlock())
		if err != nil {
			return false, nil
		}
		_ = conn.Close()
		return true, nil
	})
}

// WaitForHTTPReady waits until a region reports ready.
func (f *Framework) WaitForHTTPReady(ctx context.Context, regionName string) error {
	return f.WaitForCondition(ctx, f.cfg.BootstrapTimeout, func() (bool, error) {
		region, ok := f.GetRegion(regionName)
		if !ok {
			return false, fmt.Errorf("region not found: %s", regionName)
		}
		request, err := http.NewRequestWithContext(ctx, http.MethodGet, region.HTTPURI+"/ready", nil)
		if err != nil {
			return false, err
		}
		response, err := (&http.Client{Timeout: 2 * time.Second}).Do(request)
		if err != nil {
			return false, nil
		}
		_ = response.Body.Close()
		return response.StatusCode == http.StatusOK, nil
	})
}

// WaitForHTTPUnavailable waits until a region no longer reports ready.
func (f *Framework) WaitForHTTPUnavailable(ctx context.Context, regionName string) error {
	return f.WaitForCondition(ctx, f.cfg.BootstrapTimeout, func() (bool, error) {
		region, ok := f.GetRegion(regionName)
		if !ok {
			return false, fmt.Errorf("region not found: %s", regionName)
		}
		request, err := http.NewRequestWithContext(ctx, http.MethodGet, region.HTTPURI+"/ready", nil)
		if err != nil {
			return false, err
		}
		response, err := (&http.Client{Timeout: 2 * time.Second}).Do(request)
		if err != nil {
			return true, nil
		}
		_ = response.Body.Close()
		return response.StatusCode != http.StatusOK, nil
	})
}

// WaitForGRPCUnavailable waits until a region refuses gRPC connections.
func (f *Framework) WaitForGRPCUnavailable(ctx context.Context, regionName string) error {
	return f.WaitForCondition(ctx, f.cfg.BootstrapTimeout, func() (bool, error) {
		region, ok := f.GetRegion(regionName)
		if !ok {
			return false, fmt.Errorf("region not found: %s", regionName)
		}
		probeCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
		defer cancel()
		conn, err := grpc.DialContext(probeCtx, region.APIServerURI, grpc.WithTransportCredentials(insecure.NewCredentials()), grpc.WithBlock())
		if err != nil {
			return true, nil
		}
		_ = conn.Close()
		return false, nil
	})
}

// DisconnectRegions stops the source API service to make delivery unavailable.
func (f *Framework) DisconnectRegions(ctx context.Context, source, _ string) error {
	return f.StopRegion(ctx, source)
}

// ReconnectRegions starts the source API service and waits for readiness.
func (f *Framework) ReconnectRegions(ctx context.Context, source, _ string) error {
	return f.StartRegion(ctx, source)
}

func (f *Framework) serviceName(regionName string) string {
	if regionName == f.cfg.SecondaryRegion {
		return "api-server-secondary"
	}
	return "api-server-primary"
}

// PromoteRegion promotes the secondary region.
func (f *Framework) PromoteRegion(ctx context.Context, jobID, idempotencyKey string) error {
	if jobID == "" || len(idempotencyKey) < 16 {
		return errors.New("job ID and idempotency key are required")
	}
	conn, err := f.GetConnection(ctx, f.cfg.SecondaryRegion)
	if err != nil {
		return err
	}
	_, err = controlv1.NewRegionPromotionServiceClient(conn).PromoteRegion(ctx, &controlv1.PromoteRegionRequest{
		JobId: jobID, TargetRegion: f.cfg.SecondaryRegion, IdempotencyKey: idempotencyKey,
	})
	return err
}

// WaitForCondition waits for a condition to be true.
func (f *Framework) WaitForCondition(ctx context.Context, timeout time.Duration, condition func() (bool, error)) error {
	timer := time.After(timeout)
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-timer:
			return errors.New("condition timeout")
		case <-ticker.C:
			ok, err := condition()
			if err != nil {
				f.logger.Warn("condition check", zap.Error(err))
				continue
			}
			if ok {
				return nil
			}
		}
	}
}

// FrameworkVersion is the framework version.
const FrameworkVersion = "1.0.0"
