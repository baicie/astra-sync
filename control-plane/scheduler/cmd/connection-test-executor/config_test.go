package main

import (
	"testing"
	"time"
)

func TestLoadConfigBuildsBoundedExecutorDefaults(t *testing.T) {
	t.Parallel()
	values := map[string]string{
		"DATABASE_URL": "postgres://test", "POD_NAME": "executor-0",
	}
	configuration, err := loadConfig(func(name string) string { return values[name] })
	if err != nil {
		t.Fatalf("load config: %v", err)
	}
	if configuration.executor.ExecutorID != "executor-0" ||
		configuration.executor.Concurrency != 4 || configuration.executor.ClaimBatch != 4 ||
		configuration.executor.ProbeTimeout != 20*time.Second ||
		configuration.executor.LeaseDuration != 30*time.Second ||
		configuration.maximumDNSAnswers != 8 {
		t.Fatalf("unexpected defaults: %+v", configuration)
	}
}

func TestLoadConfigRejectsUnsafeResolverAndTiming(t *testing.T) {
	t.Parallel()
	for _, test := range []map[string]string{
		{
			"DATABASE_URL": "postgres://test", "POD_NAME": "executor-0",
			"CONNECTION_TEST_DNS_SERVER": "dns.example:53",
		},
		{
			"DATABASE_URL": "postgres://test", "POD_NAME": "executor-0",
			"CONNECTION_TEST_PROBE_TIMEOUT": "20s", "CONNECTION_TEST_LEASE_DURATION": "20s",
		},
		{
			"DATABASE_URL": "postgres://test", "POD_NAME": "executor-0",
			"CONNECTION_TEST_CONCURRENCY": "2", "CONNECTION_TEST_CLAIM_BATCH": "3",
		},
	} {
		values := test
		if _, err := loadConfig(func(name string) string { return values[name] }); err == nil {
			t.Fatalf("expected configuration to be rejected: %+v", values)
		}
	}
}
