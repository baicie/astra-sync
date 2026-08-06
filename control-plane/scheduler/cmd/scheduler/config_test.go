package main

import (
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
)

func TestLoadConfigBuildsValidatedSchedulerAndExecutionDefaults(t *testing.T) {
	environment := map[string]string{
		"DATABASE_URL":                "postgres://scheduler",
		"POD_NAME":                    "scheduler-0",
		"POD_NAMESPACE":               "astrasync",
		"SCHEDULER_PROGRESS_CLAIM":    "progress",
		"SCHEDULER_COORDINATOR_IMAGE": "coordinator:test",
		"SCHEDULER_WORKER_IMAGE":      "worker:test",
		"SCHEDULER_HEARTBEAT_URL":     "http://scheduler:8082",
	}
	configuration, err := loadConfig(func(name string) string { return environment[name] })
	if err != nil {
		t.Fatalf("load config: %v", err)
	}
	if configuration.scheduler.OwnerID != "scheduler-0" || configuration.scheduler.MaximumActive != 1 {
		t.Fatalf("unexpected scheduler identity/capacity: %+v", configuration.scheduler)
	}
	if configuration.scheduler.LeaseDuration != 30*time.Second ||
		configuration.scheduler.HeartbeatTimeout != 2*time.Minute ||
		configuration.scheduler.OperationTimeout != 10*time.Second {
		t.Fatalf("unexpected scheduler timing: %+v", configuration.scheduler)
	}
	if configuration.dispatcher.Namespace != "astrasync" || configuration.dispatcher.WorkerReplicas != 2 {
		t.Fatalf("unexpected dispatch defaults: %+v", configuration.dispatcher)
	}
	if configuration.dispatcher.HeartbeatURL != "http://scheduler:8082" ||
		configuration.dispatcher.HeartbeatIntervalMillis != 10000 {
		t.Fatalf("unexpected heartbeat dispatch config: %+v", configuration.dispatcher)
	}
	if configuration.dispatcher.ImagePullPolicy != corev1.PullIfNotPresent {
		t.Fatalf("unexpected image pull policy: %s", configuration.dispatcher.ImagePullPolicy)
	}
}

func TestLoadConfigRejectsMissingIdentityAndInvalidExecutionCapacity(t *testing.T) {
	if _, err := loadConfig(func(string) string { return "" }); err == nil {
		t.Fatal("expected missing database URL failure")
	}
	environment := map[string]string{
		"DATABASE_URL":                "postgres://scheduler",
		"POD_NAME":                    "scheduler-0",
		"SCHEDULER_PROGRESS_CLAIM":    "progress",
		"SCHEDULER_COORDINATOR_IMAGE": "coordinator:test",
		"SCHEDULER_WORKER_IMAGE":      "worker:test",
		"SCHEDULER_HEARTBEAT_URL":     "http://scheduler:8082",
		"SCHEDULER_WORKER_PORT":       "70000",
	}
	if _, err := loadConfig(func(name string) string { return environment[name] }); err == nil {
		t.Fatal("expected invalid Worker port failure")
	}
}

func TestLoadConfigRequiresHeartbeatTimeoutToCoverMissedReports(t *testing.T) {
	environment := map[string]string{
		"DATABASE_URL":                    "postgres://scheduler",
		"POD_NAME":                        "scheduler-0",
		"SCHEDULER_PROGRESS_CLAIM":        "progress",
		"SCHEDULER_COORDINATOR_IMAGE":     "coordinator:test",
		"SCHEDULER_WORKER_IMAGE":          "worker:test",
		"SCHEDULER_HEARTBEAT_URL":         "http://scheduler:8082",
		"SCHEDULER_HEARTBEAT_INTERVAL_MS": "10000",
		"SCHEDULER_HEARTBEAT_TIMEOUT":     "20s",
	}
	if _, err := loadConfig(func(name string) string { return environment[name] }); err == nil {
		t.Fatal("expected heartbeat timeout/interval validation failure")
	}
}
