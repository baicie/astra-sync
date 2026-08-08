package server_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/encoding/protojson"

	"io.astrasync/console/internal/server"
	jobv1 "io.astrasync/control-plane/api-server/gen/go/v1"
)

type fakeJobReader struct {
	list        *jobv1.ListJobsResponse
	job         *jobv1.Job
	jobStatus   *jobv1.JobStatus
	listError   error
	jobError    error
	statusError error
	lastList    *jobv1.ListJobsRequest
	lastGet     *jobv1.GetJobRequest
	lastStatus  *jobv1.GetJobStatusRequest
}

func (f *fakeJobReader) ListJobs(_ context.Context, request *jobv1.ListJobsRequest) (*jobv1.ListJobsResponse, error) {
	f.lastList = request
	return f.list, f.listError
}

func (f *fakeJobReader) GetJob(_ context.Context, request *jobv1.GetJobRequest) (*jobv1.Job, error) {
	f.lastGet = request
	return f.job, f.jobError
}

func (f *fakeJobReader) GetJobStatus(_ context.Context, request *jobv1.GetJobStatusRequest) (*jobv1.JobStatus, error) {
	f.lastStatus = request
	return f.jobStatus, f.statusError
}

func TestHandlerServesHealthAndEmbeddedConsole(t *testing.T) {
	reader := &fakeJobReader{}
	console, err := server.New(reader, "tenant-a")
	if err != nil {
		t.Fatalf("create server: %v", err)
	}
	handler := console.Handler()

	response := request(handler, http.MethodGet, "/health")
	if response.Code != http.StatusOK || response.Body.String() != "ok\n" {
		t.Fatalf("unexpected health response: %d %q", response.Code, response.Body.String())
	}

	response = request(handler, http.MethodGet, "/")
	if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), "AstraSync Console") {
		t.Fatalf("unexpected console page: %d %q", response.Code, response.Body.String())
	}
}

func TestReadEndpointsUseConfiguredNamespaceAndPagination(t *testing.T) {
	reader := &fakeJobReader{
		list:      &jobv1.ListJobsResponse{Total: 1, Items: []*jobv1.Job{{Name: "orders", Namespace: "tenant-a"}}},
		job:       &jobv1.Job{Name: "orders", Namespace: "tenant-a"},
		jobStatus: &jobv1.JobStatus{Epoch: 3},
	}
	console, err := server.New(reader, "tenant-a")
	if err != nil {
		t.Fatalf("create server: %v", err)
	}
	handler := console.Handler()

	response := request(handler, http.MethodGet, "/api/jobs?page=2&page_size=10")
	if response.Code != http.StatusOK || response.Header().Get("X-Astra-Namespace") != "tenant-a" {
		t.Fatalf("unexpected list response: %d %q", response.Code, response.Body.String())
	}
	var list jobv1.ListJobsResponse
	if err := protojson.Unmarshal(response.Body.Bytes(), &list); err != nil {
		t.Fatalf("decode list: %v", err)
	}
	if list.GetTotal() != 1 || reader.lastList.GetNamespace() != "tenant-a" || reader.lastList.GetPage() != 2 || reader.lastList.GetPageSize() != 10 {
		t.Fatalf("unexpected list request/response: request=%+v response=%+v", reader.lastList, &list)
	}

	response = request(handler, http.MethodGet, "/api/jobs/orders")
	if response.Code != http.StatusOK || reader.lastGet.GetNamespace() != "tenant-a" || reader.lastGet.GetName() != "orders" {
		t.Fatalf("unexpected detail response/request: %d %q %+v", response.Code, response.Body.String(), reader.lastGet)
	}

	response = request(handler, http.MethodGet, "/api/jobs/orders/status")
	if response.Code != http.StatusOK || reader.lastStatus.GetNamespace() != "tenant-a" || reader.lastStatus.GetName() != "orders" {
		t.Fatalf("unexpected status response/request: %d %q %+v", response.Code, response.Body.String(), reader.lastStatus)
	}
}

func TestNamespaceOverrideAndMutationRoutesAreRejected(t *testing.T) {
	reader := &fakeJobReader{}
	console, err := server.New(reader, "tenant-a")
	if err != nil {
		t.Fatalf("create server: %v", err)
	}
	handler := console.Handler()

	response := request(handler, http.MethodGet, "/api/jobs?namespace=tenant-b")
	if response.Code != http.StatusForbidden {
		t.Fatalf("expected namespace override to be forbidden, got %d", response.Code)
	}
	response = request(handler, http.MethodPost, "/api/jobs")
	if response.Code != http.StatusNotFound {
		t.Fatalf("expected mutation method to have no route, got %d", response.Code)
	}
}

func TestReadyReflectsControlPlaneAndMapsErrors(t *testing.T) {
	reader := &fakeJobReader{list: &jobv1.ListJobsResponse{}}
	console, err := server.New(reader, "tenant-a")
	if err != nil {
		t.Fatalf("create server: %v", err)
	}
	handler := console.Handler()
	response := request(handler, http.MethodGet, "/ready")
	if response.Code != http.StatusOK || reader.lastList.GetPageSize() != 1 {
		t.Fatalf("unexpected ready response: %d %q", response.Code, response.Body.String())
	}

	reader.listError = status.Error(codes.Unavailable, "backend is down")
	response = request(handler, http.MethodGet, "/api/jobs")
	if response.Code != http.StatusServiceUnavailable {
		t.Fatalf("expected unavailable mapping, got %d", response.Code)
	}
	reader.listError = status.Error(codes.NotFound, "missing")
	response = request(handler, http.MethodGet, "/api/jobs")
	if response.Code != http.StatusNotFound {
		t.Fatalf("expected not found mapping, got %d", response.Code)
	}
}

func TestInvalidPaginationAndConstructorValidation(t *testing.T) {
	if _, err := server.New(nil, "tenant-a"); err == nil {
		t.Fatal("expected nil reader error")
	}
	reader := &fakeJobReader{}
	if _, err := server.New(reader, " "); err == nil {
		t.Fatal("expected empty namespace error")
	}
	console, err := server.New(reader, "tenant-a")
	if err != nil {
		t.Fatalf("create server: %v", err)
	}
	response := request(console.Handler(), http.MethodGet, "/api/jobs?page_size=101")
	if response.Code != http.StatusBadRequest {
		t.Fatalf("expected invalid page size, got %d", response.Code)
	}
}

func request(handler http.Handler, method, path string) *httptest.ResponseRecorder {
	request := httptest.NewRequest(method, path, nil)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	return response
}
