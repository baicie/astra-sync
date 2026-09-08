package server

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"

	"io.astrasync/console/internal/syncjobcr"
	jobv1 "io.astrasync/control-plane/api-server/gen/go/v1"
)

// jobSpecToCR converts an api-server JobSpec into the syncjobcr.SyncJobSpec the
// Console forwards to the Kubernetes API server. The conversion is intentionally
// lossy: source/sink/transforms/delivery/runtime fields carry the same protojson
// bytes the api-server validated; the Console does not re-encode them. The
// State field is intentionally left empty so the controller reconciles desired
// state from the durable job.Job row, not from CR.spec.state. ADR-073 §4.
func jobSpecToCR(spec *jobv1.JobSpec) *syncjobcr.SyncJobSpec {
	if spec == nil {
		return &syncjobcr.SyncJobSpec{}
	}
	return &syncjobcr.SyncJobSpec{
		Source:     marshalMessage(spec.Source),
		Sink:       marshalMessage(spec.Sink),
		Transforms: marshalTransformSlice(spec.Transforms),
		Delivery:   marshalMessage(spec.Delivery),
		Runtime:    marshalMessage(spec.Runtime),
	}
}

func marshalMessage(message proto.Message) json.RawMessage {
	if message == nil {
		return nil
	}
	raw, err := protojson.Marshal(message)
	if err != nil {
		return nil
	}
	return raw
}

func marshalTransformSlice(values []*jobv1.TransformConfig) []json.RawMessage {
	if len(values) == 0 {
		return nil
	}
	out := make([]json.RawMessage, 0, len(values))
	for _, value := range values {
		if value == nil {
			continue
		}
		raw, err := protojson.Marshal(value)
		if err != nil {
			continue
		}
		out = append(out, raw)
	}
	return out
}

type jobBody struct {
	Name            string          `json:"name,omitempty"`
	ExpectedVersion int64           `json:"expectedVersion,omitempty"`
	Spec            json.RawMessage `json:"spec"`
	Purpose         string          `json:"purpose,omitempty"`
}

// tenantScopeCheck is the ADR-072 defense-in-depth guard: after scope resolves,
// mutation handlers MUST reject before touching the backend when tenantID is
// empty. The scope() function also guards this case, but an explicit check
// here makes the invariant visible and self-documenting.
func tenantScopeCheck(scope scope) error {
	if scope.tenantID == "" {
		return status.Error(codes.PermissionDenied, "tenant scope denied")
	}
	return nil
}

func (s *Server) createJob(response http.ResponseWriter, request *http.Request) {
	scope, session, err := s.scope(request)
	if err != nil {
		writeError(response, err)
		return
	}
	if err := tenantScopeCheck(scope); err != nil {
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
	ctx, cancel := s.backendContextWithTenant(request, session, writeTimeout, scope.tenantID)
	defer cancel()
	result, err := s.mutations.CreateJob(ctx, &jobv1.CreateJobRequest{
		Name: body.Name, Namespace: scope.namespace, Spec: spec, IdempotencyKey: idempotencyKey(request),
	})
	if err != nil {
		writeError(response, err)
		return
	}
	s.crWriter.WriteCR(ctx, syncjobcr.Scope{TenantID: scope.tenantID, Namespace: scope.namespace},
		body.Name, syncjobcr.MutationCreate, jobSpecToCR(spec))
	s.setScope(response, scope)
	writeProtoJSON(response, result)
}

func (s *Server) updateJob(response http.ResponseWriter, request *http.Request) {
	scope, session, err := s.scope(request)
	if err != nil {
		writeError(response, err)
		return
	}
	if err := tenantScopeCheck(scope); err != nil {
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
	ctx, cancel := s.backendContextWithTenant(request, session, writeTimeout, scope.tenantID)
	defer cancel()
	result, err := s.mutations.UpdateJob(ctx, &jobv1.UpdateJobRequest{
		Name: resourceName(request, "name"), Namespace: scope.namespace, ExpectedVersion: body.ExpectedVersion,
		Spec: spec, IdempotencyKey: idempotencyKey(request),
	})
	if err != nil {
		writeError(response, err)
		return
	}
	s.crWriter.WriteCR(ctx, syncjobcr.Scope{TenantID: scope.tenantID, Namespace: scope.namespace},
		resourceName(request, "name"), syncjobcr.MutationUpdate, jobSpecToCR(spec))
	s.setScope(response, scope)
	writeProtoJSON(response, result)
}

func (s *Server) deleteJob(response http.ResponseWriter, request *http.Request) {
	scope, session, err := s.scope(request)
	if err != nil {
		writeError(response, err)
		return
	}
	if err := tenantScopeCheck(scope); err != nil {
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
	ctx, cancel := s.backendContextWithTenant(request, session, writeTimeout, scope.tenantID)
	defer cancel()
	if _, err := s.mutations.DeleteJob(ctx, &jobv1.DeleteJobRequest{
		Name: resourceName(request, "name"), Namespace: scope.namespace, ExpectedVersion: body.ExpectedVersion,
		IdempotencyKey: idempotencyKey(request),
	}); err != nil {
		writeError(response, err)
		return
	}
	s.crWriter.WriteCR(ctx, syncjobcr.Scope{TenantID: scope.tenantID, Namespace: scope.namespace},
		resourceName(request, "name"), syncjobcr.MutationDelete, nil)
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
	if err := tenantScopeCheck(scope); err != nil {
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
	ctx, cancel := s.backendContextWithTenant(request, session, writeTimeout, scope.tenantID)
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
	if err := tenantScopeCheck(scope); err != nil {
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
	ctx, cancel := s.backendContextWithTenant(request, session, writeTimeout, scope.tenantID)
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
