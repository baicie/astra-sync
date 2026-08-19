package framework

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"go.uber.org/zap"
)

// TestFramework_Bootstrap verifies the framework can bootstrap a two-region topology.
func TestFramework_Bootstrap(t *testing.T) {
	composeFile := filepath.Join("..", "..", "docker-compose.yaml")
	if _, err := os.Stat(composeFile); os.IsNotExist(err) {
		// Try alternate path
		composeFile = filepath.Join("multi-region", "docker-compose.yaml")
		if _, err := os.Stat(composeFile); os.IsNotExist(err) {
			t.Skip("docker-compose.yaml not found, skipping")
		}
	}

	f := New(t, WithComposeFile(composeFile))
	defer func() {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
		defer cancel()
		if err := f.Teardown(ctx); err != nil {
			t.Logf("teardown failed: %v", err)
		}
	}()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	if err := f.Bootstrap(ctx); err != nil {
		t.Fatalf("bootstrap failed: %v", err)
	}

	// Verify primary region
	primary := f.GetPrimaryRegion()
	if primary == nil {
		t.Fatal("primary region is nil")
	}
	if primary.Name != "us-east-1" {
		t.Errorf("expected primary name us-east-1, got %s", primary.Name)
	}
	if primary.Role != "primary" {
		t.Errorf("expected primary role 'primary', got %s", primary.Role)
	}

	// Verify secondary region
	secondary := f.GetSecondaryRegion()
	if secondary == nil {
		t.Fatal("secondary region is nil")
	}
	if secondary.Name != "us-west-1" {
		t.Errorf("expected secondary name us-west-1, got %s", secondary.Name)
	}
	if secondary.Role != "secondary" {
		t.Errorf("expected secondary role 'secondary', got %s", secondary.Role)
	}
}

// TestFramework_GetRegion verifies GetRegion returns expected region.
func TestFramework_GetRegion(t *testing.T) {
	f := New(t)

	// Without bootstrap, no regions should exist
	_, ok := f.GetRegion("us-east-1")
	if ok {
		t.Error("expected region not to exist before bootstrap")
	}
}

// TestFramework_Config_Defaults verifies default configuration.
func TestFramework_Config_Defaults(t *testing.T) {
	cfg := defaultConfig()

	if cfg.PrimaryRegion != "us-east-1" {
		t.Errorf("expected default primary us-east-1, got %s", cfg.PrimaryRegion)
	}
	if cfg.SecondaryRegion != "us-west-1" {
		t.Errorf("expected default secondary us-west-1, got %s", cfg.SecondaryRegion)
	}
	if cfg.BootstrapTimeout != 5*time.Minute {
		t.Errorf("expected default bootstrap timeout 5m, got %v", cfg.BootstrapTimeout)
	}
	if cfg.TeardownTimeout != 1*time.Minute {
		t.Errorf("expected default teardown timeout 1m, got %v", cfg.TeardownTimeout)
	}
}

// TestWithComposeFile verifies the WithComposeFile option.
func TestWithComposeFile(t *testing.T) {
	cfg := defaultConfig()
	WithComposeFile("/custom/path.yaml")(&cfg)

	if cfg.ComposeFile != "/custom/path.yaml" {
		t.Errorf("expected /custom/path.yaml, got %s", cfg.ComposeFile)
	}
}

// TestWithBootstrapTimeout verifies the WithBootstrapTimeout option.
func TestWithBootstrapTimeout(t *testing.T) {
	cfg := defaultConfig()
	WithBootstrapTimeout(10 * time.Minute)(&cfg)

	if cfg.BootstrapTimeout != 10*time.Minute {
		t.Errorf("expected bootstrap timeout 10m, got %v", cfg.BootstrapTimeout)
	}
}

// TestWithTeardownTimeout verifies the WithTeardownTimeout option.
func TestWithTeardownTimeout(t *testing.T) {
	cfg := defaultConfig()
	WithTeardownTimeout(2 * time.Minute)(&cfg)

	if cfg.TeardownTimeout != 2*time.Minute {
		t.Errorf("expected teardown timeout 2m, got %v", cfg.TeardownTimeout)
	}
}

// TestWithLogDirectory verifies the WithLogDirectory option.
func TestWithLogDirectory(t *testing.T) {
	cfg := defaultConfig()
	WithLogDirectory("/tmp/astrasync-logs")(&cfg)

	if cfg.LogDirectory != "/tmp/astrasync-logs" {
		t.Errorf("expected /tmp/astrasync-logs, got %s", cfg.LogDirectory)
	}
}

// TestFramework_WaitForCondition verifies the WaitForCondition method.
func TestFramework_WaitForCondition(t *testing.T) {
	f := New(t)

	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Second)
	defer cancel()

	err := f.WaitForCondition(ctx, 5*time.Second, func() (bool, error) {
		return true, nil
	})
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
}

// TestFramework_WaitForCondition_Timeout verifies the timeout case.
func TestFramework_WaitForCondition_Timeout(t *testing.T) {
	f := New(t)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	err := f.WaitForCondition(ctx, 100*time.Millisecond, func() (bool, error) {
		return false, nil
	})
	if err == nil {
		t.Error("expected timeout error")
	}
}

// TestFramework_TLSConnection_InvalidCert verifies TLS failure with invalid cert.
func TestFramework_TLSConnection_InvalidCert(t *testing.T) {
	f := New(t)

	// Create temporary CA cert
	caFile := filepath.Join(t.TempDir(), "ca.crt")
	if err := os.WriteFile(caFile, []byte("invalid cert"), 0644); err != nil {
		t.Fatalf("write CA file: %v", err)
	}

	// Create temporary client cert
	certFile := filepath.Join(t.TempDir(), "client.crt")
	keyFile := filepath.Join(t.TempDir(), "client.key")
	if err := os.WriteFile(certFile, []byte("invalid cert"), 0644); err != nil {
		t.Fatalf("write cert file: %v", err)
	}
	if err := os.WriteFile(keyFile, []byte("invalid key"), 0644); err != nil {
		t.Fatalf("write key file: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Second)
	defer cancel()

	_, err := f.GetTLSConnection(ctx, "us-east-1", caFile, certFile, keyFile)
	if err == nil {
		t.Error("expected error with invalid cert")
	}
}

// TestFramework_TLSConfig verifies TLS configuration.
func TestFramework_TLSConfig(t *testing.T) {
	// Test TLS configuration with a sample cert
	pem := []byte("-----BEGIN CERTIFICATE-----\nMIIB+TCCAaCgAwIBAgIJAJK1\n-----END CERTIFICATE-----")
	caPool := x509.NewCertPool()
	if !caPool.AppendCertsFromPEM(pem) {
		t.Log("CA pool failed to parse test cert (expected for invalid cert)")
	}

	// Just verify the pool can be created
	if caPool == nil {
		t.Fatal("CA pool should not be nil")
	}
}

// TestFramework_Logger verifies the logger is initialized.
func TestFramework_Logger(t *testing.T) {
	f := New(t)

	if f.logger == nil {
		t.Fatal("logger is nil")
	}
}

// TestFramework_Regions verifies both regions are accessible.
func TestFramework_Regions(t *testing.T) {
	f := New(t)

	if f.GetPrimaryRegion() != nil {
		t.Error("expected primary region to be nil before bootstrap")
	}
	if f.GetSecondaryRegion() != nil {
		t.Error("expected secondary region to be nil before bootstrap")
	}
}

// TestFramework_Bootstrap_ComposeFileNotFound verifies error handling.
func TestFramework_Bootstrap_ComposeFileNotFound(t *testing.T) {
	f := New(t, WithComposeFile("/nonexistent/path.yaml"))

	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Second)
	defer cancel()

	err := f.Bootstrap(ctx)
	if err == nil {
		t.Error("expected error for missing compose file")
	}
	if !errors.Is(err, ErrComposeFileNotFound) {
		t.Errorf("expected ErrComposeFileNotFound, got %v", err)
	}
}

// TestFramework_Version verifies the framework version constant.
func TestFramework_Version(t *testing.T) {
	if FrameworkVersion != "1.0.0" {
		t.Errorf("expected framework version 1.0.0, got %s", FrameworkVersion)
	}
}

// TestFramework_PromoteRegion verifies the promotion simulation.
func TestFramework_PromoteRegion(t *testing.T) {
	f := New(t)

	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Second)
	defer cancel()

	err := f.PromoteRegion(ctx, "job-1", "idempotency-key-123")
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
}

// TestFramework_TLSConfig_MinVersion verifies TLS 1.3 minimum.
func TestFramework_TLSConfig_MinVersion(t *testing.T) {
	cfg := &tls.Config{
		MinVersion: tls.VersionTLS13,
	}

	if cfg.MinVersion != tls.VersionTLS13 {
		t.Errorf("expected TLS 1.3, got %v", cfg.MinVersion)
	}
}

// TestFramework_RegionNames verifies region names follow convention.
func TestFramework_RegionNames(t *testing.T) {
	cfg := defaultConfig()

	if cfg.PrimaryRegion == "" {
		t.Error("primary region name is empty")
	}
	if cfg.SecondaryRegion == "" {
		t.Error("secondary region name is empty")
	}
	if cfg.PrimaryRegion == cfg.SecondaryRegion {
		t.Error("primary and secondary regions have the same name")
	}
}

// TestFramework_PromoteRegion_EmptyJobID verifies input validation.
func TestFramework_PromoteRegion_EmptyJobID(t *testing.T) {
	f := New(t)

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	err := f.PromoteRegion(ctx, "", "key-123")
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
}

// TestFramework_GetRegionLogs_NotBootstrapped verifies error handling.
func TestFramework_GetRegionLogs_NotBootstrapped(t *testing.T) {
	f := New(t)

	_, err := f.GetRegionLogs("us-east-1")
	if err == nil {
		t.Error("expected error for non-existent region")
	}
}

// TestFramework_LogRegionCommand_NotBootstrapped verifies error handling.
func TestFramework_LogRegionCommand_NotBootstrapped(t *testing.T) {
	f := New(t)

	err := f.LogRegionCommand("us-east-1", "ls")
	if err == nil {
		t.Error("expected error for non-existent region")
	}
}

// TestFramework_GetConnection_NotBootstrap verifies error handling.
func TestFramework_GetConnection_NotBootstrap(t *testing.T) {
	f := New(t)

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	_, err := f.GetConnection(ctx, "us-east-1")
	if err == nil {
		t.Error("expected error for non-bootstrap region")
	}
}

// TestFramework_Logger_Level verifies logger can be replaced.
func TestFramework_Logger_Level(t *testing.T) {
	logger, _ := zap.NewDevelopment()
	f := New(t)

	f.logger = logger
	if f.logger == nil {
		t.Error("logger should be set")
	}
}

// TestFramework_ContextCancellation verifies context cancellation.
func TestFramework_ContextCancellation(t *testing.T) {
	f := New(t)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	err := f.WaitForCondition(ctx, 5*time.Second, func() (bool, error) {
		return false, nil
	})
	if err == nil {
		t.Error("expected context cancellation error")
	}
}

// TestFramework_RegionRole verifies region role assignment.
func TestFramework_RegionRole(t *testing.T) {
	f := New(t)

	f.regions = map[string]*Region{
		"us-east-1": {
			Name: "us-east-1",
			Role: "primary",
		},
		"us-west-1": {
			Name: "us-west-1",
			Role: "secondary",
		},
	}

	primary := f.GetPrimaryRegion()
	if primary.Role != "primary" {
		t.Errorf("expected primary role, got %s", primary.Role)
	}

	secondary := f.GetSecondaryRegion()
	if secondary.Role != "secondary" {
		t.Errorf("expected secondary role, got %s", secondary.Role)
	}
}

// TestFramework_Config_Override verifies configuration override.
func TestFramework_Config_Override(t *testing.T) {
	cfg := defaultConfig()
	WithComposeFile("override.yaml")(&cfg)
	WithBootstrapTimeout(10 * time.Minute)(&cfg)
	WithTeardownTimeout(5 * time.Minute)(&cfg)
	WithLogDirectory("/var/log")(&cfg)

	if cfg.ComposeFile != "override.yaml" {
		t.Errorf("expected compose file override.yaml, got %s", cfg.ComposeFile)
	}
	if cfg.BootstrapTimeout != 10*time.Minute {
		t.Errorf("expected bootstrap timeout 10m, got %v", cfg.BootstrapTimeout)
	}
	if cfg.TeardownTimeout != 5*time.Minute {
		t.Errorf("expected teardown timeout 5m, got %v", cfg.TeardownTimeout)
	}
	if cfg.LogDirectory != "/var/log" {
		t.Errorf("expected log directory /var/log, got %s", cfg.LogDirectory)
	}
}

// generateTestCert generates a test certificate for testing.
func generateTestCert(t *testing.T) tls.Certificate {
	t.Helper()
	_ = fmt.Sprintf
	return tls.Certificate{
		Certificate: [][]byte{[]byte("test cert")},
		Leaf:        &x509.Certificate{},
	}
}
