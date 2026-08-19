// Package wal provides Write-Ahead Log (WAL) functionality for cross-region
// checkpoint replication. WAL entries are appended by the primary region and
// consumed by secondary regions to maintain eventual consistency.
package wal

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"hash/crc32"
	"io"
	"sort"
	"sync"
	"time"

	"github.com/google/uuid"
	"go.uber.org/zap"
)

// Common errors for WAL operations.
var (
	ErrClosed            = errors.New("wal: writer is closed")
	ErrEmptySequence     = errors.New("wal: sequence must be positive")
	ErrInvalidCheckpoint = errors.New("wal: checkpoint URI cannot be empty")
	ErrMismatchedRegion  = errors.New("wal: region mismatch between writer and entry")
)

// Entry represents a WAL entry for cross-region replication.
type Entry struct {
	Sequence      int64
	Region        string
	Epoch         int64
	CheckpointURI string
	JobID         string
	Timestamp     time.Time
	CRC32C        uint32
}

// MarshalBinary encodes an Entry into a binary format suitable for storage.
// Format: sequence(8) | epoch(8) | job_id_len(4) | job_id | region_len(4) | region |
//         | checkpoint_uri_len(4) | checkpoint_uri | timestamp_unix_nano(8) | crc32c(4)
func (e *Entry) MarshalBinary() ([]byte, error) {
	if e.Sequence <= 0 {
		return nil, ErrEmptySequence
	}
	if e.CheckpointURI == "" {
		return nil, ErrInvalidCheckpoint
	}

	// Calculate required size
	jobIDLen := len(e.JobID)
	regionLen := len(e.Region)
	checkpointLen := len(e.CheckpointURI)
	totalSize := 8 + 8 + 4 + jobIDLen + 4 + regionLen + 4 + checkpointLen + 8 + 4

	buf := make([]byte, totalSize)
	offset := 0

	binary.LittleEndian.PutUint64(buf[offset:], uint64(e.Sequence))
	offset += 8

	binary.LittleEndian.PutUint64(buf[offset:], uint64(e.Epoch))
	offset += 8

	binary.LittleEndian.PutUint32(buf[offset:], uint32(jobIDLen))
	offset += 4
	copy(buf[offset:], e.JobID)
	offset += jobIDLen

	binary.LittleEndian.PutUint32(buf[offset:], uint32(regionLen))
	offset += 4
	copy(buf[offset:], e.Region)
	offset += regionLen

	binary.LittleEndian.PutUint32(buf[offset:], uint32(checkpointLen))
	offset += 4
	copy(buf[offset:], e.CheckpointURI)
	offset += checkpointLen

	binary.LittleEndian.PutUint64(buf[offset:], uint64(e.Timestamp.UnixNano()))
	offset += 8

	// Calculate CRC over everything except the CRC field itself
	dataLen := totalSize - 4
	e.CRC32C = crc32.ChecksumIEEE(buf[:dataLen])
	binary.LittleEndian.PutUint32(buf[offset:], e.CRC32C)

	return buf, nil
}

// UnmarshalBinary decodes a binary-encoded Entry.
func (e *Entry) UnmarshalBinary(data []byte) error {
	if len(data) < 44 { // Minimum size: 8+8+4+0+4+0+4+0+8+4
		return errors.New("wal: data too short")
	}

	offset := 0

	e.Sequence = int64(binary.LittleEndian.Uint64(data[offset:]))
	offset += 8

	e.Epoch = int64(binary.LittleEndian.Uint64(data[offset:]))
	offset += 8

	jobIDLen := int(binary.LittleEndian.Uint32(data[offset:]))
	offset += 4
	if offset+jobIDLen > len(data) {
		return errors.New("wal: truncated job_id")
	}
	e.JobID = string(data[offset : offset+jobIDLen])
	offset += jobIDLen

	regionLen := int(binary.LittleEndian.Uint32(data[offset:]))
	offset += 4
	if offset+regionLen > len(data) {
		return errors.New("wal: truncated region")
	}
	e.Region = string(data[offset : offset+regionLen])
	offset += regionLen

	checkpointLen := int(binary.LittleEndian.Uint32(data[offset:]))
	offset += 4
	if offset+checkpointLen > len(data) {
		return errors.New("wal: truncated checkpoint_uri")
	}
	e.CheckpointURI = string(data[offset : offset+checkpointLen])
	offset += checkpointLen

	e.Timestamp = time.Unix(0, int64(binary.LittleEndian.Uint64(data[offset:])))
	offset += 8

	storedCRC := binary.LittleEndian.Uint32(data[offset:])

	// Verify CRC
	dataLen := len(data) - 4
	computedCRC := crc32.ChecksumIEEE(data[:dataLen])
	if storedCRC != computedCRC {
		return fmt.Errorf("wal: CRC mismatch: expected 0x%08x, got 0x%08x", computedCRC, storedCRC)
	}
	e.CRC32C = storedCRC

	return nil
}

// ObjectStorage defines the interface for object storage operations.
type ObjectStorage interface {
	// PutObject stores an object with the given key and data.
	PutObject(ctx context.Context, key string, data []byte) error
	// GetObject retrieves an object by key.
	GetObject(ctx context.Context, key string) ([]byte, error)
	// ListObjects lists objects with a given prefix.
	ListObjects(ctx context.Context, prefix string) ([]string, error)
	// DeleteObject deletes an object by key.
	DeleteObject(ctx context.Context, key string) error
}

// Config holds the configuration for a WAL writer.
type Config struct {
	Region      string
	Bucket      string
	WALPrefix   string
	FlushInterval time.Duration
	BatchSize   int
}

// Option is a functional option for WAL writer configuration.
type Option func(*Config)

// WithFlushInterval sets the interval between forced flushes.
func WithFlushInterval(d time.Duration) Option {
	return func(c *Config) {
		c.FlushInterval = d
	}
}

// WithBatchSize sets the number of entries to batch before flushing.
func WithBatchSize(n int) Option {
	return func(c *Config) {
		c.BatchSize = n
	}
}

// Writer appends WAL entries to object storage.
type Writer struct {
	cfg    Config
	store  ObjectStorage
	logger *zap.Logger

	mu       sync.Mutex
	closed   bool
	sequence int64
	pending  []*Entry
}

// NewWriter creates a new WAL writer.
func NewWriter(ctx context.Context, store ObjectStorage, logger *zap.Logger, region, bucket, walPrefix string, opts ...Option) (*Writer, error) {
	cfg := Config{
		Region:        region,
		Bucket:        bucket,
		WALPrefix:     walPrefix,
		FlushInterval: time.Second,
		BatchSize:     100,
	}
	for _, opt := range opts {
		opt(&cfg)
	}

	w := &Writer{
		cfg:     cfg,
		store:   store,
		logger:  logger.With(zap.String("region", region), zap.String("walPrefix", walPrefix)),
		sequence: 0,
		pending:  make([]*Entry, 0, cfg.BatchSize),
	}

	// Resume sequence from existing entries
	if err := w.resumeSequence(ctx); err != nil {
		logger.Warn("failed to resume sequence, starting from 0", zap.Error(err))
	}

	return w, nil
}

// resumeSequence finds the highest sequence number in the WAL prefix.
func (w *Writer) resumeSequence(ctx context.Context) error {
	prefix := fmt.Sprintf("%s/", w.cfg.WALPrefix)
	entries, err := w.store.ListObjects(ctx, prefix)
	if err != nil {
		return fmt.Errorf("list objects: %w", err)
	}

	if len(entries) == 0 {
		w.sequence = 0
		return nil
	}

	// Parse sequence numbers from entry keys: <prefix>/<region>/<sequence>.wal
	maxSeq := int64(0)
	for _, entry := range entries {
		var seq int64
		if _, err := fmt.Sscanf(entry, fmt.Sprintf("%s%%s/%%d.wal", w.cfg.WALPrefix), &seq); err == nil {
			if seq > maxSeq {
				maxSeq = seq
			}
		}
	}

	w.sequence = maxSeq
	w.logger.Info("resumed sequence", zap.Int64("sequence", w.sequence))
	return nil
}

// Append adds a new entry to the WAL. The entry's Sequence, Region, and CRC32C
// are set by the writer. The caller should set Epoch, CheckpointURI, JobID, and Timestamp.
func (w *Writer) Append(ctx context.Context, entry *Entry) error {
	w.mu.Lock()
	defer w.mu.Unlock()

	if w.closed {
		return ErrClosed
	}
	if entry.Region != "" && entry.Region != w.cfg.Region {
		return fmt.Errorf("%w: got %q, want %q", ErrMismatchedRegion, entry.Region, w.cfg.Region)
	}

	w.sequence++
	entry.Sequence = w.sequence
	entry.Region = w.cfg.Region
	entry.Timestamp = time.Now().UTC()

	data, err := entry.MarshalBinary()
	if err != nil {
		return fmt.Errorf("marshal entry: %w", err)
	}

	key := w.entryKey(entry.Sequence)
	if err := w.store.PutObject(ctx, key, data); err != nil {
		return fmt.Errorf("put object %s: %w", key, err)
	}

	w.logger.Debug("appended WAL entry",
		zap.Int64("sequence", entry.Sequence),
		zap.Int64("epoch", entry.Epoch),
		zap.String("jobID", entry.JobID),
		zap.String("checkpointURI", entry.CheckpointURI),
	)

	return nil
}

// entryKey generates the object storage key for a WAL entry.
func (w *Writer) entryKey(sequence int64) string {
	return fmt.Sprintf("%s/%s/%016d.wal", w.cfg.WALPrefix, w.cfg.Region, sequence)
}

// Close flushes any pending entries and closes the writer.
func (w *Writer) Close(ctx context.Context) error {
	w.mu.Lock()
	defer w.mu.Unlock()

	if w.closed {
		return nil
	}
	w.closed = true

	if len(w.pending) > 0 {
		if err := w.flushLocked(ctx); err != nil {
			return err
		}
	}

	w.logger.Info("closed WAL writer", zap.Int64("finalSequence", w.sequence))
	return nil
}

// flushLocked flushes pending entries. Caller must hold the lock.
func (w *Writer) flushLocked(ctx context.Context) error {
	for _, entry := range w.pending {
		data, err := entry.MarshalBinary()
		if err != nil {
			return fmt.Errorf("marshal entry: %w", err)
		}
		key := w.entryKey(entry.Sequence)
		if err := w.store.PutObject(ctx, key, data); err != nil {
			return fmt.Errorf("put object %s: %w", key, err)
		}
	}
	w.pending = w.pending[:0]
	return nil
}

// Sequence returns the current sequence number.
func (w *Writer) Sequence() int64 {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.sequence
}

// Reader reads WAL entries from object storage.
type Reader struct {
	cfg    Config
	store  ObjectStorage
	logger *zap.Logger

	mu          sync.Mutex
	region      string
	resumeSeq   int64
	lastReadSeq int64
}

// NewReader creates a new WAL reader for a secondary region.
func NewReader(ctx context.Context, store ObjectStorage, logger *zap.Logger, region, bucket, walPrefix string, resumeFrom int64) (*Reader, error) {
	r := &Reader{
		cfg:      Config{Region: region, Bucket: bucket, WALPrefix: walPrefix},
		store:    store,
		logger:   logger.With(zap.String("region", region)),
		region:   region,
		resumeSeq: resumeFrom,
	}
	return r, nil
}

// ReadNext reads the next WAL entry after the last read sequence.
func (r *Reader) ReadNext(ctx context.Context) (*Entry, error) {
	r.mu.Lock()
	startSeq := r.lastReadSeq + 1
	r.mu.Unlock()

	entry, err := r.readEntry(ctx, startSeq)
	if err != nil {
		return nil, err
	}

	r.mu.Lock()
	r.lastReadSeq = entry.Sequence
	r.mu.Unlock()

	return entry, nil
}

// readEntry reads a specific sequence entry.
func (r *Reader) readEntry(ctx context.Context, sequence int64) (*Entry, error) {
	key := fmt.Sprintf("%s/%s/%016d.wal", r.cfg.WALPrefix, r.region, sequence)
	data, err := r.store.GetObject(ctx, key)
	if err != nil {
		return nil, fmt.Errorf("get object %s: %w", key, err)
	}

	entry := &Entry{}
	if err := entry.UnmarshalBinary(data); err != nil {
		return nil, fmt.Errorf("unmarshal entry: %w", err)
	}

	return entry, nil
}

// ReadBatch reads multiple entries starting from the last read sequence.
func (r *Reader) ReadBatch(ctx context.Context, maxCount int) ([]*Entry, error) {
	r.mu.Lock()
	startSeq := r.lastReadSeq + 1
	r.mu.Unlock()

	prefix := fmt.Sprintf("%s/%s/", r.cfg.WALPrefix, r.region)
	keys, err := r.store.ListObjects(ctx, prefix)
	if err != nil {
		return nil, fmt.Errorf("list objects: %w", err)
	}

	// Filter and sort keys by sequence number
	var entrySeqs []int64
	for _, key := range keys {
		seq := parseSequenceFromKey(key)
		if seq >= startSeq {
			entrySeqs = append(entrySeqs, seq)
		}
	}
	sort.Slice(entrySeqs, func(i, j int) bool { return entrySeqs[i] < entrySeqs[j] })

	if len(entrySeqs) > maxCount {
		entrySeqs = entrySeqs[:maxCount]
	}

	entries := make([]*Entry, 0, len(entrySeqs))
	for _, seq := range entrySeqs {
		entry, err := r.readEntry(ctx, seq)
		if err != nil {
			// Skip missing entries (gap)
			r.logger.Warn("skipped missing entry", zap.Int64("sequence", seq), zap.Error(err))
			continue
		}
		entries = append(entries, entry)
	}

	if len(entries) > 0 {
		r.mu.Lock()
		r.lastReadSeq = entries[len(entries)-1].Sequence
		r.mu.Unlock()
	}

	return entries, nil
}

// parseSequenceFromKey extracts the sequence number from a WAL entry key.
func parseSequenceFromKey(key string) int64 {
	// Key format: <prefix>/<region>/<sequence>.wal
	// Sequence is 16-digit zero-padded
	idx := len(key)
	if idx < 4 || key[idx-4:] != ".wal" {
		return 0
	}
	seqPart := key[:idx-4]
	// Find last '/'
	lastSlash := -1
	for i := len(seqPart) - 1; i >= 0; i-- {
		if seqPart[i] == '/' {
			lastSlash = i
			break
		}
	}
	if lastSlash < 0 {
		return 0
	}
	numStr := seqPart[lastSlash+1:]

	var seq int64
	_, err := fmt.Sscanf(numStr, "%d", &seq)
	if err != nil {
		return 0
	}
	return seq
}

// LastSequence returns the last read sequence number.
func (r *Reader) LastSequence() int64 {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.lastReadSeq
}

// NewUUIDEntry creates a new entry with a generated JobID.
func NewUUIDEntry(epoch int64, checkpointURI string) *Entry {
	return &Entry{
		JobID:         uuid.New().String(),
		Epoch:         epoch,
		CheckpointURI: checkpointURI,
	}
}

// EntryIterator provides sequential access to WAL entries.
type EntryIterator struct {
	reader *Reader
	ctx    context.Context
}

// NewEntryIterator creates an iterator starting from the given sequence.
func NewEntryIterator(ctx context.Context, reader *Reader) *EntryIterator {
	return &EntryIterator{
		reader: reader,
		ctx:    ctx,
	}
}

// Next returns the next entry or io.EOF when no more entries are available.
func (it *EntryIterator) Next() (*Entry, error) {
	entry, err := it.reader.ReadNext(it.ctx)
	if err != nil {
		if errors.Is(err, io.EOF) {
			return nil, io.EOF
		}
		return nil, err
	}
	return entry, nil
}
