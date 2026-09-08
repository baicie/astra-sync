package v1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"io.astrasync/control-plane/job"
)

type SyncJobSpec struct {
	Source     job.ConnectorSpec   `json:"source"`
	Sink       job.ConnectorSpec   `json:"sink"`
	Transforms []job.TransformSpec `json:"transforms,omitempty"`
	Delivery   job.DeliverySpec    `json:"delivery"`
	Runtime    job.RuntimeSpec     `json:"runtime"`
	// +kubebuilder:default=STOPPED
	State job.DesiredState `json:"state,omitempty"`
}

type SyncJobStatus struct {
	Desired        job.DesiredState `json:"desiredState,omitempty"`
	State          job.State        `json:"state,omitempty"`
	Epoch          int64            `json:"epoch,omitempty"`
	RestartCount   int32            `json:"restartCount,omitempty"`
	StartTime      *metav1.Time     `json:"startTime,omitempty"`
	EndTime        *metav1.Time     `json:"endTime,omitempty"`
	LastCheckpoint *CheckpointInfo  `json:"lastCheckpoint,omitempty"`
	Failure        *FailureInfo     `json:"failure,omitempty"`
}

type CheckpointInfo struct {
	ID         int64       `json:"id"`
	Timestamp  metav1.Time `json:"timestamp"`
	StateSize  int64       `json:"stateSize"`
	DurationMS int32       `json:"durationMs"`
}

type FailureInfo struct {
	Reason    string      `json:"reason"`
	RootCause string      `json:"rootCause,omitempty"`
	Timestamp metav1.Time `json:"timestamp"`
	Host      string      `json:"host,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:scope=Namespaced,singular=syncjob,shortName=sj
// +kubebuilder:printcolumn:name="Desired",type="string",JSONPath=".spec.state"
// +kubebuilder:printcolumn:name="State",type="string",JSONPath=".status.state"
// +kubebuilder:printcolumn:name="Epoch",type="integer",JSONPath=".status.epoch"
// +kubebuilder:printcolumn:name="Age",type="date",JSONPath=".metadata.creationTimestamp"

// SyncJob represents a data synchronization job managed by the AstraSync
// controller. The following labels are required for observability:
//
//   - astrasync.io/tenant-id: canonical lowercase UUID of the tenant that
//     owns this job. Used by controller_job_state_total to label job state
//     transitions per tenant (ADR-058 §2, ADR-066). If absent, the
//     controller emits _unknown for the tenant_id label.
//
// Labels are set by the API server at creation time; they are not enforced
// by the Kubernetes apiserver. A future slice will add kubebuilder
// validation markers to enforce the tenant-id label presence.
//
// The Kubernetes Namespace field is used as the Prometheus "namespace"
// label in controller_job_state_total. This is distinct from the
// astrasync.io/tenant-id label.
//
// See ADR-029 (durable state machine), ADR-053 (production hardening),
// and ADR-066 (controller emission slice 43.3.5).
type SyncJob struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`
	Spec              SyncJobSpec   `json:"spec"`
	Status            SyncJobStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

type SyncJobList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []SyncJob `json:"items"`
}
