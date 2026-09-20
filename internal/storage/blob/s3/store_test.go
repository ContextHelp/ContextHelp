package s3

import (
	"context"
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
