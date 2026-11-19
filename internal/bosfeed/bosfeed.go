// Package bosfeed reads the change feed of an external authority (bos,
// bos.peer/v1) and feeds it to what builds from it: the record-level access
// state (what each record stands under) and the ingest of records.
package bosfeed

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"time"

	"github.com/ideacrafterslabs/ctxt/internal/bosaccess"
)

// ErrCursorExpired: the cursor is older than the authority's outbox
// retains; the reader must start over from the oldest retained event.
var ErrCursorExpired = errors.New("bosfeed: cursor expired")

// Page is one page of the feed.
type Page struct {
	Events  []bosaccess.Event `json:"events"`
	After   string            `json:"after"`
	Next    string            `json:"next"`
	HasMore bool              `json:"has_more"`
}

// PageSource reads a page of the feed after a cursor.
type PageSource interface {
	Page(ctx context.Context, after string, limit int) (Page, error)
}

// Client reads the feed over the authority's command surface:
// GET /v1/commands/peer/feed?peer=&scope=&after=&limit=.
type Client struct {
	BaseURL string
	Peer    string
	Scope   string
	// Token is the bearer credential of the service principal that reads.
	Token string
	HTTP  *http.Client
}

// Page implements PageSource.
func (c *Client) Page(ctx context.Context, after string, limit int) (Page, error) {
	q := url.Values{"peer": {c.Peer}, "after": {after}}
	if c.Scope != "" {
		q.Set("scope", c.Scope)
	}
	if limit > 0 {
		q.Set("limit", strconv.Itoa(limit))
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.BaseURL+"/v1/commands/peer/feed?"+q.Encode(), nil)
	if err != nil {
		return Page{}, err
	}
	if c.Token != "" {
		req.Header.Set("Authorization", "Bearer "+c.Token)
	}
	hc := c.HTTP
	if hc == nil {
		hc = &http.Client{Timeout: 30 * time.Second}
	}
	resp, err := hc.Do(req)
	if err != nil {
		return Page{}, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 64<<20))
	if err != nil {
		return Page{}, err
	}
	if resp.StatusCode != http.StatusOK {
		if code := errorCode(body); resp.StatusCode == http.StatusConflict && code == "cursor_expired" || resp.StatusCode == http.StatusGone {
			return Page{}, ErrCursorExpired
		}
		return Page{}, fmt.Errorf("bosfeed: feed answered %d: %s", resp.StatusCode, errorCode(body))
	}
	var env struct {
		Data []Page `json:"data"`
	}
	if err := json.Unmarshal(body, &env); err != nil || len(env.Data) != 1 {
		return Page{}, fmt.Errorf("bosfeed: feed page unreadable")
	}
	return env.Data[0], nil
}

// errorCode reads the error code of an error body, flat or enveloped.
func errorCode(body []byte) string {
	var e struct {
		Code  string `json:"code"`
		Error struct {
			Code string `json:"code"`
		} `json:"error"`
	}
	_ = json.Unmarshal(body, &e)
	if e.Code != "" {
		return e.Code
	}
	return e.Error.Code
}

// Basis is what the feed keeps current for record-level access.
type Basis interface {
	Consume(bosaccess.Event) error
	CatchUp() error
}

// Sink builds from each event (ingest).
type Sink interface {
	Apply(ctx context.Context, ev bosaccess.Event) error
}

// CursorStore keeps the cursor of the last page applied.
type CursorStore interface {
	Load() (string, error)
	Save(string) error
}

// MemCursor is a CursorStore in memory.
type MemCursor struct{ V string }

// Load implements CursorStore.
func (m *MemCursor) Load() (string, error) { return m.V, nil }

// Save implements CursorStore.
func (m *MemCursor) Save(v string) error { m.V = v; return nil }

// Reader applies the feed: each event reaches the basis, then the sinks; the
// cursor moves only after a whole page was applied, so a failure replays the
// page (delivery is at-least-once; sinks and the basis are idempotent). When
// the reader has caught up with the head of the feed it tells the basis, which
// is what keeps record-level reads allowed.
type Reader struct {
	Source PageSource
	Basis  Basis
	Sinks  []Sink
	Cursor CursorStore
	Limit  int
}

// Sync reads pages until the head of the feed. An expired cursor starts over
// from the oldest retained event (the first return value says so); what the
// authority retracted before then is retracted by the reconcile it runs.
func (r *Reader) Sync(ctx context.Context) (reset bool, err error) {
	after, err := r.Cursor.Load()
	if err != nil {
		return false, err
	}
	if after == "" {
		after = "0"
	}
	for {
		page, err := r.Source.Page(ctx, after, r.Limit)
		if errors.Is(err, ErrCursorExpired) && after != "0" {
			after, reset = "0", true
			if err := r.Cursor.Save(after); err != nil {
				return reset, err
			}
			continue
		}
		if err != nil {
			return reset, err
		}
		for _, ev := range page.Events {
			if err := r.Basis.Consume(ev); err != nil {
				return reset, err
			}
			for _, s := range r.Sinks {
				if err := s.Apply(ctx, ev); err != nil {
					return reset, fmt.Errorf("event %s: %w", ev.ID, err)
				}
			}
		}
		after = page.Next
		if err := r.Cursor.Save(after); err != nil {
			return reset, err
		}
		if !page.HasMore {
			return reset, r.Basis.CatchUp()
		}
	}
}

// Run syncs every interval until ctx ends; a failed sync is reported to
// onError and tried again at the next tick.
func (r *Reader) Run(ctx context.Context, interval time.Duration, onError func(error)) {
	t := time.NewTicker(interval)
	defer t.Stop()
	for {
		if _, err := r.Sync(ctx); err != nil && ctx.Err() == nil && onError != nil {
			onError(err)
		}
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
	}
}
