package browserhistory

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ideacrafterslabs/ctxt/internal/urlfilter"
)

type payloadPub struct {
	mu       sync.Mutex
	filtered []map[string]any
}

func (p *payloadPub) Publish(_ context.Context, topic, _ string, payload any) error {
	if topic != "ctxt.ambient.event.filtered" {
		return nil
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	p.filtered = append(p.filtered, payload.(map[string]any))
	return nil
}

func (p *payloadPub) first() (map[string]any, bool) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if len(p.filtered) == 0 {
		return nil, false
	}
	return p.filtered[0], true
}

func TestSource_CompiledFilterAndFilteredPayloadOmitsURL(t *testing.T) {
	t.Parallel()
	f, err := urlfilter.Config{
		Browsers: map[string]urlfilter.BrowserConfig{
			"chrome": {Rules: urlfilter.Rules{Deny: []string{"*://crm.example.net/*"}}},
		},
	}.For("chrome")
	if err != nil {
		t.Fatal(err)
	}
	br := &fakeBrowser{name: "chrome"}
	br.AddVisits(
		Visit{URL: "https://crm.example.net/deals/42?owner=someone", VisitedAt: time.Now()},
		Visit{URL: "http://localhost:8080/admin", VisitedAt: time.Now().Add(time.Second)},
		Visit{URL: "https://example.com/news", VisitedAt: time.Now().Add(2 * time.Second)},
	)
	pub := &payloadPub{}
	s, _ := New(Config{Browsers: []BrowserClient{br}, PollInterval: 50 * time.Millisecond, Filter: f})
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	_ = s.Start(ctx, pub)
	t.Cleanup(func() { _ = s.Stop(context.Background()) })

	ev := drainEventOrFail(t, s, time.Second)
	if string(ev.Payload) != "https://example.com/news" {
		t.Fatalf("Payload = %q, want only the allowed URL", ev.Payload)
	}
	got, ok := pub.first()
	if !ok {
		t.Fatal("no filtered event")
	}
	if got["rule"] != "*://crm.example.net/*" || got["scope"] != "browser:chrome" || got["reason"] != "deny_rule" {
		t.Errorf("filtered payload = %v", got)
	}
	if s := fmt.Sprint(got); strings.Contains(s, "deals/42") || strings.Contains(s, "owner=") {
		t.Errorf("filtered payload leaks URL: %v", got)
	}
}

func TestSource_NilFilterDropsUnparseableURL(t *testing.T) {
	t.Parallel()
	br := &fakeBrowser{name: "chrome"}
	br.AddVisits(
		Visit{URL: "http://[::1", VisitedAt: time.Now()},
		Visit{URL: "https://example.com/ok", VisitedAt: time.Now().Add(time.Second)},
	)
	s, _ := New(Config{Browsers: []BrowserClient{br}, PollInterval: 50 * time.Millisecond})
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	_ = s.Start(ctx, nil)
	t.Cleanup(func() { _ = s.Stop(context.Background()) })

	ev := drainEventOrFail(t, s, time.Second)
	if string(ev.Payload) != "https://example.com/ok" {
		t.Errorf("Payload = %q, want the unparseable URL dropped", ev.Payload)
	}
}
