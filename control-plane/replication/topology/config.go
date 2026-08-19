// Package topology provides region topology discovery through Kubernetes ConfigMap.
package topology

import (
	"context"
	"errors"
	"fmt"
	"os"
	"sync"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"go.uber.org/zap"
	"gopkg.in/yaml.v3"
)

// Common errors for topology operations.
var (
	ErrRegionNotFound   = errors.New("topology: region not found")
	ErrInvalidConfig   = errors.New("topology: invalid configuration")
	ErrNoPrimaryRegion = errors.New("topology: no primary region configured")
	ErrDuplicateRegion = errors.New("topology: duplicate region name")
)

// RegionRole represents the role of a region in the topology.
type RegionRole string

const (
	RegionRolePrimary RegionRole = "primary"
	RegionRoleStandby RegionRole = "standby"
)

// Region represents a single region in the multi-region topology.
type Region struct {
	Name                 string    `yaml:"name"`
	Role                RegionRole `yaml:"role"`
	APIServerEndpoint   string    `yaml:"apiServerEndpoint"`
	PostgresEndpoint    string    `yaml:"postgresEndpoint"`
	ObjectStorageBucket string    `yaml:"objectStorageBucket"`
	ObjectStoragePrefix string    `yaml:"objectStoragePrefix"`
}

// TopologyConfig represents the full topology configuration.
type TopologyConfig struct {
	Regions                    []Region `yaml:"regions"`
	ReplicationLagThresholdSec int64    `yaml:"replicationLagThresholdSec"`
}

// Loader loads region topology from Kubernetes ConfigMap.
type Loader struct {
	logger    *zap.Logger
	clientset kubernetes.Interface
	namespace string
	configMap string
	key       string

	mu      sync.RWMutex
	config  *TopologyConfig
	version int64
}

// LoaderOption is a functional option for topology loader configuration.
type LoaderOption func(*Loader)

// WithNamespace sets the Kubernetes namespace for the ConfigMap.
func WithNamespace(ns string) LoaderOption {
	return func(l *Loader) {
		l.namespace = ns
	}
}

// WithConfigMapKey sets the key within the ConfigMap that contains the topology YAML.
func WithConfigMapKey(key string) LoaderOption {
	return func(l *Loader) {
		l.key = key
	}
}

// NewLoader creates a new topology loader.
func NewLoader(ctx context.Context, logger *zap.Logger, configMap, ns, key string, opts ...LoaderOption) (*Loader, error) {
	// Default namespace from environment or "astrasync"
	if ns == "" {
		ns = os.Getenv("POD_NAMESPACE")
		if ns == "" {
			ns = "astrasync"
		}
	}

	// Default key
	if key == "" {
		key = "regions.yaml"
	}

	l := &Loader{
		logger:    logger.With(zap.String("configMap", configMap), zap.String("namespace", ns)),
		namespace: ns,
		configMap: configMap,
		key:       key,
	}

	for _, opt := range opts {
		opt(l)
	}

	// Try to create Kubernetes client if not in-cluster
	config, err := rest.InClusterConfig()
	if err != nil {
		logger.Info("not in cluster, will use file-based loading", zap.Error(err))
		l.clientset = nil
	} else {
		l.clientset, err = kubernetes.NewForConfig(config)
		if err != nil {
			return nil, fmt.Errorf("create Kubernetes client: %w", err)
		}
	}

	// Load initial config
	if err := l.load(ctx); err != nil {
		return nil, fmt.Errorf("load initial topology: %w", err)
	}

	return l, nil
}

// NewFileLoader creates a loader that reads from a local file (for testing).
func NewFileLoader(ctx context.Context, logger *zap.Logger, path string) (*Loader, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read topology file: %w", err)
	}

	return parseTopology(logger, data, 1)
}

// parseTopology parses YAML configuration into TopologyConfig.
func parseTopology(logger *zap.Logger, data []byte, version int64) (*Loader, error) {
	var cfg TopologyConfig
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("%w: %v", ErrInvalidConfig, err)
	}

	// Validate regions
	if len(cfg.Regions) == 0 {
		return nil, errors.New("topology: at least one region is required")
	}

	seenNames := make(map[string]bool)
	for _, r := range cfg.Regions {
		if seenNames[r.Name] {
			return nil, fmt.Errorf("%w: %q", ErrDuplicateRegion, r.Name)
		}
		seenNames[r.Name] = true

		if r.Role != RegionRolePrimary && r.Role != RegionRoleStandby {
			logger.Warn("unknown region role, treating as standby",
				zap.String("region", r.Name),
				zap.String("role", string(r.Role)))
		}
	}

	// Set defaults
	if cfg.ReplicationLagThresholdSec == 0 {
		cfg.ReplicationLagThresholdSec = 5
	}

	l := &Loader{
		logger:  logger.With(zap.String("source", "file")),
		config:  &cfg,
		version: version,
	}

	logger.Info("loaded topology",
		zap.Int("regionCount", len(cfg.Regions)),
		zap.Int64("replicationLagThreshold", cfg.ReplicationLagThresholdSec))

	return l, nil
}

// load reloads the topology from the ConfigMap.
func (l *Loader) load(ctx context.Context) error {
	if l.clientset == nil {
		return nil // No Kubernetes client, file-based loading only
	}

	cm, err := l.clientset.CoreV1().ConfigMaps(l.namespace).Get(ctx, l.configMap, metav1.GetOptions{})
	if err != nil {
		return fmt.Errorf("get ConfigMap: %w", err)
	}

	data, ok := cm.Data[l.key]
	if !ok {
		return fmt.Errorf("key %q not found in ConfigMap", l.key)
	}

	newVersion := cm.ResourceVersion

	l.mu.Lock()
	defer l.mu.Unlock()

	if newVersion == l.versionStringLocked() {
		return nil // No change
	}

	newLoader, err := parseTopology(l.logger, []byte(data), 0)
	if err != nil {
		return err
	}

	l.config = newLoader.config
	l.version = newLoader.version

	l.logger.Info("reloaded topology",
		zap.Int("regionCount", len(l.config.Regions)),
		zap.String("version", newVersion))

	return nil
}

// versionStringLocked returns the current version string. Caller must hold the lock.
func (l *Loader) versionStringLocked() string {
	if l.version == 0 {
		return ""
	}
	return fmt.Sprintf("%d", l.version)
}

// GetRegion returns a region by name.
func (l *Loader) GetRegion(name string) (*Region, error) {
	l.mu.RLock()
	defer l.mu.RUnlock()

	for _, r := range l.config.Regions {
		if r.Name == name {
			return &r, nil
		}
	}
	return nil, fmt.Errorf("%w: %q", ErrRegionNotFound, name)
}

// GetPrimaryRegion returns the primary region.
func (l *Loader) GetPrimaryRegion() (*Region, error) {
	l.mu.RLock()
	defer l.mu.RUnlock()

	for _, r := range l.config.Regions {
		if r.Role == RegionRolePrimary {
			return &r, nil
		}
	}
	return nil, ErrNoPrimaryRegion
}

// GetStandbyRegions returns all standby regions.
func (l *Loader) GetStandbyRegions() []Region {
	l.mu.RLock()
	defer l.mu.RUnlock()

	var standbys []Region
	for _, r := range l.config.Regions {
		if r.Role == RegionRoleStandby {
			standbys = append(standbys, r)
		}
	}
	return standbys
}

// GetAllRegions returns all regions.
func (l *Loader) GetAllRegions() []Region {
	l.mu.RLock()
	defer l.mu.RUnlock()

	regions := make([]Region, len(l.config.Regions))
	copy(regions, l.config.Regions)
	return regions
}

// GetConfig returns the full topology configuration.
func (l *Loader) GetConfig() *TopologyConfig {
	l.mu.RLock()
	defer l.mu.RUnlock()

	cfg := &TopologyConfig{
		Regions: make([]Region, len(l.config.Regions)),
		ReplicationLagThresholdSec: l.config.ReplicationLagThresholdSec,
	}
	copy(cfg.Regions, l.config.Regions)
	return cfg
}

// ReplicationLagThreshold returns the configured replication lag threshold.
func (l *Loader) ReplicationLagThreshold() int64 {
	l.mu.RLock()
	defer l.mu.RUnlock()
	return l.config.ReplicationLagThresholdSec
}

// Reload triggers a manual reload of the topology.
func (l *Loader) Reload(ctx context.Context) error {
	return l.load(ctx)
}

// GetRegionNames returns the names of all regions.
func (l *Loader) GetRegionNames() []string {
	l.mu.RLock()
	defer l.mu.RUnlock()

	names := make([]string, len(l.config.Regions))
	for i, r := range l.config.Regions {
		names[i] = r.Name
	}
	return names
}

// IsPrimary returns true if the given region name is the primary region.
func (l *Loader) IsPrimary(regionName string) bool {
	l.mu.RLock()
	defer l.mu.RUnlock()

	for _, r := range l.config.Regions {
		if r.Name == regionName && r.Role == RegionRolePrimary {
			return true
		}
	}
	return false
}

// IsStandby returns true if the given region name is a standby region.
func (l *Loader) IsStandby(regionName string) bool {
	l.mu.RLock()
	defer l.mu.RUnlock()

	for _, r := range l.config.Regions {
		if r.Name == regionName && r.Role == RegionRoleStandby {
			return true
		}
	}
	return false
}

// TopologyWatcher watches for topology changes and calls the callback on updates.
type TopologyWatcher struct {
	loader   *Loader
	logger   *zap.Logger
	interval int64 // seconds between polls
	stopCh   chan struct{}
	doneCh   chan struct{}
	onChange func(*TopologyConfig)
}

// Watch starts watching for topology changes.
func (w *TopologyWatcher) Watch(ctx context.Context, onChange func(*TopologyConfig)) {
	w.onChange = onChange
	w.stopCh = make(chan struct{})
	w.doneCh = make(chan struct{})

	go w.run(ctx)
}

// run is the main watcher loop.
func (w *TopologyWatcher) run(ctx context.Context) {
	defer close(w.doneCh)

	ticker := time.NewTicker(time.Duration(w.interval) * time.Second)
	defer ticker.Stop()

	// Initial callback
	if w.onChange != nil {
		w.onChange(w.loader.GetConfig())
	}

	for {
		select {
		case <-ctx.Done():
			return
		case <-w.stopCh:
			return
		case <-ticker.C:
			if err := w.loader.Reload(ctx); err != nil {
				w.logger.Warn("failed to reload topology", zap.Error(err))
				continue
			}
			if w.onChange != nil {
				w.onChange(w.loader.GetConfig())
			}
		}
	}
}

// Stop stops the watcher.
func (w *TopologyWatcher) Stop() {
	close(w.stopCh)
	<-w.doneCh
}

// NewWatcher creates a new topology watcher.
func NewWatcher(loader *Loader, logger *zap.Logger, intervalSec int64) *TopologyWatcher {
	if intervalSec <= 0 {
		intervalSec = 30
	}
	return &TopologyWatcher{
		loader:   loader,
		logger:   logger,
		interval: intervalSec,
	}
}
