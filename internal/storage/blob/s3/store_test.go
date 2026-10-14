package s3

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/ideacrafterslabs/ctxt/internal/config"
	"github.com/ideacrafterslabs/ctxt/internal/storage"
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

// Get pins the not-found half of the BlobStore contract: S3's
// NoSuchKey is storage.ErrBlobNotFound; any other failure is an error
// that is not, so callers can tell a blob that is gone from a store
// they could not read. A bare 404 names no key: S3, MinIO and Garage
// all answer a missing key with NoSuchKey, so a bare one is more likely
// a misrouted endpoint than a lost blob.
func TestGetNotFound(t *testing.T) {
	const noSuchKey = `<?xml version="1.0" encoding="UTF-8"?>
<Error><Code>NoSuchKey</Code><Message>The specified key does not exist.</Message></Error>`
	for _, tc := range []struct {
		name     string
		status   int
		body     string
		notFound bool
	}{
		{"NoSuchKey", http.StatusNotFound, noSuchKey, true},
		{"bare 404", http.StatusNotFound, "", false},
		{"access denied", http.StatusForbidden,
			`<Error><Code>AccessDenied</Code><Message>Access Denied</Message></Error>`, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				if tc.body != "" {
					w.Header().Set("Content-Type", "application/xml")
				}
				w.WriteHeader(tc.status)
				fmt.Fprint(w, tc.body)
			}))
			defer srv.Close()

			_, _, err := newTestStore(t, srv).Get(context.Background(), "abcdef1234")
			if err == nil {
				t.Fatal("Get: no error")
			}
			if got := errors.Is(err, storage.ErrBlobNotFound); got != tc.notFound {
				t.Errorf("errors.Is(err, ErrBlobNotFound) = %v, want %v; err = %v", got, tc.notFound, err)
			}
		})
	}
}
