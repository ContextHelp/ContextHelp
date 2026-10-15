package http

import (
	"math"
	"net/http"
	"strconv"
	"sync"
	"time"

	"github.com/ideacrafterslabs/ctxt/internal/security"
)

// Sign-in ceremonies (a login-code exchange, an invite or sign-in link
// redemption) are rate limited per client address (ADR-079 §3): each
// address holds a token bucket of DefaultCeremonyAttempts that refills
// over DefaultCeremonyWindow, so the N+1th attempt inside the window is
// 429 RATE_LIMITED with a Retry-After. The pinned hop.top/kit has no
// transport rate-limit middleware (its only limiter is the conformance
// test server's), so this bucket stands in until one lands.
const (
	DefaultCeremonyAttempts = 10
	DefaultCeremonyWindow   = time.Minute
	// limiterSweepEvery bounds how often idle buckets are dropped.
	limiterSweepEvery = 5 * time.Minute
)

// CodeRateLimited is the error code of a 429.
const CodeRateLimited = "RATE_LIMITED"

// AddrLimiter is a per-address token bucket.
type AddrLimiter struct {
	attempts float64
	window   time.Duration
	now      func() time.Time

	mu        sync.Mutex
	buckets   map[string]*addrBucket
	lastSweep time.Time
}

type addrBucket struct {
	tokens float64
	seen   time.Time
}

// NewAddrLimiter returns a limiter allowing attempts per window for
// each address, refilled continuously. attempts and window left zero
// take the Default* values; now is the clock, nil means time.Now.
func NewAddrLimiter(attempts int, window time.Duration, now func() time.Time) *AddrLimiter {
	if attempts <= 0 {
		attempts = DefaultCeremonyAttempts
	}
	if window <= 0 {
		window = DefaultCeremonyWindow
	}
	if now == nil {
		now = time.Now
	}
	return &AddrLimiter{attempts: float64(attempts), window: window, now: now, buckets: map[string]*addrBucket{}, lastSweep: now()}
}

// Allow charges one attempt to addr. When the bucket is empty it
// reports false and how long until the next attempt is allowed.
func (l *AddrLimiter) Allow(addr string) (ok bool, retryAfter time.Duration) {
	now := l.now()
	l.mu.Lock()
	defer l.mu.Unlock()
	l.sweep(now)
	b, found := l.buckets[addr]
	if !found {
		b = &addrBucket{tokens: l.attempts, seen: now}
		l.buckets[addr] = b
	}
	refill := now.Sub(b.seen).Seconds() / l.window.Seconds() * l.attempts
	b.tokens = math.Min(l.attempts, b.tokens+math.Max(refill, 0))
	b.seen = now
	if b.tokens >= 1 {
		b.tokens--
		return true, 0
	}
	perToken := l.window.Seconds() / l.attempts
	return false, time.Duration((1 - b.tokens) * perToken * float64(time.Second))
}

// sweep drops buckets that have been full again for a while, so the
// map does not grow with every address ever seen.
func (l *AddrLimiter) sweep(now time.Time) {
	if now.Sub(l.lastSweep) < limiterSweepEvery {
		return
	}
	l.lastSweep = now
	for addr, b := range l.buckets {
		if now.Sub(b.seen) >= l.window {
			delete(l.buckets, addr)
		}
	}
}

// LimitByAddr returns middleware that charges each request to l under
// the client address and answers 429 RATE_LIMITED, with Retry-After,
// once the address is out of attempts. A refusal is recorded on sec
// (nil-safe) as an auth failure for the address.
func LimitByAddr(l *AddrLimiter, sec *security.Emitter) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			addr := remoteHost(r)
			ok, retry := l.Allow(addr)
			if ok {
				next.ServeHTTP(w, r)
				return
			}
			if sec != nil {
				sec.RecordAuthFailure(r.Context(), addr)
			}
			secs := int(math.Ceil(retry.Seconds()))
			if secs < 1 {
				secs = 1
			}
			w.Header().Set("Retry-After", strconv.Itoa(secs))
			w.Header().Set("Cache-Control", "no-store")
			WriteError(w, http.StatusTooManyRequests, CodeRateLimited,
				"too many sign-in attempts from this address; try again in "+strconv.Itoa(secs)+"s")
		})
	}
}
