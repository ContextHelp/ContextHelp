package local

import (
	"bytes"
	"context"
	"errors"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"

	"github.com/ideacrafterslabs/ctxt/internal/storage"
)

// failingReader yields n bytes of data, then err.
type failingReader struct {
	data []byte
	err  error
}

func (r *failingReader) Read(p []byte) (int, error) {
	if len(r.data) == 0 {
		return 0, r.err
	}
	n := copy(p, r.data)
	r.data = r.data[n:]
	return n, nil
}

// listAll returns every regular file path under root, relative.
func listAll(t *testing.T, root string) []string {
	t.Helper()
	var out []string
	err := filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !d.IsDir() {
			rel, _ := filepath.Rel(root, p)
			out = append(out, rel)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return out
}

func TestPutMidCopyErrorLeavesNothing(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	boom := errors.New("boom")
	key := "abcd1234"

	err := s.Put(ctx, key, &failingReader{data: bytes.Repeat([]byte("x"), 4096), err: boom}, storage.BlobMeta{})
	if !errors.Is(err, boom) {
		t.Fatalf("Put err = %v, want boom", err)
	}
	if ok, _ := s.Exists(ctx, key); ok {
		t.Fatal("Exists true after failed Put")
	}
	if files := listAll(t, s.root); len(files) != 0 {
		t.Fatalf("residue after failed Put: %v", files)
	}
	items, err := s.List(ctx, "")
	if err != nil || len(items) != 0 {
		t.Fatalf("List = %v, %v; want empty", items, err)
	}
}

func TestPutMidCopyErrorKeepsPreviousVersion(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	key := "abcd1234"
	if err := s.Put(ctx, key, strings.NewReader("old"), storage.BlobMeta{}); err != nil {
		t.Fatal(err)
	}
	err := s.Put(ctx, key, &failingReader{data: []byte("partial"), err: io.ErrUnexpectedEOF}, storage.BlobMeta{})
	if err == nil {
		t.Fatal("want error")
	}
	rc, _, err := s.Get(ctx, key)
	if err != nil {
		t.Fatal(err)
	}
	defer rc.Close()
	got, _ := io.ReadAll(rc)
	if string(got) != "old" {
		t.Fatalf("got %q, want old", got)
	}
}

func TestPutHappyPathNoResidue(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	key := "abcd1234"
	meta := storage.BlobMeta{ContentType: "text/plain", Size: 5, Filename: "a.txt"}
	if err := s.Put(ctx, key, strings.NewReader("hello"), meta); err != nil {
		t.Fatal(err)
	}
	rc, gotMeta, err := s.Get(ctx, key)
	if err != nil {
		t.Fatal(err)
	}
	defer rc.Close()
	got, _ := io.ReadAll(rc)
	if string(got) != "hello" || gotMeta.ContentType != meta.ContentType || gotMeta.Filename != meta.Filename || gotMeta.Size != meta.Size {
		t.Fatalf("round trip: %q %+v", got, gotMeta)
	}
	files := listAll(t, s.root)
	if len(files) != 2 {
		t.Fatalf("want blob + meta only, got %v", files)
	}
	for _, f := range files {
		if strings.Contains(f, ".tmp") {
			t.Fatalf("temp residue: %s", f)
		}
	}
	st, err := os.Stat(s.blobPath(key))
	if err != nil {
		t.Fatal(err)
	}
	if runtime.GOOS != "windows" && st.Mode().Perm() != filePerm {
		t.Fatalf("mode %v, want %v", st.Mode().Perm(), os.FileMode(filePerm))
	}
}

// A failing meta write (final meta path is a non-empty directory so the
// rename cannot land) fails Put and leaves no temp file. The blob is
// committed first, whole.
func TestPutMetaWriteFailure(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	key := "abcd1234"
	mp := s.metaPath(key)
	if err := os.MkdirAll(filepath.Join(mp, "sub"), dirPerm); err != nil {
		t.Fatal(err)
	}
	err := s.Put(ctx, key, strings.NewReader("hello"), storage.BlobMeta{ContentType: "x"})
	if err == nil {
		t.Fatal("want meta write error")
	}
	for _, f := range listAll(t, s.root) {
		if strings.Contains(f, ".tmp") {
			t.Fatalf("temp residue: %s", f)
		}
	}
}

func TestListIgnoresTempFiles(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	if err := s.Put(ctx, "abcd1234", strings.NewReader("hello"), storage.BlobMeta{}); err != nil {
		t.Fatal(err)
	}
	dir := filepath.Dir(s.blobPath("abcd1234"))
	for _, n := range []string{".abcd1234.tmp-deadbeef", ".abcd1234.meta.json.tmp-deadbeef"} {
		if err := os.WriteFile(filepath.Join(dir, n), []byte("junk"), filePerm); err != nil {
			t.Fatal(err)
		}
	}
	items, err := s.List(ctx, "")
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 1 || items[0].Key != "abcd1234" {
		t.Fatalf("List = %+v", items)
	}
}

func TestPutConcurrentSameKey(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	key := "abcd1234"
	const n = 16
	var wg sync.WaitGroup
	for i := range n {
		wg.Add(1)
		go func() {
			defer wg.Done()
			body := bytes.Repeat([]byte{byte('a' + i)}, 64*1024)
			if err := s.Put(ctx, key, bytes.NewReader(body), storage.BlobMeta{Size: int64(len(body))}); err != nil {
				t.Errorf("put %d: %v", i, err)
			}
		}()
	}
	wg.Wait()
	rc, meta, err := s.Get(ctx, key)
	if err != nil {
		t.Fatal(err)
	}
	defer rc.Close()
	got, _ := io.ReadAll(rc)
	if len(got) != 64*1024 || meta.Size != int64(len(got)) {
		t.Fatalf("len %d meta %d", len(got), meta.Size)
	}
	for _, b := range got {
		if b != got[0] {
			t.Fatal("mixed content from concurrent puts")
		}
	}
	if files := listAll(t, s.root); len(files) != 2 {
		t.Fatalf("residue: %v", files)
	}
}
