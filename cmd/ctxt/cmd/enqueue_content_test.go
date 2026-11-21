package cmd

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/ideacrafterslabs/ctxt/internal/contentenc"
	"github.com/ideacrafterslabs/ctxt/internal/dpkmsclient"
)

// The importers' single enqueue path sends binary content base64-encoded
// with the encoding named, and text as it is.
func TestEnqueueContentEncodesBinary(t *testing.T) {
	var body map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewDecoder(r.Body).Decode(&body)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusAccepted)
		_, _ = w.Write([]byte(`{"job_id":"j-1"}`))
	}))
	defer srv.Close()
	dc, err := dpkmsclient.New(dpkmsclient.Endpoint{URL: srv.URL})
	require.NoError(t, err)

	bin := []byte{0x89, 'P', 'N', 'G', 0x00, 0xff, 0xfe}
	_, err = enqueueContent(context.Background(), dc, string(bin), objectTypeText, "p", "import:x")
	require.NoError(t, err)
	require.Equal(t, "base64", body["content_encoding"])
	got, err := contentenc.Decode(body["content"].(string), "base64")
	require.NoError(t, err)
	require.Equal(t, bin, []byte(got))

	body = nil
	_, err = enqueueContent(context.Background(), dc, "héllo", objectTypeText, "p", "import:x")
	require.NoError(t, err)
	require.Equal(t, "héllo", body["content"])
	_, present := body["content_encoding"]
	require.False(t, present)
}
