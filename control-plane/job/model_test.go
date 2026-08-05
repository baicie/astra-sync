package job_test

import (
	"testing"
	"time"

	"github.com/google/uuid"

	"io.astrasync/control-plane/job"
)

func TestNewJobCopiesSpecificationAndStartsStopped(t *testing.T) {
	now := time.Date(2026, 8, 5, 5, 0, 0, 0, time.UTC)
	spec := validSpec()
	created, err := job.New(job.Key{Namespace: "default", Name: "orders"}, uuid.NewString(), spec, now)
	if err != nil {
		t.Fatalf("new job: %v", err)
	}

	spec.Source.Options["table"] = "changed"
	if got := created.Spec.Source.Options["table"]; got != "orders" {
		t.Fatalf("stored source option changed through caller map: %q", got)
	}
	if created.Status.Desired != job.DesiredStopped || created.Status.State != job.StateCreated {
		t.Fatalf("unexpected initial status: %+v", created.Status)
	}
	if created.Version != 1 || !created.CreatedAt.Equal(now) || !created.UpdatedAt.Equal(now) {
		t.Fatalf("unexpected identity metadata: %+v", created)
	}
}

func TestSpecValidationRejectsInvalidControlPlaneInputs(t *testing.T) {
	tests := []struct {
		name string
		edit func(*job.Spec)
	}{
		{name: "source connector", edit: func(spec *job.Spec) { spec.Source.Connector = " " }},
		{name: "non-canonical connector", edit: func(spec *job.Spec) { spec.Source.Connector = "MySQL CDC" }},
		{name: "sink option", edit: func(spec *job.Spec) { spec.Sink.Options[" "] = "value" }},
		{name: "delivery", edit: func(spec *job.Spec) { spec.Delivery.Guarantee = "sometimes" }},
		{name: "batch size", edit: func(spec *job.Spec) { spec.Runtime.MaxBatchRecords = 0 }},
		{name: "transform", edit: func(spec *job.Spec) {
			spec.Transforms = []job.TransformSpec{{Type: " "}}
		}},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			spec := validSpec()
			test.edit(&spec)
			if err := spec.Validate(); err == nil {
				t.Fatal("expected validation failure")
			}
		})
	}
}

func TestKeyRequiresLowercaseDNSLabels(t *testing.T) {
	for _, key := range []job.Key{
		{Namespace: "", Name: "orders"},
		{Namespace: "default", Name: "Orders"},
		{Namespace: "default", Name: "-orders"},
	} {
		if err := key.Validate(); err == nil {
			t.Fatalf("expected invalid key: %+v", key)
		}
	}
}

func TestJobValidationRejectsInvalidIdentityAndStateCombination(t *testing.T) {
	created := newTestJob(t, time.Date(2026, 8, 5, 5, 0, 0, 0, time.UTC))
	created.UID = "not-a-uuid"
	if err := created.Validate(); err == nil {
		t.Fatal("expected invalid UID failure")
	}

	created = newTestJob(t, time.Date(2026, 8, 5, 5, 0, 0, 0, time.UTC))
	created.Status.Desired = job.DesiredRunning
	if err := created.Validate(); err == nil {
		t.Fatal("expected inconsistent desired and observed state failure")
	}
}

func validSpec() job.Spec {
	return job.Spec{
		Source: job.ConnectorSpec{
			Connector: "mysql-cdc",
			Options:   map[string]string{"table": "orders"},
		},
		Sink: job.ConnectorSpec{
			Connector: "jdbc",
			Options:   map[string]string{"table": "orders"},
		},
		Delivery: job.DeliverySpec{Guarantee: job.DeliveryExactlyOnce},
		Runtime:  job.RuntimeSpec{MaxBatchRecords: 128},
	}
}
