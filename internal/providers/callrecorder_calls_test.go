package providers

import (
	"context"
	"testing"
)

// The recorder counts completed calls by kind: a failed call is not one, and
// with no recorder reporting does nothing.
func TestCallRecorderCountsCompletedCallsByKind(t *testing.T) {
	ctx, calls := WithCallRecorder(context.Background())
	ok := recordLLM(NewOllamaLLMProvider("http://ollama.invalid", "m").WithHTTPClient(answer(t, 200, `{"response":"hi"}`)))
	bad := recordLLM(NewOllamaLLMProvider("http://ollama.invalid", "m").WithHTTPClient(answer(t, 503, `{}`)))
	if _, err := ok.Generate(ctx, "p"); err != nil {
		t.Fatal(err)
	}
	if _, err := ok.Generate(ctx, "p"); err != nil {
		t.Fatal(err)
	}
	if _, err := bad.Generate(ctx, "p"); err == nil {
		t.Fatal("503 must fail")
	}
	ReportCallSuccess(ctx, "embedding")
	got := calls.Calls()
	if got["llm"] != 2 || got["embedding"] != 1 || got["vision"] != 0 {
		t.Fatalf("calls = %v: two LLM calls completed, the failed one does not count", got)
	}
	got["llm"] = 99 // a copy
	if calls.Calls()["llm"] != 2 {
		t.Fatal("Calls shares the recorder's map")
	}
	ReportCallSuccess(context.Background(), "llm") // no recorder: nothing, no panic
}
