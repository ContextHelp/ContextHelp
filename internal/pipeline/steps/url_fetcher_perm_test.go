package steps

import (
	"context"
	"errors"
	"net/http"
	"testing"

	"github.com/ideacrafterslabs/ctxt/internal/pipeline"
	"github.com/ideacrafterslabs/ctxt/internal/storage"
)

// roundTripFunc adapts a function to http.RoundTripper.
type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestURLFetcher_DNSError_IsPermanent(t *testing.T) {
	client := &http.Client{
		Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
			return nil, errors.New("dial tcp: lookup dead.example.com: no such host")
		}),
	}
	step := NewURLFetcher(WithURLHTTPClient(client))
	draft := &storage.KnowledgeObject{Source: "https://dead.example.com/page"}

	_, err := step.Run(context.Background(), draft)
	if err == nil {
		t.Fatal("expected error")
	}

	var perm *pipeline.PermanentError
	if !errors.As(err, &perm) {
		t.Errorf("expected PermanentError, got: %v", err)
	}
}

func TestURLFetcher_TLSError_IsPermanent(t *testing.T) {
	client := &http.Client{
		Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
			return nil, errors.New("tls: handshake failure")
		}),
	}
	step := NewURLFetcher(WithURLHTTPClient(client))
	draft := &storage.KnowledgeObject{Source: "https://badtls.example.com/page"}

	_, err := step.Run(context.Background(), draft)
	if err == nil {
		t.Fatal("expected error")
	}

	var perm *pipeline.PermanentError
	if !errors.As(err, &perm) {
		t.Errorf("expected PermanentError, got: %v", err)
	}
}

func TestURLFetcher_X509Error_IsPermanent(t *testing.T) {
	client := &http.Client{
		Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
			return nil, errors.New("x509: certificate signed by unknown authority")
		}),
	}
	step := NewURLFetcher(WithURLHTTPClient(client))
	draft := &storage.KnowledgeObject{Source: "https://badcert.example.com/page"}

	_, err := step.Run(context.Background(), draft)
	if err == nil {
		t.Fatal("expected error")
	}

	var perm *pipeline.PermanentError
	if !errors.As(err, &perm) {
		t.Errorf("expected PermanentError, got: %v", err)
	}
}

func TestURLFetcher_TimeoutError_IsRetryable(t *testing.T) {
	client := &http.Client{
		Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
			return nil, errors.New("dial tcp 1.2.3.4:443: i/o timeout")
		}),
	}
	step := NewURLFetcher(WithURLHTTPClient(client))
	draft := &storage.KnowledgeObject{Source: "https://slow.example.com/page"}

	_, err := step.Run(context.Background(), draft)
	if err == nil {
		t.Fatal("expected error")
	}

	var perm *pipeline.PermanentError
	if errors.As(err, &perm) {
		t.Errorf("timeout errors should be retryable, got PermanentError: %v", err)
	}
}

func TestURLFetcher_ConnectionRefused_IsRetryable(t *testing.T) {
	client := &http.Client{
		Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
			return nil, errors.New("dial tcp 1.2.3.4:443: connection refused")
		}),
	}
	step := NewURLFetcher(WithURLHTTPClient(client))
	draft := &storage.KnowledgeObject{Source: "https://down.example.com/page"}

	_, err := step.Run(context.Background(), draft)
	if err == nil {
		t.Fatal("expected error")
	}

	var perm *pipeline.PermanentError
	if errors.As(err, &perm) {
		t.Errorf("connection refused should be retryable, got PermanentError: %v", err)
	}
}
