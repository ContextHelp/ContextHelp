package browserhistory

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/ideacrafterslabs/ctxt/internal/ambient"
)

// fakeBrowser is a controllable BrowserClient.
type fakeBrowser struct {
	name string

	mu     sync.Mutex
	visits []Visit
	err    error
	calls  int
}

func (f *fakeBrowser) Name() string { return f.name }

func (f *fakeBrowser) VisitsSince(_ context.Context, since time.Time) ([]Visit, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls++
	if f.err != nil {
		return nil, f.err
	}
	out := make([]Visit, 0)
	for _, v := range f.visits {
		if v.VisitedAt.After(since) {
			out = append(out, v)
		}
	}
	return out, nil
}

func (f *fakeBrowser) AddVisits(visits ...Visit) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.visits = append(f.visits, visits...)
}

func (f *fakeBrowser) SetError(err error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.err = err
}

type capturingPub struct {
	mu     sync.Mutex
	topics []string
}

func (p *capturingPub) Publish(_ context.Context, topic, _ string, _ any) error {
	p.mu.Lock()
	p.topics = append(p.topics, topic)
	p.mu.Unlock()
	return nil
}

func (p *capturingPub) HasTopic(t string) bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	for _, s := range p.topics {
		if s == t {
			return true
		}
	}
	return false
}

func drainEventOrFail(t *testing.T, src *Source, deadline time.Duration) ambient.RawEvent {
	t.Helper()
	select {
	case ev := <-src.Events():
		return ev
	case <-time.After(deadline):
		t.Fatalf("did not receive event within %s", deadline)
	}
	return ambient.RawEvent{}
}

func TestNew_RequiresAtLeastOneBrowser(t *testing.T) {
	t.Parallel()
	if _, err := New(Config{}); err == nil {
		t.Fatal("New with no browsers: expected error")
	}
}

func TestSource_NameIsConstant(t *testing.T) {
	t.Parallel()
	s, _ := New(Config{Browsers: []BrowserClient{&fakeBrowser{name: "test"}}})
	if s.Name() != "browserhistory" {
		t.Errorf("Name = %q, want browserhistory", s.Name())
	}
}

func TestSource_EmitsURLEventForNewVisit(t *testing.T) {
	t.Parallel()
	br := &fakeBrowser{name: "chrome"}
	br.AddVisits(Visit{
		URL:       "https://example.com/article",
		Title:     "Example",
		VisitedAt: time.Now(),
		Browser:   "chrome",
	})

	s, _ := New(Config{
		Browsers:     []BrowserClient{br},
		PollInterval: 50 * time.Millisecond,
	})
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	_ = s.Start(ctx, nil)
	t.Cleanup(func() { _ = s.Stop(context.Background()) })

	ev := drainEventOrFail(t, s, time.Second)
	if ev.Kind != ambient.KindURL {
		t.Errorf("Kind = %q, want %q", ev.Kind, ambient.KindURL)
	}
	if string(ev.Payload) != "https://example.com/article" {
		t.Errorf("Payload = %q", ev.Payload)
	}
	if ev.SuggestedPipeline != "url.generic" {
		t.Errorf("SuggestedPipeline = %q, want url.generic", ev.SuggestedPipeline)
	}
	if ev.Metadata["browser"] != "chrome" {
		t.Errorf("Metadata[browser] = %v, want chrome", ev.Metadata["browser"])
	}
	if ev.Metadata["title"] != "Example" {
		t.Errorf("Metadata[title] = %v, want Example", ev.Metadata["title"])
	}
}

func TestSource_RoutesGitHubToRepoPipeline(t *testing.T) {
	t.Parallel()
	br := &fakeBrowser{name: "chrome"}
	br.AddVisits(Visit{URL: "https://github.com/user/repo", VisitedAt: time.Now()})
	s, _ := New(Config{Browsers: []BrowserClient{br}, PollInterval: 50 * time.Millisecond})
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	_ = s.Start(ctx, nil)
	t.Cleanup(func() { _ = s.Stop(context.Background()) })

	ev := drainEventOrFail(t, s, time.Second)
	if ev.SuggestedPipeline != "url.repo" {
		t.Errorf("SuggestedPipeline = %q, want url.repo", ev.SuggestedPipeline)
	}
}

func TestSource_TagsSearchQueriesAsSubtype(t *testing.T) {
	t.Parallel()
	br := &fakeBrowser{name: "chrome"}
	br.AddVisits(Visit{URL: "https://www.google.com/search?q=hello", VisitedAt: time.Now()})
	s, _ := New(Config{Browsers: []BrowserClient{br}, PollInterval: 50 * time.Millisecond})
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	_ = s.Start(ctx, nil)
	t.Cleanup(func() { _ = s.Stop(context.Background()) })

	ev := drainEventOrFail(t, s, time.Second)
	if ev.Metadata["subtype"] != "search-query" {
		t.Errorf("Metadata[subtype] = %v, want search-query", ev.Metadata["subtype"])
	}
}

func TestSource_FilterDenyDropsURL(t *testing.T) {
	t.Parallel()
	br := &fakeBrowser{name: "chrome"}
	br.AddVisits(
		Visit{URL: "https://bank.example.com/account", VisitedAt: time.Now()},
		Visit{URL: "https://example.com/news", VisitedAt: time.Now().Add(time.Second)},
	)
	pub := &capturingPub{}
	s, _ := New(Config{
		Browsers:     []BrowserClient{br},
		PollInterval: 50 * time.Millisecond,
		Filter:       URLFilter{Deny: []string{"*bank.example.com*"}},
	})
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	_ = s.Start(ctx, pub)
	t.Cleanup(func() { _ = s.Stop(context.Background()) })

	ev := drainEventOrFail(t, s, time.Second)
	if string(ev.Payload) != "https://example.com/news" {
		t.Errorf("Payload = %q, want only the non-denied URL", ev.Payload)
	}
	// The denied URL should have triggered ctxt.ambient.event.filtered.
	deadline := time.After(500 * time.Millisecond)
	for {
		if pub.HasTopic("ctxt.ambient.event.filtered") {
			return
		}
		select {
		case <-deadline:
			t.Fatal("expected ctxt.ambient.event.filtered for denied URL")
		case <-time.After(5 * time.Millisecond):
		}
	}
}

func TestSource_PersistsLastSeenAcrossPolls(t *testing.T) {
	t.Parallel()
	br := &fakeBrowser{name: "chrome"}
	t1 := time.Now().Add(-1 * time.Hour)
	br.AddVisits(Visit{URL: "https://example.com/one", VisitedAt: t1})

	s, _ := New(Config{Browsers: []BrowserClient{br}, PollInterval: 30 * time.Millisecond})
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	_ = s.Start(ctx, nil)
	t.Cleanup(func() { _ = s.Stop(context.Background()) })

	_ = drainEventOrFail(t, s, time.Second)

	// Second poll without new visits should not re-emit.
	select {
	case ev := <-s.Events():
		t.Errorf("re-emit of same visit: got %+v", ev)
	case <-time.After(100 * time.Millisecond):
	}

	// Add a NEWER visit and confirm it's emitted.
	t2 := time.Now()
	br.AddVisits(Visit{URL: "https://example.com/two", VisitedAt: t2})
	ev := drainEventOrFail(t, s, time.Second)
	if string(ev.Payload) != "https://example.com/two" {
		t.Errorf("Payload = %q, want https://example.com/two", ev.Payload)
	}
}

func TestSource_SetLastSeenSeedsCutoff(t *testing.T) {
	t.Parallel()
	br := &fakeBrowser{name: "chrome"}
	t1 := time.Now().Add(-1 * time.Hour)
	br.AddVisits(Visit{URL: "https://example.com/old", VisitedAt: t1})

	s, _ := New(Config{Browsers: []BrowserClient{br}, PollInterval: 50 * time.Millisecond})
	// Seed last-seen AFTER the visit's timestamp; first poll should NOT emit.
	s.SetLastSeen("chrome", time.Now())

	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	_ = s.Start(ctx, nil)
	t.Cleanup(func() { _ = s.Stop(context.Background()) })

	select {
	case ev := <-s.Events():
		t.Errorf("expected no emit when SetLastSeen seeds future cutoff; got %+v", ev)
	case <-time.After(150 * time.Millisecond):
	}
}

func TestSource_PublishesFailedOnBrowserError(t *testing.T) {
	t.Parallel()
	br := &fakeBrowser{name: "chrome"}
	br.SetError(errors.New("sqlite locked"))
	pub := &capturingPub{}

	s, _ := New(Config{Browsers: []BrowserClient{br}, PollInterval: 30 * time.Millisecond})
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	_ = s.Start(ctx, pub)
	t.Cleanup(func() { _ = s.Stop(context.Background()) })

	deadline := time.After(time.Second)
	for {
		if pub.HasTopic("ctxt.ambient.source.failed") {
			return
		}
		select {
		case <-deadline:
			t.Fatal("expected ctxt.ambient.source.failed on browser error")
		case <-time.After(5 * time.Millisecond):
		}
	}
}

func TestSource_MultipleBrowsersConcurrent(t *testing.T) {
	t.Parallel()
	chrome := &fakeBrowser{name: "chrome"}
	firefox := &fakeBrowser{name: "firefox"}
	chrome.AddVisits(Visit{URL: "https://chrome-example.com", VisitedAt: time.Now(), Browser: "chrome"})
	firefox.AddVisits(Visit{URL: "https://firefox-example.com", VisitedAt: time.Now(), Browser: "firefox"})

	s, _ := New(Config{
		Browsers:     []BrowserClient{chrome, firefox},
		PollInterval: 50 * time.Millisecond,
	})
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	_ = s.Start(ctx, nil)
	t.Cleanup(func() { _ = s.Stop(context.Background()) })

	got := make(map[string]bool)
	for range 2 {
		ev := drainEventOrFail(t, s, time.Second)
		got[ev.Metadata["browser"].(string)] = true
	}
	if !got["chrome"] || !got["firefox"] {
		t.Errorf("expected events from both browsers; got %v", got)
	}
}

func TestRoutePipeline(t *testing.T) {
	t.Parallel()
	cases := []struct {
		url, want string
	}{
		{"https://github.com/foo", "url.repo"},
		{"https://www.github.com/foo", "url.repo"},
		{"https://gitlab.com/foo", "url.repo"},
		{"https://bitbucket.org/foo", "url.repo"},
		{"https://example.com", "url.generic"},
		{"https://news.ycombinator.com", "url.generic"},
	}
	for _, c := range cases {
		c := c
		t.Run(c.url, func(t *testing.T) {
			t.Parallel()
			if got := routePipeline(c.url); got != c.want {
				t.Errorf("routePipeline(%q) = %q, want %q", c.url, got, c.want)
			}
		})
	}
}

func TestIsSearchQuery(t *testing.T) {
	t.Parallel()
	cases := []struct {
		url  string
		want bool
	}{
		{"https://www.google.com/search?q=hello", true},
		{"https://duckduckgo.com/?q=hello", true},
		{"https://www.bing.com/search?q=hello", true},
		{"https://kagi.com/search?q=hello", true},
		{"https://github.com/search?q=foo", false},
		{"https://example.com", false},
	}
	for _, c := range cases {
		c := c
		t.Run(c.url, func(t *testing.T) {
			t.Parallel()
			if got := isSearchQuery(c.url); got != c.want {
				t.Errorf("isSearchQuery(%q) = %v, want %v", c.url, got, c.want)
			}
		})
	}
}

func TestFingerprint_DeterministicAndDistinct(t *testing.T) {
	t.Parallel()
	now := time.Now()
	a := fingerprint("https://example.com", now)
	b := fingerprint("https://example.com", now)
	c := fingerprint("https://example.com/other", now)
	if a != b {
		t.Error("identical inputs must yield identical fingerprints")
	}
	if a == c {
		t.Error("distinct URLs must yield distinct fingerprints")
	}
}

func TestFingerprint_BucketsToMinute(t *testing.T) {
	t.Parallel()
	t1 := time.Date(2026, 5, 5, 14, 30, 5, 0, time.UTC)
	t2 := time.Date(2026, 5, 5, 14, 30, 45, 0, time.UTC) // same minute
	a := fingerprint("https://example.com", t1)
	b := fingerprint("https://example.com", t2)
	if a != b {
		t.Errorf("same URL in same minute should produce same fingerprint; got %s vs %s", a, b)
	}
}

func TestSource_StartTwiceRejected(t *testing.T) {
	t.Parallel()
	s, _ := New(Config{Browsers: []BrowserClient{&fakeBrowser{name: "chrome"}}, PollInterval: 100 * time.Millisecond})
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	_ = s.Start(ctx, nil)
	t.Cleanup(func() { _ = s.Stop(context.Background()) })
	if err := s.Start(ctx, nil); err == nil {
		t.Error("second Start: expected error")
	}
}

// Compile-time check.
var _ ambient.Source = (*Source)(nil)
