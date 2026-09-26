package browserhistory

import (
	"context"
	"time"

	"github.com/ideacrafterslabs/ctxt/internal/browser/chromium"
)

// RangeReader is an optional BrowserClient capability: reading the visits
// in a time range rather than since a cursor. Callers type-assert for it.
type RangeReader interface {
	// VisitsBetween returns visits with from <= VisitedAt < to,
	// oldest-first. A zero to means no upper bound.
	VisitsBetween(ctx context.Context, from, to time.Time) ([]Visit, error)
}

// DefaultChromiumBatchSize caps how many visits one VisitsSince call
// returns.
const DefaultChromiumBatchSize = 500

// ChromiumClient reads one Chromium-family browser profile's History.
type ChromiumClient struct {
	browser chromium.Browser
	profile chromium.Profile
	// BatchSize caps VisitsSince results (visits tied on the last
	// timestamp are still all returned). <= 0 means no cap.
	BatchSize int
}

// NewChromiumClient resolves profile (display name or folder name) for
// browser and returns a client reading its history.
func NewChromiumClient(browser chromium.Browser, profile string, opts ...chromium.Option) (*ChromiumClient, error) {
	p, err := chromium.ResolveProfile(browser, profile, opts...)
	if err != nil {
		return nil, err
	}
	return &ChromiumClient{browser: browser, profile: p, BatchSize: DefaultChromiumBatchSize}, nil
}

// Name is "<browser>:<profile folder>", e.g. "brave:Profile 3", so the
// Source keeps a separate last-seen position per profile. The folder name
// is used because display names can be renamed.
func (c *ChromiumClient) Name() string {
	return string(c.browser) + ":" + c.profile.DirName
}

// VisitsSince returns the oldest batch of visits strictly after since.
func (c *ChromiumClient) VisitsSince(ctx context.Context, since time.Time) ([]Visit, error) {
	// Visit times have microsecond precision; since+1ns rounds up to the
	// next representable instant, making the inclusive bound strict.
	return c.read(ctx, since.Add(time.Nanosecond), time.Time{}, c.BatchSize)
}

// VisitsBetween implements RangeReader. The whole range is returned.
func (c *ChromiumClient) VisitsBetween(ctx context.Context, from, to time.Time) ([]Visit, error) {
	return c.read(ctx, from, to, 0)
}

func (c *ChromiumClient) read(ctx context.Context, from, to time.Time, limit int) ([]Visit, error) {
	hv, err := chromium.HistoryVisits(ctx, c.profile.Dir, from, to, limit)
	if err != nil {
		return nil, err
	}
	out := make([]Visit, len(hv))
	for i, v := range hv {
		out[i] = Visit{URL: v.URL, Title: v.Title, VisitedAt: v.VisitedAt, Browser: string(c.browser)}
	}
	return out, nil
}
