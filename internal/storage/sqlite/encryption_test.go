package sqlite

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ideacrafterslabs/ctxt/internal/keys"
	"github.com/ideacrafterslabs/ctxt/internal/storage"
	"github.com/ideacrafterslabs/ctxt/internal/storage/blob/local"
)

func mustLocalBlobStore(t *testing.T, dir string) storage.BlobStore {
	t.Helper()
	bs, err := local.New(filepath.Join(dir, "blobs"))
	if err != nil {
		t.Fatal(err)
	}
	return bs
}

func mustMemoryKey(t *testing.T, id string) *keys.MemoryProvider {
	t.Helper()
	p, err := keys.NewMemory(id)
	if err != nil {
		t.Fatal(err)
	}
	return p
}

func assertNoPlaintextOnDisk(t *testing.T, dbPath, sentinel string) {
	t.Helper()
	for _, path := range []string{dbPath, dbPath + "-wal", dbPath + "-shm"} {
		raw, err := os.ReadFile(path)
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			t.Fatalf("read %s: %v", path, err)
		}
		if bytes.Contains(raw, []byte(sentinel)) {
			t.Fatalf("plaintext sentinel leaked into %s", path)
		}
		if bytes.Contains(raw, []byte("SQLite format 3")) {
			t.Fatalf("unencrypted SQLite header leaked into %s", path)
		}
	}
}

func TestSQLiteEncryption_PlaintextRefusalAndKeyRequired(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	bs := mustLocalBlobStore(t, dir)

	// Existing plaintext DB opened with KeyProvider must refuse until migrated.
	plainPath := filepath.Join(dir, "plain.sqlite")
	plainDrv, err := New(plainPath, bs)
	if err != nil {
		t.Fatal(err)
	}
	if err := plainDrv.Init(ctx); err != nil {
		t.Fatal(err)
	}
	_ = plainDrv.Close(ctx)

	p1 := mustMemoryKey(t, "k1")
	if _, err := New(plainPath, bs, WithKeyProvider(p1)); err == nil || !strings.Contains(err.Error(), "dpkms storage encrypt") {
		t.Fatalf("open plaintext DB with KeyProvider err = %v, want migration refusal", err)
	}

	// Sidecar .key envelope: opening without KeyProvider or with wrong KEK fails closed.
	encPath := filepath.Join(dir, "enc.sqlite")
	_, env, err := createAndWrapDBKey(ctx, encPath, p1)
	if err != nil {
		t.Fatal(err)
	}
	if err := writeKeyEnvelopeAtomic(encPath, env); err != nil {
		t.Fatal(err)
	}
	if _, err := New(encPath, bs); !errors.Is(err, ErrEncryptionKeyRequired) {
		t.Fatalf("open with .key and nil KeyProvider err = %v, want ErrEncryptionKeyRequired", err)
	}
	wrongP := mustMemoryKey(t, "k1")
	if _, err := New(encPath, bs, WithKeyProvider(wrongP)); !errors.Is(err, keys.ErrUnwrap) {
		t.Fatalf("open with wrong KEK err = %v, want ErrUnwrap", err)
	}
}

func TestSQLiteEncryption_KeyEnvelopeRewrapAndStockCleanup(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	bs := mustLocalBlobStore(t, dir)
	p1 := mustMemoryKey(t, "k1")

	encPath := filepath.Join(dir, "enc.sqlite")
	origHex, env, err := createAndWrapDBKey(ctx, encPath, p1)
	if err != nil {
		t.Fatal(err)
	}
	if err := writeKeyEnvelopeAtomic(encPath, env); err != nil {
		t.Fatal(err)
	}

	if _, err := p1.Rotate("k2"); err != nil {
		t.Fatal(err)
	}
	usedID, err := RewrapDatabaseKey(ctx, encPath, p1, "k2")
	if err != nil || usedID != "k2" {
		t.Fatalf("RewrapDatabaseKey = (%q, %v), want (k2, nil)", usedID, err)
	}
	if err := p1.Retire("k1"); err != nil {
		t.Fatal(err)
	}
	gotHex, gotCipher, err := unwrapDBKey(ctx, encPath, p1)
	if err != nil || gotHex != origHex || gotCipher != sqliteCipherName {
		t.Fatalf("unwrapDBKey after retiring k1 failed: err=%v cipher=%q", err, gotCipher)
	}

	if !CodecSupported(ctx) {
		freshPath := filepath.Join(dir, "fresh.sqlite")
		if _, err := New(freshPath, bs, WithKeyProvider(p1)); !errors.Is(err, ErrEncryptionUnsupported) {
			t.Fatalf("stock build encrypted open err = %v, want ErrEncryptionUnsupported", err)
		}
		if HasKeyEnvelope(freshPath) {
			t.Fatalf("orphan key envelope was not cleaned up after failed encrypted open")
		}
	}
}

func verifyEncryptedFTS5AndVec0(t *testing.T, drv *Driver, sentinel string) {
	t.Helper()
	ctx := t.Context()
	hits, err := drv.Objects().FTSSearch(ctx, sentinel, storage.ObjectFilter{Limit: 10})
	if err != nil || len(hits) != 1 || hits[0].ID != "enc-obj-1" {
		t.Fatalf("FTSSearch = (%d hits, %v), want 1 (enc-obj-1)", len(hits), err)
	}

	const modelID = "test/vec-enc"
	insertModelRow(t, drv, modelID, "test", 4)
	spec := storage.EmbeddingModelSpec{ModelID: modelID, Provider: "test", Dimension: 4}
	if err := drv.Embeddings().EnsureIndex(ctx, spec); err != nil {
		t.Fatalf("EnsureIndex: %v", err)
	}
	if err := drv.Embeddings().Put(ctx, "enc-obj-1", []storage.ObjectVector{
		{ModelID: modelID, ChunkIdx: 0, Variant: storage.EmbeddingVariantRaw, Vector: []float32{1, 0, 0, 0}, Text: sentinel},
	}); err != nil {
		t.Fatalf("Embeddings.Put: %v", err)
	}
	vecHits, err := drv.Embeddings().Search(ctx, storage.VectorQuery{
		ModelID: modelID, Vector: []float32{1, 0, 0, 0}, TopK: 5,
	})
	if err != nil || len(vecHits) != 1 || vecHits[0].ObjectID != "enc-obj-1" {
		t.Fatalf("Embeddings.Search = (%+v, %v), want enc-obj-1", vecHits, err)
	}
}

func verifyEncryptedAccessControls(t *testing.T, dbPath string, bs storage.BlobStore, p keys.Provider, sentinel string) {
	t.Helper()
	ctx := t.Context()
	ro, err := NewReadOnly(dbPath, bs, WithKeyProvider(p))
	if err != nil {
		t.Fatalf("NewReadOnly encrypted: %v", err)
	}
	got, err := ro.Objects().Get(ctx, "enc-obj-1")
	_ = ro.Close(ctx)
	if err != nil || got.TextContent != "Classified "+sentinel+" payload" {
		t.Fatalf("ReadOnly Get = (%+v, %v)", got, err)
	}
	if _, err := New(dbPath, bs); !errors.Is(err, ErrEncryptionKeyRequired) {
		t.Fatalf("reopen without key err = %v, want ErrEncryptionKeyRequired", err)
	}
	wrongP := mustMemoryKey(t, "k1")
	if _, err := New(dbPath, bs, WithKeyProvider(wrongP)); !errors.Is(err, keys.ErrUnwrap) {
		t.Fatalf("reopen wrong key err = %v, want ErrUnwrap", err)
	}
}

func TestSQLiteEncryption_EndToEnd_FTS5_Vec0_ZeroPlaintext(t *testing.T) {
	ctx := context.Background()
	if !CodecSupported(ctx) {
		t.Skip("requires -tags \"libsqlite3 fts5\" linked against SQLite3MultipleCiphers")
	}

	dir := t.TempDir()
	dbPath := filepath.Join(dir, "encrypted.sqlite")
	bs := mustLocalBlobStore(t, dir)
	p := mustMemoryKey(t, "k1")

	drv, err := New(dbPath, bs, WithKeyProvider(p))
	if err != nil {
		t.Fatalf("New encrypted: %v", err)
	}
	if err := drv.Init(ctx); err != nil {
		t.Fatalf("Init encrypted: %v", err)
	}

	const sentinel = "SQLITE-ENC-SECRET-SENTINEL-7741"
	now := time.Now().UTC().Truncate(time.Second)
	if err := drv.Objects().Create(ctx, &storage.KnowledgeObject{
		ID: "enc-obj-1", Type: "note",
		RawContent:  "Classified " + sentinel + " payload",
		TextContent: "Classified " + sentinel + " payload",
		Status:      storage.ObjectStatusActive, CreatedAt: now, UpdatedAt: now,
	}); err != nil {
		t.Fatalf("Create: %v", err)
	}

	verifyEncryptedFTS5AndVec0(t, drv, sentinel)

	snapPath, err := writeMigrateSnapshot(ctx, drv, SnapshotDir(dbPath), 65, 66, now)
	if err != nil || !strings.HasSuffix(snapPath, ".sqlite.enc") {
		t.Fatalf("writeMigrateSnapshot = (%q, %v)", snapPath, err)
	}
	assertNoPlaintextOnDisk(t, dbPath, sentinel)

	snapOut := filepath.Join(dir, "online-snap.sqlite")
	if err := SnapshotDatabase(ctx, dbPath, snapOut, p); err != nil {
		t.Fatalf("SnapshotDatabase: %v", err)
	}
	assertNoPlaintextOnDisk(t, snapOut, sentinel)

	if err := drv.Close(ctx); err != nil {
		t.Fatalf("Close: %v", err)
	}
	assertNoPlaintextOnDisk(t, dbPath, sentinel)
	verifyEncryptedAccessControls(t, dbPath, bs, p, sentinel)
}

func seedPlaintextDB(t *testing.T, dbPath string, bs storage.BlobStore, sentinel string) {
	t.Helper()
	ctx := t.Context()
	plainDrv, err := New(dbPath, bs)
	if err != nil {
		t.Fatal(err)
	}
	if err := plainDrv.Init(ctx); err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC().Truncate(time.Second)
	if err := plainDrv.Objects().Create(ctx, &storage.KnowledgeObject{
		ID: "m1", Type: "note", RawContent: sentinel, TextContent: sentinel,
		Status: storage.ObjectStatusActive, CreatedAt: now, UpdatedAt: now,
	}); err != nil {
		t.Fatal(err)
	}
	_ = plainDrv.Close(ctx)
	rawBefore, err := os.ReadFile(dbPath)
	if err != nil || !bytes.HasPrefix(rawBefore, []byte("SQLite format 3\x00")) {
		t.Fatalf("expected plaintext SQLite header before migration (err=%v)", err)
	}
}

func TestSQLiteEncryption_MigratePlaintextAndRekey(t *testing.T) {
	ctx := context.Background()
	if !CodecSupported(ctx) {
		t.Skip("requires -tags \"libsqlite3 fts5\" linked against SQLite3MultipleCiphers")
	}

	dir := t.TempDir()
	dbPath := filepath.Join(dir, "migrate.sqlite")
	bs := mustLocalBlobStore(t, dir)
	const sentinel = "MIGRATE-PLAIN-TO-ENC-SENTINEL-8852"
	seedPlaintextDB(t, dbPath, bs, sentinel)

	p1 := mustMemoryKey(t, "k1")
	migrated, err := EncryptDatabase(ctx, dbPath, p1)
	if err != nil || !migrated {
		t.Fatalf("EncryptDatabase = (%v, %v), want (true, nil)", migrated, err)
	}
	if migrated2, err := EncryptDatabase(ctx, dbPath, p1); err != nil || migrated2 {
		t.Fatalf("second EncryptDatabase = (%v, %v), want (false, nil)", migrated2, err)
	}
	assertNoPlaintextOnDisk(t, dbPath, sentinel)

	if _, err := p1.Rotate("k2"); err != nil {
		t.Fatal(err)
	}
	if _, err := RewrapDatabaseKey(ctx, dbPath, p1, "k2"); err != nil {
		t.Fatalf("RewrapDatabaseKey: %v", err)
	}
	if err := p1.Retire("k1"); err != nil {
		t.Fatal(err)
	}
	if err := RekeyDatabase(ctx, dbPath, p1); err != nil {
		t.Fatalf("RekeyDatabase: %v", err)
	}
	assertNoPlaintextOnDisk(t, dbPath, sentinel)

	encDrv, err := New(dbPath, bs, WithKeyProvider(p1))
	if err != nil {
		t.Fatalf("reopen after rekey: %v", err)
	}
	defer func() { _ = encDrv.Close(ctx) }()
	hits, err := encDrv.Objects().FTSSearch(ctx, sentinel, storage.ObjectFilter{Limit: 10})
	if err != nil || len(hits) != 1 || hits[0].ID != "m1" {
		t.Fatalf("FTSSearch after rekey = (%d hits, %v), want 1 (m1)", len(hits), err)
	}
}
