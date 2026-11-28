package pipeline

import (
	"errors"
	"fmt"
	"net/url"
	"strings"
)

// PermanentError wraps an error that should not be retried.
// Use Permanent() to construct one and errors.As() to detect it.
type PermanentError struct {
	Err error
}

func (e *PermanentError) Error() string {
	if e == nil || e.Err == nil {
		return "<nil>"
	}
	return "permanent: " + e.Err.Error()
}

func (e *PermanentError) Unwrap() error { return e.Err }

// Permanent wraps err as a PermanentError.
// Returns nil when err is nil; avoids double-wrapping if err is already permanent.
func Permanent(err error) error {
	if err == nil {
		return nil
	}
	var pe *PermanentError
	if errors.As(err, &pe) {
		return err
	}
	return &PermanentError{Err: err}
}

// IsPermanent reports whether err (or any error in its chain) is permanent.
func IsPermanent(err error) bool {
	var pe *PermanentError
	return errors.As(err, &pe)
}

// StepError is a step's failure, naming the step, so a caller can tell a
// failed fetch from a failed enrichment.
type StepError struct {
	Step string
	Err  error
}

func (e *StepError) Error() string { return fmt.Sprintf("step %s: %v", e.Step, e.Err) }
func (e *StepError) Unwrap() error { return e.Err }

// FetchError is a failed fetch from a source: which host it was addressed
// to, and the HTTP status when the source answered (0 when it did not: no
// such host, TLS, a refused or timed-out connection, a blocked
// destination). A caller that must tell "this object's source is gone"
// from "this network cannot reach sources" reads it with errors.As, never
// from the message.
type FetchError struct {
	// Host is the host the fetch was addressed to, see FetchHost.
	Host   string
	Status int
	// RateLimited: the host asked to slow down (429, a Retry-After header,
	// a GitHub rate limit). It says nothing about the object fetched.
	RateLimited bool
	Err         error
}

func (e *FetchError) Error() string { return e.Err.Error() }
func (e *FetchError) Unwrap() error { return e.Err }

// FetchHost is the comparable host of a fetched URL: lower case, with its
// port, without a leading "www."; "" for anything that is not a URL with a
// host.
func FetchHost(raw string) string {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || u.Host == "" {
		return ""
	}
	return strings.TrimPrefix(strings.ToLower(u.Host), "www.")
}
