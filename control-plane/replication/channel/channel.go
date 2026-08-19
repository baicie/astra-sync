// Package channel provides cross-region gRPC channel with mutual TLS.
package channel

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"os"
	"sync"
	"sync/atomic"
	"time"

	"go.uber.org/zap"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/status"
)

// Common errors for channel operations.
var (
	ErrClosed             = errors.New("channel: client is closed")
	ErrNoPeerEndpoint     = errors.New("channel: no peer endpoint configured")
	ErrTLSConfigMissing    = errors.New("channel: TLS configuration incomplete")
	ErrCertLoadFailed      = errors.New("channel: failed to load certificates")
	ErrCAVerificationFailed = errors.New("channel: CA verification failed")
)

// EventType defines the type of cross-region event.
type EventType int

const (
	EventTypeUnknown EventType = iota
	EventTypeCheckpoint
	EventTypeTopology
	EventTypeHealth
)

// String returns the string representation of the event type.
func (e EventType) String() string {
	switch e {
	case EventTypeCheckpoint:
		return "checkpoint"
	case EventTypeTopology:
		return "topology"
	case EventTypeHealth:
		return "health"
	default:
		return "unknown"
	}
}

// Event represents a cross-region event.
type Event struct {
	Type        EventType
	SourceRegion string
	TargetRegion string
	Timestamp   time.Time
	Payload     []byte
}

// Config holds the configuration for a cross-region channel.
type Config struct {
	Region         string
	PeerRegion     string
	PeerEndpoint   string
	CACertPath     string
	ClientCertPath string
	ClientKeyPath  string
	ServerName     string
	EnableTLS      bool
}

// Option is a functional option for channel configuration.
type Option func(*Config)

// WithTLS enables mutual TLS.
func WithTLS(caCertPath, clientCertPath, clientKeyPath, serverName string) Option {
	return func(c *Config) {
		c.EnableTLS = true
		c.CACertPath = caCertPath
		c.ClientCertPath = clientCertPath
		c.ClientKeyPath = clientKeyPath
		c.ServerName = serverName
	}
}

// WithPeerEndpoint sets the peer region endpoint.
func WithPeerEndpoint(endpoint string) Option {
	return func(c *Config) {
		c.PeerEndpoint = endpoint
	}
}

// EventHandler handles incoming events from the cross-region channel.
type EventHandler interface {
	HandleEvent(ctx context.Context, event *Event) error
}

// EventHandlerFunc is a function adapter for EventHandler.
type EventHandlerFunc func(ctx context.Context, event *Event) error

// HandleEvent calls the underlying function.
func (f EventHandlerFunc) HandleEvent(ctx context.Context, event *Event) error {
	return f(ctx, event)
}

// Client manages the cross-region gRPC connection.
type Client struct {
	cfg     Config
	logger  *zap.Logger
	handler EventHandler

	mu          sync.RWMutex
	conn        *grpc.ClientConn
	closed      atomic.Bool
	reconnectCh chan struct{}
}

// NewClient creates a new cross-region channel client.
func NewClient(ctx context.Context, logger *zap.Logger, region string, handler EventHandler, opts ...Option) (*Client, error) {
	cfg := Config{
		Region: region,
	}

	for _, opt := range opts {
		opt(&cfg)
	}

	if cfg.PeerEndpoint == "" {
		return nil, ErrNoPeerEndpoint
	}

	c := &Client{
		cfg:         cfg,
		logger:      logger.With(zap.String("region", region), zap.String("peer", cfg.PeerRegion)),
		handler:     handler,
		reconnectCh: make(chan struct{}, 1),
	}

	return c, nil
}

// Connect establishes the gRPC connection to the peer region.
func (c *Client) Connect(ctx context.Context) error {
	return c.dial(ctx)
}

// Close closes the channel client.
func (c *Client) Close() error {
	if c.closed.Swap(true) {
		return nil
	}

	c.mu.Lock()
	conn := c.conn
	c.conn = nil
	c.mu.Unlock()

	if conn != nil {
		if err := conn.Close(); err != nil {
			c.logger.Warn("failed to close gRPC connection", zap.Error(err))
		}
	}

	return nil
}

// SendEvent sends an event to the peer region.
// Note: This is a placeholder for the actual gRPC stream implementation.
func (c *Client) SendEvent(ctx context.Context, event *Event) error {
	if c.closed.Load() {
		return ErrClosed
	}

	c.logger.Debug("event queued for transmission",
		zap.String("type", event.Type.String()),
		zap.String("source", event.SourceRegion),
		zap.String("target", event.TargetRegion))

	return nil
}

// dial establishes the underlying gRPC connection.
func (c *Client) dial(ctx context.Context) error {
	var opts []grpc.DialOption

	if c.cfg.EnableTLS {
		tlsConfig, err := c.loadTLSConfig()
		if err != nil {
			return fmt.Errorf("load TLS config: %w", err)
		}
		opts = append(opts, grpc.WithTransportCredentials(credentials.NewTLS(tlsConfig)))
	} else {
		opts = append(opts, grpc.WithTransportCredentials(insecure.NewCredentials()))
	}

	conn, err := grpc.DialContext(ctx, c.cfg.PeerEndpoint, opts...)
	if err != nil {
		return fmt.Errorf("dial %s: %w", c.cfg.PeerEndpoint, err)
	}

	c.mu.Lock()
	c.conn = conn
	c.mu.Unlock()

	c.logger.Info("connected to peer region", zap.String("endpoint", c.cfg.PeerEndpoint))
	return nil
}

// loadTLSConfig loads the mTLS configuration from disk.
func (c *Client) loadTLSConfig() (*tls.Config, error) {
	if c.cfg.CACertPath == "" || c.cfg.ClientCertPath == "" || c.cfg.ClientKeyPath == "" {
		return nil, ErrTLSConfigMissing
	}

	// Load CA cert
	caCert, err := os.ReadFile(c.cfg.CACertPath)
	if err != nil {
		return nil, fmt.Errorf("%w: CA cert: %v", ErrCertLoadFailed, err)
	}

	caPool := x509.NewCertPool()
	if !caPool.AppendCertsFromPEM(caCert) {
		return nil, ErrCAVerificationFailed
	}

	// Load client cert
	clientCert, err := tls.LoadX509KeyPair(c.cfg.ClientCertPath, c.cfg.ClientKeyPath)
	if err != nil {
		return nil, fmt.Errorf("%w: client keypair: %v", ErrCertLoadFailed, err)
	}

	return &tls.Config{
		RootCAs:      caPool,
		Certificates: []tls.Certificate{clientCert},
		ServerName:   c.cfg.ServerName,
		MinVersion:   tls.VersionTLS13,
	}, nil
}

// IsConnected returns true if the client is connected.
func (c *Client) IsConnected() bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.conn != nil
}

// StatusCode extracts the gRPC status code from an error.
func StatusCode(err error) uint32 {
	if s, ok := status.FromError(err); ok {
		return uint32(s.Code())
	}
	return 0
}

// Server accepts incoming cross-region events.
type Server struct {
	cfg     Config
	logger  *zap.Logger
	handler EventHandler

	mu     sync.RWMutex
	streams map[string]chan *Event
}

// NewServer creates a new cross-region channel server.
func NewServer(logger *zap.Logger, region string, handler EventHandler) *Server {
	return &Server{
		cfg:     Config{Region: region},
		logger:  logger.With(zap.String("region", region)),
		handler: handler,
		streams: make(map[string]chan *Event),
	}
}

// NewTLSServer creates a new cross-region channel server with TLS.
func NewTLSServer(logger *zap.Logger, region string, handler EventHandler, caCertPath, serverCertPath, serverKeyPath string) (*Server, error) {
	s := NewServer(logger, region, handler)
	s.cfg.CACertPath = caCertPath
	s.cfg.ClientCertPath = serverCertPath
	s.cfg.ClientKeyPath = serverKeyPath
	s.cfg.EnableTLS = true
	return s, nil
}

// HandleEvent handles an incoming event from the channel.
func (s *Server) HandleEvent(ctx context.Context, event *Event) error {
	if event == nil {
		return errors.New("nil event")
	}

	if event.TargetRegion != "" && event.TargetRegion != s.cfg.Region && event.TargetRegion != "*" {
		// Not for this region, drop
		return nil
	}

	s.logger.Debug("received event",
		zap.String("type", event.Type.String()),
		zap.String("source", event.SourceRegion),
		zap.String("target", event.TargetRegion))

	if s.handler != nil {
		return s.handler.HandleEvent(ctx, event)
	}

	return nil
}

// RegisterStream registers an event stream from a peer region.
func (s *Server) RegisterStream(peerRegion string, events <-chan *Event) {
	s.mu.Lock()
	defer s.mu.Unlock()

	ch := make(chan *Event, 100)
	s.streams[peerRegion] = ch

	go func() {
		for event := range events {
			select {
			case ch <- event:
			default:
				s.logger.Warn("dropping event, channel full",
					zap.String("peer", peerRegion))
			}
		}
	}()
}

// UnregisterStream removes an event stream for a peer region.
func (s *Server) UnregisterStream(peerRegion string) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if ch, ok := s.streams[peerRegion]; ok {
		close(ch)
		delete(s.streams, peerRegion)
	}
}

// ChannelStats holds statistics for a channel.
type ChannelStats struct {
	EventsSent     int64
	EventsReceived int64
	Connected      bool
	LastError      string
}

// Stats returns the current channel statistics.
func (c *Client) Stats() ChannelStats {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return ChannelStats{
		Connected: c.conn != nil,
	}
}

// NewHealthEvent creates a health event.
func NewHealthEvent(sourceRegion, targetRegion string, healthy bool) *Event {
	payload := []byte{}
	if healthy {
		payload = []byte("healthy")
	} else {
		payload = []byte("unhealthy")
	}

	return &Event{
		Type:         EventTypeHealth,
		SourceRegion: sourceRegion,
		TargetRegion: targetRegion,
		Timestamp:    time.Now().UTC(),
		Payload:      payload,
	}
}

// NewCheckpointEvent creates a checkpoint event.
func NewCheckpointEvent(sourceRegion, targetRegion, checkpointURI string, epoch int64) *Event {
	return &Event{
		Type:         EventTypeCheckpoint,
		SourceRegion: sourceRegion,
		TargetRegion: targetRegion,
		Timestamp:    time.Now().UTC(),
		Payload:      []byte(fmt.Sprintf("checkpoint:%s:epoch=%d", checkpointURI, epoch)),
	}
}

// NewTopologyEvent creates a topology event.
func NewTopologyEvent(sourceRegion, targetRegion string, version int64) *Event {
	return &Event{
		Type:         EventTypeTopology,
		SourceRegion: sourceRegion,
		TargetRegion: targetRegion,
		Timestamp:    time.Now().UTC(),
		Payload:      []byte(fmt.Sprintf("topology-version:%d", version)),
	}
}