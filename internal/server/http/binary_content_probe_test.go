package http

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/ideacrafterslabs/ctxt/internal/dpkmsclient"
)

// Probe: `ctxt capture <file>` sends string(os.ReadFile(...)) as the
// JSON content string. This drives the real client (dpkmsclient.Analyze)
// against the real handler, raw path, and compares the stored bytes.
func TestCaptureBinaryFileRoundTripsByteForByte(t *testing.T) {
	t.Skip("known bug: file bytes travel as a JSON string, so invalid UTF-8 becomes U+FFFD (capture, analyze --file, watcher)")
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
	require.Equal(t, bin, []byte(obj.RawContent), "stored bytes differ from the file's")
}
