package job

import (
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/google/uuid"
)

const DefaultMaxBatchRecords = 1024

var (
	dnsLabel      = regexp.MustCompile(`^[a-z0-9](?:[-a-z0-9]*[a-z0-9])?$`)
	connectorName = regexp.MustCompile(`^[a-z0-9](?:[a-z0-9._-]*[a-z0-9])?$`)
)

type Key struct {
	Namespace string `json:"namespace"`
	Name      string `json:"name"`
}

func NewKey(namespace, name string) (Key, error) {
	key := Key{Namespace: namespace, Name: name}
	if err := key.Validate(); err != nil {
		return Key{}, err
	}
	return key, nil
}

func (k Key) Validate() error {
	if err := validateDNSLabel("namespace", k.Namespace); err != nil {
		return err
	}
	return validateDNSLabel("name", k.Name)
}

type ConnectorSpec struct {
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:MaxLength=128
	// +kubebuilder:validation:Pattern=`^[a-z0-9](?:[a-z0-9._-]*[a-z0-9])?$`
	Connector     string            `json:"connector"`
	ConnectionRef string            `json:"connectionRef,omitempty"`
	Options       map[string]string `json:"options,omitempty"`
}

type TransformSpec struct {
	Type    string            `json:"type"`
	Options map[string]string `json:"options,omitempty"`
}

// +kubebuilder:validation:Enum=at-most-once;at-least-once;exactly-once
type DeliveryGuarantee string

const (
	DeliveryAtMostOnce  DeliveryGuarantee = "at-most-once"
	DeliveryAtLeastOnce DeliveryGuarantee = "at-least-once"
	DeliveryExactlyOnce DeliveryGuarantee = "exactly-once"
)

type DeliverySpec struct {
	Guarantee DeliveryGuarantee `json:"guarantee"`
}

type Spec struct {
	Source     ConnectorSpec   `json:"source"`
	Sink       ConnectorSpec   `json:"sink"`
	Transforms []TransformSpec `json:"transforms,omitempty"`
	Delivery   DeliverySpec    `json:"delivery"`
	Runtime    RuntimeSpec     `json:"runtime"`
}

type RuntimeSpec struct {
	// +kubebuilder:default=1024
	// +kubebuilder:validation:Minimum=1
	MaxBatchRecords int32 `json:"maxBatchRecords"`
}

func (s Spec) Validate() error {
	if err := validateConnector("source", s.Source); err != nil {
		return err
	}
	if err := validateConnector("sink", s.Sink); err != nil {
		return err
	}
	for index, transform := range s.Transforms {
		if strings.TrimSpace(transform.Type) == "" {
			return fmt.Errorf("transform %d type must not be blank", index)
		}
		if err := validateOptions(fmt.Sprintf("transform %d", index), transform.Options); err != nil {
			return err
		}
	}
	switch s.Delivery.Guarantee {
	case DeliveryAtMostOnce, DeliveryAtLeastOnce, DeliveryExactlyOnce:
	default:
		return fmt.Errorf("unsupported delivery guarantee %q", s.Delivery.Guarantee)
	}
	if s.Runtime.MaxBatchRecords <= 0 {
		return fmt.Errorf("maxBatchRecords must be positive")
	}
	return nil
}

func (s Spec) Clone() Spec {
	copySpec := s
	copySpec.Source.Options = cloneMap(s.Source.Options)
	copySpec.Sink.Options = cloneMap(s.Sink.Options)
	copySpec.Transforms = make([]TransformSpec, len(s.Transforms))
	for index, transform := range s.Transforms {
		copySpec.Transforms[index] = transform
		copySpec.Transforms[index].Options = cloneMap(transform.Options)
	}
	return copySpec
}

// +kubebuilder:validation:Enum=STOPPED;RUNNING
type DesiredState string

const (
	DesiredStopped DesiredState = "STOPPED"
	DesiredRunning DesiredState = "RUNNING"
)

// +kubebuilder:validation:Enum=CREATED;INITIALIZING;RUNNING;CANCELING;CANCELED;FINISHED;FAILED
type State string

const (
	StateCreated      State = "CREATED"
	StateInitializing State = "INITIALIZING"
	StateRunning      State = "RUNNING"
	StateCanceling    State = "CANCELING"
	StateCanceled     State = "CANCELED"
	StateFinished     State = "FINISHED"
	StateFailed       State = "FAILED"
)

type Checkpoint struct {
	ID         int64     `json:"id"`
	Timestamp  time.Time `json:"timestamp"`
	StateSize  int64     `json:"stateSize"`
	DurationMS int32     `json:"durationMs"`
}

type Failure struct {
	Reason    string    `json:"reason"`
	RootCause string    `json:"rootCause,omitempty"`
	Timestamp time.Time `json:"timestamp"`
	Host      string    `json:"host,omitempty"`
}

type Status struct {
	Desired        DesiredState `json:"desiredState"`
	State          State        `json:"state"`
	Epoch          int64        `json:"epoch"`
	RestartCount   int32        `json:"restartCount"`
	StartTime      *time.Time   `json:"startTime,omitempty"`
	EndTime        *time.Time   `json:"endTime,omitempty"`
	LastCheckpoint *Checkpoint  `json:"lastCheckpoint,omitempty"`
	Failure        *Failure     `json:"failure,omitempty"`
}

type Job struct {
	Key       Key       `json:"key"`
	UID       string    `json:"uid"`
	Version   int64     `json:"version"`
	Spec      Spec      `json:"spec"`
	Status    Status    `json:"status"`
	CreatedAt time.Time `json:"createdAt"`
	UpdatedAt time.Time `json:"updatedAt"`
}

func New(key Key, uid string, spec Spec, now time.Time) (Job, error) {
	if err := key.Validate(); err != nil {
		return Job{}, err
	}
	if _, err := uuid.Parse(uid); err != nil {
		return Job{}, fmt.Errorf("uid must be a UUID")
	}
	if err := spec.Validate(); err != nil {
		return Job{}, err
	}
	now = now.UTC()
	return Job{
		Key:       key,
		UID:       uid,
		Version:   1,
		Spec:      spec.Clone(),
		Status:    Status{Desired: DesiredStopped, State: StateCreated},
		CreatedAt: now,
		UpdatedAt: now,
	}, nil
}

func (j Job) Clone() Job {
	copyJob := j
	copyJob.Spec = j.Spec.Clone()
	copyJob.Status.StartTime = cloneTime(j.Status.StartTime)
	copyJob.Status.EndTime = cloneTime(j.Status.EndTime)
	if j.Status.LastCheckpoint != nil {
		checkpoint := *j.Status.LastCheckpoint
		copyJob.Status.LastCheckpoint = &checkpoint
	}
	if j.Status.Failure != nil {
		failure := *j.Status.Failure
		copyJob.Status.Failure = &failure
	}
	return copyJob
}

func (j Job) Validate() error {
	if err := j.Key.Validate(); err != nil {
		return err
	}
	if _, err := uuid.Parse(j.UID); err != nil {
		return fmt.Errorf("uid must be a UUID")
	}
	if j.Version <= 0 {
		return fmt.Errorf("version must be positive")
	}
	if err := j.Spec.Validate(); err != nil {
		return err
	}
	if err := j.Status.Validate(); err != nil {
		return err
	}
	if j.CreatedAt.IsZero() || j.UpdatedAt.IsZero() {
		return fmt.Errorf("job timestamps must be set")
	}
	if j.UpdatedAt.Before(j.CreatedAt) {
		return fmt.Errorf("updatedAt must not precede createdAt")
	}
	return nil
}

func (s Status) Validate() error {
	switch s.Desired {
	case DesiredStopped, DesiredRunning:
	default:
		return fmt.Errorf("unsupported desired state %q", s.Desired)
	}
	switch s.State {
	case StateCreated, StateInitializing, StateRunning, StateCanceling, StateCanceled, StateFinished, StateFailed:
	default:
		return fmt.Errorf("unsupported job state %q", s.State)
	}
	if s.Epoch < 0 || s.RestartCount < 0 {
		return fmt.Errorf("epoch and restartCount must not be negative")
	}
	if s.State == StateCreated && s.Epoch != 0 {
		return fmt.Errorf("created job must have epoch zero")
	}
	if s.State != StateCreated && s.Epoch == 0 {
		return fmt.Errorf("non-created job must have a positive epoch")
	}
	if (s.State == StateInitializing || s.State == StateRunning) != (s.Desired == DesiredRunning) {
		return fmt.Errorf("desired state must be RUNNING exactly for initializing or running jobs")
	}
	if s.Epoch == 0 && s.StartTime != nil || s.Epoch > 0 && s.StartTime == nil {
		return fmt.Errorf("startTime must be present exactly when an execution epoch exists")
	}
	if terminal(s.State) != (s.EndTime != nil) {
		return fmt.Errorf("endTime must be present exactly for terminal states")
	}
	if (s.State == StateFailed) != (s.Failure != nil) {
		return fmt.Errorf("failure details must be present exactly for FAILED state")
	}
	if s.Failure != nil && (strings.TrimSpace(s.Failure.Reason) == "" || s.Failure.Timestamp.IsZero()) {
		return fmt.Errorf("failure reason and timestamp must be set")
	}
	if s.LastCheckpoint != nil && (s.LastCheckpoint.ID < 0 || s.LastCheckpoint.Timestamp.IsZero() ||
		s.LastCheckpoint.StateSize < 0 || s.LastCheckpoint.DurationMS < 0) {
		return fmt.Errorf("checkpoint values must not be negative and timestamp must be set")
	}
	return nil
}

func validateConnector(field string, connector ConnectorSpec) error {
	if len(connector.Connector) > 128 || !connectorName.MatchString(connector.Connector) {
		return fmt.Errorf("%s connector has an invalid canonical name", field)
	}
	return validateOptions(field, connector.Options)
}

func validateOptions(field string, options map[string]string) error {
	for key := range options {
		if strings.TrimSpace(key) == "" {
			return fmt.Errorf("%s option key must not be blank", field)
		}
	}
	return nil
}

func validateDNSLabel(field, value string) error {
	if len(value) == 0 || len(value) > 63 || !dnsLabel.MatchString(value) {
		return fmt.Errorf("%s must be a lowercase DNS label", field)
	}
	return nil
}

func cloneMap(source map[string]string) map[string]string {
	if source == nil {
		return nil
	}
	result := make(map[string]string, len(source))
	for key, value := range source {
		result[key] = value
	}
	return result
}

func cloneTime(source *time.Time) *time.Time {
	if source == nil {
		return nil
	}
	copyTime := *source
	return &copyTime
}
