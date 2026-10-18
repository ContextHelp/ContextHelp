package security

import (
	"bytes"
	"context"
	"log/slog"
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
	}{
		// Port 1 is closed: the transport error quotes the request URL.
		{"unreachable", "http://127.0.0.1:1/hook/SECRET?token=SECRET"},
		// A URL that does not parse: the parse error quotes it.
		{"unparseable", "http://127.0.0.1:1/hook/SECRET?token=SECRET\x7f"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			logs := captureLog(t)
			send := webhookHandler(tc.target, newWebhookClient(nil))
			send(context.Background(), Alert{Kind: EventAuthFailure})

			out := logs.String()
			if !strings.Contains(out, "webhook delivery error") {
				t.Fatalf("delivery failure not logged:\n%s", out)
			}
			if strings.Contains(out, "SECRET") {
				t.Errorf("log carries the webhook URL's secret:\n%s", out)
			}
		})
	}
}
