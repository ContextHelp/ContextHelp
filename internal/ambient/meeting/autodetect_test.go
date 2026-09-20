package meeting

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

type adClock struct{ ns atomic.Int64 }

func newADClock(t time.Time) *adClock {
	c := &adClock{}
	c.ns.Store(t.UnixNano())
	return c
}
func (c *adClock) Now() time.Time          { return time.Unix(0, c.ns.Load()) }
func (c *adClock) Advance(d time.Duration) { c.ns.Add(int64(d)) }

type adPub struct {
	mu     sync.Mutex
	topics []string
}

func (p *adPub) Publish(_ context.Context, topic, _ string, _ any) error {
	p.mu.Lock()
	p.topics = append(p.topics, topic)
	p.mu.Unlock()
	return nil
}

func (p *adPub) HasTopic(t string) bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	for _, s := range p.topics {
		if s == t {
			return true
		}
	}
	return false
}

func TestAutoDetect_DefaultsAreApplied(t *testing.T) {
	t.Parallel()
	a := NewAutoDetect(AutoDetectConfig{})
	if a.cfg.PromptTimeout != 10*time.Second {
		t.Errorf("PromptTimeout = %s, want 10s", a.cfg.PromptTimeout)
	}
	if len(a.cfg.Bundles) == 0 {
		t.Error("default bundles should be applied when none supplied")
	}
}

func TestAutoDetect_KnownBundleProducesPromptRequest(t *testing.T) {
	t.Parallel()
	clk := newADClock(time.Now())
	a := NewAutoDetect(AutoDetectConfig{Now: clk.Now})
	pub := &adPub{}
	a.SetPublisher(pub)

	req := a.OnForegroundChange(context.Background(), "us.zoom.xos")
	if req == nil {
		t.Fatal("expected PromptRequest for known meeting bundle")
	}
	if req.BundleID != "us.zoom.xos" {
		t.Errorf("BundleID = %q, want us.zoom.xos", req.BundleID)
	}
	if req.Action != "prompt" {
		t.Errorf("Action = %q, want prompt", req.Action)
	}
	if !pub.HasTopic("ctxt.ambient.meeting.auto_detected") {
		t.Error("missing ctxt.ambient.meeting.auto_detected topic")
	}
}

func TestAutoDetect_UnknownBundleReturnsNil(t *testing.T) {
	t.Parallel()
	a := NewAutoDetect(AutoDetectConfig{})
	if req := a.OnForegroundChange(context.Background(), "com.example.app"); req != nil {
		t.Errorf("unknown bundle should return nil; got %+v", req)
	}
}

func TestAutoDetect_NeverActionSuppresses(t *testing.T) {
	t.Parallel()
	a := NewAutoDetect(AutoDetectConfig{
		Bundles: []DetectionRule{
			{BundleID: "com.electron.discord", Mode: ModeFull, Action: "never"},
		},
	})
	if req := a.OnForegroundChange(context.Background(), "com.electron.discord"); req != nil {
		t.Errorf("never action should suppress; got %+v", req)
	}
}

func TestAutoDetect_AutoStartActionSkipsPrompt(t *testing.T) {
	t.Parallel()
	a := NewAutoDetect(AutoDetectConfig{
		Bundles: []DetectionRule{
			{BundleID: "us.zoom.xos", Mode: ModeFull, Action: "auto-start"},
		},
	})
	pub := &adPub{}
	a.SetPublisher(pub)

	req := a.OnForegroundChange(context.Background(), "us.zoom.xos")
	if req == nil || req.Action != "auto-start" {
		t.Errorf("expected auto-start action; got %+v", req)
	}
}

func TestAutoDetect_DuplicatePromptSuppressedWithinWindow(t *testing.T) {
	t.Parallel()
	clk := newADClock(time.Now())
	a := NewAutoDetect(AutoDetectConfig{
		Bundles:       []DetectionRule{{BundleID: "us.zoom.xos", Mode: ModeFull, Action: "prompt"}},
		PromptTimeout: 10 * time.Second,
		Now:           clk.Now,
	})

	if req := a.OnForegroundChange(context.Background(), "us.zoom.xos"); req == nil {
		t.Fatal("first prompt should fire")
	}
	if req := a.OnForegroundChange(context.Background(), "us.zoom.xos"); req != nil {
		t.Errorf("second prompt within window should be suppressed; got %+v", req)
	}
}

func TestAutoDetect_AfterTimeoutAllowsRePrompt(t *testing.T) {
	t.Parallel()
	clk := newADClock(time.Now())
	a := NewAutoDetect(AutoDetectConfig{
		Bundles:       []DetectionRule{{BundleID: "us.zoom.xos", Mode: ModeFull, Action: "prompt"}},
		PromptTimeout: 1 * time.Second,
		Now:           clk.Now,
	})
	if req := a.OnForegroundChange(context.Background(), "us.zoom.xos"); req == nil {
		t.Fatal("first prompt should fire")
	}
	clk.Advance(2 * time.Second)
	a.CheckPromptTimeout(context.Background())
	if req := a.OnForegroundChange(context.Background(), "us.zoom.xos"); req == nil {
		t.Error("after timeout, re-prompt should fire")
	}
}

func TestAutoDetect_ConfirmReturnsPendingBundle(t *testing.T) {
	t.Parallel()
	a := NewAutoDetect(AutoDetectConfig{})
	if req := a.OnForegroundChange(context.Background(), "us.zoom.xos"); req == nil {
		t.Fatal("prompt should fire")
	}
	bundle, mode, ok := a.Confirm(context.Background())
	if !ok {
		t.Fatal("Confirm: expected ok=true")
	}
	if bundle != "us.zoom.xos" || mode != ModeFull {
		t.Errorf("Confirm returned (%q, %q); want (us.zoom.xos, full)", bundle, mode)
	}
}

func TestAutoDetect_DismissEmitsTopic(t *testing.T) {
	t.Parallel()
	a := NewAutoDetect(AutoDetectConfig{})
	pub := &adPub{}
	a.SetPublisher(pub)

	_ = a.OnForegroundChange(context.Background(), "us.zoom.xos")
	a.Dismiss(context.Background(), "user")
	if !pub.HasTopic("ctxt.ambient.meeting.prompt_dismissed") {
		t.Error("missing prompt_dismissed topic")
	}
}

func TestAutoDetect_CheckPromptTimeoutFiresDismiss(t *testing.T) {
	t.Parallel()
	clk := newADClock(time.Now())
	a := NewAutoDetect(AutoDetectConfig{
		PromptTimeout: 100 * time.Millisecond,
		Now:           clk.Now,
	})
	pub := &adPub{}
	a.SetPublisher(pub)

	_ = a.OnForegroundChange(context.Background(), "us.zoom.xos")
	clk.Advance(200 * time.Millisecond)
	a.CheckPromptTimeout(context.Background())
	if !pub.HasTopic("ctxt.ambient.meeting.prompt_dismissed") {
		t.Error("expected prompt_dismissed after timeout")
	}
}

func TestAutoDetect_BundleGlobMatching(t *testing.T) {
	t.Parallel()
	a := NewAutoDetect(AutoDetectConfig{
		Bundles: []DetectionRule{
			{BundleID: "com.electron.*", Mode: ModeFull, Action: "prompt"},
		},
	})
	if req := a.OnForegroundChange(context.Background(), "com.electron.discord"); req == nil {
		t.Error("glob pattern should match com.electron.*")
	}
}
