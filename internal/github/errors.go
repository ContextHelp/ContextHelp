package github

import (
	"errors"
	"fmt"
	"time"
)

// Sentinel errors, matched with errors.Is. Every error the client returns
// for an HTTP failure is an *APIError (or *RateLimitError) that wraps one.
var (
	// ErrNotFound: 404. The repository or issue does not exist, or the
	// token in use cannot see it (GitHub answers 404 for both).
	ErrNotFound = errors.New("github: not found")
	// ErrUnauthorized: 401. The token was rejected; retrying cannot help.
	ErrUnauthorized = errors.New("github: unauthorized")
	// ErrForbidden: 403 that is not a rate limit (blocked repository,
	// missing scope, SAML enforcement).
	ErrForbidden = errors.New("github: forbidden")
	// ErrRateLimited: primary or secondary rate limit; see RateLimitError.
	ErrRateLimited = errors.New("github: rate limited")
	// ErrServer: 5xx after the client's retries.
	ErrServer = errors.New("github: server error")
)

// APIError is a non-success GitHub API response.
type APIError struct {
	Status  int
	Method  string
	URL     string
	Message string // GitHub's "message" field, when the body had one
	err     error  // sentinel
}

func (e *APIError) Error() string {
	msg := e.Message
	if msg == "" {
		msg = e.err.Error()
	}
	return fmt.Sprintf("github: %s %s: HTTP %d: %s", e.Method, e.URL, e.Status, msg)
}

func (e *APIError) Unwrap() error { return e.err }

// Permanent reports whether retrying the same request cannot succeed:
// not found, unauthorized and forbidden. Rate limits and server errors
// are transient.
func (e *APIError) Permanent() bool {
	return errors.Is(e.err, ErrNotFound) || errors.Is(e.err, ErrUnauthorized) || errors.Is(e.err, ErrForbidden)
}

// LimitKind says which GitHub rate limit was hit.
type LimitKind string

const (
	// LimitPrimary is the hourly request budget (X-RateLimit-*).
	LimitPrimary LimitKind = "primary"
	// LimitSecondary is the abuse/concurrency limit (Retry-After).
	LimitSecondary LimitKind = "secondary"
)

// RateLimitError is a rate-limited response the client did not sleep out:
// the wait exceeded Config.MaxWait, or the retries ran out.
type RateLimitError struct {
	APIError
	Kind LimitKind
	// RetryAfter is how long GitHub asked the caller to wait.
	RetryAfter time.Duration
	// Reset is when a primary budget refills (zero when unknown).
	Reset time.Time
}

func (e *RateLimitError) Error() string {
	return fmt.Sprintf("%s (%s limit, retry after %s)", e.APIError.Error(), e.Kind, e.RetryAfter.Round(time.Second))
}

func (e *RateLimitError) Unwrap() error { return ErrRateLimited }

// IsPermanent reports whether err is a GitHub failure that retrying
// cannot fix (404, 401, non-rate-limit 403).
func IsPermanent(err error) bool {
	var ae *APIError
	if errors.As(err, &ae) {
		return ae.Permanent()
	}
	return false
}
