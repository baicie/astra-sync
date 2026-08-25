// Package runtime assembles the long-lived replication components for one process.
package runtime

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"

	"go.uber.org/zap"
	"google.golang.org/grpc"

	"io.astrasync/control-plane/replication/channel"
	"io.astrasync/control-plane/replication/metrics"
	"io.astrasync/control-plane/replication/promotion"
	"io.astrasync/control-plane/replication/recovery"
)

var (
	// ErrInvalidDependencies indicates that a required runtime dependency is missing.
	ErrInvalidDependencies = errors.New("replication runtime: invalid dependencies")
	// ErrClosed indicates that the runtime has already been closed.
	ErrClosed = errors.New("replication runtime: closed")
)

// Config controls the region identity and optional component behavior.
type Config struct {
	Region                string
	PeerRegion            string
	PeerEndpoint          string
	CACertPath            string
	ClientCertPath        string
	ClientKeyPath         string
	ServerName            string
	EnableTLS             bool
	Metrics               *metrics.Bundle
	ChannelOpts           []channel.Option
	PromotionOpts         []promotion.Option
	RecoveryOpts          []recovery.Option
	ReplicatorConfig      ReplicatorConfig
	ReplicationResumeFrom int64
	ProgressStore         ProgressStore
}

// Dependencies contains the deployment-owned implementations used by runtime components.
type Dependencies struct {
	EventHandler       channel.EventHandler
	EventSender        channel.EventSender
	EventSenderFactory channel.EventSenderFactory

	PromotionStore promotion.PromotionStore
	EpochAssigner  promotion.EpochAssigner
	JobReader      promotion.JobReader
	JobWriter      promotion.JobWriter
	Revalidator    promotion.CapabilityRevalidator
	Recovery       promotion.RecoveryCoordinator
	RemoteRecovery RemoteRecovery
	Fencer         promotion.EpochFencer
	Topology       promotion.RegionTopology

	WALEntryReader recovery.WALEntryReader
	ObjectStorage  recovery.ObjectStorage
	ManifestParser recovery.ManifestParser
	Validator      recovery.Validator
	StateRestorer  recovery.StateRestorer
	Auditor        recovery.AuditLogger
	EventEncoder   EventEncoder
	ProgressStore  ProgressStore
}

// RemoteRecovery invokes checkpoint recovery on a promoted target region.
type RemoteRecovery interface {
	Recover(context.Context, string, string, string, int64, string) error
}

type recoveryCoordinator struct {
	manager *recovery.Manager
}

type targetRecoveryCoordinator struct {
	client RemoteRecovery
	source string
	target string
}

func (c targetRecoveryCoordinator) Recover(ctx context.Context, jobID string, newEpoch int64, promotionID string) error {
	return c.client.Recover(ctx, jobID, c.source, c.target, newEpoch, promotionID)
}

func (c recoveryCoordinator) Recover(ctx context.Context, jobID string, newEpoch int64, _ string) error {
	result, err := c.manager.Recover(ctx, jobID, newEpoch)
	if err != nil {
		return err
	}
	if result == nil || !result.IsComplete() || result.IsFailed() {
		return fmt.Errorf("recovery did not complete")
	}
	return nil
}

// Runtime owns the replication components and their lifecycle.
type Runtime struct {
	metrics     *metrics.Bundle
	channel     *channel.Client
	promotion   *promotion.Manager
	recovery    *recovery.Manager
	replicator  *Replicator
	resumeStart int64
	region      string
	peerRegion  string
	progress    ProgressStore
	state       atomic.Value

	mu               sync.Mutex
	started          bool
	starting         bool
	closed           bool
	startDone        chan struct{}
	lifecycle        context.Context
	cancel           context.CancelFunc
	watcherDone      chan struct{}
	replicationDone  chan struct{}
	replicationStart bool
}

// New validates deployment dependencies and assembles all replication components.
func New(logger *zap.Logger, cfg Config, deps Dependencies) (*Runtime, error) {
	if logger == nil || cfg.Metrics == nil || cfg.Region == "" || cfg.PeerRegion == "" || cfg.PeerEndpoint == "" {
		return nil, fmt.Errorf("%w: logger, metrics, region, peer region, and peer endpoint are required", ErrInvalidDependencies)
	}
	if deps.EventHandler == nil || (deps.EventSender == nil && deps.EventSenderFactory == nil) || deps.PromotionStore == nil || deps.EpochAssigner == nil || deps.JobReader == nil || deps.JobWriter == nil {
		return nil, fmt.Errorf("%w: channel and promotion dependencies are required", ErrInvalidDependencies)
	}
	if deps.WALEntryReader == nil || deps.ObjectStorage == nil || deps.ManifestParser == nil || deps.Validator == nil || deps.StateRestorer == nil {
		return nil, fmt.Errorf("%w: recovery dependencies are required", ErrInvalidDependencies)
	}
	progress := cfg.ProgressStore
	if progress == nil {
		progress = deps.ProgressStore
	}

	channelOpts := append([]channel.Option{}, cfg.ChannelOpts...)
	channelOpts = append(channelOpts,
		channel.WithPeerEndpoint(cfg.PeerEndpoint),
		channel.WithEventSender(deps.EventSender),
		channel.WithEventSenderFactory(deps.EventSenderFactory),
		channel.WithMetrics(cfg.Metrics.Recorder),
	)
	if cfg.EnableTLS {
		channelOpts = append(channelOpts, channel.WithTLS(cfg.CACertPath, cfg.ClientCertPath, cfg.ClientKeyPath, cfg.ServerName))
	}
	ch, err := channel.NewClient(context.Background(), logger, cfg.Region, deps.EventHandler, channelOpts...)
	if err != nil {
		return nil, fmt.Errorf("create replication channel: %w", err)
	}

	recoveryOpts := append([]recovery.Option{}, cfg.RecoveryOpts...)
	recoveryOpts = append(recoveryOpts,
		recovery.WithTargetRegion(cfg.Region),
		recovery.WithMetrics(cfg.Metrics.Recorder),
	)
	rm := recovery.NewManager(logger, deps.WALEntryReader, deps.ObjectStorage, deps.ManifestParser, deps.Validator, deps.StateRestorer, deps.Auditor, recoveryOpts...)

	promotionOpts := append([]promotion.Option{}, cfg.PromotionOpts...)
	promotionOpts = append(promotionOpts,
		promotion.WithCurrentRegion(cfg.Region),
		promotion.WithMetrics(cfg.Metrics.Recorder),
	)
	if deps.Revalidator != nil {
		promotionOpts = append(promotionOpts, promotion.WithCapabilityRevalidator(deps.Revalidator))
	}
	if deps.Recovery != nil {
		promotionOpts = append(promotionOpts, promotion.WithRecoveryCoordinator(deps.Recovery))
	} else if deps.RemoteRecovery != nil {
		promotionOpts = append(promotionOpts, promotion.WithRecoveryCoordinator(targetRecoveryCoordinator{client: deps.RemoteRecovery, source: cfg.Region, target: cfg.PeerRegion}))
	} else {
		promotionOpts = append(promotionOpts, promotion.WithRecoveryCoordinator(recoveryCoordinator{manager: rm}))
	}
	if deps.Fencer != nil {
		promotionOpts = append(promotionOpts, promotion.WithEpochFencer(deps.Fencer))
	}
	if deps.Topology != nil {
		promotionOpts = append(promotionOpts, promotion.WithTopology(deps.Topology))
	}
	pm, err := promotion.NewManager(logger, deps.PromotionStore, deps.EpochAssigner, deps.JobReader, deps.JobWriter, promotionOpts...)
	if err != nil {
		_ = ch.Close()
		return nil, fmt.Errorf("create promotion manager: %w", err)
	}

	var replicator *Replicator
	if deps.EventEncoder != nil {
		r, replicatorErr := NewReplicator(logger, deps.WALEntryReader, ch, deps.EventEncoder, cfg.ReplicatorConfig)
		if replicatorErr != nil {
			_ = ch.Close()
			return nil, fmt.Errorf("create replication replicator: %w", replicatorErr)
		}
		replicator = r
	}
	lifecycle, cancel := context.WithCancel(context.Background())
	runtime := &Runtime{
		metrics: cfg.Metrics, channel: ch, promotion: pm, recovery: rm, replicator: replicator,
		resumeStart: cfg.ReplicationResumeFrom, region: cfg.Region, peerRegion: cfg.PeerRegion, progress: progress,
		lifecycle: lifecycle, cancel: cancel,
		watcherDone: make(chan struct{}),
	}
	runtime.replicationDone = make(chan struct{})
	if runtime.replicator != nil && progress != nil {
		runtime.replicator.SetAcknowledgeCallback(func(ctx context.Context, sequence int64) error {
			return progress.SaveReplicationProgress(ctx, cfg.Region, cfg.PeerRegion, sequence)
		})
	}
	if runtime.replicator == nil {
		close(runtime.replicationDone)
	}
	runtime.setState(StateCreated)
	go runtime.watchCancellation()
	return runtime, nil
}

// Connection returns the active peer connection owned by the replication channel.
// Callers must not close the returned connection.
func (r *Runtime) Connection() *grpc.ClientConn {
	return r.channel.Connection()
}

func (r *Runtime) watchCancellation() {
	<-r.lifecycle.Done()
	_ = r.channel.Close()
	close(r.watcherDone)
}

// Start connects the cross-region channel. Concurrent callers share one attempt.
func (r *Runtime) Start(ctx context.Context) error {
	r.mu.Lock()
	if r.closed {
		r.mu.Unlock()
		return ErrClosed
	}
	if r.started {
		r.mu.Unlock()
		return nil
	}
	if r.starting {
		done := r.startDone
		r.mu.Unlock()
		select {
		case <-done:
			return r.Start(ctx)
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	r.starting = true
	r.setState(StateStarting)
	r.startDone = make(chan struct{})
	done := r.startDone
	r.mu.Unlock()

	connectCtx, cancelConnect := context.WithCancel(r.lifecycle)
	stopCaller := context.AfterFunc(ctx, cancelConnect)
	err := r.channel.Connect(connectCtx)
	stopCaller()
	cancelConnect()

	r.mu.Lock()
	r.starting = false
	closed := r.closed
	if err == nil && !closed {
		r.started = true
		if r.replicator != nil {
			resumeFrom := r.resumeFrom()
			if r.progress != nil {
				stored, progressErr := r.progress.LoadReplicationProgress(r.lifecycle, r.region, r.peerRegion)
				if progressErr != nil {
					r.started = false
					err = fmt.Errorf("load replication progress: %w", progressErr)
				} else if stored > resumeFrom {
					resumeFrom = stored
				}
			}
			if err == nil {
				r.replicationStart = true
				go r.runReplicator(resumeFrom)
			}
		} else {
			r.replicationStart = true
		}
	}
	close(done)
	r.mu.Unlock()
	if err != nil {
		r.setState(StateFailed)
		return fmt.Errorf("connect replication channel: %w", err)
	}
	if closed {
		r.setState(StateClosed)
		_ = r.channel.Close()
		return ErrClosed
	}
	if !r.started {
		r.setState(StateFailed)
		return err
	}
	r.setState(StateRunning)
	return nil
}

// Close releases runtime resources. Calling Close more than once is safe.
func (r *Runtime) Close() error {
	r.mu.Lock()
	if r.closed {
		r.mu.Unlock()
		return nil
	}
	r.closed = true
	r.setState(StateClosed)
	r.cancel()
	r.mu.Unlock()
	<-r.watcherDone
	r.mu.Lock()
	replicationStarted := r.replicationStart
	r.mu.Unlock()
	if replicationStarted {
		<-r.replicationDone
	}
	return nil
}

func (r *Runtime) runReplicator(resumeFrom int64) {
	defer close(r.replicationDone)
	if err := r.replicator.Run(r.lifecycle, resumeFrom); err != nil && !errors.Is(err, context.Canceled) {
		r.mu.Lock()
		closed := r.closed
		r.mu.Unlock()
		if !closed {
			r.setState(StateFailed)
		}
	}
}

func (r *Runtime) resumeFrom() int64 {
	return r.resumeStart
}

// Metrics returns the shared metrics bundle.
func (r *Runtime) Metrics() *metrics.Bundle { return r.metrics }

// Channel returns the cross-region channel client.
func (r *Runtime) Channel() *channel.Client { return r.channel }

// Promotion returns the promotion manager.
func (r *Runtime) Promotion() *promotion.Manager { return r.promotion }

// Recovery returns the recovery manager.
func (r *Runtime) Recovery() *recovery.Manager { return r.recovery }
