// Package bffbackend exposes the Console BFF's Backend interface to
// external consumers (cross-module test fixtures, e.g. ADR-076 / 080).
//
// The interface was previously typed as `any` in
// console/internal/server.Config.Backend and duck-typed by the test
// fakes under console/internal/server. ADR-080 promotes it to a
// real exported interface so the cross-module chain test fixture
// can implement it from a sibling module without violating Go's
// `internal/` package rule.
package bffbackend

import (
	"context"

	"google.golang.org/protobuf/types/known/emptypb"
	jobv1 "io.astrasync/control-plane/api-server/gen/go/v1"
)

// Backend is the gRPC client surface the Console BFF delegates to.
// It mirrors the JobService / ConnectorCatalogService /
// ConnectionService / AuditService generated clients.
type Backend interface {
	// Job read methods (read-only, BFF exposes a subset).
	ListJobs(context.Context, *jobv1.ListJobsRequest) (*jobv1.ListJobsResponse, error)
	GetJob(context.Context, *jobv1.GetJobRequest) (*jobv1.Job, error)
	GetJobStatus(context.Context, *jobv1.GetJobStatusRequest) (*jobv1.JobStatus, error)

	// Job mutation methods. The BFF attaches `x-astra-tenant-id` to
	// outgoing gRPC metadata on each of these calls (ADR-072).
	CreateJob(context.Context, *jobv1.CreateJobRequest) (*jobv1.Job, error)
	UpdateJob(context.Context, *jobv1.UpdateJobRequest) (*jobv1.Job, error)
	DeleteJob(context.Context, *jobv1.DeleteJobRequest) (*emptypb.Empty, error)
	StartJob(context.Context, *jobv1.StartJobRequest) (*jobv1.Job, error)
	StopJob(context.Context, *jobv1.StopJobRequest) (*jobv1.Job, error)
	ValidateJobSpec(context.Context, *jobv1.ValidateJobSpecRequest) (*jobv1.JobValidationResult, error)

	// Connector catalog.
	ListConnectorDescriptors(context.Context, *jobv1.ListConnectorDescriptorsRequest) (*jobv1.ListConnectorDescriptorsResponse, error)
	GetConnectorDescriptor(context.Context, *jobv1.GetConnectorDescriptorRequest) (*jobv1.GetConnectorDescriptorResponse, error)

	// Connection lifecycle.
	CreateConnection(context.Context, *jobv1.CreateConnectionRequest) (*jobv1.Connection, error)
	GetConnection(context.Context, *jobv1.GetConnectionRequest) (*jobv1.Connection, error)
	ListConnections(context.Context, *jobv1.ListConnectionsRequest) (*jobv1.ListConnectionsResponse, error)
	UpdateConnection(context.Context, *jobv1.UpdateConnectionRequest) (*jobv1.Connection, error)
	RotateConnection(context.Context, *jobv1.RotateConnectionRequest) (*jobv1.Connection, error)
	EnableConnection(context.Context, *jobv1.EnableConnectionRequest) (*jobv1.Connection, error)
	DisableConnection(context.Context, *jobv1.DisableConnectionRequest) (*jobv1.Connection, error)
	DeleteConnection(context.Context, *jobv1.DeleteConnectionRequest) (*jobv1.DeleteConnectionResponse, error)
	TestConnection(context.Context, *jobv1.TestConnectionRequest) (*jobv1.ConnectionTest, error)
	GetConnectionTest(context.Context, *jobv1.GetConnectionTestRequest) (*jobv1.ConnectionTest, error)

	// Audit.
	ListAuditEvents(context.Context, *jobv1.ListAuditEventsRequest) (*jobv1.ListAuditEventsResponse, error)
}
