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
	State       string          `json:"state,omitempty"`
}

type SourceSpec struct {
	Connector     string        `json:"connector"`
	ConnectionRef string        `json:"connectionRef"`
	Tables        TableSelector `json:"tables"`
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
	Guarantee DeliveryGuarantee `json:"guarantee"`
}

type DeliveryGuarantee string

const (
	ExactlyOnce DeliveryGuarantee = "exactly-once"
	AtLeastOnce DeliveryGuarantee = "at-least-once"
	AtMostOnce  DeliveryGuarantee = "at-most-once"
)

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
	Phase           string          `json:"phase"`
	JobID           string          `json:"jobId,omitempty"`
	Epoch           int64           `json:"epoch,omitempty"`
	CurrentAttempt  int32           `json:"currentAttempt,omitempty"`
	RestartCount    int32           `json:"restartCount,omitempty"`
	StartTime       *metav1.Time    `json:"startTime,omitempty"`
	CompletionTime  *metav1.Time    `json:"completionTime,omitempty"`
	LastCheckpoint  *CheckpointInfo `json:"lastCheckpoint,omitempty"`
	Failed          bool            `json:"failed"`
	FailureInfo     *FailureInfo    `json:"failureInfo,omitempty"`
}

type CheckpointInfo struct {
	ID            int64      `json:"id"`
	Timestamp     metav1.Time `json:"timestamp"`
	StateSize     int64      `json:"stateSize"`
	DurationMs    int64      `json:"durationMs"`
	IsAligned     bool       `json:"isAligned"`
}

type FailureInfo struct {
	Reason      string `json:"reason"`
	RootCause   string `json:"rootCause,omitempty"`
	StackTrace  string `json:"stackTrace,omitempty"`
	Timestamp   int64  `json:"timestamp"`
	Host        string `json:"host,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:scope=Namespaced,singular=syncjob,shortName=sj,plural=syncjobs
// +kubebuilder:printcolumn:name="State",type="string",JSONPath=".spec.state"
// +kubebuilder:printcolumn:name="Phase",type="string",JSONPath=".status.phase"
// +kubebuilder:printcolumn:name="JobID",type="string",JSONPath=".status.jobId"
// +kubebuilder:printcolumn:name="Age",type="date",JSONPath=".metadata.creationTimestamp"
// +kubebuilder:storageversion

type SyncJob struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata"`
	Spec   SyncJobSpec   `json:"spec"`
	Status SyncJobStatus `json:"status,omitempty"`
}

type ConnectionSpec struct {
	Type        ConnectionType      `json:"type"`
	Host        string              `json:"host"`
	Port        int                 `json:"port"`
	Database    string              `json:"database"`
	Username    string              `json:"username"`
	SecretRef   string              `json:"secretRef"`
	Properties  map[string]string    `json:"properties,omitempty"`
}

type ConnectionType string

const (
	TypeMySQL       ConnectionType = "mysql"
	TypePostgreSQL  ConnectionType = "postgresql"
	TypeOracle      ConnectionType = "oracle"
	TypeSQLServer   ConnectionType = "sqlserver"
	TypeMongoDB     ConnectionType = "mongodb"
	TypeKafka       ConnectionType = "kafka"
	TypePulsar      ConnectionType = "pulsar"
	TypeS3          ConnectionType = "s3"
	TypeGCS         ConnectionType = "gcs"
	TypeHDFS        ConnectionType = "hdfs"
	TypeJDBC        ConnectionType = "jdbc"
	TypeIceberg     ConnectionType = "iceberg"
	TypeClickHouse  ConnectionType = "clickhouse"
)

type ConnectionStatus struct {
	Available   bool      `json:"available"`
	LastTested  metav1.Time `json:"lastTested,omitempty"`
	LastError   string    `json:"lastError,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:resource:scope=Namespaced,singular=connection,shortName=conn,plural=connections
// +kubebuilder:printcolumn:name="Type",type="string",JSONPath=".spec.type"
// +kubebuilder:printcolumn:name="Host",type="string",JSONPath=".spec.host"
// +kubebuilder:printcolumn:name="Available",type="boolean",JSONPath=".status.available"
// +kubebuilder:printcolumn:name="Age",type="date",JSONPath=".metadata.creationTimestamp"

type Connection struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata"`
	Spec   ConnectionSpec   `json:"spec"`
	Status ConnectionStatus  `json:"status,omitempty"`
}
