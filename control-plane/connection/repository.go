package connection

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
)

type MutationKind string

const (
	MutationCreate  MutationKind = "CREATE"
	MutationUpdate  MutationKind = "UPDATE"
	MutationRotate  MutationKind = "ROTATE"
	MutationEnable  MutationKind = "ENABLE"
	MutationDisable MutationKind = "DISABLE"
	MutationDelete  MutationKind = "DELETE"
	MutationTest    MutationKind = "TEST"
)

type MutationIdentity struct {
	ActorID        string
	Method         string
	KeyFingerprint string
	RequestDigest  string
	RequestID      string
	AuditEventID   string
	OccurredAt     time.Time
}

func (i MutationIdentity) Validate() error {
	if strings.TrimSpace(i.ActorID) == "" || len(i.ActorID) > 256 ||
		strings.TrimSpace(i.Method) == "" || len(i.Method) > 256 ||
		!revisionPattern.MatchString(i.KeyFingerprint) || !revisionPattern.MatchString(i.RequestDigest) ||
		strings.TrimSpace(i.RequestID) == "" || len(i.RequestID) > 128 ||
		strings.TrimSpace(i.AuditEventID) == "" || len(i.AuditEventID) > 128 || i.OccurredAt.IsZero() {
		return fmt.Errorf("Connection mutation identity is invalid")
	}
	return nil
}

type Mutation struct {
	Kind            MutationKind
	TenantID        string
	Name            string
	ExpectedVersion int64
	Candidate       *Connection
	Test            *TestOperation
	Identity        MutationIdentity
	AuditAttributes map[string]any
}

type MutationOutcome string

const (
	OutcomeChanged  MutationOutcome = "CHANGED"
	OutcomeNoChange MutationOutcome = "NO_CHANGE"
	OutcomeReplayed MutationOutcome = "REPLAYED"
)

type Tombstone struct {
	TenantID  string
	Name      string
	UID       string
	DeletedAt time.Time
}

type MutationResult struct {
	Connection *Connection
	Test       *TestOperation
	Tombstone  *Tombstone
	Outcome    MutationOutcome
}

type ListFilter struct {
	Connector string
	State     State
	AfterName string
	AfterUID  string
	Limit     int
}

type ListResult struct {
	Connections []Connection
	Revision    string
	HasMore     bool
}

type ReferenceCounts struct {
	Jobs               int32
	Executions         int32
	Tests              int32
	CleanupObligations int32
}

type Repository interface {
	Get(context.Context, string, string) (Connection, error)
	GetByUID(context.Context, string, string) (Connection, error)
	List(context.Context, string, ListFilter) (ListResult, error)
	ReferenceCounts(context.Context, string) (ReferenceCounts, error)
	Apply(context.Context, Mutation) (MutationResult, error)
	GetTest(context.Context, string, string) (TestOperation, error)
	LatestTest(context.Context, string) (TestOperation, error)
	ClaimTests(context.Context, string, int, time.Duration, time.Time) ([]TestWork, error)
	CompleteTest(context.Context, string, string, TestCompletion, time.Time) (TestOperation, error)
	ExpireTests(context.Context, time.Time) (int64, error)
}

const (
	MaximumTestExecutorID       = 128
	MaximumTestClaimBatch       = 64
	MaximumTestLease            = 5 * time.Minute
	MaximumTenantActiveTests    = 8
	MaximumActorActiveTests     = 2
	MaximumConnectionActiveTest = 1
	MaximumTenantDailyTests     = 100
)

type TestState string

const (
	TestQueued    TestState = "QUEUED"
	TestRunning   TestState = "RUNNING"
	TestSucceeded TestState = "SUCCEEDED"
	TestFailed    TestState = "FAILED"
	TestTimedOut  TestState = "TIMED_OUT"
	TestCanceled  TestState = "CANCELED"
	TestExpired   TestState = "EXPIRED"
)

type TestPhase string

const (
	TestPhasePolicy         TestPhase = "POLICY"
	TestPhaseDNS            TestPhase = "DNS"
	TestPhaseTransport      TestPhase = "TRANSPORT"
	TestPhaseTLS            TestPhase = "TLS"
	TestPhaseAuthentication TestPhase = "AUTHENTICATION"
	TestPhaseHandshake      TestPhase = "HANDSHAKE"
	TestPhaseComplete       TestPhase = "COMPLETE"
)

type TestResultCode string

const (
	TestResultOK                   TestResultCode = "OK"
	TestResultPolicyDenied         TestResultCode = "POLICY_DENIED"
	TestResultSecretUnavailable    TestResultCode = "SECRET_UNAVAILABLE"
	TestResultDNSFailed            TestResultCode = "DNS_FAILED"
	TestResultTransportFailed      TestResultCode = "TRANSPORT_FAILED"
	TestResultTLSFailed            TestResultCode = "TLS_FAILED"
	TestResultAuthenticationFailed TestResultCode = "AUTHENTICATION_FAILED"
	TestResultHandshakeFailed      TestResultCode = "HANDSHAKE_FAILED"
	TestResultDeadlineExceeded     TestResultCode = "DEADLINE_EXCEEDED"
	TestResultExecutorUnavailable  TestResultCode = "EXECUTOR_UNAVAILABLE"
)

type TestOperation struct {
	TenantID           string
	OperationID        string
	ConnectionUID      string
	Generation         int64
	DescriptorRevision string
	ActorID            string           `json:"-"`
	EgressPolicy       TestEgressPolicy `json:"-"`
	State              TestState
	Phase              TestPhase
	ResultCode         TestResultCode
	Success            bool
	RemediationKey     string
	CreatedAt          time.Time
	DeadlineAt         time.Time `json:"-"`
	StartedAt          *time.Time
	CompletedAt        *time.Time
	ExpiresAt          time.Time
}

type GenerationSnapshot struct {
	TenantID        string
	TenantNamespace string
	ConnectionUID   string
	Connector       string
	Generation      Generation
}

func (s GenerationSnapshot) Validate() error {
	if _, err := uuid.Parse(s.TenantID); err != nil {
		return fmt.Errorf("test generation tenant ID must be a UUID")
	}
	if strings.TrimSpace(s.TenantNamespace) == "" || len(s.TenantNamespace) > 63 {
		return fmt.Errorf("test generation tenant namespace is invalid")
	}
	if _, err := uuid.Parse(s.ConnectionUID); err != nil ||
		!connectorPattern.MatchString(s.Connector) || s.Generation.Number <= 0 {
		return fmt.Errorf("test generation Connection identity is invalid")
	}
	return s.Generation.Validate()
}

func (s GenerationSnapshot) Clone() GenerationSnapshot {
	result := s
	result.Generation = s.Generation.Clone()
	return result
}

type TestWork struct {
	Operation      TestOperation
	Generation     GenerationSnapshot
	ExecutorID     string
	Attempt        int32
	LeaseExpiresAt time.Time
}

func (w TestWork) Validate() error {
	if err := w.Operation.Validate(); err != nil || w.Operation.State != TestRunning {
		return fmt.Errorf("claimed Connection test operation is invalid")
	}
	if err := w.Generation.Validate(); err != nil ||
		w.Generation.TenantID != w.Operation.TenantID ||
		w.Generation.ConnectionUID != w.Operation.ConnectionUID ||
		w.Generation.Generation.Number != w.Operation.Generation ||
		w.Generation.Generation.DescriptorRevision != w.Operation.DescriptorRevision {
		return fmt.Errorf("claimed Connection test generation is invalid")
	}
	if strings.TrimSpace(w.ExecutorID) == "" || len(w.ExecutorID) > MaximumTestExecutorID ||
		w.Attempt <= 0 || w.LeaseExpiresAt.IsZero() {
		return fmt.Errorf("claimed Connection test lease is invalid")
	}
	return nil
}

type TestCompletion struct {
	State          TestState
	Phase          TestPhase
	ResultCode     TestResultCode
	Success        bool
	RemediationKey string
}

func (c TestCompletion) Validate() error {
	if c.State != TestSucceeded && c.State != TestFailed && c.State != TestTimedOut &&
		c.State != TestCanceled {
		return fmt.Errorf("Connection test completion state is invalid")
	}
	if !validTestPhase(c.Phase) || !validTestResultCode(c.ResultCode) ||
		len(c.RemediationKey) > 128 || strings.ContainsAny(c.RemediationKey, "\x00\r\n") {
		return fmt.Errorf("Connection test completion result is invalid")
	}
	if c.State == TestSucceeded {
		if !c.Success || c.Phase != TestPhaseComplete || c.ResultCode != TestResultOK || c.RemediationKey != "" {
			return fmt.Errorf("successful Connection test completion is inconsistent")
		}
	} else if c.Success || c.ResultCode == TestResultOK {
		return fmt.Errorf("failed Connection test completion is inconsistent")
	}
	if c.State == TestTimedOut && c.ResultCode != TestResultDeadlineExceeded {
		return fmt.Errorf("timed out Connection test completion is inconsistent")
	}
	return nil
}

func (t TestOperation) Validate() error {
	if _, err := uuid.Parse(t.TenantID); err != nil {
		return fmt.Errorf("test tenant ID must be a UUID")
	}
	if _, err := uuid.Parse(t.OperationID); err != nil {
		return fmt.Errorf("test operation ID must be a UUID")
	}
	if _, err := uuid.Parse(t.ConnectionUID); err != nil || t.Generation <= 0 ||
		!revisionPattern.MatchString(t.DescriptorRevision) || t.CreatedAt.IsZero() ||
		strings.TrimSpace(t.ActorID) == "" || len(t.ActorID) > 256 ||
		t.DeadlineAt.IsZero() || !t.DeadlineAt.After(t.CreatedAt) ||
		!t.ExpiresAt.After(t.DeadlineAt) {
		return fmt.Errorf("Connection test identity is invalid")
	}
	if err := t.EgressPolicy.Validate(); err != nil {
		return err
	}
	switch t.State {
	case TestQueued:
		if t.Phase != "" || t.ResultCode != "" || t.Success || t.RemediationKey != "" ||
			t.StartedAt != nil || t.CompletedAt != nil {
			return fmt.Errorf("queued Connection test state is inconsistent")
		}
	case TestRunning:
		if !validRunningTestPhase(t.Phase) || t.ResultCode != "" || t.Success ||
			t.RemediationKey != "" || t.StartedAt == nil || t.CompletedAt != nil {
			return fmt.Errorf("running Connection test state is inconsistent")
		}
	case TestSucceeded, TestFailed, TestTimedOut, TestCanceled:
		if t.StartedAt == nil || t.CompletedAt == nil || t.CompletedAt.Before(*t.StartedAt) {
			return fmt.Errorf("terminal Connection test timestamps are inconsistent")
		}
		if err := (TestCompletion{
			State: t.State, Phase: t.Phase, ResultCode: t.ResultCode,
			Success: t.Success, RemediationKey: t.RemediationKey,
		}).Validate(); err != nil {
			return err
		}
	case TestExpired:
		if t.Phase != "" || t.ResultCode != "" || t.Success || t.RemediationKey != "" ||
			t.StartedAt == nil || t.CompletedAt == nil || t.CompletedAt.Before(*t.StartedAt) {
			return fmt.Errorf("expired Connection test state is inconsistent")
		}
	default:
		return fmt.Errorf("Connection test state is invalid")
	}
	return nil
}

func validRunningTestPhase(value TestPhase) bool {
	return value == TestPhasePolicy || value == TestPhaseDNS || value == TestPhaseTransport ||
		value == TestPhaseTLS || value == TestPhaseAuthentication || value == TestPhaseHandshake
}

func validTestPhase(value TestPhase) bool {
	return validRunningTestPhase(value) || value == TestPhaseComplete
}

func validTestResultCode(value TestResultCode) bool {
	switch value {
	case TestResultOK, TestResultPolicyDenied, TestResultSecretUnavailable, TestResultDNSFailed,
		TestResultTransportFailed, TestResultTLSFailed, TestResultAuthenticationFailed,
		TestResultHandshakeFailed, TestResultDeadlineExceeded, TestResultExecutorUnavailable:
		return true
	default:
		return false
	}
}

func cloneTest(source TestOperation) TestOperation {
	result := source
	result.EgressPolicy = source.EgressPolicy.Clone()
	if source.StartedAt != nil {
		value := *source.StartedAt
		result.StartedAt = &value
	}
	if source.CompletedAt != nil {
		value := *source.CompletedAt
		result.CompletedAt = &value
	}
	return result
}
