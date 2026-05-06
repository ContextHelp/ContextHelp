package enqueue

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/ideacrafterslabs/ctxt/internal/ambient"
)

// receivedRequest captures the shape of a request the capturingHandler
// observed; tests assert against these.
type receivedRequest struct {
	Path        string
	Method      string
	Auth        string
	ContentType string
	Body        map[string]any
}

// capturingHandler wraps a fixed-status fixed-body server and exposes the
// request log to the test.
type capturingHandler struct {
	mu       sync.Mutex
	requests []receivedRequest
	status   int
	body     string
}

func newCapturingHandler(status int, body string) (*httptest.Server, *capturingHandler) {
	h := &capturingHandler{status: status, body: body}
	if h.status == 0 {
		h.status = http.StatusOK
	}
	srv := httptest.NewServer(http.HandlerFunc(h.serve))
	return srv, h
}

func (h *capturingHandler) serve(w http.ResponseWriter, r *http.Request) {
	bodyBytes, _ := io.ReadAll(r.Body)
	var parsed map[string]any
	_ = json.Unmarshal(bodyBytes, &parsed)
	h.mu.Lock()
	h.requests = append(h.requests, receivedRequest{
		Path:        r.URL.Path,
		Method:      r.Method,
		Auth:        r.Header.Get("Authorization"),
		ContentType: r.Header.Get("Content-Type"),
		Body:        parsed,
	})
	h.mu.Unlock()
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(h.status)
	_, _ = io.WriteString(w, h.body)
}

func (h *capturingHandler) Last() receivedRequest {
	h.mu.Lock()
	defer h.mu.Unlock()
	if len(h.requests) == 0 {
		return receivedRequest{}
	}
	return h.requests[len(h.requests)-1]
}

func (h *capturingHandler) Count() int {
	h.mu.Lock()
	defer h.mu.Unlock()
	return len(h.requests)
}

func TestNew_RequiresEndpoint(t *testing.T) {
	t.Parallel()
	if _, err := New(""); err == nil {
		t.Fatal("New(\"\"): expected error, got nil")
	}
}

func TestNew_TrimsTrailingSlash(t *testing.T) {
	t.Parallel()
	c, err := New("http://example.com/")
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if c.Endpoint() != "http://example.com" {
		t.Errorf("Endpoint = %q, want trimmed", c.Endpoint())
	}
}

func TestEnqueue_PostsToAnalyzeEndpoint(t *testing.T) {
	t.Parallel()
	srv, h := newCapturingHandler(http.StatusOK, `{"id":"obj_xyz"}`)
	defer srv.Close()

	c, _ := New(srv.URL)
	ev := ambient.RawEvent{
		Source:            "clipboard",
		Kind:              ambient.KindURL,
		Payload:           []byte("https://example.com"),
		Fingerprint:       "fp-1",
		SuggestedPipeline: "url.generic",
		SessionID:         "sess_a1b2c3",
		Metadata:          map[string]any{"foreground_bundle_id": "com.apple.Safari"},
	}

	if err := c.Enqueue(context.Background(), ev); err != nil {
		t.Fatalf("Enqueue: %v", err)
	}

	if h.Count() != 1 {
		t.Fatalf("requests received = %d, want 1", h.Count())
	}
	req := h.Last()
	if req.Path != "/api/v1/analyze" {
		t.Errorf("Path = %q, want /api/v1/analyze", req.Path)
	}
	if req.Method != http.MethodPost {
		t.Errorf("Method = %q, want POST", req.Method)
	}
	if req.ContentType != "application/json" {
		t.Errorf("Content-Type = %q, want application/json", req.ContentType)
	}
	if req.Auth != "" {
		t.Errorf("Authorization = %q, want empty (no token configured)", req.Auth)
	}
	if got := req.Body["content"]; got != "https://example.com" {
		t.Errorf("body.content = %v, want https://example.com", got)
	}
	if got := req.Body["type"]; got != "url" {
		t.Errorf("body.type = %v, want url", got)
	}
	if got := req.Body["ambient_source"]; got != "clipboard" {
		t.Errorf("body.ambient_source = %v, want clipboard", got)
	}
	if got := req.Body["fingerprint"]; got != "fp-1" {
		t.Errorf("body.fingerprint = %v, want fp-1", got)
	}
	if got := req.Body["session_id"]; got != "sess_a1b2c3" {
		t.Errorf("body.session_id = %v, want sess_a1b2c3", got)
	}
	if got := req.Body["pipeline"]; got != "url.generic" {
		t.Errorf("body.pipeline = %v, want url.generic", got)
	}
}

func TestEnqueue_AddsBearerWhenTokenConfigured(t *testing.T) {
	t.Parallel()
	srv, h := newCapturingHandler(http.StatusOK, `{}`)
	defer srv.Close()

	c, _ := New(srv.URL, WithAuthToken("secret-token"))
	if err := c.Enqueue(context.Background(), ambient.RawEvent{Source: "test", Kind: ambient.KindText, Payload: []byte("x")}); err != nil {
		t.Fatalf("Enqueue: %v", err)
	}
	if got := h.Last().Auth; got != "Bearer secret-token" {
		t.Errorf("Authorization = %q, want Bearer secret-token", got)
	}
}

func TestEnqueue_ReturnsErrorOn4xx(t *testing.T) {
	t.Parallel()
	srv, _ := newCapturingHandler(http.StatusBadRequest, `{"error":"bad payload"}`)
	defer srv.Close()

	c, _ := New(srv.URL)
	err := c.Enqueue(context.Background(), ambient.RawEvent{Source: "test", Kind: ambient.KindText, Payload: []byte("x")})
	if err == nil {
		t.Fatal("Enqueue with 400 status: expected error, got nil")
	}
	if !strings.Contains(err.Error(), "400") {
		t.Errorf("error %q does not mention status code 400", err.Error())
	}
}

func TestEnqueue_ReturnsErrorOn5xx(t *testing.T) {
	t.Parallel()
	srv, _ := newCapturingHandler(http.StatusInternalServerError, `{"error":"internal"}`)
	defer srv.Close()

	c, _ := New(srv.URL)
	err := c.Enqueue(context.Background(), ambient.RawEvent{Source: "test", Kind: ambient.KindText, Payload: []byte("x")})
	if err == nil {
		t.Fatal("Enqueue with 500 status: expected error, got nil")
	}
}

func TestEnqueue_RespectsContextCancel(t *testing.T) {
	t.Parallel()
	// httptest server that hangs forever; we test that context cancellation
	// aborts the request.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		<-r.Context().Done()
	}))
	defer srv.Close()

	c, _ := New(srv.URL)
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // cancel immediately

	err := c.Enqueue(ctx, ambient.RawEvent{Source: "test", Kind: ambient.KindText, Payload: []byte("x")})
	if err == nil {
		t.Fatal("Enqueue with cancelled context: expected error")
	}
	if !errors.Is(err, context.Canceled) && !strings.Contains(err.Error(), "context") {
		t.Errorf("error %q does not surface context cancellation", err.Error())
	}
}

// Compile-time assertion: HTTPClient satisfies ambient.Enqueuer.
var _ ambient.Enqueuer = (*HTTPClient)(nil)
