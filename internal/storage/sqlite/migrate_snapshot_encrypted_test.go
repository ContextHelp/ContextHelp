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

func TestMigrateSnapshot_EncryptedAtRest(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "db.sqlite")
	bs, err := local.New(filepath.Join(dir, "blobs"))
	if err != nil {
		t.Fatal(err)
	}
	p, err := keys.NewMemory("k1")
	if err != nil {
		t.Fatal(err)
	}
	d, err := New(dbPath, bs)
	if err != nil {
		t.Fatal(err)
	}
	if err := d.Init(ctx); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = d.Close(ctx) }()
	d.keyProvider = p

	const sentinel = "MIGRATE-SNAPSHOT-SECRET-SENTINEL-9081"
	now := time.Now().UTC().Truncate(time.Second)
	if err := d.Objects().Create(ctx, &storage.KnowledgeObject{
		ID: "s1", Type: "note", RawContent: sentinel, TextContent: sentinel,
		Status: storage.ObjectStatusActive, CreatedAt: now, UpdatedAt: now,
	}); err != nil {
		t.Fatal(err)
	}

	snapDir := SnapshotDir(dbPath)
	snapPath, err := writeMigrateSnapshot(ctx, d, snapDir, 64, 65, now)
	if err != nil {
		t.Fatalf("writeMigrateSnapshot: %v", err)
	}
	if !strings.HasSuffix(snapPath, ".sqlite.enc") {
		t.Fatalf("snapshot path = %q, want .sqlite.enc", snapPath)
	}

	// Raw disk scan: zero plaintext and no leftover .partial files.
	entries, err := os.ReadDir(snapDir)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if strings.HasSuffix(e.Name(), ".partial") {
			t.Fatalf("leftover partial snapshot file: %s", e.Name())
		}
		raw, rerr := os.ReadFile(filepath.Join(snapDir, e.Name()))
		if rerr != nil {
			t.Fatal(rerr)
		}
		if bytes.Contains(raw, []byte(sentinel)) || bytes.Contains(raw, []byte("SQLite format 3")) {
			t.Fatalf("plaintext leaked into snapshot file %s", e.Name())
		}
	}

	// Wrong key fails closed.
	wrongP, _ := keys.NewMemory("k1")
	if _, err := ReadMigrateSnapshot(ctx, snapPath, wrongP); !errors.Is(err, keys.ErrUnwrap) {
		t.Fatalf("ReadMigrateSnapshot wrong key err = %v, want ErrUnwrap", err)
	}

	// Right key recovers valid SQLite database containing sentinel.
	plain, err := ReadMigrateSnapshot(ctx, snapPath, p)
	if err != nil {
		t.Fatalf("ReadMigrateSnapshot: %v", err)
	}
	if !bytes.HasPrefix(plain, []byte("SQLite format 3\x00")) || !bytes.Contains(plain, []byte(sentinel)) {
		t.Fatal("decrypted snapshot did not contain expected SQLite header and sentinel")
	}
}
