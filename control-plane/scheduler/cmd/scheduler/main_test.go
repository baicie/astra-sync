package main

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/google/uuid"
	"k8s.io/client-go/kubernetes/fake"

	"io.astrasync/control-plane/scheduler/internal/dispatch"
)

func TestHeartbeatEndpointAuthenticatesAndRecordsExecutionIdentity(t *testing.T) {
	clock := time.Date(2026, 8, 6, 1, 0, 0, 0, time.UTC)
	recorder := &fakeHeartbeatRecorder{}
	handler := applicationHandler(alwaysReadyDatabase{}, fake.NewSimpleClientset(), recorder, func() time.Time {
		return clock
	})
	identity := dispatch.Identity{JobUID: uuid.NewString(), Epoch: 7}
	token := uuid.NewString()
	request := httptest.NewRequest(
		http.MethodPost,
		"/v1/executions/"+identity.JobUID+"/7/heartbeat",
		nil,
	)
	request.Header.Set("Authorization", "Bearer "+token)
	response := httptest.NewRecorder()

	handler.ServeHTTP(response, request)

	if response.Code != http.StatusNoContent {
		t.Fatalf("heartbeat response: code=%d body=%s", response.Code, response.Body.String())
	}
	if recorder.identity != identity || recorder.token != token || !recorder.timestamp.Equal(clock) {
		t.Fatalf("heartbeat was not recorded exactly: %+v", recorder)
	}
}

func TestHeartbeatEndpointRejectsMissingTokenAndStaleExecution(t *testing.T) {
	recorder := &fakeHeartbeatRecorder{err: dispatch.ErrLeaseLost}
	handler := applicationHandler(alwaysReadyDatabase{}, fake.NewSimpleClientset(), recorder, time.Now)
	identity := dispatch.Identity{JobUID: uuid.NewString(), Epoch: 1}
	endpoint := "/v1/executions/" + identity.JobUID + "/1/heartbeat"

	missingToken := httptest.NewRecorder()
	handler.ServeHTTP(missingToken, httptest.NewRequest(http.MethodPost, endpoint, nil))
	if missingToken.Code != http.StatusUnauthorized || recorder.calls != 0 {
		t.Fatalf("missing token reached persistence: code=%d calls=%d", missingToken.Code, recorder.calls)
	}

	staleRequest := httptest.NewRequest(http.MethodPost, endpoint, nil)
	staleRequest.Header.Set("Authorization", "Bearer "+uuid.NewString())
	staleResponse := httptest.NewRecorder()
	handler.ServeHTTP(staleResponse, staleRequest)
	if staleResponse.Code != http.StatusNotFound || recorder.calls != 1 {
		t.Fatalf("stale heartbeat response: code=%d calls=%d", staleResponse.Code, recorder.calls)
	}
}

type alwaysReadyDatabase struct{}

func (alwaysReadyDatabase) PingContext(context.Context) error { return nil }

type fakeHeartbeatRecorder struct {
	identity  dispatch.Identity
	token     string
	timestamp time.Time
	err       error
	calls     int
}

func (r *fakeHeartbeatRecorder) RecordHeartbeat(
	_ context.Context, identity dispatch.Identity, token string, timestamp time.Time,
) error {
	r.calls++
	r.identity = identity
	r.token = token
	r.timestamp = timestamp
	return r.err
}

var _ heartbeatRecorder = (*fakeHeartbeatRecorder)(nil)
