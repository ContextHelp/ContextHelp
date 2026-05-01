package federation

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ideacrafterslabs/ctxt/internal/storage"
)

func TestRemotePusher_Success(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	p := NewRemotePusher("test", srv.URL, "")
	err := p.Push(context.Background(), []storage.KnowledgeObject{makeObject("o1", "h1")}, nil, nil)
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}
}

func TestRemotePusher_500_Retries_ThenFails(t *testing.T) {
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	// Override backoff to zero via a custom client with zero timeout on retry.
	// We patch the pusher directly: use a modified version that uses tiny backoff.
	p := NewRemotePusher("test", srv.URL, "")

	err := p.Push(context.Background(), []storage.KnowledgeObject{makeObject("o2", "h2")}, nil, nil)
	if err == nil {
		t.Fatal("expected error after all retries, got nil")
	}
	if calls.Load() != 3 {
		t.Errorf("expected 3 attempts, got %d", calls.Load())
	}
}

func TestRemotePusher_400_NoRetry(t *testing.T) {
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		w.WriteHeader(http.StatusBadRequest)
	}))
	defer srv.Close()

	p := NewRemotePusher("test", srv.URL, "")
	err := p.Push(context.Background(), []storage.KnowledgeObject{makeObject("o3", "h3")}, nil, nil)
	if err == nil {
		t.Fatal("expected error for 400, got nil")
	}
	if calls.Load() != 1 {
		t.Errorf("expected 1 attempt (no retry on 4xx), got %d", calls.Load())
	}
}

func TestRemotePusher_BearerToken(t *testing.T) {
	var gotAuth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	p := NewRemotePusher("test", srv.URL, "secret-token")
	if err := p.Push(context.Background(), []storage.KnowledgeObject{makeObject("o4", "h4")}, nil, nil); err != nil {
		t.Fatalf("push: %v", err)
	}
	if gotAuth != "Bearer secret-token" {
		t.Errorf("Authorization header: got %q, want %q", gotAuth, "Bearer secret-token")
	}
}

func TestRemotePusher_NoToken_NoAuthHeader(t *testing.T) {
	var gotAuth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	p := NewRemotePusher("test", srv.URL, "") // empty token
	if err := p.Push(context.Background(), []storage.KnowledgeObject{makeObject("o5", "h5")}, nil, nil); err != nil {
		t.Fatalf("push: %v", err)
	}
	if gotAuth != "" {
		t.Errorf("expected no Authorization header, got %q", gotAuth)
	}
}

// TestRemotePusher_AdvancesWatermarkAfterCommit verifies T-0188: a successful
// HTTP push (200) advances the per-federation watermark on the source-side
// WatermarkStore. Without this advance, the worker re-pushes the same batch
// every tick and the receiver returns 5xx on duplicate ids, causing the
// "all 3 attempts failed" log line founder saw in the scenario-1 demo.
func TestRemotePusher_AdvancesWatermarkAfterCommit(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	src, _ := newTestDB(t)

	p := NewRemotePusherWithWatermarks("rwm-fed", srv.URL, "", src.Watermarks())

	before := time.Now().UTC()
	if err := p.Push(context.Background(),
		[]storage.KnowledgeObject{makeObject("rwm-1", "rwm-h1")}, nil, nil); err != nil {
		t.Fatalf("push: %v", err)
	}
	after := time.Now().UTC()

	got, err := src.Watermarks().GetWatermark(context.Background(), "rwm-fed")
	if err != nil {
		t.Fatalf("GetWatermark: %v", err)
	}
	epoch := time.Unix(0, 0).UTC()
	if !got.After(epoch) {
		t.Errorf("watermark not advanced past epoch: got %v", got)
	}
	if got.Before(before.Add(-time.Second)) || got.After(after.Add(time.Second)) {
		t.Errorf("watermark outside call window: got %v, before=%v after=%v",
			got, before, after)
	}
}

// TestRemotePusher_DoesNotAdvanceWatermarkOnFailure verifies the watermark
// stays at epoch when the receiver returns a non-2xx status. Mirrors AC #4 of
// US-0319 for the remote variant — failed batches must be retried on the
// next tick.
func TestRemotePusher_DoesNotAdvanceWatermarkOnFailure(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	src, _ := newTestDB(t)

	p := NewRemotePusherWithWatermarks("rwm-fail-fed", srv.URL, "", src.Watermarks())

	if err := p.Push(context.Background(),
		[]storage.KnowledgeObject{makeObject("rwmf-1", "rwmf-h1")}, nil, nil); err == nil {
		t.Fatal("expected error after retries, got nil")
	}

	got, err := src.Watermarks().GetWatermark(context.Background(), "rwm-fail-fed")
	if err != nil {
		t.Fatalf("GetWatermark: %v", err)
	}
	epoch := time.Unix(0, 0).UTC()
	if !got.Equal(epoch) {
		t.Errorf("failed push advanced watermark: got %v, want epoch", got)
	}
}
