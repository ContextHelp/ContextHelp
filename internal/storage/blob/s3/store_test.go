package s3

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/ideacrafterslabs/ctxt/internal/config"
	"github.com/ideacrafterslabs/ctxt/internal/storage"
	"github.com/ideacrafterslabs/ctxt/internal/storage/blob/blobtest"
)

func TestNewMissingBucket(t *testing.T) {
	cfg := config.BlobS3Config{
		Region: "us-east-1",
	}
	_, err := New(cfg)
	if err == nil {
		t.Fatal("expected error for missing bucket")
	}
}

func TestKeyPath(t *testing.T) {
	s := &Store{prefix: "data"}
	got := s.keyPath("abcdef1234")
	want := "data/ab/cd/abcdef1234"
	if got != want {
		t.Errorf("keyPath: got %q, want %q", got, want)
	}
}

func TestKeyPathNoPrefix(t *testing.T) {
	s := &Store{}
	got := s.keyPath("abcdef1234")
	want := "ab/cd/abcdef1234"
	if got != want {
		t.Errorf("keyPath: got %q, want %q", got, want)
	}
}

var _ storage.BlobStore = (*Store)(nil)

// newTestStore builds a Store pointed at srv, using path-style addressing so
// the bucket lands in the URL path rather than the hostname.
func newTestStore(t *testing.T, srv *httptest.Server) *Store {
	t.Helper()
	s, err := New(config.BlobS3Config{
		Bucket:       "test-bucket",
		Region:       "us-east-1",
		AccessKey:    "key",
		SecretKey:    "secret",
		Endpoint:     srv.URL,
		UsePathStyle: true,
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return s
}

// TestExistsNotFound pins the "object absent" half of the BlobStore contract:
// a 404 from HeadObject is not an error, it is a definitive "no".
func TestExistsNotFound(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer srv.Close()

	ok, err := newTestStore(t, srv).Exists(context.Background(), "abcdef1234")
	if err != nil {
		t.Fatalf("404 must not surface as an error: %v", err)
	}
	if ok {
		t.Error("Exists = true for a missing object, want false")
	}
}

// TestExistsPresent pins the positive case.
func TestExistsPresent(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	ok, err := newTestStore(t, srv).Exists(context.Background(), "abcdef1234")
	if err != nil {
		t.Fatalf("Exists: %v", err)
	}
	if !ok {
		t.Error("Exists = false for a present object, want true")
	}
}

// TestExistsAccessDeniedIsError is the regression guard. A 403 means the
// bucket is unreachable for us, NOT that the object is absent. Reporting
// (false, nil) here tells callers "no such blob", which invites overwriting
// or re-uploading live data. It must surface as an error, mirroring the
// local store's behavior for a non-ErrNotExist stat failure.
func TestExistsAccessDeniedIsError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusForbidden)
	}))
	defer srv.Close()

	ok, err := newTestStore(t, srv).Exists(context.Background(), "abcdef1234")
	if err == nil {
		t.Fatal("access-denied must surface as an error, got (false, nil) — " +
			"callers cannot distinguish it from a genuinely missing object")
	}
	if ok {
		t.Error("Exists = true alongside an error, want false")
	}
}

// TestListFollowsContinuation pins that List returns every page of a
// truncated ListObjectsV2 listing, not just the first (S3 caps a page at
// 1000 keys). The orphan-blob sweep relies on seeing every key.
func TestListFollowsContinuation(t *testing.T) {
	const page = `<?xml version="1.0" encoding="UTF-8"?>
<ListBucketResult xmlns="http://s3.amazonaws.com/doc/2006-03-01/">
<Name>test-bucket</Name><IsTruncated>%s</IsTruncated>%s
<Contents><Key>%s</Key><Size>3</Size><LastModified>2026-01-02T03:04:05.000Z</LastModified></Contents>
<Contents><Key>%s.meta.json</Key><Size>2</Size><LastModified>2026-01-02T03:04:05.000Z</LastModified></Contents>
</ListBucketResult>`
	var calls int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		w.Header().Set("Content-Type", "application/xml")
		if r.URL.Query().Get("continuation-token") == "" {
			fmt.Fprintf(w, page, "true", "<NextContinuationToken>next</NextContinuationToken>", "ab/cd/abcdfirst", "ab/cd/abcdfirst")
			return
		}
		fmt.Fprintf(w, page, "false", "", "ef/01/ef01second", "ef/01/ef01second")
	}))
	defer srv.Close()

	items, err := newTestStore(t, srv).List(context.Background(), "")
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	var keys []string
	for _, it := range items {
		keys = append(keys, it.Key)
	}
	if len(keys) != 2 || keys[0] != "abcdfirst" || keys[1] != "ef01second" {
		t.Errorf("List keys = %v, want [abcdfirst ef01second] (calls: %d)", keys, calls)
	}
}

// TestListConformance runs the backend-shared List(prefix) contract
// against the in-process fake, with and without a store prefix.
func TestListConformance(t *testing.T) {
	t.Run("no store prefix", func(t *testing.T) {
		_, s := newFakeS3(t, "test-bucket", "")
		blobtest.ListConformance(t, s)
	})
	t.Run("store prefix", func(t *testing.T) {
		f, s := newFakeS3(t, "test-bucket", "data")
		// Another prefix sharing the store prefix's first characters
		// holds none of this store's blobs.
		f.put("data2/ab/cd/abcdother", []byte("not ours"))
		f.put("data2/ab9", []byte("not ours"))
		blobtest.ListConformance(t, s)
	})
}

// TestFakeRoundTrip checks the fake against the store's own calls, so
// a List failure is List's and not the fake's.
func TestFakeRoundTrip(t *testing.T) {
	ctx := context.Background()
	f, s := newFakeS3(t, "test-bucket", "")
	if err := s.Put(ctx, "abcdef01", strings.NewReader("payload"), storage.BlobMeta{ContentType: "text/plain"}); err != nil {
		t.Fatalf("Put: %v", err)
	}
	if _, ok := f.objects["ab/cd/abcdef01"]; !ok {
		t.Fatalf("Put stored %v, want ab/cd/abcdef01 and its meta", f.objects)
	}
	rc, meta, err := s.Get(ctx, "abcdef01")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	got, _ := io.ReadAll(rc)
	rc.Close()
	if string(got) != "payload" || meta.ContentType != "text/plain" {
		t.Errorf("Get = %q, %+v", got, meta)
	}
	if ok, err := s.Exists(ctx, "abcdef01"); err != nil || !ok {
		t.Errorf("Exists = %v, %v", ok, err)
	}
	if err := s.Delete(ctx, "abcdef01"); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if ok, err := s.Exists(ctx, "abcdef01"); err != nil || ok {
		t.Errorf("Exists after Delete = %v, %v", ok, err)
	}
}
