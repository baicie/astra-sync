package server

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/encoding/protojson"

	jobv1 "io.astrasync/control-plane/api-server/gen/go/v1"
)

type jobBody struct {
	Name            string          `json:"name,omitempty"`
	ExpectedVersion int64           `json:"expectedVersion,omitempty"`
	Spec            json.RawMessage `json:"spec"`
	Purpose         string          `json:"purpose,omitempty"`
}

func (s *Server) createJob(response http.ResponseWriter, request *http.Request) {
	scope, session, err := s.scope(request)
	if err != nil {
		writeError(response, err)
		return
	}
	if err := s.requireMutation(request, session); err != nil {
		writeError(response, err)
		return
	}
	var body jobBody
	if err := parseJSONBody(request, &body, s.maximumBody); err != nil || strings.TrimSpace(body.Name) == "" {
		writeError(response, status.Error(codes.InvalidArgument, "job request is invalid"))
		return
	}
	spec, err := decodeJobSpec(body.Spec)
	if err != nil {
		writeError(response, status.Error(codes.InvalidArgument, "job specification is invalid"))
		return
	}
	ctx, cancel := s.backendContext(request, session, writeTimeout)
	defer cancel()
	result, err := s.mutations.CreateJob(ctx, &jobv1.CreateJobRequest{
		Name: body.Name, Namespace: scope.namespace, Spec: spec, IdempotencyKey: idempotencyKey(request),
	})
	if err != nil {
		writeError(response, err)
		return
	}
	s.setScope(response, scope)
	writeProtoJSON(response, result)
}

func (s *Server) updateJob(response http.ResponseWriter, request *http.Request) {
	scope, session, err := s.scope(request)
	if err != nil {
		writeError(response, err)
		return
	}
	if err := s.requireMutation(request, session); err != nil {
		writeError(response, err)
		return
	}
	var body jobBody
	if err := parseJSONBody(request, &body, s.maximumBody); err != nil || body.ExpectedVersion <= 0 {
		writeError(response, status.Error(codes.InvalidArgument, "job request is invalid"))
		return
	}
	spec, err := decodeJobSpec(body.Spec)
	if err != nil {
		writeError(response, status.Error(codes.InvalidArgument, "job specification is invalid"))
		return
	}
	ctx, cancel := s.backendContext(request, session, writeTimeout)
	defer cancel()
	result, err := s.mutations.UpdateJob(ctx, &jobv1.UpdateJobRequest{
		Name: resourceName(request, "name"), Namespace: scope.namespace, ExpectedVersion: body.ExpectedVersion,
		Spec: spec, IdempotencyKey: idempotencyKey(request),
	})
	if err != nil {
		writeError(response, err)
		return
	}
	s.setScope(response, scope)
	writeProtoJSON(response, result)
}

func (s *Server) deleteJob(response http.ResponseWriter, request *http.Request) {
	scope, session, err := s.scope(request)
	if err != nil {
		writeError(response, err)
		return
	}
	if err := s.requireMutation(request, session); err != nil {
		writeError(response, err)
		return
	}
	var body jobBody
	if err := parseJSONBody(request, &body, s.maximumBody); err != nil || body.ExpectedVersion <= 0 {
		writeError(response, status.Error(codes.InvalidArgument, "job request is invalid"))
		return
	}
	ctx, cancel := s.backendContext(request, session, writeTimeout)
	defer cancel()
	if _, err := s.mutations.DeleteJob(ctx, &jobv1.DeleteJobRequest{
		Name: resourceName(request, "name"), Namespace: scope.namespace, ExpectedVersion: body.ExpectedVersion,
		IdempotencyKey: idempotencyKey(request),
	}); err != nil {
		writeError(response, err)
		return
	}
	s.setScope(response, scope)
	response.WriteHeader(http.StatusNoContent)
}

func (s *Server) startJob(response http.ResponseWriter, request *http.Request) {
	s.mutateJobDesiredState(response, request, true)
}

func (s *Server) stopJob(response http.ResponseWriter, request *http.Request) {
	s.mutateJobDesiredState(response, request, false)
}

func (s *Server) mutateJobDesiredState(response http.ResponseWriter, request *http.Request, start bool) {
	scope, session, err := s.scope(request)
	if err != nil {
		writeError(response, err)
		return
	}
	if err := s.requireMutation(request, session); err != nil {
		writeError(response, err)
		return
	}
	var body jobBody
	if err := parseJSONBody(request, &body, s.maximumBody); err != nil || body.ExpectedVersion <= 0 {
		writeError(response, status.Error(codes.InvalidArgument, "job request is invalid"))
		return
	}
	ctx, cancel := s.backendContext(request, session, writeTimeout)
	defer cancel()
	var result any
	if start {
		result, err = s.mutations.StartJob(ctx, &jobv1.StartJobRequest{Name: resourceName(request, "name"), Namespace: scope.namespace,
			ExpectedVersion: body.ExpectedVersion, IdempotencyKey: idempotencyKey(request)})
	} else {
		result, err = s.mutations.StopJob(ctx, &jobv1.StopJobRequest{Name: resourceName(request, "name"), Namespace: scope.namespace,
			ExpectedVersion: body.ExpectedVersion, IdempotencyKey: idempotencyKey(request)})
	}
	if err != nil {
		writeError(response, err)
		return
	}
	s.setScope(response, scope)
	if message, ok := result.(*jobv1.Job); ok {
		writeProtoJSON(response, message)
		return
	}
	writeError(response, status.Error(codes.Internal, "job response is invalid"))
}

func (s *Server) validateJob(response http.ResponseWriter, request *http.Request) {
	scope, session, err := s.scope(request)
	if err != nil {
		writeError(response, err)
		return
	}
	if err := s.requireMutation(request, session); err != nil {
		writeError(response, err)
		return
	}
	var body jobBody
	if err := parseJSONBody(request, &body, s.maximumBody); err != nil {
		writeError(response, status.Error(codes.InvalidArgument, "validation request is invalid"))
		return
	}
	spec, err := decodeJobSpec(body.Spec)
	if err != nil {
		writeError(response, status.Error(codes.InvalidArgument, "job specification is invalid"))
		return
	}
	purpose := jobv1.JobValidationPurpose_JOB_VALIDATION_PURPOSE_UPDATE
	switch strings.ToUpper(strings.TrimSpace(body.Purpose)) {
	case "CREATE", "JOB_VALIDATION_PURPOSE_CREATE":
		purpose = jobv1.JobValidationPurpose_JOB_VALIDATION_PURPOSE_CREATE
	case "START", "JOB_VALIDATION_PURPOSE_START":
		purpose = jobv1.JobValidationPurpose_JOB_VALIDATION_PURPOSE_START
	case "", "UPDATE", "JOB_VALIDATION_PURPOSE_UPDATE":
	default:
		writeError(response, status.Error(codes.InvalidArgument, "validation purpose is invalid"))
		return
	}
	ctx, cancel := s.backendContext(request, session, writeTimeout)
	defer cancel()
	result, err := s.validator.ValidateJobSpec(ctx, &jobv1.ValidateJobSpecRequest{
		Namespace: scope.namespace, Name: resourceName(request, "name"), Purpose: purpose,
		ExpectedVersion: body.ExpectedVersion, Spec: spec,
	})
	if err != nil {
		writeError(response, err)
		return
	}
	s.setScope(response, scope)
	writeProtoJSON(response, result)
}

func decodeJobSpec(raw json.RawMessage) (*jobv1.JobSpec, error) {
	if len(raw) == 0 {
		return nil, fmt.Errorf("spec is required")
	}
	spec := &jobv1.JobSpec{}
	if err := (protojson.UnmarshalOptions{DiscardUnknown: false}).Unmarshal(raw, spec); err != nil {
		return nil, err
	}
	return spec, nil
}

func idempotencyKey(request *http.Request) string {
	value := strings.TrimSpace(request.Header.Get("Idempotency-Key"))
	if len(value) >= 16 && len(value) <= 128 && !strings.ContainsAny(value, "\r\n\x00") {
		return value
	}
	return requestIDToken()
}
