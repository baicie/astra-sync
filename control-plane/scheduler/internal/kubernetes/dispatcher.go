package kubernetes

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"strconv"
	"strings"

	"github.com/google/uuid"
	appsv1 "k8s.io/api/apps/v1"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/client-go/kubernetes"

	"io.astrasync/control-plane/job"
	"io.astrasync/control-plane/scheduler/internal/dispatch"
	"io.astrasync/control-plane/scheduler/internal/materialization"
	"io.astrasync/control-plane/scheduler/internal/scheduler"
)

const (
	jobSpecPath       = "/etc/astrasync/job/job.yaml"
	progressMountPath = "/var/lib/astrasync/progress"
	heartbeatTokenKey = "heartbeat-token"
	labelJobUID       = "sync.astrasync.io/job-uid"
	labelEpoch        = "sync.astrasync.io/execution-epoch"
	labelComponent    = "app.kubernetes.io/component"
	annotationSpec    = "sync.astrasync.io/job-spec-sha256"
)

type Config struct {
	Namespace                        string
	ServiceAccount                   string
	CoordinatorImage                 string
	WorkerImage                      string
	HeartbeatURL                     string
	HeartbeatIntervalMillis          int32
	ImagePullPolicy                  corev1.PullPolicy
	ProgressClaim                    string
	WorkerReplicas                   int32
	WorkerPort                       int32
	WorkerTimeoutMillis              int32
	CoordinatorMaxInFlightTasks      int32
	WorkerMaxInFlightBatches         int32
	WorkerMaxConcurrentTasks         int32
	WorkerTaskQueueCapacity          int32
	WorkerMaxConnections             int32
	CoordinatorBackoffLimit          int32
	TTLSecondsAfterFinished          *int32
	CoordinatorResources             corev1.ResourceRequirements
	WorkerResources                  corev1.ResourceRequirements
	CoordinatorJavaToolOptions       string
	WorkerJavaToolOptions            string
	ConnectionMaterializationEnabled bool
}

type Dispatcher struct {
	client      kubernetes.Interface
	config      Config
	credentials CredentialMaterializer
}

type CredentialMaterializer interface {
	Ensure(context.Context, dispatch.Record) (materialization.Result, error)
	Cleanup(context.Context, dispatch.Identity) error
}

func New(client kubernetes.Interface, config Config) (*Dispatcher, error) {
	return newDispatcher(client, config, nil)
}

func NewWithCredentialMaterializer(
	client kubernetes.Interface, config Config, credentials CredentialMaterializer,
) (*Dispatcher, error) {
	return newDispatcher(client, config, credentials)
}

func newDispatcher(
	client kubernetes.Interface, config Config, credentials CredentialMaterializer,
) (*Dispatcher, error) {
	if client == nil {
		return nil, fmt.Errorf("Kubernetes client must not be nil")
	}
	if config.Namespace == "" || config.ServiceAccount == "" || config.CoordinatorImage == "" ||
		config.WorkerImage == "" || config.ProgressClaim == "" || config.HeartbeatURL == "" {
		return nil, fmt.Errorf("Kubernetes dispatch identity, images, and progress claim are required")
	}
	if config.WorkerReplicas <= 0 || config.WorkerPort <= 0 || config.WorkerPort > 65535 ||
		config.WorkerTimeoutMillis <= 0 || config.CoordinatorMaxInFlightTasks <= 0 ||
		config.WorkerMaxInFlightBatches <= 0 || config.WorkerMaxConcurrentTasks <= 0 ||
		config.WorkerTaskQueueCapacity < 0 || config.WorkerMaxConnections <= 0 ||
		config.CoordinatorBackoffLimit < 0 || config.HeartbeatIntervalMillis <= 0 {
		return nil, fmt.Errorf("Kubernetes dispatch numeric configuration is invalid")
	}
	heartbeatURL, err := url.Parse(config.HeartbeatURL)
	if err != nil || heartbeatURL.Host == "" ||
		(heartbeatURL.Scheme != "http" && heartbeatURL.Scheme != "https") ||
		heartbeatURL.RawQuery != "" || heartbeatURL.Fragment != "" {
		return nil, fmt.Errorf("Kubernetes dispatch heartbeat URL must use HTTP or HTTPS")
	}
	config.HeartbeatURL = strings.TrimRight(config.HeartbeatURL, "/")
	if config.ConnectionMaterializationEnabled && credentials == nil {
		return nil, fmt.Errorf("Connection materialization is enabled without a credential materializer")
	}
	return &Dispatcher{client: client, config: config, credentials: credentials}, nil
}

func (d *Dispatcher) Reconcile(
	ctx context.Context, candidate job.Job, record dispatch.Record,
) (scheduler.Observation, error) {
	identity := record.Identity
	if candidate.UID != identity.JobUID {
		return scheduler.Observation{}, scheduler.Permanent(fmt.Errorf("dispatch record does not match the Job execution"))
	}
	if _, err := uuid.Parse(record.HeartbeatToken); err != nil {
		return scheduler.Observation{}, scheduler.Permanent(fmt.Errorf("dispatch heartbeat token must be a UUID"))
	}
	base, err := resourceBase(identity)
	if err != nil {
		return scheduler.Observation{}, scheduler.Permanent(err)
	}
	document, fingerprint, err := jobDocument(candidate)
	if err != nil {
		return scheduler.Observation{}, scheduler.Permanent(err)
	}
	credentialSecret := ""
	credentialCompilerRevision := ""
	if d.config.ConnectionMaterializationEnabled {
		materialized, materializeErr := d.credentials.Ensure(ctx, record)
		if materializeErr != nil {
			if errors.Is(materializeErr, materialization.ErrProviderPolicy) ||
				errors.Is(materializeErr, materialization.ErrRevisionMismatch) ||
				errors.Is(materializeErr, materialization.ErrReceiptConflict) {
				return scheduler.Observation{}, scheduler.Permanent(materializeErr)
			}
			return scheduler.Observation{}, materializeErr
		}
		if err := verifyMaterializedRoles(candidate, materialized); err != nil {
			return scheduler.Observation{}, scheduler.Permanent(err)
		}
		if materialized.Required {
			credentialSecret = materialized.SecretName
			credentialCompilerRevision = materialized.CompilerRevision
			fingerprint = combinedFingerprint(fingerprint, materialized.IdentityFingerprint)
		}
	} else if candidate.Spec.Source.ConnectionRef != "" || candidate.Spec.Sink.ConnectionRef != "" {
		return scheduler.Observation{}, scheduler.Permanent(fmt.Errorf("Connection materialization is disabled"))
	}

	coordinatorName := base + "-coordinator"
	existing, err := d.client.BatchV1().Jobs(d.config.Namespace).Get(ctx, coordinatorName, metav1.GetOptions{})
	if err == nil {
		if err := verifyManaged(existing.Labels, existing.Annotations, identity, fingerprint); err != nil {
			return scheduler.Observation{}, scheduler.Permanent(err)
		}
		if observation, terminal := observeJob(existing); terminal {
			if cleanupErr := d.cleanupAuxiliary(ctx, base); cleanupErr != nil {
				return scheduler.Observation{}, cleanupErr
			}
			return observation, nil
		}
	} else if !apierrors.IsNotFound(err) {
		return scheduler.Observation{}, fmt.Errorf("get Coordinator Job: %w", err)
	} else {
		existing = nil
	}

	if err := d.ensureSecret(ctx, base, identity, fingerprint, document, record.HeartbeatToken); err != nil {
		return scheduler.Observation{}, err
	}
	if err := d.ensureService(ctx, base, identity, fingerprint); err != nil {
		return scheduler.Observation{}, err
	}
	workers, err := d.ensureWorkers(
		ctx, base, identity, fingerprint, credentialSecret, credentialCompilerRevision)
	if err != nil {
		return scheduler.Observation{}, err
	}
	if workers.Status.ReadyReplicas < d.config.WorkerReplicas {
		return scheduler.Observation{State: scheduler.ObservationPending}, nil
	}
	if existing == nil {
		created, createErr := d.client.BatchV1().Jobs(d.config.Namespace).Create(
			ctx,
			d.coordinatorJob(base, identity, fingerprint, credentialSecret, credentialCompilerRevision),
			metav1.CreateOptions{},
		)
		if apierrors.IsAlreadyExists(createErr) {
			existing, err = d.client.BatchV1().Jobs(d.config.Namespace).Get(
				ctx, coordinatorName, metav1.GetOptions{})
			if err != nil {
				return scheduler.Observation{}, fmt.Errorf("get concurrently created Coordinator Job: %w", err)
			}
			if err := verifyManaged(existing.Labels, existing.Annotations, identity, fingerprint); err != nil {
				return scheduler.Observation{}, scheduler.Permanent(err)
			}
		} else if createErr != nil {
			return scheduler.Observation{}, fmt.Errorf("create Coordinator Job: %w", createErr)
		} else {
			existing = created
		}
	}
	if existing != nil && existing.Status.Active > 0 {
		return scheduler.Observation{State: scheduler.ObservationRunning}, nil
	}
	return scheduler.Observation{State: scheduler.ObservationPending}, nil
}

func (d *Dispatcher) Stop(ctx context.Context, identity dispatch.Identity) (bool, error) {
	base, err := resourceBase(identity)
	if err != nil {
		return false, err
	}
	deletePolicy := metav1.DeletePropagationForeground
	var failures []error
	if err := d.client.BatchV1().Jobs(d.config.Namespace).Delete(
		ctx, base+"-coordinator", metav1.DeleteOptions{PropagationPolicy: &deletePolicy}); err != nil && !apierrors.IsNotFound(err) {
		failures = append(failures, fmt.Errorf("delete Coordinator Job: %w", err))
	}
	if err := d.deleteAuxiliary(ctx, base, deletePolicy); err != nil {
		failures = append(failures, err)
	}
	if len(failures) > 0 {
		return false, errors.Join(failures...)
	}
	gone, err := d.resourcesGone(ctx, base)
	if err != nil || !gone {
		return gone, err
	}
	if d.credentials != nil {
		if err := d.credentials.Cleanup(ctx, identity); err != nil {
			return false, err
		}
	}
	return true, nil
}

// Cleanup removes the Secret and Worker resources while retaining the terminal
// Coordinator Job until its Kubernetes TTL expires for post-mortem inspection.
func (d *Dispatcher) Cleanup(ctx context.Context, identity dispatch.Identity) error {
	base, err := resourceBase(identity)
	if err != nil {
		return err
	}
	if err := d.cleanupAuxiliary(ctx, base); err != nil {
		return err
	}
	if d.credentials != nil {
		return d.credentials.Cleanup(ctx, identity)
	}
	return nil
}

// SweepOrphans removes auxiliary resources for non-active executions and Coordinator Jobs that no
// longer have any dispatch record. Known terminal Coordinator Jobs remain available for their TTL.
func (d *Dispatcher) SweepOrphans(ctx context.Context, records []dispatch.Record) error {
	active := make(map[dispatch.Identity]struct{})
	known := make(map[dispatch.Identity]struct{}, len(records))
	for _, record := range records {
		known[record.Identity] = struct{}{}
		if dispatch.Active(record.Phase) {
			active[record.Identity] = struct{}{}
		}
	}
	selector := labelComponent + "!=coordinator,app.kubernetes.io/managed-by=astrasync-scheduler"
	var failures []error
	credentialOrphans := make([]dispatch.Identity, 0)
	secrets, err := d.client.CoreV1().Secrets(d.config.Namespace).List(ctx, metav1.ListOptions{LabelSelector: selector})
	if err != nil {
		failures = append(failures, fmt.Errorf("list orphan JobSpec Secrets: %w", err))
	} else {
		for index := range secrets.Items {
			resource := &secrets.Items[index]
			if orphan, identity := orphanResource(resource.Labels, active); orphan {
				if resource.Labels[labelComponent] == "execution-credentials" && d.credentials != nil {
					credentialOrphans = append(credentialOrphans, identity)
					continue
				}
				if deleteErr := d.client.CoreV1().Secrets(d.config.Namespace).Delete(
					ctx, resource.Name, metav1.DeleteOptions{}); deleteErr != nil && !apierrors.IsNotFound(deleteErr) {
					failures = append(failures, fmt.Errorf("delete orphan Secret %s: %w", resource.Name, deleteErr))
				}
			}
		}
	}

	services, err := d.client.CoreV1().Services(d.config.Namespace).List(ctx, metav1.ListOptions{LabelSelector: selector})
	if err != nil {
		failures = append(failures, fmt.Errorf("list orphan Worker Services: %w", err))
	} else {
		for index := range services.Items {
			resource := &services.Items[index]
			if orphan, _ := orphanResource(resource.Labels, active); orphan {
				if deleteErr := d.client.CoreV1().Services(d.config.Namespace).Delete(
					ctx, resource.Name, metav1.DeleteOptions{}); deleteErr != nil && !apierrors.IsNotFound(deleteErr) {
					failures = append(failures, fmt.Errorf("delete orphan Service %s: %w", resource.Name, deleteErr))
				}
			}
		}
	}

	statefulSets, err := d.client.AppsV1().StatefulSets(d.config.Namespace).List(ctx, metav1.ListOptions{LabelSelector: selector})
	if err != nil {
		failures = append(failures, fmt.Errorf("list orphan Worker StatefulSets: %w", err))
	} else {
		for index := range statefulSets.Items {
			resource := &statefulSets.Items[index]
			if orphan, _ := orphanResource(resource.Labels, active); orphan {
				policy := metav1.DeletePropagationForeground
				if deleteErr := d.client.AppsV1().StatefulSets(d.config.Namespace).Delete(
					ctx, resource.Name, metav1.DeleteOptions{PropagationPolicy: &policy}); deleteErr != nil && !apierrors.IsNotFound(deleteErr) {
					failures = append(failures, fmt.Errorf("delete orphan StatefulSet %s: %w", resource.Name, deleteErr))
				}
			}
		}
	}

	coordinators, err := d.client.BatchV1().Jobs(d.config.Namespace).List(ctx, metav1.ListOptions{
		LabelSelector: labelComponent + "=coordinator,app.kubernetes.io/managed-by=astrasync-scheduler",
	})
	if err != nil {
		failures = append(failures, fmt.Errorf("list orphan Coordinator Jobs: %w", err))
	} else {
		for index := range coordinators.Items {
			resource := &coordinators.Items[index]
			if orphan, _ := orphanResource(resource.Labels, known); orphan {
				policy := metav1.DeletePropagationForeground
				if deleteErr := d.client.BatchV1().Jobs(d.config.Namespace).Delete(
					ctx, resource.Name, metav1.DeleteOptions{PropagationPolicy: &policy}); deleteErr != nil && !apierrors.IsNotFound(deleteErr) {
					failures = append(failures, fmt.Errorf("delete orphan Coordinator Job %s: %w", resource.Name, deleteErr))
				}
			}
		}
	}
	for _, identity := range credentialOrphans {
		base, baseErr := resourceBase(identity)
		if baseErr != nil {
			continue
		}
		gone, goneErr := d.dataPlaneAuxiliaryGone(ctx, base)
		if goneErr != nil {
			failures = append(failures, goneErr)
			continue
		}
		if gone {
			if cleanupErr := d.credentials.Cleanup(ctx, identity); cleanupErr != nil {
				failures = append(failures, cleanupErr)
			}
		}
	}
	return errors.Join(failures...)
}

func orphanResource(resourceLabels map[string]string, active map[dispatch.Identity]struct{}) (bool, dispatch.Identity) {
	identity := dispatch.Identity{JobUID: resourceLabels[labelJobUID]}
	epoch, err := strconv.ParseInt(resourceLabels[labelEpoch], 10, 64)
	if err != nil {
		return false, dispatch.Identity{}
	}
	identity.Epoch = epoch
	if err := identity.Validate(); err != nil {
		return false, dispatch.Identity{}
	}
	if _, err := uuid.Parse(identity.JobUID); err != nil {
		return false, dispatch.Identity{}
	}
	if _, exists := active[identity]; exists {
		return false, identity
	}
	return true, identity
}

func (d *Dispatcher) ensureSecret(
	ctx context.Context,
	base string,
	identity dispatch.Identity,
	fingerprint string,
	document []byte,
	heartbeatToken string,
) error {
	name := base + "-spec"
	existing, err := d.client.CoreV1().Secrets(d.config.Namespace).Get(ctx, name, metav1.GetOptions{})
	if err == nil {
		if err := verifyManaged(existing.Labels, existing.Annotations, identity, fingerprint); err != nil {
			return scheduler.Permanent(err)
		}
		if string(existing.Data[heartbeatTokenKey]) != heartbeatToken {
			return scheduler.Permanent(fmt.Errorf("Kubernetes execution heartbeat token changed"))
		}
		return nil
	}
	if !apierrors.IsNotFound(err) {
		return fmt.Errorf("get JobSpec Secret: %w", err)
	}
	immutable := true
	_, err = d.client.CoreV1().Secrets(d.config.Namespace).Create(ctx, &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name: name, Namespace: d.config.Namespace,
			Labels: labels(identity, "job-spec"), Annotations: annotations(fingerprint),
		},
		Immutable: &immutable,
		Type:      corev1.SecretTypeOpaque,
		Data: map[string][]byte{
			"job.yaml": document, heartbeatTokenKey: []byte(heartbeatToken),
		},
	}, metav1.CreateOptions{})
	if apierrors.IsAlreadyExists(err) {
		existing, err = d.client.CoreV1().Secrets(d.config.Namespace).Get(ctx, name, metav1.GetOptions{})
		if err != nil {
			return fmt.Errorf("get concurrently created JobSpec Secret: %w", err)
		}
		if err := verifyManaged(existing.Labels, existing.Annotations, identity, fingerprint); err != nil {
			return scheduler.Permanent(err)
		}
		if string(existing.Data[heartbeatTokenKey]) != heartbeatToken {
			return scheduler.Permanent(fmt.Errorf("Kubernetes execution heartbeat token changed"))
		}
		return nil
	}
	if err != nil {
		return fmt.Errorf("create JobSpec Secret: %w", err)
	}
	return nil
}

func (d *Dispatcher) ensureService(
	ctx context.Context, base string, identity dispatch.Identity, fingerprint string,
) error {
	name := base + "-worker"
	existing, err := d.client.CoreV1().Services(d.config.Namespace).Get(ctx, name, metav1.GetOptions{})
	if err == nil {
		if err := verifyManaged(existing.Labels, existing.Annotations, identity, fingerprint); err != nil {
			return scheduler.Permanent(err)
		}
		return nil
	}
	if !apierrors.IsNotFound(err) {
		return fmt.Errorf("get Worker Service: %w", err)
	}
	workerLabels := labels(identity, "execution-worker")
	_, err = d.client.CoreV1().Services(d.config.Namespace).Create(ctx, &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name: name, Namespace: d.config.Namespace,
			Labels: labels(identity, "worker-service"), Annotations: annotations(fingerprint),
		},
		Spec: corev1.ServiceSpec{
			ClusterIP:                corev1.ClusterIPNone,
			PublishNotReadyAddresses: true,
			Selector:                 workerLabels,
			Ports: []corev1.ServicePort{{
				Name: "worker", Port: d.config.WorkerPort, TargetPort: intstr.FromString("worker"),
			}},
		},
	}, metav1.CreateOptions{})
	if apierrors.IsAlreadyExists(err) {
		existing, err = d.client.CoreV1().Services(d.config.Namespace).Get(ctx, name, metav1.GetOptions{})
		if err != nil {
			return fmt.Errorf("get concurrently created Worker Service: %w", err)
		}
		if err := verifyManaged(existing.Labels, existing.Annotations, identity, fingerprint); err != nil {
			return scheduler.Permanent(err)
		}
		return nil
	}
	if err != nil {
		return fmt.Errorf("create Worker Service: %w", err)
	}
	return nil
}

func (d *Dispatcher) ensureWorkers(
	ctx context.Context,
	base string,
	identity dispatch.Identity,
	fingerprint string,
	credentialSecret string,
	credentialCompilerRevision string,
) (*appsv1.StatefulSet, error) {
	name := base + "-worker"
	existing, err := d.client.AppsV1().StatefulSets(d.config.Namespace).Get(ctx, name, metav1.GetOptions{})
	if err == nil {
		if err := verifyManaged(existing.Labels, existing.Annotations, identity, fingerprint); err != nil {
			return nil, scheduler.Permanent(err)
		}
		return existing, nil
	}
	if !apierrors.IsNotFound(err) {
		return nil, fmt.Errorf("get Worker StatefulSet: %w", err)
	}
	created, err := d.client.AppsV1().StatefulSets(d.config.Namespace).Create(
		ctx,
		d.workerStatefulSet(base, identity, fingerprint, credentialSecret, credentialCompilerRevision),
		metav1.CreateOptions{})
	if err != nil {
		if apierrors.IsAlreadyExists(err) {
			existing, err = d.client.AppsV1().StatefulSets(d.config.Namespace).Get(
				ctx, name, metav1.GetOptions{})
			if err != nil {
				return nil, fmt.Errorf("get concurrently created Worker StatefulSet: %w", err)
			}
			if err := verifyManaged(existing.Labels, existing.Annotations, identity, fingerprint); err != nil {
				return nil, scheduler.Permanent(err)
			}
			return existing, nil
		}
		return nil, fmt.Errorf("create Worker StatefulSet: %w", err)
	}
	return created, nil
}

func (d *Dispatcher) workerStatefulSet(
	base string,
	identity dispatch.Identity,
	fingerprint string,
	credentialSecret string,
	credentialCompilerRevision string,
) *appsv1.StatefulSet {
	name := base + "-worker"
	podLabels := labels(identity, "execution-worker")
	worker := &appsv1.StatefulSet{
		ObjectMeta: metav1.ObjectMeta{
			Name: name, Namespace: d.config.Namespace,
			Labels: labels(identity, "worker"), Annotations: annotations(fingerprint),
		},
		Spec: appsv1.StatefulSetSpec{
			ServiceName:         name,
			PodManagementPolicy: appsv1.ParallelPodManagement,
			Replicas:            ptr(d.config.WorkerReplicas),
			Selector:            &metav1.LabelSelector{MatchLabels: podLabels},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{Labels: podLabels, Annotations: annotations(fingerprint)},
				Spec: corev1.PodSpec{
					ServiceAccountName: d.config.ServiceAccount,
					SecurityContext:    podSecurityContext(),
					Containers: []corev1.Container{{
						Name:            "worker",
						Image:           d.config.WorkerImage,
						ImagePullPolicy: d.config.ImagePullPolicy,
						SecurityContext: containerSecurityContext(),
						Resources:       d.config.WorkerResources,
						Ports: []corev1.ContainerPort{{
							Name: "worker", ContainerPort: d.config.WorkerPort, Protocol: corev1.ProtocolTCP,
						}},
						ReadinessProbe: &corev1.Probe{
							ProbeHandler: corev1.ProbeHandler{TCPSocket: &corev1.TCPSocketAction{
								Port: intstr.FromString("worker"),
							}},
							InitialDelaySeconds: 3, PeriodSeconds: 3, TimeoutSeconds: 2,
						},
						Env: []corev1.EnvVar{
							{Name: "ASTRASYNC_EXECUTION_JOB_UID", Value: identity.JobUID},
							{Name: "ASTRASYNC_EXECUTION_EPOCH", Value: strconv.FormatInt(identity.Epoch, 10)},
							{Name: "ASTRASYNC_WORKER_ID", ValueFrom: &corev1.EnvVarSource{
								FieldRef: &corev1.ObjectFieldSelector{FieldPath: "metadata.name"},
							}},
							{Name: "ASTRASYNC_WORKER_PORT", Value: strconv.Itoa(int(d.config.WorkerPort))},
							{Name: "ASTRASYNC_TASK_FACTORY_PROVIDER", Value: "io.astrasync.engine.worker.JdbcWorkerTaskFactoryProvider"},
							{Name: "ASTRASYNC_WORKER_JOB_SPEC", Value: jobSpecPath},
							{Name: "ASTRASYNC_WORKER_MAX_IN_FLIGHT_BATCHES", Value: strconv.Itoa(int(d.config.WorkerMaxInFlightBatches))},
							{Name: "ASTRASYNC_MAX_CONCURRENT_TASKS", Value: strconv.Itoa(int(d.config.WorkerMaxConcurrentTasks))},
							{Name: "ASTRASYNC_TASK_QUEUE_CAPACITY", Value: strconv.Itoa(int(d.config.WorkerTaskQueueCapacity))},
							{Name: "ASTRASYNC_MAX_CONNECTIONS", Value: strconv.Itoa(int(d.config.WorkerMaxConnections))},
							{Name: "JAVA_TOOL_OPTIONS", Value: d.config.WorkerJavaToolOptions},
						},
						VolumeMounts: []corev1.VolumeMount{
							{Name: "scratch", MountPath: "/tmp"},
							{Name: "job-spec", MountPath: "/etc/astrasync/job", ReadOnly: true},
						},
					}},
					Volumes: []corev1.Volume{
						{Name: "scratch", VolumeSource: corev1.VolumeSource{EmptyDir: &corev1.EmptyDirVolumeSource{}}},
						{Name: "job-spec", VolumeSource: corev1.VolumeSource{Secret: &corev1.SecretVolumeSource{
							SecretName: base + "-spec", Items: []corev1.KeyToPath{{Key: "job.yaml", Path: "job.yaml"}},
						}}},
					},
				},
			},
		},
	}
	if credentialSecret != "" {
		container := &worker.Spec.Template.Spec.Containers[0]
		container.Env = append(container.Env, corev1.EnvVar{
			Name: "ASTRASYNC_RUNTIME_CREDENTIALS", Value: materialization.CredentialMountPath(),
		}, corev1.EnvVar{
			Name: "ASTRASYNC_EXECUTION_COMPILER_REVISION", Value: credentialCompilerRevision,
		})
		container.VolumeMounts = append(container.VolumeMounts, corev1.VolumeMount{
			Name: "runtime-credentials", MountPath: materialization.CredentialMountPath(), ReadOnly: true,
		})
		worker.Spec.Template.Spec.Volumes = append(worker.Spec.Template.Spec.Volumes, corev1.Volume{
			Name: "runtime-credentials", VolumeSource: corev1.VolumeSource{Secret: &corev1.SecretVolumeSource{
				SecretName: credentialSecret,
			}},
		})
	}
	return worker
}

func (d *Dispatcher) coordinatorJob(
	base string,
	identity dispatch.Identity,
	fingerprint string,
	credentialSecret string,
	credentialCompilerRevision string,
) *batchv1.Job {
	workerName := base + "-worker"
	workerEndpoints := make([]string, d.config.WorkerReplicas)
	for index := int32(0); index < d.config.WorkerReplicas; index++ {
		podName := workerName + "-" + strconv.Itoa(int(index))
		workerEndpoints[index] = fmt.Sprintf(
			"%s@%s.%s:%d", podName, podName, workerName, d.config.WorkerPort)
	}
	jobLabels := labels(identity, "coordinator")
	coordinator := &batchv1.Job{
		ObjectMeta: metav1.ObjectMeta{
			Name: base + "-coordinator", Namespace: d.config.Namespace,
			Labels: jobLabels, Annotations: annotations(fingerprint),
		},
		Spec: batchv1.JobSpec{
			BackoffLimit:            ptr(d.config.CoordinatorBackoffLimit),
			TTLSecondsAfterFinished: d.config.TTLSecondsAfterFinished,
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{Labels: jobLabels, Annotations: annotations(fingerprint)},
				Spec: corev1.PodSpec{
					RestartPolicy:      corev1.RestartPolicyNever,
					ServiceAccountName: d.config.ServiceAccount,
					SecurityContext:    podSecurityContext(),
					Containers: []corev1.Container{{
						Name:            "coordinator",
						Image:           d.config.CoordinatorImage,
						ImagePullPolicy: d.config.ImagePullPolicy,
						SecurityContext: containerSecurityContext(),
						Resources:       d.config.CoordinatorResources,
						Env: []corev1.EnvVar{
							{Name: "ASTRASYNC_EXECUTION_JOB_UID", Value: identity.JobUID},
							{Name: "ASTRASYNC_EXECUTION_EPOCH", Value: strconv.FormatInt(identity.Epoch, 10)},
							{Name: "ASTRASYNC_COORDINATOR_JOB_SPEC", Value: jobSpecPath},
							{Name: "ASTRASYNC_COORDINATOR_PROGRESS_DIR", Value: progressMountPath + "/" + runtimeID(identity.JobUID)},
							{Name: "ASTRASYNC_COORDINATOR_WORKERS", Value: strings.Join(workerEndpoints, ",")},
							{Name: "ASTRASYNC_COORDINATOR_WORKER_TIMEOUT_MS", Value: strconv.Itoa(int(d.config.WorkerTimeoutMillis))},
							{Name: "ASTRASYNC_COORDINATOR_MAX_IN_FLIGHT_TASKS", Value: strconv.Itoa(int(d.config.CoordinatorMaxInFlightTasks))},
							{Name: "ASTRASYNC_COORDINATOR_MAX_IN_FLIGHT_BATCHES", Value: strconv.Itoa(int(d.config.WorkerMaxInFlightBatches))},
							{Name: "ASTRASYNC_COORDINATOR_EXECUTION_EPOCH", Value: strconv.FormatInt(identity.Epoch, 10)},
							{Name: "ASTRASYNC_COORDINATOR_HEARTBEAT_URL", Value: fmt.Sprintf(
								"%s/v1/executions/%s/%d/heartbeat", d.config.HeartbeatURL, identity.JobUID, identity.Epoch,
							)},
							{Name: "ASTRASYNC_COORDINATOR_HEARTBEAT_TOKEN", ValueFrom: &corev1.EnvVarSource{
								SecretKeyRef: &corev1.SecretKeySelector{LocalObjectReference: corev1.LocalObjectReference{
									Name: base + "-spec",
								}, Key: heartbeatTokenKey},
							}},
							{Name: "ASTRASYNC_COORDINATOR_HEARTBEAT_INTERVAL_MS", Value: strconv.Itoa(int(d.config.HeartbeatIntervalMillis))},
							{Name: "JAVA_TOOL_OPTIONS", Value: d.config.CoordinatorJavaToolOptions},
						},
						VolumeMounts: []corev1.VolumeMount{
							{Name: "scratch", MountPath: "/tmp"},
							{Name: "job-spec", MountPath: "/etc/astrasync/job", ReadOnly: true},
							{Name: "progress", MountPath: progressMountPath},
						},
					}},
					Volumes: []corev1.Volume{
						{Name: "scratch", VolumeSource: corev1.VolumeSource{EmptyDir: &corev1.EmptyDirVolumeSource{}}},
						{Name: "job-spec", VolumeSource: corev1.VolumeSource{Secret: &corev1.SecretVolumeSource{
							SecretName: base + "-spec", Items: []corev1.KeyToPath{{Key: "job.yaml", Path: "job.yaml"}},
						}}},
						{Name: "progress", VolumeSource: corev1.VolumeSource{PersistentVolumeClaim: &corev1.PersistentVolumeClaimVolumeSource{
							ClaimName: d.config.ProgressClaim,
						}}},
					},
				},
			},
		},
	}
	if credentialSecret != "" {
		container := &coordinator.Spec.Template.Spec.Containers[0]
		container.Env = append(container.Env, corev1.EnvVar{
			Name: "ASTRASYNC_RUNTIME_CREDENTIALS", Value: materialization.CredentialMountPath(),
		}, corev1.EnvVar{
			Name: "ASTRASYNC_EXECUTION_COMPILER_REVISION", Value: credentialCompilerRevision,
		})
		container.VolumeMounts = append(container.VolumeMounts, corev1.VolumeMount{
			Name: "runtime-credentials", MountPath: materialization.CredentialMountPath(), ReadOnly: true,
		})
		coordinator.Spec.Template.Spec.Volumes = append(coordinator.Spec.Template.Spec.Volumes, corev1.Volume{
			Name: "runtime-credentials", VolumeSource: corev1.VolumeSource{Secret: &corev1.SecretVolumeSource{
				SecretName: credentialSecret,
			}},
		})
	}
	return coordinator
}

func observeJob(execution *batchv1.Job) (scheduler.Observation, bool) {
	for _, condition := range execution.Status.Conditions {
		if condition.Status != corev1.ConditionTrue {
			continue
		}
		switch condition.Type {
		case batchv1.JobComplete:
			return scheduler.Observation{State: scheduler.ObservationSucceeded}, true
		case batchv1.JobFailed:
			return scheduler.Observation{
				State: scheduler.ObservationFailed, Reason: condition.Reason, Message: condition.Message,
			}, true
		}
	}
	if execution.Status.Succeeded > 0 {
		return scheduler.Observation{State: scheduler.ObservationSucceeded}, true
	}
	if execution.Status.Active > 0 {
		return scheduler.Observation{State: scheduler.ObservationRunning}, false
	}
	return scheduler.Observation{State: scheduler.ObservationPending}, false
}

func (d *Dispatcher) cleanupAuxiliary(ctx context.Context, base string) error {
	if err := d.deleteAuxiliary(ctx, base, metav1.DeletePropagationBackground); err != nil {
		return err
	}
	gone, err := d.dataPlaneAuxiliaryGone(ctx, base)
	if err != nil {
		return err
	}
	if !gone {
		return fmt.Errorf("execution resources are still terminating")
	}
	return nil
}

func (d *Dispatcher) deleteAuxiliary(
	ctx context.Context, base string, deletePolicy metav1.DeletionPropagation,
) error {
	var failures []error
	if err := d.client.AppsV1().StatefulSets(d.config.Namespace).Delete(
		ctx, base+"-worker", metav1.DeleteOptions{PropagationPolicy: &deletePolicy}); err != nil && !apierrors.IsNotFound(err) {
		failures = append(failures, fmt.Errorf("delete Worker StatefulSet: %w", err))
	}
	if err := d.client.CoreV1().Services(d.config.Namespace).Delete(
		ctx, base+"-worker", metav1.DeleteOptions{}); err != nil && !apierrors.IsNotFound(err) {
		failures = append(failures, fmt.Errorf("delete Worker Service: %w", err))
	}
	if err := d.client.CoreV1().Secrets(d.config.Namespace).Delete(
		ctx, base+"-spec", metav1.DeleteOptions{}); err != nil && !apierrors.IsNotFound(err) {
		failures = append(failures, fmt.Errorf("delete JobSpec Secret: %w", err))
	}
	return errors.Join(failures...)
}

func (d *Dispatcher) resourcesGone(ctx context.Context, base string) (bool, error) {
	checks := []func() error{
		func() error {
			_, err := d.client.BatchV1().Jobs(d.config.Namespace).Get(ctx, base+"-coordinator", metav1.GetOptions{})
			return err
		},
		func() error {
			_, err := d.client.AppsV1().StatefulSets(d.config.Namespace).Get(ctx, base+"-worker", metav1.GetOptions{})
			return err
		},
		func() error {
			_, err := d.client.CoreV1().Services(d.config.Namespace).Get(ctx, base+"-worker", metav1.GetOptions{})
			return err
		},
		func() error {
			_, err := d.client.CoreV1().Secrets(d.config.Namespace).Get(ctx, base+"-spec", metav1.GetOptions{})
			return err
		},
	}
	for _, check := range checks {
		err := check()
		if err == nil {
			return false, nil
		}
		if !apierrors.IsNotFound(err) {
			return false, err
		}
	}
	return true, nil
}

func (d *Dispatcher) dataPlaneAuxiliaryGone(ctx context.Context, base string) (bool, error) {
	checks := []func() error{
		func() error {
			_, err := d.client.AppsV1().StatefulSets(d.config.Namespace).Get(
				ctx, base+"-worker", metav1.GetOptions{})
			return err
		},
		func() error {
			_, err := d.client.CoreV1().Services(d.config.Namespace).Get(
				ctx, base+"-worker", metav1.GetOptions{})
			return err
		},
		func() error {
			_, err := d.client.CoreV1().Secrets(d.config.Namespace).Get(
				ctx, base+"-spec", metav1.GetOptions{})
			return err
		},
	}
	for _, check := range checks {
		err := check()
		if err == nil {
			return false, nil
		}
		if !apierrors.IsNotFound(err) {
			return false, err
		}
	}
	return true, nil
}

func verifyMaterializedRoles(candidate job.Job, result materialization.Result) error {
	expected := make(map[materialization.Role]struct{}, 2)
	if candidate.Spec.Source.ConnectionRef != "" {
		expected[materialization.RoleSource] = struct{}{}
	}
	if candidate.Spec.Sink.ConnectionRef != "" {
		expected[materialization.RoleSink] = struct{}{}
	}
	if result.Required != (len(expected) != 0) || len(result.Roles) != len(expected) {
		return fmt.Errorf("execution Connection bindings do not match the accepted Job")
	}
	for _, role := range result.Roles {
		if _, found := expected[role]; !found {
			return fmt.Errorf("execution Connection bindings do not match the accepted Job")
		}
		delete(expected, role)
	}
	if len(expected) != 0 {
		return fmt.Errorf("execution Connection bindings do not match the accepted Job")
	}
	return nil
}

func combinedFingerprint(jobFingerprint, materializationFingerprint string) string {
	digest := sha256.Sum256([]byte(jobFingerprint + "|" + materializationFingerprint))
	return hex.EncodeToString(digest[:])
}

func jobDocument(candidate job.Job) ([]byte, string, error) {
	if err := candidate.Spec.Validate(); err != nil {
		return nil, "", err
	}
	runtimeSpec := candidate.Spec.Clone()
	runtimeSpec.Source.ConnectionRef = ""
	runtimeSpec.Sink.ConnectionRef = ""
	document := struct {
		APIVersion string      `json:"apiVersion"`
		Kind       string      `json:"kind"`
		Metadata   interface{} `json:"metadata"`
		Spec       job.Spec    `json:"spec"`
	}{
		APIVersion: "sync.astrasync.io/v1",
		Kind:       "SyncJob",
		Metadata: struct {
			Name string `json:"name"`
		}{Name: runtimeID(candidate.UID)},
		Spec: runtimeSpec,
	}
	encoded, err := json.MarshalIndent(document, "", "  ")
	if err != nil {
		return nil, "", fmt.Errorf("encode Coordinator JobSpec: %w", err)
	}
	encoded = append(encoded, '\n')
	digest := sha256.Sum256(encoded)
	return encoded, hex.EncodeToString(digest[:]), nil
}

func resourceBase(identity dispatch.Identity) (string, error) {
	if err := identity.Validate(); err != nil {
		return "", err
	}
	if _, err := uuid.Parse(identity.JobUID); err != nil {
		return "", fmt.Errorf("job UID must be a UUID")
	}
	return runtimeID(identity.JobUID) + "-e" + strconv.FormatInt(identity.Epoch, 36), nil
}

func runtimeID(jobUID string) string {
	return "job-" + strings.ReplaceAll(strings.ToLower(jobUID), "-", "")
}

func labels(identity dispatch.Identity, component string) map[string]string {
	return map[string]string{
		labelJobUID:                    identity.JobUID,
		labelEpoch:                     strconv.FormatInt(identity.Epoch, 10),
		labelComponent:                 component,
		"app.kubernetes.io/managed-by": "astrasync-scheduler",
	}
}

func annotations(fingerprint string) map[string]string {
	return map[string]string{annotationSpec: fingerprint}
}

func verifyManaged(
	labels map[string]string,
	annotations map[string]string,
	identity dispatch.Identity,
	fingerprint string,
) error {
	if labels[labelJobUID] != identity.JobUID || labels[labelEpoch] != strconv.FormatInt(identity.Epoch, 10) {
		return fmt.Errorf("Kubernetes resource name collides with another execution")
	}
	if annotations[annotationSpec] != fingerprint {
		return fmt.Errorf("Kubernetes execution JobSpec fingerprint changed")
	}
	return nil
}

func podSecurityContext() *corev1.PodSecurityContext {
	user := int64(1000)
	nonRoot := true
	return &corev1.PodSecurityContext{RunAsUser: &user, RunAsNonRoot: &nonRoot, FSGroup: &user}
}

func containerSecurityContext() *corev1.SecurityContext {
	allowPrivilegeEscalation := false
	readOnlyRootFilesystem := true
	return &corev1.SecurityContext{
		AllowPrivilegeEscalation: &allowPrivilegeEscalation,
		ReadOnlyRootFilesystem:   &readOnlyRootFilesystem,
		Capabilities:             &corev1.Capabilities{Drop: []corev1.Capability{"ALL"}},
	}
}

func ptr[T any](value T) *T {
	return &value
}
