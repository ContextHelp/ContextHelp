package meeting

import (
	"context"
	"strings"
	"sync"
	"time"

	"github.com/ideacrafterslabs/ctxt/internal/ambient"
)

// AutoDetect observes foreground-window events and surfaces a prompt
// when a known meeting bundle-id comes to focus (per ADR-069 + US-0221).
//
// Strict rules:
//   - NEVER starts a recording without explicit user confirmation OR
//     an explicit per-bundle remember-my-choice rule.
//   - Default state: enabled=false. Auto-detect is opt-in.
//   - Default prompt timeout: 10 seconds; auto-dismiss if no action.
//
// AutoDetect emits bus events but does NOT directly call meeting.Source:
// the daemon driver (cmd/ctxd) wires the prompt response back to
// Source.Trigger when the user confirms. This keeps AutoDetect free of
// recording-state concerns.
type AutoDetect struct {
	cfg AutoDetectConfig
	publisher ambient.Publisher

	mu               sync.Mutex
	pendingPromptFor string
	pendingExpiresAt time.Time
}

// AutoDetectConfig configures the auto-detect prompt.
type AutoDetectConfig struct {
	// Bundles is the set of bundle ids that trigger a prompt. Default
	// includes Zoom, Teams, Slack (Huddles), Discord, FaceTime.
	Bundles []DetectionRule
	// PromptTimeout is how long the prompt stays active before
	// auto-dismiss. Default: 10 seconds.
	PromptTimeout time.Duration
	// Now overrides the wall clock; tests use a fake.
	Now func() time.Time
}

// DetectionRule names a bundle and the recording mode that should be
// requested when the user confirms. Per-app rules can also include
// "remember-my-choice" actions (auto-confirm, never-prompt).
type DetectionRule struct {
	BundleID string
	Mode     Mode
	// Action: prompt (default), auto-start, never. "auto-start" skips
	// the prompt and directly invokes Source.Trigger when the bundle
	// comes to focus. "never" suppresses detection entirely.
	Action string
}

// Default detection rules per ADR-069 + US-0221.
var DefaultDetectionBundles = []DetectionRule{
	{BundleID: "us.zoom.xos", Mode: ModeFull, Action: "prompt"},
	{BundleID: "com.microsoft.teams2", Mode: ModeFull, Action: "prompt"},
	{BundleID: "com.tinyspeck.slackmacgap", Mode: ModeAudioOnly, Action: "prompt"},
	{BundleID: "com.electron.discord", Mode: ModeFull, Action: "prompt"},
	{BundleID: "com.apple.FaceTime", Mode: ModeFull, Action: "prompt"},
}

// NewAutoDetect constructs an AutoDetect observer.
func NewAutoDetect(cfg AutoDetectConfig) *AutoDetect {
	if cfg.PromptTimeout <= 0 {
		cfg.PromptTimeout = 10 * time.Second
	}
	if cfg.Now == nil {
		cfg.Now = time.Now
	}
	if len(cfg.Bundles) == 0 {
		cfg.Bundles = DefaultDetectionBundles
	}
	return &AutoDetect{cfg: cfg}
}

// SetPublisher wires the bus publisher. Called by the daemon driver at
// startup; AutoDetect emits ctxt.ambient.meeting.{auto_detected,
// prompt_dismissed} on this bus.
func (a *AutoDetect) SetPublisher(p ambient.Publisher) {
	a.publisher = p
}

// OnForegroundChange is called by cmd/ctxd whenever the foreground source
// emits a window-focus event. AutoDetect inspects the bundle id and
// either:
//   - Returns a non-empty prompt request (BundleID + Mode); the daemon
//     should surface the prompt UI and call Confirm/Dismiss based on the
//     user's response.
//   - Returns empty if the bundle isn't in the detection list, or if
//     the rule is "never", or if a prompt is already pending for this
//     bundle.
func (a *AutoDetect) OnForegroundChange(ctx context.Context, bundleID string) *PromptRequest {
	rule := a.matchRule(bundleID)
	if rule == nil {
		return nil
	}
	if rule.Action == "never" {
		return nil
	}

	a.mu.Lock()
	defer a.mu.Unlock()

	now := a.cfg.Now()
	// If a prompt is already pending for this same bundle, don't fire a
	// duplicate. If pending has expired, allow re-prompt.
	if a.pendingPromptFor == bundleID && now.Before(a.pendingExpiresAt) {
		return nil
	}

	if rule.Action == "auto-start" {
		// Auto-confirm path; daemon driver should call Source.Trigger
		// directly. Bus event records the auto-start for audit.
		if a.publisher != nil {
			_ = a.publisher.Publish(ctx, "ctxt.ambient.meeting.auto_detected", "meeting",
				map[string]any{
					"bundle_id": bundleID,
					"mode":      string(rule.Mode),
					"action":    "auto-start",
				})
		}
		return &PromptRequest{
			BundleID: bundleID,
			Mode:     rule.Mode,
			Action:   "auto-start",
		}
	}

	// Default: prompt the user.
	a.pendingPromptFor = bundleID
	a.pendingExpiresAt = now.Add(a.cfg.PromptTimeout)
	if a.publisher != nil {
		_ = a.publisher.Publish(ctx, "ctxt.ambient.meeting.auto_detected", "meeting",
			map[string]any{
				"bundle_id": bundleID,
				"mode":      string(rule.Mode),
				"action":    "prompt",
				"timeout_seconds": a.cfg.PromptTimeout.Seconds(),
			})
	}
	return &PromptRequest{
		BundleID: bundleID,
		Mode:     rule.Mode,
		Action:   "prompt",
	}
}

// Confirm clears the pending prompt and signals that the daemon should
// invoke Source.Trigger. Returns the bundle / mode for the daemon to use.
func (a *AutoDetect) Confirm(ctx context.Context) (bundleID string, mode Mode, ok bool) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.pendingPromptFor == "" {
		return "", "", false
	}
	pending := a.pendingPromptFor
	a.pendingPromptFor = ""
	a.pendingExpiresAt = time.Time{}
	rule := a.matchRule(pending)
	if rule == nil {
		return "", "", false
	}
	return pending, rule.Mode, true
}

// Dismiss clears the pending prompt and emits the dismissed event.
func (a *AutoDetect) Dismiss(ctx context.Context, reason string) {
	a.mu.Lock()
	pending := a.pendingPromptFor
	a.pendingPromptFor = ""
	a.pendingExpiresAt = time.Time{}
	a.mu.Unlock()
	if pending != "" && a.publisher != nil {
		_ = a.publisher.Publish(ctx, "ctxt.ambient.meeting.prompt_dismissed", "meeting",
			map[string]any{"bundle_id": pending, "dismiss_reason": reason})
	}
}

// CheckPromptTimeout fires the dismiss path when a pending prompt has
// expired. Daemon ticker calls this every second or so.
func (a *AutoDetect) CheckPromptTimeout(ctx context.Context) {
	a.mu.Lock()
	expired := a.pendingPromptFor != "" && a.cfg.Now().After(a.pendingExpiresAt)
	pending := a.pendingPromptFor
	if expired {
		a.pendingPromptFor = ""
		a.pendingExpiresAt = time.Time{}
	}
	a.mu.Unlock()
	if expired && a.publisher != nil {
		_ = a.publisher.Publish(ctx, "ctxt.ambient.meeting.prompt_dismissed", "meeting",
			map[string]any{"bundle_id": pending, "dismiss_reason": "timeout"})
	}
}

// matchRule returns the first matching rule for bundleID, or nil.
func (a *AutoDetect) matchRule(bundleID string) *DetectionRule {
	for i := range a.cfg.Bundles {
		if matchBundle(a.cfg.Bundles[i].BundleID, bundleID) {
			return &a.cfg.Bundles[i]
		}
	}
	return nil
}

// PromptRequest is what OnForegroundChange returns when a prompt should
// be surfaced (or auto-start should fire).
type PromptRequest struct {
	BundleID string
	Mode     Mode
	Action   string // "prompt" | "auto-start"
}

// matchBundle is the same trailing/leading wildcard matcher used by the
// foreground source. Reused here for pattern consistency.
//
// (Already defined in screenshot.go / foreground.go; redefined here
// rather than imported to keep the meeting package self-contained.)
func init() {
	// Compile-time intent: ensure matchBundle is defined for this package's
	// auto-detect path. Implemented inline below.
}

func matchBundle(pattern, bundleID string) bool {
	if !strings.Contains(pattern, "*") {
		return pattern == bundleID
	}
	if strings.HasSuffix(pattern, "*") {
		prefix := strings.TrimSuffix(pattern, "*")
		return strings.HasPrefix(bundleID, prefix)
	}
	if strings.HasPrefix(pattern, "*") {
		suffix := strings.TrimPrefix(pattern, "*")
		return strings.HasSuffix(bundleID, suffix)
	}
	parts := strings.SplitN(pattern, "*", 2)
	return strings.HasPrefix(bundleID, parts[0]) && strings.HasSuffix(bundleID, parts[1])
}
