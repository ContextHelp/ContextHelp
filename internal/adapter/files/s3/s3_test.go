package s3

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/ideacrafterslabs/ctxt/internal/adapter"
	"github.com/ideacrafterslabs/ctxt/internal/ingest"
)

// TestAdapterIdentity locks protocol+backend identifiers.
func TestAdapterIdentity(t *testing.T) {
	a := New(Config{BucketName: "b", Region: "us-east-1", CredentialsRef: "op://x/y"})
	if got := a.Protocol(); got != "files" {
		t.Errorf("Protocol = %q, want files", got)
	}
	if got := a.Backend(); got != "s3" {
		t.Errorf("Backend = %q, want s3", got)
	}
}

// TestAdapterDeclaresFetchOnly confirms capability honesty.
func TestAdapterDeclaresFetchOnly(t *testing.T) {
	a := New(Config{BucketName: "b", Region: "us-east-1", CredentialsRef: "op://x/y"})
	if !adapter.HasCapability(a, adapter.CapFetch) {
		t.Error("missing CapFetch")
	}
	if !adapter.HasCapability(a, adapter.CapEmitEvents) {
		t.Error("missing CapEmitEvents")
	}
	for _, c := range a.Capabilities() {
		if c == adapter.CapServe || c == adapter.CapSubmit {
			t.Errorf("unexpected capability %q on fetch-only sensor", c)
		}
	}
}

// TestAdapterRejectsUndeclaredCapabilities pins the substrate contract.
func TestAdapterRejectsUndeclaredCapabilities(t *testing.T) {
	a := New(Config{BucketName: "b", Region: "us-east-1", CredentialsRef: "op://x/y"})
	if err := a.Submit(context.Background(), ingest.Object{ID: "x"}); !errors.Is(err, adapter.ErrCapabilityNotDeclared) {
		t.Errorf("Submit: want ErrCapabilityNotDeclared, got %v", err)
	}
	if err := a.Serve(context.Background(), nil); !errors.Is(err, adapter.ErrCapabilityNotDeclared) {
		t.Errorf("Serve: want ErrCapabilityNotDeclared, got %v", err)
	}
}

// TestNewRejectsMissingConfig validates non-empty fields up front so
// operator typos in policy/ambient.yaml fail loud at registration
// rather than silently no-oping the sensor.
func TestNewRejectsMissingConfig(t *testing.T) {
	cases := []struct {
		name string
		cfg  Config
	}{
		{"missing bucket", Config{Region: "us-east-1", CredentialsRef: "op://x/y"}},
		{"missing region", Config{BucketName: "b", CredentialsRef: "op://x/y"}},
		{"missing credentials", Config{BucketName: "b", Region: "us-east-1"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			a := New(tc.cfg)
			if _, err := a.Fetch(context.Background()); err == nil {
				t.Errorf("Fetch with %s: want error, got nil", tc.name)
			}
		})
	}
}

// TestFetchListsObjects exercises the happy path against a fake S3
// endpoint. The httptest.Server returns a canned ListObjectsV2 XML
// response; the adapter should produce one ingest.Object per <Contents>
// entry. Credentials resolution is stubbed (the ref is recorded but
// not actually fetched in this PR per the brief).
func TestFetchListsObjects(t *testing.T) {
	const body = `<?xml version="1.0" encoding="UTF-8"?>
<ListBucketResult xmlns="http://s3.amazonaws.com/doc/2006-03-01/">
  <Name>my-bucket</Name>
  <Prefix>papers/</Prefix>
  <KeyCount>2</KeyCount>
  <MaxKeys>1000</MaxKeys>
  <IsTruncated>false</IsTruncated>
  <Contents>
    <Key>papers/one.pdf</Key>
    <LastModified>2026-05-05T10:00:00.000Z</LastModified>
    <ETag>"abc123"</ETag>
    <Size>1024</Size>
    <StorageClass>STANDARD</StorageClass>
  </Contents>
  <Contents>
    <Key>papers/two.pdf</Key>
    <LastModified>2026-05-05T11:00:00.000Z</LastModified>
    <ETag>"def456"</ETag>
    <Size>2048</Size>
    <StorageClass>STANDARD</StorageClass>
  </Contents>
</ListBucketResult>`
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/xml")
		_, _ = w.Write([]byte(body))
	}))
	defer srv.Close()

	a := New(Config{
		BucketName:     "my-bucket",
		Region:         "us-east-1",
		Endpoint:       srv.URL,
		CredentialsRef: "op://Personal/aws-test",
		Prefix:         "papers/",
	})
	objs, err := a.Fetch(context.Background())
	if err != nil {
		t.Fatalf("Fetch: %v", err)
	}
	if len(objs) != 2 {
		t.Fatalf("Fetch: got %d objects, want 2", len(objs))
	}
	if objs[0].Type != "s3-object" {
		t.Errorf("Type = %q, want s3-object", objs[0].Type)
	}
	if got, _ := objs[0].Metadata["key"].(string); got != "papers/one.pdf" {
		t.Errorf("Metadata[key] = %q, want papers/one.pdf", got)
	}
	if got, _ := objs[0].Metadata["bucket"].(string); got != "my-bucket" {
		t.Errorf("Metadata[bucket] = %q, want my-bucket", got)
	}
}
