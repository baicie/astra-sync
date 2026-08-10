package connectiontest

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"net/netip"
	"testing"
	"time"

	"github.com/google/uuid"

	"io.astrasync/control-plane/connection"
	connectionmemory "io.astrasync/control-plane/connection/memory"
	"io.astrasync/control-plane/scheduler/internal/materialization"
)

type fakeCredentialProvider struct {
	fields map[string][]byte
	err    error
}

func (p fakeCredentialProvider) Resolve(
	context.Context, materialization.CredentialRequest,
) (*materialization.Values, error) {
	if p.err != nil {
		return nil, p.err
	}
	return materialization.NewValues(p.fields, materialization.ProviderReceipt{
		Kind: connection.ProviderKubernetesSecretV1, ObjectUID: uuid.NewString(), VersionToken: "1",
	})
}

type validatingProbe struct {
	t *testing.T
}

func (p validatingProbe) Execute(
	_ context.Context,
	configuration *Configuration,
	_ *EgressGuard,
	_ connection.TestEgressPolicy,
) ProbeResult {
	p.t.Helper()
	if value, found := configuration.required("hostname"); !found || value != "db.example" {
		p.t.Fatalf("missing restricted setting")
	}
	if value, found := configuration.required("password"); !found || value != "credential-sentinel" {
		p.t.Fatalf("missing credential field")
	}
	return SuccessfulProbe()
}

func TestExecutorCompletesClaimedProbeWithCapturedCredentials(t *testing.T) {
	t.Parallel()
	repository, tenantID, operationID := seedExecutorTest(t)
	executor := newTestExecutor(t, repository, fakeCredentialProvider{
		fields: map[string][]byte{"password": []byte("credential-sentinel")},
	}, validatingProbe{t: t})
	claimed, err := executor.RunOnce(context.Background())
	if err != nil || claimed != 1 {
		t.Fatalf("run executor: claimed=%d err=%v", claimed, err)
	}
	result, err := repository.GetTest(context.Background(), tenantID, operationID)
	if err != nil || result.State != connection.TestSucceeded || !result.Success ||
		result.ResultCode != connection.TestResultOK {
		t.Fatalf("unexpected completed test: result=%+v err=%v", result, err)
	}
}

func TestExecutorSanitizesCredentialProviderFailure(t *testing.T) {
	t.Parallel()
	repository, tenantID, operationID := seedExecutorTest(t)
	sentinel := "secret-name-and-vendor-text-sentinel"
	executor := newTestExecutor(t, repository, fakeCredentialProvider{
		err: errors.New(sentinel),
	}, validatingProbe{t: t})
	claimed, err := executor.RunOnce(context.Background())
	if err != nil || claimed != 1 {
		t.Fatalf("run executor: claimed=%d err=%v", claimed, err)
	}
	result, err := repository.GetTest(context.Background(), tenantID, operationID)
	if err != nil || result.State != connection.TestFailed ||
		result.Phase != connection.TestPhaseAuthentication ||
		result.ResultCode != connection.TestResultSecretUnavailable ||
		result.RemediationKey != "connection.test.secret" {
		t.Fatalf("unexpected provider failure projection: result=%+v err=%v", result, err)
	}
}

func newTestExecutor(
	t *testing.T,
	repository *connectionmemory.Repository,
	provider materialization.CredentialProvider,
	probe Probe,
) *Executor {
	t.Helper()
	registry, err := NewRegistry(map[string]Probe{"fake": probe})
	if err != nil {
		t.Fatalf("construct registry: %v", err)
	}
	guard, err := NewEgressGuard(&staticResolver{
		addresses: []netip.Addr{netip.MustParseAddr("203.0.113.10")},
	}, nil, 8, time.Second)
	if err != nil {
		t.Fatalf("construct guard: %v", err)
	}
	executor, err := NewExecutor(
		repository, provider, registry, guard,
		ExecutorConfig{
			ExecutorID: "executor-test", Concurrency: 1, ClaimBatch: 1,
			ClaimInterval: time.Second, LeaseDuration: 4 * time.Second,
			ProbeTimeout: 2 * time.Second, CompletionTimeout: time.Second,
		}, time.Now,
	)
	if err != nil {
		t.Fatalf("construct executor: %v", err)
	}
	return executor
}

func seedExecutorTest(t *testing.T) (*connectionmemory.Repository, string, string) {
	t.Helper()
	now := time.Now().UTC()
	tenantID := uuid.NewString()
	repository := connectionmemory.New()
	stored, err := connection.New(
		tenantID, "executor-db", uuid.NewString(), "fake", "Executor", "test",
		connection.Generation{
			Number: 1, DescriptorRevision: executorDigest("descriptor"),
			ConnectionSchemaRevision: executorDigest("schema"),
			Settings: []connection.Setting{{
				Key: "hostname", Value: "db.example", Sensitivity: connection.SensitivityRestricted,
			}},
			SecretLocator: connection.SecretLocator{
				Provider:   connection.ProviderKubernetesSecretV1,
				SecretName: "executor-credentials", SecretUID: uuid.NewString(),
				Fields: []connection.SecretFieldMapping{{LogicalField: "password", SecretKey: "password"}},
			},
			CreatedAt: now,
		}, now,
	)
	if err != nil {
		t.Fatalf("construct Connection: %v", err)
	}
	applyExecutorMutation(t, repository, connection.Mutation{
		Kind: connection.MutationCreate, TenantID: tenantID, Name: stored.Name,
		Candidate: &stored, Identity: executorIdentity("create", now),
	})
	operationID := uuid.NewString()
	operation := connection.TestOperation{
		TenantID: tenantID, OperationID: operationID, ConnectionUID: stored.UID,
		Generation: 1, DescriptorRevision: stored.Current.DescriptorRevision,
		ActorID: "principal-test", EgressPolicy: connection.DefaultTestEgressPolicy(),
		State: connection.TestQueued, CreatedAt: now.Add(time.Millisecond),
		DeadlineAt: now.Add(30 * time.Second), ExpiresAt: now.Add(time.Hour),
	}
	applyExecutorMutation(t, repository, connection.Mutation{
		Kind: connection.MutationTest, TenantID: tenantID, Name: stored.Name,
		ExpectedVersion: 1, Test: &operation,
		Identity: executorIdentity("queue", now.Add(time.Millisecond)),
	})
	return repository, tenantID, operationID
}

func applyExecutorMutation(
	t *testing.T, repository *connectionmemory.Repository, mutation connection.Mutation,
) {
	t.Helper()
	if _, err := repository.Apply(context.Background(), mutation); err != nil {
		t.Fatalf("apply %s: %v", mutation.Kind, err)
	}
}

func executorIdentity(label string, now time.Time) connection.MutationIdentity {
	return connection.MutationIdentity{
		ActorID: "principal-test", Method: "/test/" + label,
		KeyFingerprint: executorDigest("key-" + label),
		RequestDigest:  executorDigest("request-" + label),
		RequestID:      "request-" + label, AuditEventID: uuid.NewString(), OccurredAt: now,
	}
}

func executorDigest(value string) string {
	sum := sha256.Sum256([]byte(value))
	return fmt.Sprintf("sha256:%x", sum)
}
