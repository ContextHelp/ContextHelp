package federation

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"github.com/ideacrafterslabs/ctxt/internal/storage"
)

func TestRemotePusher_Success(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	p := NewRemotePusher("test", srv.URL, "")
	err := p.Push(context.Background(), []storage.KnowledgeObject{makeObject("o1", "h1")}, nil)
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

	err := p.Push(context.Background(), []storage.KnowledgeObject{makeObject("o2", "h2")}, nil)
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
	err := p.Push(context.Background(), []storage.KnowledgeObject{makeObject("o3", "h3")}, nil)
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
	if err := p.Push(context.Background(), []storage.KnowledgeObject{makeObject("o4", "h4")}, nil); err != nil {
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
	if err := p.Push(context.Background(), []storage.KnowledgeObject{makeObject("o5", "h5")}, nil); err != nil {
		t.Fatalf("push: %v", err)
	}
	if gotAuth != "" {
		t.Errorf("expected no Authorization header, got %q", gotAuth)
	}
}
