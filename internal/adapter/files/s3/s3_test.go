package s3

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"testing"

	xrr "hop.top/xrr"
	xhttp "hop.top/xrr/adapters/http"

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

// TestFetchListsObjects exercises the happy path against a recorded
// xrr cassette. Recording mode (XRR_RECORD=1) hits an in-process
// httptest fake serving canned ListBucketResult XML; replay mode runs
// the AWS SDK against the cassette without any network. The cassette
// is sanitized — bucket name, region, and credentials ref are
// fictional placeholders so the artifact can be committed.
//
// TODO: record xrr cassette against real S3 when test creds are wired.
// The current cassette stands in for the real-world recording — it
// captures the ListObjectsV2 wire shape exactly but is sourced from
// an in-process fixture rather than AWS.
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

	httpClient, endpoint := newCassetteS3Client(t, "cassettes/list_objects", body)

	a := New(Config{
		BucketName:     "my-bucket",
		Region:         "us-east-1",
		Endpoint:       endpoint,
		CredentialsRef: "op://Personal/aws-test",
		Prefix:         "papers/",
		HTTPClient:     httpClient,
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

// newCassetteS3Client returns an awsHTTPClient that drives the AWS SDK
// through an xrr session, plus a stable Endpoint string the SDK uses
// to build URLs. Cassette fingerprints key off the stable endpoint so
// they replay deterministically across machines.
func newCassetteS3Client(t *testing.T, cassetteDir, body string) (awsHTTPClient, string) {
	t.Helper()

	const stableEndpoint = "https://s3.test"

	mode := xrr.ModeReplay
	if os.Getenv("XRR_RECORD") != "" {
		mode = xrr.ModeRecord
		if err := os.MkdirAll(cassetteDir, 0o755); err != nil {
			t.Fatalf("mkdir cassettes: %v", err)
		}
	}
	sess := xrr.NewSession(mode, xrr.NewFileCassette(cassetteDir))
	httpAdapter := xhttp.NewAdapter()

	var liveSrv *httptest.Server
	if mode == xrr.ModeRecord {
		liveSrv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Content-Type", "application/xml")
			_, _ = w.Write([]byte(body))
		}))
		t.Cleanup(liveSrv.Close)
	}

	return clientFunc(func(req *http.Request) (*http.Response, error) {
		// Rewrite the request URL to the stable endpoint so the
		// fingerprint is the same in record and replay runs even when
		// the AWS SDK switches its build-time host or port.
		stable, _ := url.Parse(stableEndpoint)
		stableURL := *req.URL
		stableURL.Scheme = stable.Scheme
		stableURL.Host = stable.Host

		var bodyBytes []byte
		if req.Body != nil {
			b, err := io.ReadAll(req.Body)
			if err != nil {
				return nil, err
			}
			bodyBytes = b
			_ = req.Body.Close()
		}
		xreq := &xhttp.Request{
			Method: req.Method,
			URL:    stableURL.String(),
			Body:   string(bodyBytes),
		}

		do := func() (xrr.Response, error) {
			liveURL := liveSrv.URL + req.URL.Path
			if req.URL.RawQuery != "" {
				liveURL += "?" + req.URL.RawQuery
			}
			liveReq, err := http.NewRequestWithContext(req.Context(), req.Method, liveURL, strings.NewReader(string(bodyBytes)))
			if err != nil {
				return nil, err
			}
			for k, vs := range req.Header {
				for _, v := range vs {
					liveReq.Header.Add(k, v)
				}
			}
			httpResp, err := http.DefaultTransport.RoundTrip(liveReq)
			if err != nil {
				return nil, err
			}
			defer httpResp.Body.Close()
			respBody, err := io.ReadAll(httpResp.Body)
			if err != nil {
				return nil, err
			}
			headers := map[string]string{}
			for k, vs := range httpResp.Header {
				if len(vs) > 0 {
					headers[k] = vs[0]
				}
			}
			return &xhttp.Response{
				Status:  httpResp.StatusCode,
				Headers: headers,
				Body:    string(respBody),
			}, nil
		}

		resp, err := sess.Record(req.Context(), httpAdapter, xreq, do)
		if err != nil {
			return nil, err
		}
		return xrrResponseToHTTP(resp)
	}), stableEndpoint
}

// xrrResponseToHTTP translates either an *xhttp.Response (record mode)
// or an *xrr.RawResponse (replay mode) into a real *http.Response the
// AWS SDK can consume.
func xrrResponseToHTTP(resp xrr.Response) (*http.Response, error) {
	var (
		status  int
		headers map[string]string
		body    string
	)
	switch r := resp.(type) {
	case *xhttp.Response:
		status, headers, body = r.Status, r.Headers, r.Body
	case *xrr.RawResponse:
		if v, ok := r.Payload["status"].(int); ok {
			status = v
		}
		if v, ok := r.Payload["body"].(string); ok {
			body = v
		}
		if h, ok := r.Payload["headers"].(map[string]any); ok {
			headers = make(map[string]string, len(h))
			for k, v := range h {
				if s, ok := v.(string); ok {
					headers[k] = s
				}
			}
		}
	default:
		return nil, errors.New("s3: unsupported xrr response type")
	}
	hdr := http.Header{}
	for k, v := range headers {
		hdr.Set(k, v)
	}
	return &http.Response{
		StatusCode: status,
		Header:     hdr,
		Body:       io.NopCloser(strings.NewReader(body)),
	}, nil
}

// clientFunc lets a function value satisfy awsHTTPClient.
type clientFunc func(*http.Request) (*http.Response, error)

func (f clientFunc) Do(r *http.Request) (*http.Response, error) { return f(r) }
