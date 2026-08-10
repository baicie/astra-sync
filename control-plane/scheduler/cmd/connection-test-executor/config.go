package main

import (
	"fmt"
	"net"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"

	"io.astrasync/control-plane/scheduler/internal/connectiontest"
)

type applicationConfig struct {
	databaseURL       string
	kubeconfig        string
	healthAddress     string
	dnsServer         string
	protectedCIDRs    []string
	maximumDNSAnswers int
	dialTimeout       time.Duration
	kubernetesTimeout time.Duration
	executor          connectiontest.ExecutorConfig
}

func loadConfig(getenv func(string) string) (applicationConfig, error) {
	databaseURL := strings.TrimSpace(getenv("DATABASE_URL"))
	if databaseURL == "" {
		return applicationConfig{}, fmt.Errorf("DATABASE_URL must be configured")
	}
	executorID := strings.TrimSpace(valueOrDefault(
		getenv("CONNECTION_TEST_EXECUTOR_ID"), getenv("POD_NAME"),
	))
	if executorID == "" {
		hostname, err := os.Hostname()
		if err != nil {
			return applicationConfig{}, fmt.Errorf("resolve Connection test executor identity: %w", err)
		}
		executorID = hostname + "-" + uuid.NewString()
	}
	concurrency, err := boundedInteger(getenv, "CONNECTION_TEST_CONCURRENCY", 4, 1, 64)
	if err != nil {
		return applicationConfig{}, err
	}
	claimBatch, err := boundedInteger(getenv, "CONNECTION_TEST_CLAIM_BATCH", concurrency, 1, concurrency)
	if err != nil {
		return applicationConfig{}, err
	}
	claimInterval, err := boundedDuration(
		getenv, "CONNECTION_TEST_CLAIM_INTERVAL", time.Second, 100*time.Millisecond, time.Minute,
	)
	if err != nil {
		return applicationConfig{}, err
	}
	probeTimeout, err := boundedDuration(
		getenv, "CONNECTION_TEST_PROBE_TIMEOUT", 20*time.Second, time.Second, 2*time.Minute,
	)
	if err != nil {
		return applicationConfig{}, err
	}
	completionTimeout, err := boundedDuration(
		getenv, "CONNECTION_TEST_COMPLETION_TIMEOUT", 3*time.Second, 100*time.Millisecond, 10*time.Second,
	)
	if err != nil {
		return applicationConfig{}, err
	}
	leaseDuration, err := boundedDuration(
		getenv, "CONNECTION_TEST_LEASE_DURATION", 30*time.Second,
		probeTimeout+completionTimeout+time.Millisecond, 5*time.Minute,
	)
	if err != nil {
		return applicationConfig{}, err
	}
	dialTimeout, err := boundedDuration(
		getenv, "CONNECTION_TEST_DIAL_TIMEOUT", 5*time.Second, 100*time.Millisecond, 30*time.Second,
	)
	if err != nil {
		return applicationConfig{}, err
	}
	kubernetesTimeout, err := boundedDuration(
		getenv, "CONNECTION_TEST_KUBERNETES_TIMEOUT", 5*time.Second, 100*time.Millisecond, 30*time.Second,
	)
	if err != nil {
		return applicationConfig{}, err
	}
	maximumDNSAnswers, err := boundedInteger(getenv, "CONNECTION_TEST_MAX_DNS_ANSWERS", 8, 1, 32)
	if err != nil {
		return applicationConfig{}, err
	}
	dnsServer := strings.TrimSpace(getenv("CONNECTION_TEST_DNS_SERVER"))
	if dnsServer != "" {
		host, port, splitErr := net.SplitHostPort(dnsServer)
		parsedPort, portErr := strconv.ParseUint(port, 10, 16)
		if splitErr != nil || net.ParseIP(host) == nil || portErr != nil || parsedPort == 0 {
			return applicationConfig{}, fmt.Errorf("CONNECTION_TEST_DNS_SERVER must be an IP address and port")
		}
	}
	protectedCIDRs := splitList(getenv("CONNECTION_TEST_PROTECTED_CIDRS"))
	configuration := applicationConfig{
		databaseURL: databaseURL, kubeconfig: strings.TrimSpace(getenv("KUBECONFIG")),
		healthAddress: valueOrDefault(getenv("HEALTH_LISTEN_ADDRESS"), ":8083"),
		dnsServer:     dnsServer, protectedCIDRs: protectedCIDRs,
		maximumDNSAnswers: maximumDNSAnswers, dialTimeout: dialTimeout,
		kubernetesTimeout: kubernetesTimeout,
		executor: connectiontest.ExecutorConfig{
			ExecutorID: executorID, Concurrency: concurrency, ClaimBatch: claimBatch,
			ClaimInterval: claimInterval, LeaseDuration: leaseDuration,
			ProbeTimeout: probeTimeout, CompletionTimeout: completionTimeout,
		},
	}
	if err := configuration.executor.Validate(); err != nil {
		return applicationConfig{}, err
	}
	return configuration, nil
}

func boundedInteger(
	getenv func(string) string, name string, defaultValue, minimum, maximum int,
) (int, error) {
	value := strings.TrimSpace(getenv(name))
	if value == "" {
		return defaultValue, nil
	}
	parsed, err := strconv.Atoi(value)
	if err != nil || parsed < minimum || parsed > maximum {
		return 0, fmt.Errorf("%s must be between %d and %d", name, minimum, maximum)
	}
	return parsed, nil
}

func boundedDuration(
	getenv func(string) string,
	name string,
	defaultValue, minimum, maximum time.Duration,
) (time.Duration, error) {
	value := strings.TrimSpace(getenv(name))
	if value == "" {
		return defaultValue, nil
	}
	parsed, err := time.ParseDuration(value)
	if err != nil || parsed < minimum || parsed > maximum {
		return 0, fmt.Errorf("%s must be between %s and %s", name, minimum, maximum)
	}
	return parsed, nil
}

func splitList(value string) []string {
	result := make([]string, 0)
	for _, item := range strings.Split(value, ",") {
		if item = strings.TrimSpace(item); item != "" {
			result = append(result, item)
		}
	}
	return result
}

func valueOrDefault(value, defaultValue string) string {
	if strings.TrimSpace(value) == "" {
		return defaultValue
	}
	return value
}
