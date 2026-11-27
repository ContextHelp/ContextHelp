package providers

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// answer serves one fixed status and body for every request.
func answer(t *testing.T, status int, body string) *http.Client {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(status)
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(srv.Close)
	return &http.Client{Transport: rewrite{to: srv.URL}}
}

// rewrite sends every request to the test server, whatever its URL: the
// hosted clients call fixed API hosts.
type rewrite struct{ to string }

func (r rewrite) RoundTrip(req *http.Request) (*http.Response, error) {
	u := *req.URL
	target, _ := http.NewRequest(req.Method, r.to+u.Path, req.Body)
	target.Header = req.Header
	return http.DefaultTransport.RoundTrip(target.WithContext(req.Context()))
}

// An LLM client turns a non-2xx answer into an error, with the status and a
// short excerpt, and a 200 with an empty reply stays a nil error and an
// empty reply: those are different facts, and the job treats them so.
func TestLLMClientsAnswerStatus(t *testing.T) {
	type client struct {
		name string
		gen  func(c *http.Client) func(context.Context, string) (string, error)
		ok   string // a normal 200 body
		// empty is a 200 whose reply is empty.
		empty string
		// unavailable is a 503 whose JSON has no error field.
		unavailable string
	}
	clients := []client{
		{"ollama",
			func(c *http.Client) func(context.Context, string) (string, error) {
				return NewOllamaLLMProvider("http://ollama.invalid", "m").WithHTTPClient(c).Generate
			},
			`{"response":"hello"}`, `{"response":""}`, `{"message":"service unavailable"}`},
		{"openai",
			func(c *http.Client) func(context.Context, string) (string, error) {
				return NewOpenAILLMProvider("k", "m").WithHTTPClient(c).Generate
			},
			`{"choices":[{"message":{"content":"hello"}}]}`, `{"choices":[{"message":{"content":""}}]}`, `{"message":"service unavailable"}`},
		{"anthropic",
			func(c *http.Client) func(context.Context, string) (string, error) {
				return NewAnthropicLLMProvider("k", "m").WithHTTPClient(c).Generate
			},
			`{"content":[{"text":"hello"}]}`, `{"content":[{"text":""}]}`, `{"message":"service unavailable"}`},
	}
	for _, c := range clients {
		t.Run(c.name, func(t *testing.T) {
			ctx := context.Background()
			if out, err := c.gen(answer(t, 200, c.ok))(ctx, "p"); err != nil || out != "hello" {
				t.Errorf("200 normal = %q, %v", out, err)
			}
			if out, err := c.gen(answer(t, 200, c.empty))(ctx, "p"); err != nil || out != "" {
				t.Errorf("200 empty reply = %q, %v; want a nil error and an empty reply", out, err)
			}
			if _, err := c.gen(answer(t, 503, c.unavailable))(ctx, "p"); err == nil {
				t.Error("503 with a JSON body without an error field was taken for a reply")
			}
			_, err := c.gen(answer(t, 401, `{"error":"unauthorized"}`))(ctx, "p")
			if err == nil {
				t.Error("401 was taken for a reply")
			}
		})
	}
}

func TestOllamaLLMErrorNamesTheStatusAndBoundsTheBody(t *testing.T) {
	long := strings.Repeat("x", 5000)
	_, err := NewOllamaLLMProvider("http://ollama.invalid", "m").WithHTTPClient(answer(t, 503, long)).Generate(context.Background(), "p")
	if err == nil || !strings.Contains(err.Error(), "HTTP 503") || len(err.Error()) > 300 {
		t.Fatalf("err = %v (%d bytes)", err, len(err.Error()))
	}
}
