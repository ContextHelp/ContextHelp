package migrate

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"github.com/ideacrafterslabs/ctxt/internal/embeddings"
	"github.com/ideacrafterslabs/ctxt/internal/embeddings/registry"
	"github.com/ideacrafterslabs/ctxt/internal/providers"
	"github.com/ideacrafterslabs/ctxt/internal/storage"
)

// Clock is the time source the rate limiter waits on; tests inject one.
type Clock interface {
	Now() time.Time
	// Sleep waits d, or returns ctx's error when it ends first.
	Sleep(ctx context.Context, d time.Duration) error
}

type wallClock struct{}

func (wallClock) Now() time.Time { return time.Now() }

func (wallClock) Sleep(ctx context.Context, d time.Duration) error {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-t.C:
		return nil
	}
}

// limiter spaces provider calls at least 1/perSecond apart.
type limiter struct {
	mu       sync.Mutex
	interval time.Duration
	clock    Clock
	next     time.Time
}

func newLimiter(perSecond float64, c Clock) *limiter {
	l := &limiter{clock: c}
	if perSecond > 0 {
		l.interval = time.Duration(float64(time.Second) / perSecond)
	}
	return l
}

// Wait blocks until the next call is allowed.
func (l *limiter) Wait(ctx context.Context) error {
	if l.interval <= 0 {
		return nil
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	now := l.clock.Now()
	if l.next.After(now) {
		if err := l.clock.Sleep(ctx, l.next.Sub(now)); err != nil {
			return err
		}
		now = l.next
	}
	l.next = now.Add(l.interval)
	return nil
}

// calls meters provider calls through the limiter and keeps the last
// error of the current object: the embedding step logs and drops a model's
// error, and the migration needs it to count the object as failed.
type calls struct {
	limit *limiter
	mu    sync.Mutex
	last  error
}

func (c *calls) reset() {
	c.mu.Lock()
	c.last = nil
	c.mu.Unlock()
}

func (c *calls) record(err error) {
	c.mu.Lock()
	c.last = err
	c.mu.Unlock()
}

func (c *calls) err() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.last
}

// meteredResolver wraps a ProviderResolver so every provider it returns
// waits on the rate limiter and records its errors.
type meteredResolver struct {
	inner embeddings.ProviderResolver
	calls *calls
}

var _ embeddings.ProviderResolver = (*meteredResolver)(nil)

func (r *meteredResolver) ForModel(ctx context.Context, m registry.Model) (providers.EmbeddingProvider, error) {
	p, err := r.inner.ForModel(ctx, m)
	if err != nil {
		r.calls.record(fmt.Errorf("resolve provider: %w", err))
		return nil, err
	}
	return &meteredProvider{EmbeddingProvider: p, calls: r.calls, dim: m.Dimension}, nil
}

func (r *meteredResolver) ForRegistration(ctx context.Context) (providers.EmbeddingProvider, json.RawMessage, error) {
	return r.inner.ForRegistration(ctx)
}

type meteredProvider struct {
	providers.EmbeddingProvider
	calls *calls
	dim   int
}

func (p *meteredProvider) Embed(ctx context.Context, text string) ([]float32, error) {
	if err := p.calls.limit.Wait(ctx); err != nil {
		p.calls.record(err)
		return nil, err
	}
	vec, err := p.EmbeddingProvider.Embed(ctx, text)
	if err != nil {
		p.calls.record(fmt.Errorf("embed: %w", err))
		return nil, err
	}
	if len(vec) != p.dim {
		p.calls.record(fmt.Errorf("provider returned %d dimensions, registry has %d: %w",
			len(vec), p.dim, storage.ErrEmbeddingDimension))
	}
	return vec, nil
}
