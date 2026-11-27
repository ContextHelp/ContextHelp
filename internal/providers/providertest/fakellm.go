package providertest

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/ideacrafterslabs/ctxt/internal/config"
	"github.com/ideacrafterslabs/ctxt/internal/providers"
)

// fakeLLMTransport is the transport of an Ollama client that answers each
// kind of prompt the built-in steps send with a reply they can use, and
// never touches the network.
type fakeLLMTransport struct{}

func (fakeLLMTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	body, _ := io.ReadAll(r.Body)
	var req struct {
		Prompt string `json:"prompt"`
	}
	_ = json.Unmarshal(body, &req)
	reply := "{}"
	switch {
	case strings.Contains(req.Prompt, "one-sentence summary of the following text"):
		reply = "A fixed section summary."
	case strings.Contains(req.Prompt, "concise tags"):
		reply = "llmtagone, llmtagtwo"
	case strings.Contains(req.Prompt, "Identify named entities"):
		reply = "@person.ada"
	case strings.Contains(req.Prompt, "knowledge extraction assistant"):
		reply = `{"summary":"Fixed summary.","topics":["fixedtopic"],"decisions":["Use SQLite"],"open_questions":[],"artifacts":[]}`
	}
	b, _ := json.Marshal(map[string]any{"response": reply})
	return &http.Response{
		StatusCode: 200, Status: "200 OK", Body: io.NopCloser(bytes.NewReader(b)),
		Header: http.Header{"Content-Type": []string{"application/json"}}, Request: r,
	}, nil
}

// FakeLLMFactory is a provider factory whose LLM is an Ollama client over
// a fake transport, every other role stubbed and every tool lookup
// refused: a registry built on it runs the LLM steps with no network and
// nothing installed.
func FakeLLMFactory(t *testing.T) *providers.Factory {
	t.Helper()
	stub := config.ProviderBackendConfig{Backend: config.BackendStub}
	cfg := config.ProvidersConfig{
		Video: stub, Document: config.ProviderBackendConfig{Backend: "golib"}, OCR: stub, Transcription: stub,
		Vision: stub, Diarization: stub, Frames: stub, Summarization: stub,
		LLM: config.ProviderBackendConfig{Backend: "ollama", Model: "fake-model", Endpoint: "http://llm.invalid:11434"},
	}
	refuse := func(name string) (string, error) { return "", &providers.ToolNotInstalledError{Tool: name} }
	return providers.NewFactory(cfg, nil).
		WithHTTPClient(&http.Client{Transport: fakeLLMTransport{}}).
		WithContext(providers.WithToolLookup(context.Background(), refuse))
}
