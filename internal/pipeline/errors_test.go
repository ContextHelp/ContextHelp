package pipeline

import (
	"errors"
	"fmt"
	"testing"
)

func TestPermanentError_ErrorMessage(t *testing.T) {
	inner := errors.New("no such host")
	err := Permanent(inner)
	want := "permanent: no such host"
	if err.Error() != want {
		t.Errorf("got %q, want %q", err.Error(), want)
	}
}

func TestPermanentError_Unwrap(t *testing.T) {
	inner := errors.New("no such host")
	err := Permanent(inner)
	if !errors.Is(err, inner) {
		t.Error("Unwrap should expose the inner error")
	}
}

func TestPermanentError_As(t *testing.T) {
	inner := errors.New("tls: handshake failure")
	err := Permanent(inner)

	var perm *PermanentError
	if !errors.As(err, &perm) {
		t.Fatal("errors.As should match PermanentError")
	}
	if perm.Err != inner { //nolint:errorlint // identity is the assertion: Permanent must retain the exact inner error
		t.Errorf("inner error: got %v, want %v", perm.Err, inner)
	}
}

func TestPermanentError_AsWrapped(t *testing.T) {
	inner := errors.New("no such host")
	perm := Permanent(inner)
	wrapped := fmt.Errorf("step url_fetcher: %w", perm)

	var got *PermanentError
	if !errors.As(wrapped, &got) {
		t.Fatal("errors.As should match PermanentError through wrapping")
	}
}
