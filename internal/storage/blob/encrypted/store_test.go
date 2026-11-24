package encrypted

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ideacrafterslabs/ctxt/internal/keys"
	"github.com/ideacrafterslabs/ctxt/internal/storage"
	"github.com/ideacrafterslabs/ctxt/internal/storage/blob/local"
)

func assertNoPlaintextOnDisk(t *testing.T, dir string, needles ...string) {
	t.Helper()
	err := filepath.WalkDir(dir, func(path string, d os.DirEntry, werr error) error {
		if werr != nil || d.IsDir() {
			return werr
		}
		raw, rerr := os.ReadFile(path)
		if rerr != nil {
			return rerr
		}
		for _, needle := range needles {
			if bytes.Contains(raw, []byte(needle)) {
				t.Fatalf("plaintext %q found on disk in %s", needle, path)
			}
		}
		return nil
	})
	if err != nil {
		t.Fatalf("WalkDir: %v", err)
	}
}

func readBlobString(ctx context.Context, t *testing.T, s storage.BlobStore, key string) (string, storage.BlobMeta) {
	t.Helper()
	rc, meta, err := s.Get(ctx, key)
	if err != nil {
		t.Fatalf("Get(%s): %v", key, err)
	}
	b, err := io.ReadAll(rc)
	_ = rc.Close()
	if err != nil {
		t.Fatalf("ReadAll(%s): %v", key, err)
	}
	return string(b), meta
}

func TestEncryptedBlobStore_RoundtripByteScanAndWrongKey(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	inner, err := local.New(dir)
	if err != nil {
		t.Fatalf("local.New: %v", err)
	}
	p, err := keys.NewMemory("k1")
	if err != nil {
		t.Fatalf("NewMemory: %v", err)
	}
	store, err := New(inner, p)
	if err != nil {
		t.Fatalf("encrypted.New: %v", err)
	}

	key1 := strings.Repeat("1", 64)
	keySwap := strings.Repeat("2", 64)
	const sentinel = "SUPER-SECRET-BLOB-PAYLOAD-SENTINEL-7711"
	const metaSentinel = "confidential-filename-7711.pdf"

	if err := store.Put(ctx, key1, strings.NewReader(sentinel), storage.BlobMeta{
		ContentType: "application/pdf",
		Filename:    metaSentinel,
	}); err != nil {
		t.Fatalf("Put: %v", err)
	}

	assertNoPlaintextOnDisk(t, dir, sentinel, metaSentinel)

	gotBody, gotMeta := readBlobString(ctx, t, store, key1)
	if gotBody != sentinel || gotMeta.Filename != metaSentinel || gotMeta.ContentType != "application/pdf" {
		t.Fatalf("Get = (%q, %+v)", gotBody, gotMeta)
	}

	// Cross-key swap fails AAD check.
	rawCT, _ := readBlobString(ctx, t, inner, key1)
	if err := inner.Put(ctx, keySwap, strings.NewReader(rawCT), storage.BlobMeta{}); err != nil {
		t.Fatal(err)
	}
	if _, _, err := store.Get(ctx, keySwap); !errors.Is(err, keys.ErrUnwrap) {
		t.Fatalf("swapped key err = %v, want ErrUnwrap", err)
	}

	// Wrong KEK provider fails closed.
	wrongP, _ := keys.NewMemory("k1")
	wrongStore, _ := New(inner, wrongP)
	if _, _, err := wrongStore.Get(ctx, key1); !errors.Is(err, keys.ErrUnwrap) {
		t.Fatalf("wrong key err = %v, want ErrUnwrap", err)
	}
}

func TestEncryptedBlobStore_RotateAndMigratePlaintext(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	inner, err := local.New(dir)
	if err != nil {
		t.Fatal(err)
	}
	p, err := keys.NewMemory("k1")
	if err != nil {
		t.Fatal(err)
	}
	store, err := New(inner, p)
	if err != nil {
		t.Fatal(err)
	}

	key1 := strings.Repeat("a", 64)
	keyLegacy := strings.Repeat("b", 64)
	const sentinel = "ROTATE-SENTINEL-1122"
	const legacySentinel = "LEGACY-PLAINTEXT-BLOB-8899"

	if err := store.Put(ctx, key1, strings.NewReader(sentinel), storage.BlobMeta{ContentType: "text/plain"}); err != nil {
		t.Fatal(err)
	}
	if _, err := p.Rotate("k2"); err != nil {
		t.Fatal(err)
	}
	if n, err := RewrapAll(ctx, inner, p, "k2"); err != nil || n != 1 {
		t.Fatalf("RewrapAll = (%d, %v), want (1, nil)", n, err)
	}
	if err := p.Retire("k1"); err != nil {
		t.Fatal(err)
	}
	if got, _ := readBlobString(ctx, t, store, key1); got != sentinel {
		t.Fatalf("body after rotate = %q", got)
	}

	if err := inner.Put(ctx, keyLegacy, strings.NewReader(legacySentinel), storage.BlobMeta{ContentType: "text/plain"}); err != nil {
		t.Fatal(err)
	}
	if _, _, err := store.Get(ctx, keyLegacy); !errors.Is(err, keys.ErrUnwrap) {
		t.Fatalf("plaintext read before migration err = %v, want ErrUnwrap", err)
	}
	if migrated, err := MigratePlaintext(ctx, inner, p); err != nil || migrated != 1 {
		t.Fatalf("MigratePlaintext = (%d, %v), want (1, nil)", migrated, err)
	}
	assertNoPlaintextOnDisk(t, dir, legacySentinel)
	if got, _ := readBlobString(ctx, t, store, keyLegacy); got != legacySentinel {
		t.Fatalf("legacy body = %q", got)
	}
}
