package security

import (
	"bytes"
	"context"
	"log/slog"
	"net/http"
	"strings"
	"testing"
)

// captureLog points the default logger at a buffer for the test.
func captureLog(t *testing.T) *bytes.Buffer {
	t.Helper()
	var buf bytes.Buffer
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&buf, nil)))
	t.Cleanup(func() { slog.SetDefault(prev) })
	return &buf
}

// A webhook URL often embeds its token (path or query). A failed
// delivery logs the endpoint's origin only, never the URL: not in the
// endpoint field, not in the error text.
func TestWebhook_DeliveryErrorLogOmitsURL(t *testing.T) {
	for _, tc := range []struct {
		name, target string
		ctx          context.Context
		base         http.RoundTripper // nil: the default transport
		want         string            // a word the log must carry: what failed
	}{
		// Port 1 is closed: the transport error quotes the request URL.
		{"unreachable", "http://127.0.0.1:1/hook/SECRET?token=SECRET", context.Background(), nil, "connection refused"},
		// A URL that does not parse: the parse error quotes it.
		{"unparseable", "http://127.0.0.1:1/hook/SECRET?token=SECRET\x7f", context.Background(), nil, "invalid control character"},
		// --offline: kit's guard names the destination, path included.
		{"offline", "https://alerts.example/hook/SECRET?token=SECRET", offlineCtx(), &recordingTransport{}, "offline"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			logs := captureLog(t)
			send := webhookHandler(tc.target, newWebhookClient(tc.base))
			send(tc.ctx, Alert{Kind: EventAuthFailure})

			out := logs.String()
			if !strings.Contains(out, "webhook delivery error") || !strings.Contains(out, tc.want) {
				t.Fatalf("delivery failure not logged with %q:\n%s", tc.want, out)
			}
			if strings.Contains(out, "SECRET") {
				t.Errorf("log carries the webhook URL's secret:\n%s", out)
			}
		})
	}
}
