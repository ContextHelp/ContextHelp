package local

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/ideacrafterslabs/ctxt/internal/ambient"
	"github.com/ideacrafterslabs/ctxt/internal/keys"
)

func TestEncryptedAmbientSpool_ZeroPlaintextAndWrongKeyFailsClosed(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	p, err := keys.NewMemory("k1")
	if err != nil {
		t.Fatal(err)
	}
	buf, err := New(Config{
		RootDir:     dir,
		KeyProvider: p,
		Now:         func() time.Time { return time.Date(2026, 11, 24, 12, 0, 0, 0, time.UTC) },
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	const sentinel = "AMBIENT-SPOOL-SECRET-PAYLOAD-4455"
	ev := ambient.RawEvent{
		Source:      "window",
		Fingerprint: "fp-1",
		OccurredAt:  time.Date(2026, 11, 24, 12, 0, 0, 0, time.UTC),
		Payload:     []byte(`{"title":"` + sentinel + `"}`),
	}
	if err := buf.Append(ctx, ev); err != nil {
		t.Fatalf("Append: %v", err)
	}

	// Raw filesystem scan: sentinel must never appear on disk.
	err = filepath.WalkDir(dir, func(path string, d os.DirEntry, werr error) error {
		if werr != nil || d.IsDir() {
			return werr
		}
		raw, rerr := os.ReadFile(path)
		if rerr != nil {
			return rerr
		}
		if bytes.Contains(raw, []byte(sentinel)) {
			t.Fatalf("plaintext ambient sentinel found on disk in %s", path)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("WalkDir: %v", err)
	}

	// Wrong key fails closed on Range & Pop.
	wrongP, _ := keys.NewMemory("k1")
	wrongBuf, err := New(Config{RootDir: dir, KeyProvider: wrongP})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := wrongBuf.Pop(ctx); !errors.Is(err, keys.ErrUnwrap) {
		t.Fatalf("Pop with wrong key err = %v, want ErrUnwrap", err)
	}

	// Right key pops intact event.
	got, err := buf.Pop(ctx)
	if err != nil {
		t.Fatalf("Pop: %v", err)
	}
	if !bytes.Contains(got.Payload, []byte(sentinel)) {
		t.Fatalf("Pop payload = %q, want sentinel", got.Payload)
	}
}
