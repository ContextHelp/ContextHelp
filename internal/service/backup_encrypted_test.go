package service

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
	"github.com/ideacrafterslabs/ctxt/internal/storage/sqlite"
)

func TestBackup_MandatoryEncryptionAndZeroPlaintext(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "db.sqlite")
	bs, err := local.New(filepath.Join(dir, "blobs"))
	if err != nil {
		t.Fatal(err)
	}
	drv, err := sqlite.New(dbPath, bs)
	if err != nil {
		t.Fatal(err)
	}
	if err := drv.Init(ctx); err != nil {
		t.Fatal(err)
	}
	const sentinel = "BACKUP-PLAINTEXT-SENTINEL-6612"
	now := time.Now().UTC().Truncate(time.Second)
	if err := drv.Objects().Create(ctx, &storage.KnowledgeObject{
		ID: "b1", Type: "note", RawContent: sentinel, TextContent: sentinel,
		Status: storage.ObjectStatusActive, CreatedAt: now, UpdatedAt: now,
	}); err != nil {
		t.Fatal(err)
	}
	_ = drv.Close(ctx)

	// EncryptionEnabled without KeyProvider must fail closed.
	outDir1 := filepath.Join(dir, "out1")
	if _, err := Backup(ctx, BackupOpts{
		DBPath:            dbPath,
		OutputDir:         outDir1,
		EncryptionEnabled: true,
	}); !errors.Is(err, keys.ErrUnavailable) {
		t.Fatalf("Backup without KeyProvider err = %v, want ErrUnavailable", err)
	}

	// With KeyProvider, produces .tar.gz.enc with zero plaintext on disk.
	p, err := keys.NewMemory("k1")
	if err != nil {
		t.Fatal(err)
	}
	outDir2 := filepath.Join(dir, "out2")
	res, err := Backup(ctx, BackupOpts{
		DBPath:            dbPath,
		OutputDir:         outDir2,
		EncryptionEnabled: true,
		KeyProvider:       p,
	})
	if err != nil {
		t.Fatalf("Backup encrypted: %v", err)
	}
	if !strings.HasSuffix(res.Path, ".tar.gz.enc") {
		t.Fatalf("backup path = %q, want .tar.gz.enc suffix", res.Path)
	}
	raw, err := os.ReadFile(res.Path)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(raw, []byte(sentinel)) || bytes.Contains(raw, []byte("SQLite format 3")) {
		t.Fatal("encrypted backup contains plaintext sentinel or SQLite header")
	}

	// Wrong key refuses decryption.
	wrongP, _ := keys.NewMemory("k1")
	if _, err := DecryptBackup(ctx, res.Path, wrongP); !errors.Is(err, keys.ErrUnwrap) {
		t.Fatalf("DecryptBackup wrong key err = %v, want ErrUnwrap", err)
	}

	// Right key decrypts valid gzip archive.
	plainTarGz, err := DecryptBackup(ctx, res.Path, p)
	if err != nil {
		t.Fatalf("DecryptBackup: %v", err)
	}
	if len(plainTarGz) < 2 || plainTarGz[0] != 0x1f || plainTarGz[1] != 0x8b {
		t.Fatalf("decrypted backup does not start with gzip magic")
	}
}
