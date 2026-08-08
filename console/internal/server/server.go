package server

import (
	"context"
	"embed"
	"fmt"
	"io/fs"
	"net/http"
	"strconv"
	"strings"
	"time"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"

	jobv1 "io.astrasync/control-plane/api-server/gen/go/v1"
)

const (
	defaultPageSize = 50
	maximumPageSize = 100
	readTimeout     = 5 * time.Second
)

//go:embed web/*
var staticFiles embed.FS

type JobReader interface {
	ListJobs(context.Context, *jobv1.ListJobsRequest) (*jobv1.ListJobsResponse, error)
	GetJob(context.Context, *jobv1.GetJobRequest) (*jobv1.Job, error)
	GetJobStatus(context.Context, *jobv1.GetJobStatusRequest) (*jobv1.JobStatus, error)
}

type Server struct {
	reader    JobReader
	namespace string
}

func New(reader JobReader, namespace string) (*Server, error) {
	namespace = strings.TrimSpace(namespace)
	if reader == nil {
		return nil, fmt.Errorf("job reader must not be nil")
	}
	if namespace == "" {
		return nil, fmt.Errorf("console namespace must not be empty")
	}
	return &Server{reader: reader, namespace: namespace}, nil
}

func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /health", s.health)
	mux.HandleFunc("GET /ready", s.ready)
	mux.HandleFunc("GET /api/jobs", s.listJobs)
	mux.HandleFunc("GET /api/jobs/{name}/status", s.getJobStatus)
	mux.HandleFunc("GET /api/jobs/{name}", s.getJob)

	content, err := fs.Sub(staticFiles, "web")
	if err != nil {
		panic(fmt.Sprintf("embedded Console assets: %v", err))
	}
	mux.Handle("/", http.FileServer(http.FS(content)))
	return mux
}

func (s *Server) health(response http.ResponseWriter, _ *http.Request) {
	response.Header().Set("Content-Type", "text/plain; charset=utf-8")
	response.WriteHeader(http.StatusOK)
	_, _ = response.Write([]byte("ok\n"))
}

func (s *Server) ready(response http.ResponseWriter, request *http.Request) {
	s.setScope(response)
	ctx, cancel := context.WithTimeout(request.Context(), time.Second)
	defer cancel()
	_, err := s.reader.ListJobs(ctx, &jobv1.ListJobsRequest{
		Namespace: s.namespace,
		Page:      1,
		PageSize:  1,
	})
	if err != nil {
		http.Error(response, "control plane unavailable", http.StatusServiceUnavailable)
		return
	}
	response.Header().Set("Content-Type", "text/plain; charset=utf-8")
	response.WriteHeader(http.StatusOK)
	_, _ = response.Write([]byte("ready\n"))
}

func (s *Server) listJobs(response http.ResponseWriter, request *http.Request) {
	s.setScope(response)
	if err := s.checkNamespaceQuery(request); err != nil {
		writeHTTPError(response, err)
		return
	}
	page, pageSize, err := parsePagination(request)
	if err != nil {
		writeHTTPError(response, status.Error(codes.InvalidArgument, err.Error()))
		return
	}
	ctx, cancel := context.WithTimeout(request.Context(), readTimeout)
	defer cancel()
	result, err := s.reader.ListJobs(ctx, &jobv1.ListJobsRequest{
		Namespace: s.namespace,
		Page:      int32(page),
		PageSize:  int32(pageSize),
	})
	if err != nil {
		writeGRPCError(response, err)
		return
	}
	writeProtoJSON(response, result)
}

func (s *Server) getJob(response http.ResponseWriter, request *http.Request) {
	s.setScope(response)
	if err := s.checkNamespaceQuery(request); err != nil {
		writeHTTPError(response, err)
		return
	}
	name := strings.TrimSpace(request.PathValue("name"))
	if name == "" {
		writeHTTPError(response, status.Error(codes.InvalidArgument, "job name must not be empty"))
		return
	}
	ctx, cancel := context.WithTimeout(request.Context(), readTimeout)
	defer cancel()
	result, err := s.reader.GetJob(ctx, &jobv1.GetJobRequest{Namespace: s.namespace, Name: name})
	if err != nil {
		writeGRPCError(response, err)
		return
	}
	writeProtoJSON(response, result)
}

func (s *Server) getJobStatus(response http.ResponseWriter, request *http.Request) {
	s.setScope(response)
	if err := s.checkNamespaceQuery(request); err != nil {
		writeHTTPError(response, err)
		return
	}
	name := strings.TrimSpace(request.PathValue("name"))
	if name == "" {
		writeHTTPError(response, status.Error(codes.InvalidArgument, "job name must not be empty"))
		return
	}
	ctx, cancel := context.WithTimeout(request.Context(), readTimeout)
	defer cancel()
	result, err := s.reader.GetJobStatus(ctx, &jobv1.GetJobStatusRequest{Namespace: s.namespace, Name: name})
	if err != nil {
		writeGRPCError(response, err)
		return
	}
	writeProtoJSON(response, result)
}

func (s *Server) setScope(response http.ResponseWriter) {
	response.Header().Set("X-Astra-Namespace", s.namespace)
}

func (s *Server) checkNamespaceQuery(request *http.Request) error {
	requested := strings.TrimSpace(request.URL.Query().Get("namespace"))
	if requested != "" && requested != s.namespace {
		return status.Error(codes.PermissionDenied, "namespace is outside the Console scope")
	}
	return nil
}

func parsePagination(request *http.Request) (int, int, error) {
	page, err := queryInt(request, "page", 1)
	if err != nil {
		return 0, 0, err
	}
	pageSize, err := queryInt(request, "page_size", defaultPageSize)
	if err != nil {
		return 0, 0, err
	}
	if page < 1 {
		return 0, 0, fmt.Errorf("page must be positive")
	}
	if pageSize < 1 || pageSize > maximumPageSize {
		return 0, 0, fmt.Errorf("page_size must be between 1 and %d", maximumPageSize)
	}
	return page, pageSize, nil
}

func queryInt(request *http.Request, key string, defaultValue int) (int, error) {
	value := request.URL.Query().Get(key)
	if value == "" {
		return defaultValue, nil
	}
	parsed, err := strconv.Atoi(value)
	if err != nil {
		return 0, fmt.Errorf("%s must be an integer", key)
	}
	return parsed, nil
}

func writeProtoJSON(response http.ResponseWriter, message proto.Message) {
	payload, err := (protojson.MarshalOptions{EmitUnpopulated: true}).Marshal(message)
	if err != nil {
		writeHTTPError(response, status.Error(codes.Internal, "failed to encode response"))
		return
	}
	response.Header().Set("Content-Type", "application/json")
	response.WriteHeader(http.StatusOK)
	_, _ = response.Write(payload)
}

func writeHTTPError(response http.ResponseWriter, err error) {
	writeGRPCError(response, err)
}

func writeGRPCError(response http.ResponseWriter, err error) {
	code := status.Code(err)
	httpCode := http.StatusInternalServerError
	switch code {
	case codes.InvalidArgument:
		httpCode = http.StatusBadRequest
	case codes.Unauthenticated:
		httpCode = http.StatusUnauthorized
	case codes.PermissionDenied:
		httpCode = http.StatusForbidden
	case codes.NotFound:
		httpCode = http.StatusNotFound
	case codes.AlreadyExists, codes.Aborted:
		httpCode = http.StatusConflict
	case codes.FailedPrecondition:
		httpCode = http.StatusPreconditionFailed
	case codes.ResourceExhausted:
		httpCode = http.StatusTooManyRequests
	case codes.Canceled, codes.DeadlineExceeded, codes.Unavailable:
		httpCode = http.StatusServiceUnavailable
	}
	http.Error(response, status.Convert(err).Message(), httpCode)
}
