package objectstore

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"testing"
)

func TestFileStoreRoundTripAndList(t *testing.T) {
	store, err := NewFileStore(filepath.Join(t.TempDir(), "objects"))
	if err != nil {
		t.Fatalf("create store: %v", err)
	}
	ctx := context.Background()
	if err := store.PutObject(ctx, "wal/00000000000000000001", []byte("entry-1")); err != nil {
		t.Fatalf("put first object: %v", err)
	}
	if err := store.PutObject(ctx, "wal/00000000000000000002", []byte("entry-2")); err != nil {
		t.Fatalf("put second object: %v", err)
	}
	if err := store.PutObject(ctx, "checkpoint/manifest.json", []byte("manifest")); err != nil {
		t.Fatalf("put checkpoint object: %v", err)
	}

	keys, err := store.ListObjects(ctx, "wal")
	if err != nil {
		t.Fatalf("list WAL objects: %v", err)
	}
	if want := []string{"wal/00000000000000000001", "wal/00000000000000000002"}; len(keys) != len(want) || keys[0] != want[0] || keys[1] != want[1] {
		t.Fatalf("WAL keys = %v, want %v", keys, want)
	}

	data, err := store.GetObject(ctx, "wal/00000000000000000001")
	if err != nil || string(data) != "entry-1" {
		t.Fatalf("get object = %q, err=%v", data, err)
	}
	reader, err := store.GetObjectReader(ctx, "checkpoint/manifest.json")
	if err != nil {
		t.Fatalf("open object reader: %v", err)
	}
	readerData, err := io.ReadAll(reader)
	if err != nil || string(readerData) != "manifest" {
		_ = reader.Close()
		t.Fatalf("read object = %q, err=%v", readerData, err)
	}
	if err := reader.Close(); err != nil {
		t.Fatalf("close object reader: %v", err)
	}

	if err := store.DeleteObject(ctx, "checkpoint/manifest.json"); err != nil {
		t.Fatalf("delete object: %v", err)
	}
	if _, err := store.GetObject(ctx, "checkpoint/manifest.json"); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("get deleted object error = %v, want os.ErrNotExist", err)
	}
}

func TestFileStoreRejectsInvalidKeysAndCancellation(t *testing.T) {
	store, err := NewFileStore(filepath.Join(t.TempDir(), "objects"))
	if err != nil {
		t.Fatalf("create store: %v", err)
	}
	ctx := context.Background()
	for _, key := range []string{"", "/absolute", "../escape", "nested/../../escape", `C:\\escape`} {
		if err := store.PutObject(ctx, key, []byte("data")); !errors.Is(err, ErrInvalidKey) {
			t.Errorf("put key %q error = %v, want ErrInvalidKey", key, err)
		}
	}
	cancelled, cancel := context.WithCancel(ctx)
	cancel()
	if err := store.PutObject(cancelled, "wal/entry", []byte("data")); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled put error = %v, want context.Canceled", err)
	}
	if _, err := store.ListObjects(cancelled, "wal"); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled list error = %v, want context.Canceled", err)
	}
}

func TestNewFileStoreRejectsBlankRoot(t *testing.T) {
	for _, root := range []string{"", "   ", "."} {
		if _, err := NewFileStore(root); err == nil {
			t.Errorf("NewFileStore(%q) succeeded, want error", root)
		}
	}
}
