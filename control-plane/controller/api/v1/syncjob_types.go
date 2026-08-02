package v1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

type SyncJobSpec struct {
	Source      SourceSpec       `json:"source"`
	Sink        SinkSpec         `json:"sink"`
	Transforms  []TransformSpec  `json:"transforms,omitempty"`
	Delivery    DeliverySpec     `json:"delivery"`
	Parallelism ParallelismSpec  `json:"parallelism"`
	Checkpoint  CheckpointSpec   `json:"checkpoint"`
}

type SourceSpec struct {
	Connector      string        `json:"connector"`
	ConnectionRef  string        `json:"connectionRef"`
	Tables         TableSelector `json:"tables"`
}

type SinkSpec struct {
	Connector     string `json:"connector"`
	ConnectionRef string `json:"connectionRef"`
	TargetTable   string `json:"targetTable"`
}

type TransformSpec struct {
	Type    string            `json:"type"`
	Options map[string]string `json:"options,omitempty"`
}

type DeliverySpec struct {
	Guarantee string `json:"guarantee"`
}

type ParallelismSpec struct {
	Initial int `json:"initial"`
	Min     int `json:"min"`
	Max     int `json:"max"`
}

type CheckpointSpec struct {
	Interval string `json:"interval"`
	Timeout  string `json:"timeout"`
}

type TableSelector struct {
	Include []string `json:"include"`
	Exclude []string `json:"exclude,omitempty"`
}

type SyncJobStatus struct {
	Phase       string         `json:"phase"`
	JobID       string         `json:"jobId,omitempty"`
	Epoch       int64          `json:"epoch,omitempty"`
	Failed      bool           `json:"failed"`
	FailureInfo *FailureInfo   `json:"failureInfo,omitempty"`
}

type FailureInfo struct {
	Reason    string `json:"reason"`
	Timestamp int64  `json:"timestamp"`
}

// +kubebuilder:object:root=true
// +kubebuilder:resource:scope=Namespaced
// +kubebuilder:printcolumn:name="State",type="string",JSONPath=".spec.state"
// +kubebuilder:printcolumn:name="Phase",type="string",JSONPath=".status.phase"
// +kubebuilder:printcolumn:name="Age",type="date",JSONPath=".metadata.creationTimestamp"

type SyncJob struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata"`

	Spec   SyncJobSpec   `json:"spec"`
	Status SyncJobStatus `json:"status"`
}

type ConnectionSpec struct {
	Type     string            `json:"type"`
	Host     string            `json:"host"`
	Port     int               `json:"port"`
	Database string            `json:"database"`
	Username string            `json:"username"`
	SecretRef string           `json:"secretRef"`
	Properties map[string]string `json:"properties,omitempty"`
}

type ConnectionStatus struct {
	Available    bool   `json:"available"`
	LastTested   int64  `json:"lastTested,omitempty"`
	LastError    string `json:"lastError,omitempty"`
}

type Connection struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata"`

	Spec   ConnectionSpec   `json:"spec"`
	Status ConnectionStatus `json:"status"`
}
