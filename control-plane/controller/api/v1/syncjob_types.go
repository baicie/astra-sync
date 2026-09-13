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

// Validation rules for the astrasync.io/tenant-id label. Kubernetes CRD
// schemas cannot validate metadata.labels, so trusted writers must enforce
// this contract before creating or updating a SyncJob. Admission-level
// enforcement is implemented by ADR-087.
//
// +kubelinter:disabled
const (
	// TenantIDLabel is the Kubernetes label key for the tenant identifier
	// on SyncJob resources. The value MUST be a canonical lowercase UUID.
	// Used by controller_job_state_total and controller_epoch_fence_total
	// for per-tenant observability (ADR-066, ADR-071).
	TenantIDLabel = "astrasync.io/tenant-id"
)

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
//     owns this job. Used by controller_job_state_total (ADR-066) and
//     controller_epoch_fence_total (ADR-069) to label transitions per
//     tenant. If absent, the controller emits _unknown for the tenant_id
//     label.
//
// The label MUST be set by the caller at SyncJob creation time. Users
// creating SyncJob resources via kubectl MUST include the label.
// Applications creating SyncJob resources programmatically (e.g. the Console)
// MUST set the label from the authenticated principal's tenant context.
//
// The Kubernetes Namespace field is used as the Prometheus "namespace"
// label in controller_job_state_total. This is distinct from the
// astrasync.io/tenant-id label.
//
// The tenant-label contract requires:
//  1. The label key 'astrasync.io/tenant-id' is present and non-empty.
//  2. The label value matches the canonical lowercase UUID pattern.
//
// See ADR-029 (durable state machine), ADR-053 (production hardening),
// ADR-066 (controller emission slice 43.3.5), ADR-069 (epoch fence
// emission), ADR-071 (tenant-id label contract), ADR-086 (admission
// enforcement correction), and ADR-087 (ValidatingAdmissionPolicy).
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
