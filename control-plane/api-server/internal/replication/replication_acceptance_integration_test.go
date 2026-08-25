//go:build integration

package replication

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"net"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/google/uuid"
	_ "github.com/jackc/pgx/v5/stdlib"
	"go.uber.org/zap"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/status"
	"google.golang.org/grpc/test/bufconn"
	"google.golang.org/protobuf/proto"

	controlv1 "io.astrasync/control-plane/api-server/gen/go/v1"
	"io.astrasync/control-plane/replication/adapters"
	"io.astrasync/control-plane/replication/channel"
	"io.astrasync/control-plane/replication/objectstore"
	"io.astrasync/control-plane/replication/promotion"
	recoverypkg "io.astrasync/control-plane/replication/recovery"
	walpkg "io.astrasync/control-plane/replication/wal"
)

func TestCrossRegionReplicationAcceptanceCoversRetryRecoveryAndPromotionFence(t *testing.T) {
	dataSourceName := testingPostgresURL(t)
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
		t.Fatalf("create replication store: %v", err)
	}
	if err := store.Migrate(ctx); err != nil {
		t.Fatalf("migrate replication metadata: %v", err)
	}
	jobID := "acceptance-job-" + uuid.NewString()
	t.Cleanup(func() {
		_, _ = database.ExecContext(ctx, `DELETE FROM astrasync_replication_checkpoint_admissions WHERE job_id=$1`, jobID)
		_, _ = database.ExecContext(ctx, `DELETE FROM astrasync_replication_promotions WHERE job_id=$1`, jobID)
		_, _ = database.ExecContext(ctx, `DELETE FROM astrasync_replication_job_epochs WHERE job_id=$1`, jobID)
	})

	standbyService := NewService(nil, nil, WithCheckpointDeduplicator(store))
	subscriber := make(chan *controlv1.CheckpointEvent, 4)
	standbyService.subscribers["eu-west-1"] = map[chan *controlv1.CheckpointEvent]struct{}{subscriber: {}}
	grpcServer, listener, serverDone := startReplicationServer(standbyService)
	firstServer := grpcServer
	firstListener := listener
	firstServerDone := serverDone
	t.Cleanup(func() {
		firstServer.Stop()
		_ = firstListener.Close()
		<-firstServerDone
	})
	conn := dialReplicationServer(t, listener)
	t.Cleanup(func() { _ = conn.Close() })
	sender := EventSenderFactory{}.NewEventSender(conn, "us-east-1", "eu-west-1")
	event := acceptanceCheckpointEvent(jobID, 1, 1, "checkpoints/acceptance.json")
	payload, err := proto.Marshal(event)
	if err != nil {
		t.Fatalf("marshal checkpoint event: %v", err)
	}
	channelEvent := &channel.Event{Type: channel.EventTypeCheckpoint, SourceRegion: "us-east-1", TargetRegion: "eu-west-1", Payload: payload}
	if err := sender.SendEvent(ctx, channelEvent); err != nil {
		t.Fatalf("send first checkpoint: %v", err)
	}
	if received := receiveCheckpoint(t, subscriber); received.GetWalEntry().GetSequence() != 1 {
		t.Fatalf("first received sequence = %d, want 1", received.GetWalEntry().GetSequence())
	}
	if err := sender.SendEvent(ctx, channelEvent); err != nil {
		t.Fatalf("send duplicate checkpoint: %v", err)
	}
	assertNoCheckpoint(t, subscriber)

	firstServer.Stop()
	<-firstServerDone
	disconnectedCtx, cancelDisconnected := context.WithTimeout(ctx, 250*time.Millisecond)
	defer cancelDisconnected()
	if err := sender.SendEvent(disconnectedCtx, channelEvent); err == nil {
		t.Fatal("send through stopped peer succeeded")
	}

	retryServer, retryListener, retryServerDone := startReplicationServer(standbyService)
	t.Cleanup(func() {
		retryServer.Stop()
		_ = retryListener.Close()
		<-retryServerDone
	})
	retryConn := dialReplicationServer(t, retryListener)
	t.Cleanup(func() { _ = retryConn.Close() })
	retrySender := EventSenderFactory{}.NewEventSender(retryConn, "us-east-1", "eu-west-1")

	queueFullSubscriber := make(chan *controlv1.CheckpointEvent, 1)
	queueFullSubscriber <- acceptanceCheckpointEvent(jobID, 99, 1, "checkpoints/filler.json")
	standbyService.subscribers["eu-west-1"] = map[chan *controlv1.CheckpointEvent]struct{}{queueFullSubscriber: {}}
	queueFullEvent := acceptanceCheckpointEvent(jobID, 2, 1, "checkpoints/acceptance-2.json")
	queueFullPayload, err := proto.Marshal(queueFullEvent)
	if err != nil {
		t.Fatalf("marshal queue-full checkpoint: %v", err)
	}
	queueFullChannelEvent := &channel.Event{Type: channel.EventTypeCheckpoint, SourceRegion: "us-east-1", TargetRegion: "eu-west-1", Payload: queueFullPayload}
	if err := retrySender.SendEvent(ctx, queueFullChannelEvent); status.Code(err) != codes.ResourceExhausted {
		t.Fatalf("queue-full checkpoint error = %v, want ResourceExhausted", err)
	}
	<-queueFullSubscriber
	if err := retrySender.SendEvent(ctx, queueFullChannelEvent); err != nil {
		t.Fatalf("retry after queue drain: %v", err)
	}
	<-queueFullSubscriber

	if err := retrySender.SendEvent(ctx, channelEvent); err != nil {
		t.Fatalf("retry checkpoint after reconnect: %v", err)
	}
	assertNoCheckpoint(t, subscriber)

	promotionEpoch, err := store.Assign(ctx, jobID)
	if err != nil || promotionEpoch != 1 {
		t.Fatalf("initial epoch = %d, err=%v; want 1", promotionEpoch, err)
	}
	manager, err := promotion.NewManager(zap.NewNop(), store, store, store, store,
		promotion.WithCurrentRegion("us-east-1"),
		promotion.WithTopology(acceptanceTopology{}),
		promotion.WithEpochFencer(store),
		promotion.WithCapabilityRevalidator(acceptanceCapabilityRevalidator{}),
	)
	if err != nil {
		t.Fatalf("create promotion manager: %v", err)
	}
	promotionResult, err := manager.Promote(ctx, jobID, "eu-west-1", "acceptance-promotion-key", promotionEpoch)
	if err != nil {
		t.Fatalf("promote standby region: %v", err)
	}
	if promotionResult.NewEpoch != 2 || promotionResult.State != promotion.StateFailoverComplete {
		t.Fatalf("promotion result = epoch %d, state %s; want epoch 2 complete", promotionResult.NewEpoch, promotionResult.State)
	}
	if err := store.Fence(ctx, jobID, promotionEpoch); !errors.Is(err, promotion.ErrEpochConflict) {
		t.Fatalf("stale epoch fence after promotion = %v, want conflict", err)
	}

	if err := runAcceptanceRecovery(ctx, t, jobID, 2); err != nil {
		t.Fatalf("recover replicated checkpoint: %v", err)
	}
}

func testingPostgresURL(t *testing.T) string {
	t.Helper()
	value := getenv("ASTRASYNC_TEST_POSTGRES_URL")
	if value == "" {
		t.Skip("ASTRASYNC_TEST_POSTGRES_URL is not configured")
	}
	return value
}

func getenv(key string) string { return os.Getenv(key) }

func startReplicationServer(service *Service) (*grpc.Server, *bufconn.Listener, <-chan struct{}) {
	listener := bufconn.Listen(1024 * 1024)
	server := grpc.NewServer()
	controlv1.RegisterReplicationServiceServer(server, service)
	done := make(chan struct{})
	go func() {
		defer close(done)
		_ = server.Serve(listener)
	}()
	return server, listener, done
}

func dialReplicationServer(t *testing.T, listener *bufconn.Listener) *grpc.ClientConn {
	t.Helper()
	conn, err := grpc.DialContext(context.Background(), "bufnet",
		grpc.WithContextDialer(func(context.Context, string) (net.Conn, error) { return listener.Dial() }),
		grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		t.Fatalf("dial replication server: %v", err)
	}
	return conn
}

func receiveCheckpoint(t *testing.T, subscriber <-chan *controlv1.CheckpointEvent) *controlv1.CheckpointEvent {
	t.Helper()
	select {
	case event := <-subscriber:
		return event
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for checkpoint")
		return nil
	}
}

func assertNoCheckpoint(t *testing.T, subscriber <-chan *controlv1.CheckpointEvent) {
	t.Helper()
	select {
	case event := <-subscriber:
		t.Fatalf("unexpected duplicate checkpoint sequence %d", event.GetWalEntry().GetSequence())
	case <-time.After(50 * time.Millisecond):
	}
}

func acceptanceCheckpointEvent(jobID string, sequence, epoch int64, uri string) *controlv1.CheckpointEvent {
	return &controlv1.CheckpointEvent{EventType: controlv1.CheckpointEventType_CHECKPOINT_EVENT_TYPE_CHECKPOINT_COMPLETED, WalEntry: &controlv1.WALEntry{Sequence: sequence, Region: "us-east-1", Epoch: epoch, CheckpointUri: uri, JobId: jobID}}
}

type acceptanceTopology struct{}

func (acceptanceTopology) IsStandby(region string) bool { return region == "eu-west-1" }

type acceptanceCapabilityRevalidator struct{}

func (acceptanceCapabilityRevalidator) Revalidate(context.Context, string, time.Duration) error {
	return nil
}

func runAcceptanceRecovery(ctx context.Context, t *testing.T, jobID string, epoch int64) error {
	t.Helper()
	store, err := objectstore.NewFileStore(filepath.Join(t.TempDir(), "objects"))
	if err != nil {
		return err
	}
	manifest := recoverypkg.CheckpointManifest{JobID: jobID, Epoch: epoch, Sequence: 1, CheckpointURI: "checkpoints/acceptance.json", Files: []recoverypkg.CheckpointFile{{Name: "state", URI: "state/acceptance", Size: 5}}}
	manifestData, err := json.Marshal(manifest)
	if err != nil {
		return err
	}
	if err := store.PutObject(ctx, manifest.CheckpointURI, manifestData); err != nil {
		return err
	}
	if err := store.PutObject(ctx, "state/acceptance", []byte("state")); err != nil {
		return err
	}
	writer, err := walpkg.NewWriter(ctx, store, zap.NewNop(), "us-east-1", "", "replication/wal", walpkg.WithBatchSize(1), walpkg.WithFlushInterval(time.Hour))
	if err != nil {
		return err
	}
	if err := writer.Append(ctx, &walpkg.Entry{Sequence: 1, Region: "us-east-1", Epoch: epoch, CheckpointURI: manifest.CheckpointURI, JobID: jobID}); err != nil {
		return err
	}
	if err := writer.Close(ctx); err != nil {
		return err
	}
	reader, err := adapters.NewWALReader(ctx, store, zap.NewNop(), "us-east-1", "replication/wal", 0)
	if err != nil {
		return err
	}
	restorer, err := adapters.NewFileStateRestorer(store)
	if err != nil {
		return err
	}
	auditor, err := adapters.NewZapAuditLogger(zap.NewNop())
	if err != nil {
		return err
	}
	manager := recoverypkg.NewManager(zap.NewNop(), reader, store, adapters.JSONManifestParser{}, adapters.CheckpointValidator{}, restorer, auditor, recoverypkg.WithTargetRegion("eu-west-1"))
	recovered, err := manager.Recover(ctx, jobID, epoch)
	if err != nil {
		return err
	}
	if recovered.State != recoverypkg.StateRecoveryComplete {
		return errors.New("recovery did not complete")
	}
	return nil
}
