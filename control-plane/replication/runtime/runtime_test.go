package runtime_test

import (
	"context"
	"errors"
	"io"
	"net"
	"testing"

	"go.uber.org/zap"
	"google.golang.org/grpc"

	"io.astrasync/control-plane/replication/channel"
	replicationmetrics "io.astrasync/control-plane/replication/metrics"
	"io.astrasync/control-plane/replication/promotion"
	"io.astrasync/control-plane/replication/recovery"
	runtimepkg "io.astrasync/control-plane/replication/runtime"
)

func TestNewRejectsIncompleteDependencies(t *testing.T) {
	_, err := runtimepkg.New(zap.NewNop(), runtimepkg.Config{Region: "us-east-1", PeerRegion: "eu-west-1", PeerEndpoint: "localhost:1"}, runtimepkg.Dependencies{})
	if !errors.Is(err, runtimepkg.ErrInvalidDependencies) {
		t.Fatalf("New() error = %v, want ErrInvalidDependencies", err)
	}
}

func TestNewSharesMetricsWithAllComponents(t *testing.T) {
	bundle, err := replicationmetrics.NewBundle()
	if err != nil {
		t.Fatalf("create metrics bundle: %v", err)
	}
	deps := testDependencies()
	rt, err := runtimepkg.New(zap.NewNop(), runtimepkg.Config{
		Region: "us-east-1", PeerRegion: "eu-west-1", PeerEndpoint: "localhost:1", Metrics: bundle,
	}, deps)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	if rt.Metrics() != bundle || rt.Channel() == nil || rt.Promotion() == nil || rt.Recovery() == nil {
		t.Fatal("runtime did not create all components with the shared metrics bundle")
	}
}

func TestStartReturnsClosedWhenRuntimeClosesDuringConnect(t *testing.T) {
	bundle, err := replicationmetrics.NewBundle()
	if err != nil {
		t.Fatalf("create metrics bundle: %v", err)
	}
	deps := testDependencies()
	rt, err := runtimepkg.New(zap.NewNop(), runtimepkg.Config{
		Region: "us-east-1", PeerRegion: "eu-west-1", PeerEndpoint: "passthrough:///peer", Metrics: bundle,
	}, deps)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	if err := rt.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}
	if err := rt.Start(context.Background()); !errors.Is(err, runtimepkg.ErrClosed) {
		t.Fatalf("Start() error = %v, want ErrClosed", err)
	}
}

func TestStartAndCloseAreIdempotent(t *testing.T) {
	bundle, err := replicationmetrics.NewBundle()
	if err != nil {
		t.Fatalf("create metrics bundle: %v", err)
	}
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	grpcServer := grpc.NewServer()
	serveDone := make(chan struct{})
	go func() {
		defer close(serveDone)
		_ = grpcServer.Serve(listener)
	}()
	t.Cleanup(func() {
		grpcServer.Stop()
		_ = listener.Close()
		<-serveDone
	})
	rt, err := runtimepkg.New(zap.NewNop(), runtimepkg.Config{
		Region: "us-east-1", PeerRegion: "eu-west-1", PeerEndpoint: listener.Addr().String(), Metrics: bundle,
	}, testDependencies())
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	if err := rt.Start(context.Background()); err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	if err := rt.Start(context.Background()); err != nil {
		t.Fatalf("second Start() error = %v", err)
	}
	if err := rt.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}
	if err := rt.Close(); err != nil {
		t.Fatalf("second Close() error = %v", err)
	}
}

type testEventHandler struct{}

func (testEventHandler) HandleEvent(context.Context, *channel.Event) error { return nil }

type testEventSender struct{}

func (testEventSender) SendEvent(context.Context, *channel.Event) error { return nil }

type testPromotionStore struct{}

func (testPromotionStore) Get(context.Context, string, string) (*promotion.Promotion, error) {
	return nil, nil
}
func (testPromotionStore) Create(context.Context, *promotion.Promotion) error { return nil }
func (testPromotionStore) Update(context.Context, *promotion.Promotion) error { return nil }
func (testPromotionStore) GetLatest(context.Context, string) (*promotion.Promotion, error) {
	return nil, nil
}

type testPromotionDeps struct{}

func (testPromotionDeps) Assign(context.Context, string) (int64, error)           { return 1, nil }
func (testPromotionDeps) GetEpoch(context.Context, string) (int64, error)         { return 0, nil }
func (testPromotionDeps) UpdateEpoch(context.Context, string, int64, int64) error { return nil }

type testWALReader struct{}

func (testWALReader) ReadEntries(context.Context, int64) ([]*recovery.WALEntry, error) {
	return nil, nil
}
func (testWALReader) GetLatestCheckpoint(context.Context) (*recovery.CheckpointManifest, error) {
	return nil, recovery.ErrCheckpointNotFound
}

type testStorage struct{}

func (testStorage) GetObject(context.Context, string) ([]byte, error)              { return nil, nil }
func (testStorage) GetObjectReader(context.Context, string) (io.ReadCloser, error) { return nil, nil }

type testParser struct{}

func (testParser) Parse([]byte) (*recovery.CheckpointManifest, error) { return nil, nil }

type testValidator struct{}

func (testValidator) Validate(context.Context, *recovery.CheckpointManifest) error { return nil }

type testRestorer struct{}

func (testRestorer) Restore(context.Context, *recovery.CheckpointManifest) error { return nil }

func testDependencies() runtimepkg.Dependencies {
	promotionDeps := testPromotionDeps{}
	return runtimepkg.Dependencies{
		EventHandler:   testEventHandler{},
		EventSender:    testEventSender{},
		PromotionStore: testPromotionStore{},
		EpochAssigner:  promotionDeps,
		JobReader:      promotionDeps,
		JobWriter:      promotionDeps,
		WALEntryReader: testWALReader{},
		ObjectStorage:  testStorage{},
		ManifestParser: testParser{},
		Validator:      testValidator{},
		StateRestorer:  testRestorer{},
	}
}
