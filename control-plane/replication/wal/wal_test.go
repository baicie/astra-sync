package wal

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"go.uber.org/zap"
)

type mockObjectStorage struct {
	objects map[string][]byte
}

func (m *mockObjectStorage) PutObject(ctx context.Context, key string, data []byte) error {
	if m.objects == nil {
		m.objects = make(map[string][]byte)
	}
	m.objects[key] = data
	return nil
}

func (m *mockObjectStorage) GetObject(ctx context.Context, key string) ([]byte, error) {
	if m.objects == nil {
		return nil, errors.New("not found")
	}
	data, ok := m.objects[key]
	if !ok {
		return nil, errors.New("not found")
	}
	return data, nil
}

func (m *mockObjectStorage) ListObjects(ctx context.Context, prefix string) ([]string, error) {
	var keys []string
	for k := range m.objects {
		if len(k) >= len(prefix) && k[:len(prefix)] == prefix {
			keys = append(keys, k)
		}
	}
	return keys, nil
}

func (m *mockObjectStorage) DeleteObject(ctx context.Context, key string) error {
	delete(m.objects, key)
	return nil
}

func TestEntry_MarshalBinary(t *testing.T) {
	entry := &Entry{
		Sequence:      123,
		Region:       "us-east-1",
		Epoch:        42,
		CheckpointURI: "s3://bucket/checkpoints/job-123/epoch-42",
		JobID:        "job-123",
		Timestamp:    time.Date(2026, 8, 18, 12, 0, 0, 0, time.UTC),
	}

	data, err := entry.MarshalBinary()
	if err != nil {
		t.Fatalf("MarshalBinary failed: %v", err)
	}

	// Verify we can read it back
	decoded := &Entry{}
	if err := decoded.UnmarshalBinary(data); err != nil {
		t.Fatalf("UnmarshalBinary failed: %v", err)
	}

	if decoded.Sequence != entry.Sequence {
		t.Errorf("Sequence mismatch: got %d, want %d", decoded.Sequence, entry.Sequence)
	}
	if decoded.Region != entry.Region {
		t.Errorf("Region mismatch: got %s, want %s", decoded.Region, entry.Region)
	}
	if decoded.Epoch != entry.Epoch {
		t.Errorf("Epoch mismatch: got %d, want %d", decoded.Epoch, entry.Epoch)
	}
	if decoded.CheckpointURI != entry.CheckpointURI {
		t.Errorf("CheckpointURI mismatch: got %s, want %s", decoded.CheckpointURI, entry.CheckpointURI)
	}
	if decoded.JobID != entry.JobID {
		t.Errorf("JobID mismatch: got %s, want %s", decoded.JobID, entry.JobID)
	}
	if decoded.CRC32C == 0 {
		t.Error("CRC32C should be non-zero")
	}
}

func TestEntry_MarshalBinary_EmptyCheckpoint(t *testing.T) {
	entry := &Entry{
		Sequence: 1,
		CheckpointURI: "",
	}
	_, err := entry.MarshalBinary()
	if !errors.Is(err, ErrInvalidCheckpoint) {
		t.Errorf("expected ErrInvalidCheckpoint, got %v", err)
	}
}

func TestEntry_MarshalBinary_ZeroSequence(t *testing.T) {
	entry := &Entry{
		Sequence:      0,
		CheckpointURI: "s3://bucket/checkpoint",
	}
	_, err := entry.MarshalBinary()
	if !errors.Is(err, ErrEmptySequence) {
		t.Errorf("expected ErrEmptySequence, got %v", err)
	}
}

func TestEntry_UnmarshalBinary_CorruptedCRC(t *testing.T) {
	entry := &Entry{
		Sequence:      1,
		Region:        "us-east-1",
		CheckpointURI: "s3://bucket/checkpoint",
		JobID:         "job-1",
	}
	data, _ := entry.MarshalBinary()

	// Corrupt the data
	corrupted := make([]byte, len(data))
	copy(corrupted, data)
	// Flip a byte in the middle
	corrupted[len(corrupted)/2] ^= 0xFF

	decoded := &Entry{}
	err := decoded.UnmarshalBinary(corrupted)
	if err == nil {
		t.Error("expected error for corrupted CRC")
	}
}

func TestEntry_UnmarshalBinary_TruncatedData(t *testing.T) {
	decoded := &Entry{}
	err := decoded.UnmarshalBinary([]byte{1, 2, 3})
	if err == nil {
		t.Error("expected error for truncated data")
	}
}

func TestWriter_Append(t *testing.T) {
	store := &mockObjectStorage{}
	entry := &Entry{
		Epoch:         1,
		CheckpointURI: "s3://bucket/checkpoint-1",
		JobID:         "job-1",
	}

	logger, _ := zap.NewDevelopment()
	writer := &Writer{
		cfg:    Config{Region: "us-east-1", WALPrefix: "replication/wal"},
		store:  store,
		logger: logger.With(zap.String("region", "us-east-1")),
		sequence: 0,
		pending: make([]*Entry, 0),
	}

	ctx := context.Background()
	err := writer.Append(ctx, entry)
	if err != nil {
		t.Fatalf("Append failed: %v", err)
	}

	if entry.Sequence != 1 {
		t.Errorf("Sequence should be 1, got %d", entry.Sequence)
	}
	if entry.Region != "us-east-1" {
		t.Errorf("Region should be us-east-1, got %s", entry.Region)
	}
	if entry.CRC32C == 0 {
		t.Error("CRC32C should be set")
	}

	// Check object was stored
	expectedKey := "replication/wal/us-east-1/0000000000000001.wal"
	if _, ok := store.objects[expectedKey]; !ok {
		t.Errorf("object not stored at expected key %s", expectedKey)
	}
}

func TestWriter_Append_RegionMismatch(t *testing.T) {
	store := &mockObjectStorage{}
	entry := &Entry{
		Region: "eu-west-1", // Different from writer's region
		CheckpointURI: "s3://bucket/checkpoint",
	}

	logger, _ := zap.NewDevelopment()
	writer := &Writer{
		cfg:    Config{Region: "us-east-1", WALPrefix: "replication/wal"},
		store:  store,
		logger: logger.With(zap.String("region", "us-east-1")),
		sequence: 0,
		pending: make([]*Entry, 0),
	}

	ctx := context.Background()
	err := writer.Append(ctx, entry)
	if err == nil {
		t.Error("expected region mismatch error")
	}
}

func TestWriter_Close(t *testing.T) {
	store := &mockObjectStorage{}
	logger, _ := zap.NewDevelopment()
	writer := &Writer{
		cfg:      Config{Region: "us-east-1", WALPrefix: "replication/wal"},
		store:    store,
		logger:   logger.With(zap.String("region", "us-east-1")),
		sequence: 5,
		closed:   false,
		pending:  make([]*Entry, 0),
	}

	ctx := context.Background()
	err := writer.Close(ctx)
	if err != nil {
		t.Fatalf("Close failed: %v", err)
	}

	if !writer.closed {
		t.Error("writer should be marked as closed")
	}
}

func TestWriter_Close_Twice(t *testing.T) {
	store := &mockObjectStorage{}
	logger, _ := zap.NewDevelopment()
	writer := &Writer{
		cfg:      Config{Region: "us-east-1", WALPrefix: "replication/wal"},
		store:    store,
		logger:   logger.With(zap.String("region", "us-east-1")),
		sequence: 0,
		closed:   true,
	}

	ctx := context.Background()
	err := writer.Close(ctx)
	if err != nil {
		t.Fatalf("second Close should not fail: %v", err)
	}
}

func TestReader_ReadBatch(t *testing.T) {
	store := &mockObjectStorage{}
	logger, _ := zap.NewDevelopment()

	// Pre-populate some entries
	for i := int64(1); i <= 5; i++ {
		entry := &Entry{
			Sequence:      i,
			Region:        "us-east-1",
			Epoch:         i,
			CheckpointURI: "s3://bucket/checkpoint",
			JobID:         "job-1",
			Timestamp:     time.Now(),
		}
		data, _ := entry.MarshalBinary()
		key := "replication/wal/us-east-1/" + formatSequence(i) + ".wal"
		store.PutObject(context.Background(), key, data)
	}

	reader := &Reader{
		cfg:         Config{Region: "us-east-1", WALPrefix: "replication/wal"},
		store:       store,
		logger:      logger.With(zap.String("region", "us-east-1")),
		region:      "us-east-1",
		lastReadSeq: 0,
	}

	entries, err := reader.ReadBatch(context.Background(), 10)
	if err != nil {
		t.Fatalf("ReadBatch failed: %v", err)
	}

	if len(entries) != 5 {
		t.Errorf("expected 5 entries, got %d", len(entries))
	}

	for i, entry := range entries {
		if entry.Sequence != int64(i+1) {
			t.Errorf("sequence %d: expected %d, got %d", i, i+1, entry.Sequence)
		}
	}
}

func formatSequence(seq int64) string {
	return fmt.Sprintf("%016d", seq)
}

// Benchmark for Entry marshaling
func BenchmarkEntry_MarshalBinary(b *testing.B) {
	entry := &Entry{
		Sequence:      12345,
		Region:        "us-east-1",
		Epoch:         42,
		CheckpointURI: "s3://bucket/path/to/checkpoint/manifest.json",
		JobID:         "job-12345-abcdef",
		Timestamp:     time.Now(),
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		entry.MarshalBinary()
	}
}

// Benchmark for Entry unmarshaling
func BenchmarkEntry_UnmarshalBinary(b *testing.B) {
	entry := &Entry{
		Sequence:      12345,
		Region:        "us-east-1",
		Epoch:         42,
		CheckpointURI: "s3://bucket/path/to/checkpoint/manifest.json",
		JobID:         "job-12345-abcdef",
		Timestamp:     time.Now(),
	}
	data, _ := entry.MarshalBinary()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		var e Entry
		e.UnmarshalBinary(data)
	}
}
