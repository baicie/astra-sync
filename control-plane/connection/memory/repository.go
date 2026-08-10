package memory

import (
	"context"
	"crypto/sha256"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	"io.astrasync/control-plane/connection"
)

type idempotencyResult struct {
	digest string
	result connection.MutationResult
}

type AuditEvent struct {
	TenantID   string
	Action     string
	ActorID    string
	RequestID  string
	Outcome    connection.MutationOutcome
	Attributes map[string]any
}

type Repository struct {
	mu          sync.RWMutex
	connections map[string]connection.Connection
	uidIndex    map[string]string
	tests       map[string]connection.TestOperation
	generations map[string]map[int64]connection.Generation
	claims      map[string]testClaim
	idempotency map[string]idempotencyResult
	references  map[string]connection.ReferenceCounts
	audits      []AuditEvent
	revision    uint64
}

type testClaim struct {
	executorID     string
	attempt        int32
	leaseExpiresAt time.Time
}

func New() *Repository {
	return &Repository{
		connections: make(map[string]connection.Connection),
		uidIndex:    make(map[string]string),
		tests:       make(map[string]connection.TestOperation),
		generations: make(map[string]map[int64]connection.Generation),
		claims:      make(map[string]testClaim),
		idempotency: make(map[string]idempotencyResult),
		references:  make(map[string]connection.ReferenceCounts),
	}
}

func (r *Repository) Get(_ context.Context, tenantID, name string) (connection.Connection, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	stored, found := r.connections[resourceKey(tenantID, name)]
	if !found {
		return connection.Connection{}, connection.ErrNotFound
	}
	return stored.Clone(), nil
}

func (r *Repository) GetByUID(_ context.Context, tenantID, uid string) (connection.Connection, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	key, found := r.uidIndex[uid]
	stored, exists := r.connections[key]
	if !found || !exists || stored.TenantID != tenantID {
		return connection.Connection{}, connection.ErrNotFound
	}
	return stored.Clone(), nil
}

func (r *Repository) List(
	_ context.Context, tenantID string, filter connection.ListFilter,
) (connection.ListResult, error) {
	if filter.Limit <= 0 || filter.Limit > 101 {
		return connection.ListResult{}, fmt.Errorf("Connection list limit is invalid")
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	values := make([]connection.Connection, 0)
	for _, stored := range r.connections {
		if stored.TenantID != tenantID || filter.Connector != "" && stored.Connector != filter.Connector ||
			filter.State != "" && stored.State != filter.State {
			continue
		}
		if stored.Name < filter.AfterName || stored.Name == filter.AfterName && stored.UID <= filter.AfterUID {
			continue
		}
		values = append(values, stored.Clone())
	}
	sort.Slice(values, func(left, right int) bool {
		if values[left].Name == values[right].Name {
			return values[left].UID < values[right].UID
		}
		return values[left].Name < values[right].Name
	})
	hasMore := len(values) > filter.Limit
	if hasMore {
		values = values[:filter.Limit]
	}
	return connection.ListResult{Connections: values, Revision: revisionString(r.revision), HasMore: hasMore}, nil
}

func (r *Repository) ReferenceCounts(
	_ context.Context, uid string,
) (connection.ReferenceCounts, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	if _, found := r.uidIndex[uid]; !found {
		return connection.ReferenceCounts{}, connection.ErrNotFound
	}
	result := r.references[uid]
	for _, test := range r.tests {
		if test.ConnectionUID == uid && (test.State == connection.TestQueued || test.State == connection.TestRunning) {
			result.Tests++
		}
	}
	return result, nil
}

func (r *Repository) Apply(
	_ context.Context, mutation connection.Mutation,
) (connection.MutationResult, error) {
	if err := mutation.Identity.Validate(); err != nil {
		return connection.MutationResult{}, err
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	idempotencyKey := mutation.TenantID + "\x00" + mutation.Identity.ActorID + "\x00" +
		mutation.Identity.Method + "\x00" + mutation.Identity.KeyFingerprint
	if existing, found := r.idempotency[idempotencyKey]; found {
		if existing.digest != mutation.Identity.RequestDigest {
			return connection.MutationResult{}, connection.ErrIdempotencyReused
		}
		replayed := cloneResult(existing.result)
		replayed.Outcome = connection.OutcomeReplayed
		return replayed, nil
	}

	result, err := r.applyLocked(mutation)
	if err != nil {
		return connection.MutationResult{}, err
	}
	r.revision++
	r.audits = append(r.audits, AuditEvent{
		TenantID: mutation.TenantID, Action: string(mutation.Kind), ActorID: mutation.Identity.ActorID,
		RequestID: mutation.Identity.RequestID, Outcome: result.Outcome,
		Attributes: cloneAttributes(mutation.AuditAttributes),
	})
	r.idempotency[idempotencyKey] = idempotencyResult{
		digest: mutation.Identity.RequestDigest, result: cloneResult(result),
	}
	return cloneResult(result), nil
}

func (r *Repository) applyLocked(mutation connection.Mutation) (connection.MutationResult, error) {
	key := resourceKey(mutation.TenantID, mutation.Name)
	current, found := r.connections[key]
	switch mutation.Kind {
	case connection.MutationCreate:
		if found {
			return connection.MutationResult{}, connection.ErrAlreadyExists
		}
		if mutation.Candidate == nil || mutation.Candidate.TenantID != mutation.TenantID ||
			mutation.Candidate.Name != mutation.Name {
			return connection.MutationResult{}, fmt.Errorf("create candidate identity is invalid")
		}
		candidate := mutation.Candidate.Clone()
		if err := candidate.Validate(); err != nil {
			return connection.MutationResult{}, err
		}
		r.connections[key] = candidate
		r.uidIndex[candidate.UID] = key
		r.generations[candidate.UID] = map[int64]connection.Generation{
			candidate.Current.Number: candidate.Current.Clone(),
		}
		return connection.MutationResult{Connection: &candidate, Outcome: connection.OutcomeChanged}, nil
	case connection.MutationUpdate, connection.MutationRotate, connection.MutationEnable, connection.MutationDisable:
		if !found {
			return connection.MutationResult{}, connection.ErrNotFound
		}
		if mutation.ExpectedVersion <= 0 || current.Version != mutation.ExpectedVersion {
			return connection.MutationResult{}, connection.ErrConflict
		}
		if mutation.Candidate == nil {
			return connection.MutationResult{}, fmt.Errorf("mutation candidate is required")
		}
		candidate := mutation.Candidate.Clone()
		if candidate.UID != current.UID || candidate.TenantID != current.TenantID ||
			candidate.Name != current.Name || candidate.Connector != current.Connector ||
			(candidate.Version != current.Version && candidate.Version != current.Version+1) ||
			(candidate.Current.Number != current.Current.Number && candidate.Current.Number != current.Current.Number+1) {
			return connection.MutationResult{}, connection.ErrConflict
		}
		if err := candidate.Validate(); err != nil {
			return connection.MutationResult{}, err
		}
		outcome := connection.OutcomeChanged
		if candidate.Version == current.Version {
			outcome = connection.OutcomeNoChange
		}
		r.connections[key] = candidate
		if candidate.Current.Number != current.Current.Number {
			r.generations[current.UID][candidate.Current.Number] = candidate.Current.Clone()
		}
		return connection.MutationResult{Connection: &candidate, Outcome: outcome}, nil
	case connection.MutationDelete:
		if !found {
			return connection.MutationResult{}, connection.ErrNotFound
		}
		if mutation.ExpectedVersion <= 0 || current.Version != mutation.ExpectedVersion {
			return connection.MutationResult{}, connection.ErrConflict
		}
		if current.State != connection.StateDisabled || !zeroReferences(r.references[current.UID]) || r.hasActiveTest(current.UID) {
			return connection.MutationResult{}, connection.ErrInUse
		}
		delete(r.connections, key)
		delete(r.uidIndex, current.UID)
		delete(r.references, current.UID)
		delete(r.generations, current.UID)
		tombstone := connection.Tombstone{
			TenantID: current.TenantID, Name: current.Name, UID: current.UID,
			DeletedAt: mutation.Identity.OccurredAt.UTC(),
		}
		return connection.MutationResult{Tombstone: &tombstone, Outcome: connection.OutcomeChanged}, nil
	case connection.MutationTest:
		if !found {
			return connection.MutationResult{}, connection.ErrNotFound
		}
		if mutation.ExpectedVersion <= 0 || current.Version != mutation.ExpectedVersion {
			return connection.MutationResult{}, connection.ErrConflict
		}
		if mutation.Test == nil {
			return connection.MutationResult{}, fmt.Errorf("test operation is required")
		}
		test := cloneTest(*mutation.Test)
		if err := test.Validate(); err != nil || test.ConnectionUID != current.UID ||
			test.Generation != current.Current.Number || test.ActorID != mutation.Identity.ActorID {
			return connection.MutationResult{}, fmt.Errorf("test operation does not match current Connection generation")
		}
		if r.testLimitExceeded(test) {
			return connection.MutationResult{}, connection.ErrTestLimitExceeded
		}
		r.tests[test.OperationID] = test
		return connection.MutationResult{Test: &test, Outcome: connection.OutcomeChanged}, nil
	default:
		return connection.MutationResult{}, fmt.Errorf("unsupported Connection mutation %q", mutation.Kind)
	}
}

func (r *Repository) GetTest(
	_ context.Context, tenantID, operationID string,
) (connection.TestOperation, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	stored, found := r.tests[operationID]
	if !found || stored.TenantID != tenantID {
		return connection.TestOperation{}, connection.ErrTestNotFound
	}
	return cloneTest(stored), nil
}

func (r *Repository) LatestTest(_ context.Context, connectionUID string) (connection.TestOperation, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	var latest connection.TestOperation
	found := false
	for _, stored := range r.tests {
		if stored.ConnectionUID == connectionUID && (!found || stored.CreatedAt.After(latest.CreatedAt)) {
			latest = stored
			found = true
		}
	}
	if !found {
		return connection.TestOperation{}, connection.ErrTestNotFound
	}
	return cloneTest(latest), nil
}

func (r *Repository) ClaimTests(
	_ context.Context, executorID string, limit int, leaseDuration time.Duration, now time.Time,
) ([]connection.TestWork, error) {
	if strings.TrimSpace(executorID) == "" || len(executorID) > connection.MaximumTestExecutorID ||
		limit <= 0 || limit > connection.MaximumTestClaimBatch || leaseDuration <= 0 ||
		leaseDuration > connection.MaximumTestLease {
		return nil, fmt.Errorf("Connection test claim is invalid")
	}
	now = now.UTC()
	r.mu.Lock()
	defer r.mu.Unlock()
	r.expireLocked(now)
	operationIDs := make([]string, 0, len(r.tests))
	for operationID, test := range r.tests {
		claim := r.claims[operationID]
		if test.State == connection.TestQueued ||
			test.State == connection.TestRunning && !claim.leaseExpiresAt.After(now) {
			operationIDs = append(operationIDs, operationID)
		}
	}
	sort.Slice(operationIDs, func(left, right int) bool {
		leftTest, rightTest := r.tests[operationIDs[left]], r.tests[operationIDs[right]]
		if leftTest.CreatedAt.Equal(rightTest.CreatedAt) {
			return operationIDs[left] < operationIDs[right]
		}
		return leftTest.CreatedAt.Before(rightTest.CreatedAt)
	})
	if len(operationIDs) > limit {
		operationIDs = operationIDs[:limit]
	}
	result := make([]connection.TestWork, 0, len(operationIDs))
	for _, operationID := range operationIDs {
		test := r.tests[operationID]
		generation, found := r.generations[test.ConnectionUID][test.Generation]
		key, connectionFound := r.uidIndex[test.ConnectionUID]
		stored, exists := r.connections[key]
		if !found || !connectionFound || !exists {
			return nil, fmt.Errorf("captured Connection test generation is unavailable")
		}
		startedAt := now
		if test.StartedAt != nil {
			startedAt = *test.StartedAt
		}
		test.State = connection.TestRunning
		test.Phase = connection.TestPhasePolicy
		test.ResultCode = ""
		test.Success = false
		test.RemediationKey = ""
		test.StartedAt = &startedAt
		test.CompletedAt = nil
		claim := r.claims[operationID]
		claim.executorID = executorID
		claim.attempt++
		claim.leaseExpiresAt = now.Add(leaseDuration)
		r.claims[operationID] = claim
		r.tests[operationID] = test
		work := connection.TestWork{
			Operation: test,
			Generation: connection.GenerationSnapshot{
				TenantID: test.TenantID, TenantNamespace: "tenant-" + test.TenantID[:8],
				ConnectionUID: test.ConnectionUID, Connector: stored.Connector, Generation: generation.Clone(),
			},
			ExecutorID: executorID, Attempt: claim.attempt, LeaseExpiresAt: claim.leaseExpiresAt,
		}
		if err := work.Validate(); err != nil {
			return nil, err
		}
		result = append(result, work)
	}
	if len(result) > 0 {
		r.revision++
	}
	return result, nil
}

func (r *Repository) CompleteTest(
	_ context.Context,
	operationID, executorID string,
	completion connection.TestCompletion,
	now time.Time,
) (connection.TestOperation, error) {
	if err := completion.Validate(); err != nil || strings.TrimSpace(executorID) == "" ||
		len(executorID) > connection.MaximumTestExecutorID {
		return connection.TestOperation{}, fmt.Errorf("Connection test completion is invalid")
	}
	now = now.UTC()
	r.mu.Lock()
	defer r.mu.Unlock()
	test, found := r.tests[operationID]
	claim := r.claims[operationID]
	if !found || test.State != connection.TestRunning || claim.executorID != executorID ||
		!claim.leaseExpiresAt.After(now) || !test.DeadlineAt.After(now) {
		return connection.TestOperation{}, connection.ErrTestLeaseLost
	}
	test.State = completion.State
	test.Phase = completion.Phase
	test.ResultCode = completion.ResultCode
	test.Success = completion.Success
	test.RemediationKey = completion.RemediationKey
	test.CompletedAt = &now
	if err := test.Validate(); err != nil {
		return connection.TestOperation{}, err
	}
	r.tests[operationID] = test
	delete(r.claims, operationID)
	r.revision++
	r.audits = append(r.audits, AuditEvent{
		TenantID: test.TenantID, Action: "TEST_COMPLETE", ActorID: "service:" + executorID,
		RequestID: operationID, Outcome: connection.OutcomeChanged,
		Attributes: map[string]any{
			"operationId": operationID, "connectionUid": test.ConnectionUID,
			"generation": test.Generation, "resultCode": string(test.ResultCode),
		},
	})
	return cloneTest(test), nil
}

func (r *Repository) ExpireTests(_ context.Context, now time.Time) (int64, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	count := r.expireLocked(now.UTC())
	if count > 0 {
		r.revision++
	}
	return count, nil
}

func (r *Repository) expireLocked(now time.Time) int64 {
	var count int64
	for operationID, test := range r.tests {
		if test.State == connection.TestExpired {
			continue
		}
		startedAt := test.CreatedAt
		if test.StartedAt != nil {
			startedAt = *test.StartedAt
		}
		action := ""
		if !test.ExpiresAt.After(now) {
			test.State = connection.TestExpired
			test.Phase = ""
			test.ResultCode = ""
			test.Success = false
			test.RemediationKey = ""
			test.StartedAt = &startedAt
			if test.CompletedAt == nil {
				test.CompletedAt = &now
			}
			action = "TEST_EXPIRE"
		} else if (test.State == connection.TestQueued || test.State == connection.TestRunning) &&
			!test.DeadlineAt.After(now) {
			test.State = connection.TestTimedOut
			if test.Phase == "" {
				test.Phase = connection.TestPhasePolicy
			}
			test.ResultCode = connection.TestResultDeadlineExceeded
			test.Success = false
			test.RemediationKey = "connection.test.deadline"
			test.StartedAt = &startedAt
			test.CompletedAt = &now
			action = "TEST_TIMEOUT"
		} else {
			continue
		}
		r.tests[operationID] = test
		delete(r.claims, operationID)
		r.audits = append(r.audits, AuditEvent{
			TenantID: test.TenantID, Action: action, ActorID: "service:connection-test-reconciler",
			RequestID: operationID, Outcome: connection.OutcomeChanged,
			Attributes: map[string]any{
				"operationId": operationID, "connectionUid": test.ConnectionUID,
				"generation": test.Generation,
			},
		})
		count++
	}
	return count
}

func (r *Repository) SetReferenceCounts(uid string, counts connection.ReferenceCounts) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.references[uid] = counts
}

func (r *Repository) AuditEvents() []AuditEvent {
	r.mu.RLock()
	defer r.mu.RUnlock()
	result := make([]AuditEvent, len(r.audits))
	copy(result, r.audits)
	return result
}

func (r *Repository) hasActiveTest(uid string) bool {
	for _, test := range r.tests {
		if test.ConnectionUID == uid && (test.State == connection.TestQueued || test.State == connection.TestRunning) {
			return true
		}
	}
	return false
}

func (r *Repository) testLimitExceeded(candidate connection.TestOperation) bool {
	var tenantActive, actorActive, connectionActive, tenantDaily int
	windowStart := candidate.CreatedAt.Add(-24 * time.Hour)
	for _, test := range r.tests {
		if test.TenantID != candidate.TenantID {
			continue
		}
		if !test.CreatedAt.Before(windowStart) {
			tenantDaily++
		}
		if test.State != connection.TestQueued && test.State != connection.TestRunning {
			continue
		}
		tenantActive++
		if test.ActorID == candidate.ActorID {
			actorActive++
		}
		if test.ConnectionUID == candidate.ConnectionUID {
			connectionActive++
		}
	}
	return tenantActive >= connection.MaximumTenantActiveTests ||
		actorActive >= connection.MaximumActorActiveTests ||
		connectionActive >= connection.MaximumConnectionActiveTest ||
		tenantDaily >= connection.MaximumTenantDailyTests
}

func resourceKey(tenantID, name string) string { return tenantID + "\x00" + name }

func revisionString(value uint64) string {
	sum := sha256.Sum256([]byte(fmt.Sprintf("connection-list:%d", value)))
	return fmt.Sprintf("sha256:%x", sum)
}

func zeroReferences(counts connection.ReferenceCounts) bool {
	return counts.Jobs == 0 && counts.Executions == 0 && counts.Tests == 0 && counts.CleanupObligations == 0
}

func cloneResult(source connection.MutationResult) connection.MutationResult {
	result := source
	if source.Connection != nil {
		value := source.Connection.Clone()
		result.Connection = &value
	}
	if source.Test != nil {
		value := cloneTest(*source.Test)
		result.Test = &value
	}
	if source.Tombstone != nil {
		value := *source.Tombstone
		result.Tombstone = &value
	}
	return result
}

func cloneTest(source connection.TestOperation) connection.TestOperation {
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

func cloneAttributes(source map[string]any) map[string]any {
	result := make(map[string]any, len(source))
	for key, value := range source {
		result[key] = value
	}
	return result
}

var _ connection.Repository = (*Repository)(nil)
