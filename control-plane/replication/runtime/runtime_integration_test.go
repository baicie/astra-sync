//go:build integration

package runtime_test

import (
	"context"
	"database/sql"
	"net"
	"os"
	"path/filepath"
	"testing"

	_ "github.com/jackc/pgx/v5/stdlib"
	"go.uber.org/zap"
	"google.golang.org/grpc"

	adapters "io.astrasync/control-plane/replication/adapters"
	replicationmetrics "io.astrasync/control-plane/replication/metrics"
	"io.astrasync/control-plane/replication/objectstore"
	runtimepkg "io.astrasync/control-plane/replication/runtime"
)

func TestRuntimeAssemblesDeploymentAdaptersAndCloses(t *testing.T) {
	dataSourceName := os.Getenv("ASTRASYNC_TEST_POSTGRES_URL")
	if dataSourceName == "" {
		t.Skip("ASTRASYNC_TEST_POSTGRES_URL is not configured")
	}
	ctx := context.Background()
	database, err := sql.Open("pgx", dataSourceName)
	if err != nil {
		t.Fatalf("open PostgreSQL: %v", err)
	}
	t.Cleanup(func() { _ = database.Close() })
	if err := database.PingContext(ctx); err != nil {
		t.Fatalf("ping PostgreSQL: %v", err)
	}
	store, err := adapters.NewPostgreSQLStore(database)
	if err != nil {
		t.Fatalf("create PostgreSQL adapter: %v", err)
	}
	if err := store.Migrate(ctx); err != nil {
		t.Fatalf("migrate replication metadata: %v", err)
	}
	objectStore, err := objectstore.NewFileStore(filepath.Join(t.TempDir(), "objects"))
	if err != nil {
		t.Fatalf("create object store: %v", err)
	}
	walReader, err := adapters.NewWALReader(ctx, objectStore, zap.NewNop(), "us-east-1", "replication/wal", 0)
	if err != nil {
		t.Fatalf("create WAL reader: %v", err)
	}
	stateRestorer, err := adapters.NewFileStateRestorer(objectStore)
	if err != nil {
		t.Fatalf("create state restorer: %v", err)
	}
	auditor, err := adapters.NewZapAuditLogger(zap.NewNop())
	if err != nil {
		t.Fatalf("create audit logger: %v", err)
	}
	metricsBundle, err := replicationmetrics.NewBundle()
	if err != nil {
		t.Fatalf("create metrics bundle: %v", err)
	}
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen for replication peer: %v", err)
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
	deps := runtimepkg.Dependencies{
		EventHandler:   testEventHandler{},
		EventSender:    testEventSender{},
		PromotionStore: store,
		EpochAssigner:  store,
		JobReader:      store,
		JobWriter:      store,
		Fencer:         store,
		Topology:       testTopology{},
		WALEntryReader: walReader,
		ObjectStorage:  objectStore,
		ManifestParser: adapters.JSONManifestParser{},
		Validator:      adapters.CheckpointValidator{},
		StateRestorer:  stateRestorer,
		Auditor:        auditor,
	}
	rt, err := runtimepkg.New(zap.NewNop(), runtimepkg.Config{
		Region: "us-east-1", PeerRegion: "eu-west-1", PeerEndpoint: listener.Addr().String(), Metrics: metricsBundle,
	}, deps)
	if err != nil {
		t.Fatalf("create runtime: %v", err)
	}
	if rt.Metrics() != metricsBundle || rt.Channel() == nil || rt.Promotion() == nil || rt.Recovery() == nil {
		t.Fatal("runtime did not retain deployment components")
	}
	if err := rt.Start(ctx); err != nil {
		t.Fatalf("start runtime: %v", err)
	}
	if err := rt.Close(); err != nil {
		t.Fatalf("close runtime: %v", err)
	}
	if err := rt.Close(); err != nil {
		t.Fatalf("close runtime a second time: %v", err)
	}
}

type testTopology struct{}

func (testTopology) IsStandby(region string) bool { return region == "eu-west-1" }
