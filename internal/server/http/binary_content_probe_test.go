package http

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/ideacrafterslabs/ctxt/internal/dpkmsclient"
	"github.com/ideacrafterslabs/ctxt/internal/storage/blob"
	"github.com/ideacrafterslabs/ctxt/internal/storage/blobref"
)

// Probe: `ctxt capture <file>` sends string(os.ReadFile(...)) as the
// JSON content string. This drives the real client (dpkmsclient.Analyze)
// against the real handler, raw path, and compares the stored bytes.
func TestCaptureBinaryFileRoundTripsByteForByte(t *testing.T) {
	ts := newTestServerBundle(t)
	defer ts.Close()

	// PNG signature, then bytes that are not valid UTF-8.
	bin := []byte{0x89, 'P', 'N', 'G', 0x0d, 0x0a, 0x1a, 0x0a, 0xff, 0xfe, 0x00, 0x80, 0xc3, 0x28}

	c, err := dpkmsclient.New(dpkmsclient.Endpoint{URL: ts.URL})
	require.NoError(t, err)
	id, err := c.Analyze(context.Background(), dpkmsclient.AnalyzeRequest{
		Content: string(bin), Type: "text", Raw: true, Source: "/tmp/x.png",
	})
	require.NoError(t, err)

	obj, err := ts.svc.Store(ownerCtx()).Objects().Get(ownerCtx(), id)
	require.NoError(t, err)
	// Binary lives in the blob store (a TEXT column cannot hold it); the
	// row keeps the ref.
	require.True(t, blobref.Is(obj.RawContent), obj.RawContent)
	got, err := blob.Content(context.Background(), ts.svc.Store(ownerCtx()).Blobs(), obj.RawContent)
	require.NoError(t, err)
	require.Equal(t, bin, []byte(got), "stored bytes differ from the file's")
}

func postAnalyzeBody(t *testing.T, url, path string, body map[string]any) (int, ErrorEnvelope) {
	t.Helper()
	b, err := json.Marshal(body)
	require.NoError(t, err)
	resp, err := http.Post(url+path, "application/json", bytes.NewReader(b))
	require.NoError(t, err)
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	var env ErrorEnvelope
	_ = json.Unmarshal(raw, &env)
	return resp.StatusCode, env
}

// A wire body naming base64 is decoded before the object is stored.
func TestAnalyzeBase64ContentStoredDecoded(t *testing.T) {
	ts := newTestServerBundle(t)
	defer ts.Close()
	bin := []byte{0xff, 0xfe, 0x00, 0x80, 'a'}
	c, err := dpkmsclient.New(dpkmsclient.Endpoint{URL: ts.URL})
	require.NoError(t, err)
	id, err := c.Analyze(context.Background(), dpkmsclient.AnalyzeRequest{
		Content: base64.StdEncoding.EncodeToString(bin), ContentEncoding: "base64", Type: "text", Raw: true,
	})
	require.NoError(t, err)
	obj, err := ts.svc.Store(ownerCtx()).Objects().Get(ownerCtx(), id)
	require.NoError(t, err)
	got, err := blob.Content(context.Background(), ts.svc.Store(ownerCtx()).Blobs(), obj.RawContent)
	require.NoError(t, err)
	require.Equal(t, bin, []byte(got))
}

func TestAnalyzeRejectsBadContentEncoding(t *testing.T) {
	ts := newTestServerBundle(t)
	defer ts.Close()
	for _, path := range []string{"/api/v1/analyze", "/api/v1/pipelines/enqueue"} {
		for name, body := range map[string]map[string]any{
			"invalid base64":   {"content": "***", "content_encoding": "base64", "type": "text", "raw": true},
			"unknown encoding": {"content": "abc", "content_encoding": "rot13", "type": "text", "raw": true},
		} {
			status, env := postAnalyzeBody(t, ts.URL, path, body)
			require.Equal(t, http.StatusBadRequest, status, path+" "+name)
			require.Equal(t, "INVALID_REQUEST", env.Error.Code, path+" "+name)
		}
	}
}

// Text is sent as it always was: no content_encoding on the wire.
func TestAnalyzeTextHasNoContentEncodingOnWire(t *testing.T) {
	var got map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewDecoder(r.Body).Decode(&got)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusAccepted)
		_, _ = w.Write([]byte(`{"job_id":"j"}`))
	}))
	defer srv.Close()
	c, err := dpkmsclient.New(dpkmsclient.Endpoint{URL: srv.URL})
	require.NoError(t, err)
	_, err = c.Analyze(context.Background(), dpkmsclient.AnalyzeRequest{Content: "héllo", Type: "text"})
	require.NoError(t, err)
	require.Equal(t, "héllo", got["content"])
	_, present := got["content_encoding"]
	require.False(t, present, "valid UTF-8 must not carry content_encoding")
}
