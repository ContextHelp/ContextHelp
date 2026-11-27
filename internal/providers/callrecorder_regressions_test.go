package providers

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"testing"

	"github.com/ideacrafterslabs/ctxt/internal/config"
)

type failingTransport struct{}

func (failingTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	return &http.Response{
		StatusCode: 200, Body: io.NopCloser(bytes.NewReader([]byte(`{"error":"model unavailable"}`))),
		Header: http.Header{}, Request: r,
	}, nil
}

func llmFactory(backend string) *Factory {
	return NewFactory(config.ProvidersConfig{LLM: config.ProviderBackendConfig{Backend: backend, Model: "m", Endpoint: "http://llm.invalid:1"}}, nil).
		WithHTTPClient(&http.Client{Transport: failingTransport{}})
}

// A failed LLM call is reported to the context's recorder and still
// returned; with no recorder nothing changes.
func TestCallRecorderRegressions_LLMFailureIsReported(t *testing.T) {
	llm := llmFactory("ollama").LLM()
	ctx, rec := WithCallRecorder(context.Background())
	if _, err := llm.Generate(ctx, "p"); err == nil {
		t.Fatal("want the provider's error returned")
	}
	if rec.Err() == nil {
		t.Error("the failed call was not recorded")
	}
	if _, err := llm.Generate(context.Background(), "p"); err == nil {
		t.Error("want an error with no recorder too")
	}
	if m, ok := llm.(interface{ Model() string }); !ok || m.Model() != "m" {
		t.Errorf("the wrapper must forward Model(): %v", ok)
	}
}

// The stub is handed out as it is: capability checks tell it apart by type.
func TestCallRecorderRegressions_StubStaysAStub(t *testing.T) {
	if _, ok := llmFactory("stub").LLM().(*StubLLMProvider); !ok {
		t.Error("the stub LLM was wrapped")
	}
}

// With no recorder in the context, reporting an unusable reply allocates
// nothing: ingest pays nothing for it.
func TestCallRecorderRegressions_NoRecorderAllocatesNothing(t *testing.T) {
	ctx := context.Background()
	if n := testing.AllocsPerRun(100, func() { ReportUnusableReply(ctx, "tagger", "empty tag list") }); n != 0 {
		t.Errorf("allocs = %v, want 0", n)
	}
	rctx, rec := WithCallRecorder(ctx)
	ReportUnusableReply(rctx, "tagger", "empty tag list")
	if err := rec.Err(); err == nil || err.Error() != "tagger: unusable reply: empty tag list" {
		t.Errorf("recorded %v", err)
	}
}
