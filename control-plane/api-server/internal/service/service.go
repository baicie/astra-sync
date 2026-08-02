package api

import (
	"context"

	"google.golang.org/protobuf/types/known/emptypb"
	"google.golang.org/protobuf/types/known/timestamppb"
)

type JobService interface {
	CreateJob(ctx context.Context, req *CreateJobRequest) (*Job, error)
	GetJob(ctx context.Context, req *GetJobRequest) (*Job, error)
	ListJobs(ctx context.Context, req *ListJobsRequest) (*ListJobsResponse, error)
	UpdateJob(ctx context.Context, req *UpdateJobRequest) (*Job, error)
	DeleteJob(ctx context.Context, req *DeleteJobRequest) (*emptypb.Empty, error)
	StartJob(ctx context.Context, req *StartJobRequest) (*Job, error)
	StopJob(ctx context.Context, req *StopJobRequest) (*Job, error)
	PauseJob(ctx context.Context, req *PauseJobRequest) (*Job, error)
	ResumeJob(ctx context.Context, req *ResumeJobRequest) (*Job, error)
	GetJobStatus(ctx context.Context, req *GetJobStatusRequest) (*JobStatus, error)
	SavepointJob(ctx context.Context, req *SavepointJobRequest) (*SavepointResponse, error)
}

type ConnectionService interface {
	CreateConnection(ctx context.Context, req *CreateConnectionRequest) (*Connection, error)
	GetConnection(ctx context.Context, req *GetConnectionRequest) (*Connection, error)
	ListConnections(ctx context.Context, req *ListConnectionsRequest) (*ListConnectionsResponse, error)
	TestConnection(ctx context.Context, req *TestConnectionRequest) (*TestConnectionResponse, error)
	DeleteConnection(ctx context.Context, req *DeleteConnectionRequest) (*emptypb.Empty, error)
}

type CatalogService interface {
	GetSchema(ctx context.Context, req *GetSchemaRequest) (*Schema, error)
	ListTables(ctx context.Context, req *ListTablesRequest) (*ListTablesResponse, error)
	GetTable(ctx context.Context, req *GetTableRequest) (*Table, error)
}
