package main

import (
	"fmt"
	"math"
	"os"
	"strconv"
	"time"

	"github.com/google/uuid"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"

	dispatchkube "io.astrasync/control-plane/scheduler/internal/kubernetes"
	schedulerinternal "io.astrasync/control-plane/scheduler/internal/scheduler"
)

type applicationConfig struct {
	databaseURL   string
	kubeconfig    string
	healthAddress string
	scheduler     schedulerinternal.Config
	dispatcher    dispatchkube.Config
}

func loadConfig(getenv func(string) string) (applicationConfig, error) {
	databaseURL, err := required(getenv, "DATABASE_URL")
	if err != nil {
		return applicationConfig{}, err
	}
	namespace := valueOrDefault(getenv("SCHEDULER_DISPATCH_NAMESPACE"), getenv("POD_NAMESPACE"))
	if namespace == "" {
		namespace = "default"
	}
	ownerID := valueOrDefault(getenv("SCHEDULER_ID"), getenv("POD_NAME"))
	if ownerID == "" {
		hostname, hostErr := os.Hostname()
		if hostErr != nil {
			return applicationConfig{}, fmt.Errorf("resolve scheduler identity: %w", hostErr)
		}
		ownerID = hostname + "-" + uuid.NewString()
	}
	maximumActive, err := positiveInt(getenv, "SCHEDULER_MAX_CONCURRENT_JOBS", 1)
	if err != nil {
		return applicationConfig{}, err
	}
	leaseDuration, err := duration(getenv, "SCHEDULER_LEASE_DURATION", 30*time.Second)
	if err != nil {
		return applicationConfig{}, err
	}
	heartbeatTimeout, err := duration(getenv, "SCHEDULER_HEARTBEAT_TIMEOUT", 2*time.Minute)
	if err != nil {
		return applicationConfig{}, err
	}
	reconcileEvery, err := duration(getenv, "SCHEDULER_RECONCILE_INTERVAL", 5*time.Second)
	if err != nil {
		return applicationConfig{}, err
	}
	operationTimeout, err := duration(getenv, "SCHEDULER_OPERATION_TIMEOUT", 10*time.Second)
	if err != nil {
		return applicationConfig{}, err
	}
	progressClaim, err := required(getenv, "SCHEDULER_PROGRESS_CLAIM")
	if err != nil {
		return applicationConfig{}, err
	}
	coordinatorImage, err := required(getenv, "SCHEDULER_COORDINATOR_IMAGE")
	if err != nil {
		return applicationConfig{}, err
	}
	workerImage, err := required(getenv, "SCHEDULER_WORKER_IMAGE")
	if err != nil {
		return applicationConfig{}, err
	}
	heartbeatURL, err := required(getenv, "SCHEDULER_HEARTBEAT_URL")
	if err != nil {
		return applicationConfig{}, err
	}
	heartbeatInterval, err := positiveInt32(getenv, "SCHEDULER_HEARTBEAT_INTERVAL_MS", 10000)
	if err != nil {
		return applicationConfig{}, err
	}
	if heartbeatTimeout <= 2*time.Duration(heartbeatInterval)*time.Millisecond {
		return applicationConfig{}, fmt.Errorf("SCHEDULER_HEARTBEAT_TIMEOUT must exceed two heartbeat intervals")
	}
	pullPolicy, err := imagePullPolicy(valueOrDefault(getenv("SCHEDULER_IMAGE_PULL_POLICY"), "IfNotPresent"))
	if err != nil {
		return applicationConfig{}, err
	}
	workerReplicas, err := positiveInt32(getenv, "SCHEDULER_WORKER_REPLICAS", 2)
	if err != nil {
		return applicationConfig{}, err
	}
	workerPort, err := positiveInt32(getenv, "SCHEDULER_WORKER_PORT", 50051)
	if err != nil || workerPort > 65535 {
		return applicationConfig{}, fmt.Errorf("SCHEDULER_WORKER_PORT must be between 1 and 65535")
	}
	workerTimeout, err := positiveInt32(getenv, "SCHEDULER_WORKER_TIMEOUT_MS", 30000)
	if err != nil {
		return applicationConfig{}, err
	}
	maxInFlightTasks, err := positiveInt32(getenv, "SCHEDULER_MAX_IN_FLIGHT_TASKS", 1)
	if err != nil {
		return applicationConfig{}, err
	}
	maxInFlightBatches, err := positiveInt32(getenv, "SCHEDULER_MAX_IN_FLIGHT_BATCHES", 1)
	if err != nil {
		return applicationConfig{}, err
	}
	maxConcurrentTasks, err := positiveInt32(getenv, "SCHEDULER_WORKER_MAX_CONCURRENT_TASKS", 1)
	if err != nil {
		return applicationConfig{}, err
	}
	queueCapacity, err := nonNegativeInt32(getenv, "SCHEDULER_WORKER_TASK_QUEUE_CAPACITY", 0)
	if err != nil {
		return applicationConfig{}, err
	}
	maxConnections, err := positiveInt32(getenv, "SCHEDULER_WORKER_MAX_CONNECTIONS", 16)
	if err != nil {
		return applicationConfig{}, err
	}
	backoffLimit, err := nonNegativeInt32(getenv, "SCHEDULER_COORDINATOR_BACKOFF_LIMIT", 3)
	if err != nil {
		return applicationConfig{}, err
	}
	ttl, err := nonNegativeInt32(getenv, "SCHEDULER_COORDINATOR_TTL_SECONDS", 86400)
	if err != nil {
		return applicationConfig{}, err
	}
	var ttlPointer *int32
	if ttl > 0 {
		ttlPointer = &ttl
	}
	coordinatorResources, err := containerResources(
		getenv,
		"SCHEDULER_COORDINATOR",
		"100m",
		"256Mi",
		"1",
		"1Gi",
	)
	if err != nil {
		return applicationConfig{}, err
	}
	workerResources, err := containerResources(
		getenv,
		"SCHEDULER_WORKER",
		"1",
		"4Gi",
		"4",
		"8Gi",
	)
	if err != nil {
		return applicationConfig{}, err
	}

	return applicationConfig{
		databaseURL:   databaseURL,
		kubeconfig:    getenv("KUBECONFIG"),
		healthAddress: valueOrDefault(getenv("HEALTH_LISTEN_ADDRESS"), ":8082"),
		scheduler: schedulerinternal.Config{
			OwnerID: ownerID, MaximumActive: maximumActive, LeaseDuration: leaseDuration,
			HeartbeatTimeout: heartbeatTimeout, ReconcileEvery: reconcileEvery, OperationTimeout: operationTimeout,
		},
		dispatcher: dispatchkube.Config{
			Namespace:                   namespace,
			ServiceAccount:              valueOrDefault(getenv("SCHEDULER_EXECUTION_SERVICE_ACCOUNT"), "default"),
			CoordinatorImage:            coordinatorImage,
			WorkerImage:                 workerImage,
			HeartbeatURL:                heartbeatURL,
			HeartbeatIntervalMillis:     heartbeatInterval,
			ImagePullPolicy:             pullPolicy,
			ProgressClaim:               progressClaim,
			WorkerReplicas:              workerReplicas,
			WorkerPort:                  workerPort,
			WorkerTimeoutMillis:         workerTimeout,
			CoordinatorMaxInFlightTasks: maxInFlightTasks,
			WorkerMaxInFlightBatches:    maxInFlightBatches,
			WorkerMaxConcurrentTasks:    maxConcurrentTasks,
			WorkerTaskQueueCapacity:     queueCapacity,
			WorkerMaxConnections:        maxConnections,
			CoordinatorBackoffLimit:     backoffLimit,
			TTLSecondsAfterFinished:     ttlPointer,
			CoordinatorResources:        coordinatorResources,
			WorkerResources:             workerResources,
			CoordinatorJavaToolOptions: valueOrDefault(
				getenv("SCHEDULER_COORDINATOR_JAVA_TOOL_OPTIONS"),
				"-Xmx768m -XX:+UseG1GC",
			),
			WorkerJavaToolOptions: valueOrDefault(
				getenv("SCHEDULER_WORKER_JAVA_TOOL_OPTIONS"),
				"-Xmx4g -XX:+UseG1GC -XX:MaxDirectMemorySize=2g",
			),
		},
	}, nil
}

func required(getenv func(string) string, name string) (string, error) {
	value := getenv(name)
	if value == "" {
		return "", fmt.Errorf("%s must be configured", name)
	}
	return value, nil
}

func positiveInt(getenv func(string) string, name string, defaultValue int) (int, error) {
	value := getenv(name)
	if value == "" {
		return defaultValue, nil
	}
	parsed, err := strconv.Atoi(value)
	if err != nil || parsed <= 0 {
		return 0, fmt.Errorf("%s must be a positive integer", name)
	}
	return parsed, nil
}

func positiveInt32(getenv func(string) string, name string, defaultValue int32) (int32, error) {
	parsed, err := integer64(getenv, name, int64(defaultValue))
	if err != nil || parsed <= 0 || parsed > math.MaxInt32 {
		return 0, fmt.Errorf("%s must be a positive 32-bit integer", name)
	}
	return int32(parsed), nil
}

func nonNegativeInt32(getenv func(string) string, name string, defaultValue int32) (int32, error) {
	parsed, err := integer64(getenv, name, int64(defaultValue))
	if err != nil || parsed < 0 || parsed > math.MaxInt32 {
		return 0, fmt.Errorf("%s must be a non-negative 32-bit integer", name)
	}
	return int32(parsed), nil
}

func integer64(getenv func(string) string, name string, defaultValue int64) (int64, error) {
	value := getenv(name)
	if value == "" {
		return defaultValue, nil
	}
	parsed, err := strconv.ParseInt(value, 10, 64)
	if err != nil {
		return 0, err
	}
	return parsed, nil
}

func duration(getenv func(string) string, name string, defaultValue time.Duration) (time.Duration, error) {
	value := getenv(name)
	if value == "" {
		return defaultValue, nil
	}
	parsed, err := time.ParseDuration(value)
	if err != nil || parsed <= 0 {
		return 0, fmt.Errorf("%s must be a positive duration", name)
	}
	return parsed, nil
}

func imagePullPolicy(value string) (corev1.PullPolicy, error) {
	switch corev1.PullPolicy(value) {
	case corev1.PullAlways, corev1.PullIfNotPresent, corev1.PullNever:
		return corev1.PullPolicy(value), nil
	default:
		return "", fmt.Errorf("SCHEDULER_IMAGE_PULL_POLICY is invalid")
	}
}

func containerResources(
	getenv func(string) string,
	prefix string,
	defaultCPURequest string,
	defaultMemoryRequest string,
	defaultCPULimit string,
	defaultMemoryLimit string,
) (corev1.ResourceRequirements, error) {
	values := map[string]string{
		"CPU_REQUEST":    defaultCPURequest,
		"MEMORY_REQUEST": defaultMemoryRequest,
		"CPU_LIMIT":      defaultCPULimit,
		"MEMORY_LIMIT":   defaultMemoryLimit,
	}
	parsed := make(map[string]resource.Quantity, len(values))
	for suffix, defaultValue := range values {
		name := prefix + "_" + suffix
		quantity, err := resource.ParseQuantity(valueOrDefault(getenv(name), defaultValue))
		if err != nil || quantity.Sign() <= 0 {
			return corev1.ResourceRequirements{}, fmt.Errorf("%s must be a positive Kubernetes quantity", name)
		}
		parsed[suffix] = quantity
	}
	return corev1.ResourceRequirements{
		Requests: corev1.ResourceList{
			corev1.ResourceCPU: parsed["CPU_REQUEST"], corev1.ResourceMemory: parsed["MEMORY_REQUEST"],
		},
		Limits: corev1.ResourceList{
			corev1.ResourceCPU: parsed["CPU_LIMIT"], corev1.ResourceMemory: parsed["MEMORY_LIMIT"],
		},
	}, nil
}

func valueOrDefault(value, defaultValue string) string {
	if value == "" {
		return defaultValue
	}
	return value
}
