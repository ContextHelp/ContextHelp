package bosfeed

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/ideacrafterslabs/ctxt/internal/bosaccess"
)

type basis struct {
	events  []string
	catchUp int
}

func (b *basis) Consume(ev bosaccess.Event) error { b.events = append(b.events, ev.ID); return nil }
func (b *basis) CatchUp() error                   { b.catchUp++; return nil }

type script struct {
	pages []Page
	errs  map[string]error
	asked []string
}

func (s *script) Page(_ context.Context, after string, _ int) (Page, error) {
	s.asked = append(s.asked, after)
	if err := s.errs[after]; err != nil {
		delete(s.errs, after)
		return Page{}, err
	}
	for _, p := range s.pages {
		if p.After == after {
			return p, nil
		}
	}
	return Page{After: after, Next: after}, nil
}

type failSink struct{ on string }

func (f failSink) Apply(_ context.Context, ev bosaccess.Event) error {
	if ev.ID == f.on {
		return errors.New("boom")
	}
	return nil
}

func ev(id string) bosaccess.Event {
	return bosaccess.Event{ID: id, Subject: "bos://core.documents/d" + id}
}

// Pages are read to the head; the basis is told it caught up once, at the
// end; the cursor is the last page's next.
func TestSyncReadsToTheHeadAndCatchesUp(t *testing.T) {
	src := &script{pages: []Page{
		{After: "0", Next: "2", HasMore: true, Events: []bosaccess.Event{ev("1"), ev("2")}},
		{After: "2", Next: "5", Events: []bosaccess.Event{ev("5")}},
	}}
	b, c := &basis{}, &MemCursor{}
	r := &Reader{Source: src, Basis: b, Cursor: c}
	if _, err := r.Sync(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(b.events) != 3 || b.catchUp != 1 || c.V != "5" {
		t.Fatalf("events %v catchUp %d cursor %q", b.events, b.catchUp, c.V)
	}
	// an empty page still confirms the catch-up: the basis stays fresh
	if _, err := r.Sync(context.Background()); err != nil || b.catchUp != 2 {
		t.Fatalf("err %v catchUp %d", err, b.catchUp)
	}
}

// A failing sink leaves the cursor before its page and no catch-up: the page
// is replayed, never skipped.
func TestSyncReplaysAPageWhoseSinkFailed(t *testing.T) {
	src := &script{pages: []Page{{After: "0", Next: "2", Events: []bosaccess.Event{ev("1"), ev("2")}}}}
	b, c := &basis{}, &MemCursor{}
	r := &Reader{Source: src, Basis: b, Cursor: c, Sinks: []Sink{failSink{on: "2"}}}
	if _, err := r.Sync(context.Background()); err == nil {
		t.Fatal("want the sink error")
	}
	if c.V != "" || b.catchUp != 0 {
		t.Fatalf("cursor %q catchUp %d after a failed page", c.V, b.catchUp)
	}
}

// An expired cursor starts over from the oldest retained event.
func TestSyncStartsOverOnAnExpiredCursor(t *testing.T) {
	src := &script{
		pages: []Page{{After: "0", Next: "9", Events: []bosaccess.Event{ev("9")}}},
		errs:  map[string]error{"4": ErrCursorExpired},
	}
	b, c := &basis{}, &MemCursor{V: "4"}
	r := &Reader{Source: src, Basis: b, Cursor: c}
	reset, err := r.Sync(context.Background())
	if err != nil || !reset || c.V != "9" || len(b.events) != 1 {
		t.Fatalf("reset %v err %v cursor %q events %v", reset, err, c.V, b.events)
	}
}

// The client reads the command surface, the envelope's page, and maps a 409
// cursor_expired.
func TestClientReadsTheCommandSurface(t *testing.T) {
	var got string
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/commands/peer/feed", func(w http.ResponseWriter, r *http.Request) {
		got = r.URL.RawQuery + "|" + r.Header.Get("Authorization")
		if r.URL.Query().Get("after") == "7" {
			w.WriteHeader(http.StatusConflict)
			_, _ = w.Write([]byte(`{"error":{"code":"cursor_expired"}}`))
			return
		}
		_, _ = w.Write([]byte(`{"exit_code":0,"data":[{"contract":"bos.peer/v1","events":[{"id":"3","subject":"bos://core.documents/x","data":{"change":"upserted","scope":"s","sensitivity":"internal","under":["a"]}}],"after":"0","next":"3","has_more":false}]}`))
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()
	c := &Client{BaseURL: srv.URL, Peer: "ctxt", Scope: "s1", Token: "tok"}
	p, err := c.Page(context.Background(), "0", 10)
	if err != nil || p.Next != "3" || len(p.Events) != 1 || p.Events[0].Data.Under[0] != "a" {
		t.Fatalf("page %+v err %v", p, err)
	}
	if got != "after=0&limit=10&peer=ctxt&scope=s1|Bearer tok" {
		t.Fatalf("request %q", got)
	}
	if _, err := c.Page(context.Background(), "7", 10); !errors.Is(err, ErrCursorExpired) {
		t.Fatalf("err %v", err)
	}
}
