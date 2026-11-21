package service

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// Only a real http(s) URL promotes a text capture to url; prose that
// merely starts with "http" stays text.
func TestAnalyzePromotesOnlyRealURLs(t *testing.T) {
	svc := newTestService(t)
	ctx := ownerCtx()
	for content, want := range map[string]string{
		"httpx notes for tomorrow": "ingest:text",
		"http is a protocol":       "ingest:text",
		"https://example.com/a":    "ingest:url",
		"  http://example.com/b  ": "ingest:url",
	} {
		id, err := svc.Analyze(ctx, AnalyzeRequest{Content: content, Type: "text", Pipeline: "text.short"})
		require.NoError(t, err, content)
		job, err := svc.store.Jobs().Get(ctx, id)
		require.NoError(t, err)
		require.Equal(t, want, job.Type, content)
	}
}
