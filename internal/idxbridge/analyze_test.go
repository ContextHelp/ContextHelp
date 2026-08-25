package idxbridge_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/ideacrafterslabs/ctxt/internal/idxbridge"
	"github.com/ideacrafterslabs/ctxt/internal/service"
)

// fakeAnalyzer records local fallback enqueues.
type fakeAnalyzer struct {
	jobID  string
	err    error
	called bool
	got    service.AnalyzeRequest
}

func (f *fakeAnalyzer) Analyze(_ context.Context, req service.AnalyzeRequest) (string, error) {
	f.called = true
	f.got = req
	return f.jobID, f.err
}

// analyzeDaemon fakes the daemon's analyze surface.
func analyzeDaemon(t *testing.T, jobID string, capture *service.AnalyzeRequest) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	mux.HandleFunc("/api/v1/analyze", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		if capture != nil {
			_ = json.NewDecoder(r.Body).Decode(capture)
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusAccepted)
		_ = json.NewEncoder(w).Encode(map[string]string{"job_id": jobID})
	})
	return httptest.NewServer(mux)
}

func TestAnalyze_ViaDaemon(t *testing.T) {
	var got service.AnalyzeRequest
	srv := analyzeDaemon(t, "job-remote-1", &got)
	defer srv.Close()

	fb := &fakeAnalyzer{jobID: "job-local-1"}
	bridge := idxbridge.New(idxbridge.Config{
		BaseURL:         srv.URL,
		AnalyzeFallback: fb,
		HTTPClient:      srv.Client(),
	})

	jobID, servedBy, err := bridge.Analyze(context.Background(), service.AnalyzeRequest{
		Content: "hello", Type: "text", Hints: []string{"research"},
	})
	if err != nil {
		t.Fatalf("Analyze: %v", err)
	}
	if jobID != "job-remote-1" {
		t.Errorf("jobID = %q, want daemon job id", jobID)
	}
	if servedBy != srv.URL {
		t.Errorf("servedBy = %q, want the daemon URL %q", servedBy, srv.URL)
	}
	if fb.called {
		t.Error("local fallback called while daemon live")
	}
	if got.Content != "hello" || got.Type != "text" || len(got.Hints) != 1 {
		t.Errorf("daemon received %+v; want request fields forwarded", got)
	}
}

func TestAnalyze_FallsBackWhenDaemonDown(t *testing.T) {
	fb := &fakeAnalyzer{jobID: "job-local-1"}
	bridge := idxbridge.New(idxbridge.Config{
		BaseURL:         "http://127.0.0.1:19999",
		ProbeTimeout:    50 * time.Millisecond,
		AnalyzeFallback: fb,
	})

	jobID, servedBy, err := bridge.Analyze(context.Background(), service.AnalyzeRequest{Content: "x", Type: "text"})
	if err != nil {
		t.Fatalf("Analyze: %v", err)
	}
	if !fb.called {
		t.Fatal("local fallback not called with daemon down")
	}
	if jobID != "job-local-1" {
		t.Errorf("jobID = %q, want local job id", jobID)
	}
	if servedBy != "" {
		t.Errorf("servedBy = %q, want empty for the local fallback", servedBy)
	}
}

func TestAnalyze_DaemonRejectionSurfacesWithoutFallback(t *testing.T) {
	// A live daemon that rejects the request made a decision; falling back
	// would locally enqueue work the daemon refused.
	mux := http.NewServeMux()
	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	mux.HandleFunc("/api/v1/analyze", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnprocessableEntity)
		_, _ = w.Write([]byte(`{"error":"PIPELINE_NOT_FOUND","message":"no pipeline for type"}`))
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	fb := &fakeAnalyzer{jobID: "job-local-1"}
	bridge := idxbridge.New(idxbridge.Config{
		BaseURL:         srv.URL,
		AnalyzeFallback: fb,
		HTTPClient:      srv.Client(),
	})

	_, _, err := bridge.Analyze(context.Background(), service.AnalyzeRequest{Content: "x", Type: "document"})
	if err == nil {
		t.Fatal("Analyze succeeded; want daemon rejection surfaced")
	}
	var rerr *idxbridge.RemoteError
	if !errors.As(err, &rerr) {
		t.Fatalf("error = %T (%v); want *RemoteError", err, err)
	}
	if rerr.StatusCode != http.StatusUnprocessableEntity {
		t.Errorf("StatusCode = %d, want 422", rerr.StatusCode)
	}
	if !strings.Contains(rerr.Body, "PIPELINE_NOT_FOUND") {
		t.Errorf("Body = %q; want daemon message preserved", rerr.Body)
	}
	if fb.called {
		t.Error("local fallback called after live-daemon rejection")
	}
}

func TestAnalyze_TransportFailureMidRequestFallsBack(t *testing.T) {
	// Health answers, then the daemon dies before /analyze: transport-level
	// failure → invalidate the probe and use the local fallback.
	mux := http.NewServeMux()
	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	mux.HandleFunc("/api/v1/analyze", func(w http.ResponseWriter, r *http.Request) {
		hj, ok := w.(http.Hijacker)
		if !ok {
			t.Fatal("hijacking unsupported")
		}
		conn, _, err := hj.Hijack()
		if err != nil {
			t.Fatalf("hijack: %v", err)
		}
		_ = conn.Close() // drop the connection mid-request
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	fb := &fakeAnalyzer{jobID: "job-local-1"}
	bridge := idxbridge.New(idxbridge.Config{
		BaseURL:         srv.URL,
		AnalyzeFallback: fb,
		HTTPClient:      srv.Client(),
	})

	jobID, _, err := bridge.Analyze(context.Background(), service.AnalyzeRequest{Content: "x", Type: "text"})
	if err != nil {
		t.Fatalf("Analyze: %v", err)
	}
	if !fb.called {
		t.Fatal("local fallback not called after transport failure")
	}
	if jobID != "job-local-1" {
		t.Errorf("jobID = %q, want local job id", jobID)
	}
}

func TestAnalyze_NoFallbackConfigured(t *testing.T) {
	bridge := idxbridge.New(idxbridge.Config{
		BaseURL:      "http://127.0.0.1:19999",
		ProbeTimeout: 50 * time.Millisecond,
		Fallback:     &fakeFallback{}, // search-only bridge
	})

	_, _, err := bridge.Analyze(context.Background(), service.AnalyzeRequest{Content: "x", Type: "text"})
	if err == nil {
		t.Fatal("Analyze with no fallback and daemon down: want error")
	}
}

func TestNew_AnalyzeOnlyConfigDoesNotPanic(t *testing.T) {
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("New panicked for analyze-only config: %v", r)
		}
	}()
	idxbridge.New(idxbridge.Config{AnalyzeFallback: &fakeAnalyzer{}})
}

func TestSearchObjects_NoSearchFallbackErrors(t *testing.T) {
	bridge := idxbridge.New(idxbridge.Config{
		BaseURL:         "http://127.0.0.1:19999",
		ProbeTimeout:    50 * time.Millisecond,
		AnalyzeFallback: &fakeAnalyzer{},
	})
	if _, _, err := bridge.SearchObjects(context.Background(), "type==note", 5, 0); err == nil {
		t.Fatal("SearchObjects with no search fallback and daemon down: want error")
	}
}

func TestAnalyzeFunc_Adapter(t *testing.T) {
	called := false
	fn := idxbridge.AnalyzeFunc(func(_ context.Context, _ service.AnalyzeRequest) (string, error) {
		called = true
		return "job-fn-1", nil
	})
	bridge := idxbridge.New(idxbridge.Config{
		BaseURL:         "http://127.0.0.1:19999",
		ProbeTimeout:    50 * time.Millisecond,
		AnalyzeFallback: fn,
	})
	jobID, _, err := bridge.Analyze(context.Background(), service.AnalyzeRequest{Content: "x", Type: "text"})
	if err != nil || jobID != "job-fn-1" || !called {
		t.Fatalf("AnalyzeFunc adapter: jobID=%q err=%v called=%v", jobID, err, called)
	}
}
