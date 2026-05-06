// Package session implements the ADR-067 session cutter: the client-side
// state machine that groups ambient captures into bounded work units.
//
// The cutter is goroutine-safe; ambient.Source goroutines call OnEvent
// from their dispatch threads and a periodic ticker calls CheckCuts. A
// single sync.Mutex serialises both paths. Algorithm matches OpenChronicle's
// session/manager.py line-by-line — see ADR-067 §Implementation Notes for
// the cross-reference.
//
// Three rules (tunable via Config):
//
//	1. Hard cut (idle):   no event for GapMinutes (default 5)
//	2. Soft cut:          single-app focus for SoftCutMinutes (default 3)
//	                      AND user is NOT frequent-switching (≥2 distinct
//	                      apps in last RecentSwitchWindow = 2 min)
//	3. Hard timeout:      session > MaxSessionHours (default 2) regardless
//	                      of activity
//
// Plus: ForceEnd on shutdown / 23:55 daily safety net.
//
// The cutter satisfies the ambient.Cutter interface (see
// internal/ambient/runner.go) so the Runner can hold one without
// conversion. Sessions are emitted to dpkms via the daemon's enqueue
// client per ADR-067 §Wire shape — that wiring lives in cmd/ctxd/.
package session

import (
	"crypto/rand"
	"encoding/hex"
	"sync"
	"time"

	"github.com/ideacrafterslabs/ctxt/internal/ambient"
)

// Defaults match ADR-067 § Decision item 2.
const (
	DefaultGapMinutes         = 5
	DefaultSoftCutMinutes     = 3
	DefaultMaxSessionHours    = 2
	DefaultRecentSwitchWindow = 2 * time.Minute
	DefaultTickSeconds        = 30
)

// EndReason is why a session ended.
type EndReason string

const (
	EndIdle             EndReason = "idle"
	EndSoftCut          EndReason = "soft_cut"
	EndTimeout          EndReason = "timeout"
	EndShutdown         EndReason = "shutdown"
	EndDailySafetyNet   EndReason = "daily_safety_net"
)

// Config tunes the cutter's three rules.
type Config struct {
	GapMinutes         int
	SoftCutMinutes     int
	MaxSessionHours    int
	RecentSwitchWindow time.Duration
	TickSeconds        int
	// Now overrides the wall clock. Tests use a fake clock; production
	// callers leave it at the default time.Now.
	Now func() time.Time
}

// Cutter is the session boundary state machine.
type Cutter struct {
	cfg Config

	// Hooks fired on session transitions. Daemon owners (cmd/ctxd) wire
	// these to persist + enqueue.
	OnStart func(id string, startedAt time.Time)
	OnEnd   func(id string, startedAt, endedAt time.Time, reason EndReason)

	mu              sync.Mutex
	activeID        string
	sessionStart    time.Time
	isActive        bool
	lastEventTime   time.Time
	lastBundleID    string
	appSwitchedAt   time.Time
	recentSwitches  []switchEntry
}

type switchEntry struct {
	at       time.Time
	bundleID string
}

// NewCutter constructs a Cutter with the supplied options.
func NewCutter(cfg Config) *Cutter {
	if cfg.GapMinutes <= 0 {
		cfg.GapMinutes = DefaultGapMinutes
	}
	if cfg.SoftCutMinutes <= 0 {
		cfg.SoftCutMinutes = DefaultSoftCutMinutes
	}
	if cfg.MaxSessionHours <= 0 {
		cfg.MaxSessionHours = DefaultMaxSessionHours
	}
	if cfg.RecentSwitchWindow <= 0 {
		cfg.RecentSwitchWindow = DefaultRecentSwitchWindow
	}
	if cfg.TickSeconds <= 0 {
		cfg.TickSeconds = DefaultTickSeconds
	}
	if cfg.Now == nil {
		cfg.Now = time.Now
	}
	return &Cutter{cfg: cfg}
}

// ActiveID implements ambient.Cutter. Returns the active session id, or
// empty when no session is open. Lock-protected for safe concurrent reads.
func (c *Cutter) ActiveID() string {
	c.mu.Lock()
	defer c.mu.Unlock()
	if !c.isActive {
		return ""
	}
	return c.activeID
}

// OnEvent implements ambient.Cutter. Called for every RawEvent emitted by
// any ambient source. Auto-starts a session when none is active; updates
// recent-switches deque + last-event timestamps otherwise.
func (c *Cutter) OnEvent(ev ambient.RawEvent) {
	now := c.cfg.Now()
	bundleID := bundleIDFromEvent(ev)

	c.mu.Lock()
	defer c.mu.Unlock()

	if !c.isActive {
		c.startLocked(now, bundleID)
		return
	}

	if bundleID != "" && bundleID != c.lastBundleID {
		c.recentSwitches = append(c.recentSwitches, switchEntry{at: now, bundleID: bundleID})
		c.appSwitchedAt = now
		c.lastBundleID = bundleID
	}
	c.lastEventTime = now
}

// CheckCuts evaluates all three cut rules. Should be called from a
// periodic ticker (every TickSeconds) so idle/timeout cuts fire even when
// no events arrive. Returns true if a cut occurred.
func (c *Cutter) CheckCuts() bool {
	c.mu.Lock()
	defer c.mu.Unlock()

	if !c.isActive {
		return false
	}
	now := c.cfg.Now()

	// Rule 1: idle gap.
	if now.Sub(c.lastEventTime) > time.Duration(c.cfg.GapMinutes)*time.Minute {
		c.endLocked(c.lastEventTime, EndIdle)
		return true
	}

	// Rule 3: timeout (check before soft-cut so a long single-app session
	// at the edge cuts via timeout reason, not soft_cut).
	if now.Sub(c.sessionStart) > time.Duration(c.cfg.MaxSessionHours)*time.Hour {
		c.endLocked(c.lastEventTime, EndTimeout)
		return true
	}

	// Rule 2: soft cut.
	if c.appSwitchedAt.IsZero() {
		return false
	}
	if now.Sub(c.appSwitchedAt) <= time.Duration(c.cfg.SoftCutMinutes)*time.Minute {
		return false
	}
	if c.isFrequentSwitchingLocked(now) {
		return false
	}
	c.endLocked(c.lastEventTime, EndSoftCut)
	return true
}

// ForceEnd cuts the active session immediately with the supplied reason.
// Used on daemon shutdown (EndShutdown) and the daily safety-net cron at
// 23:55 (EndDailySafetyNet).
func (c *Cutter) ForceEnd(reason EndReason) (id string, ended bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if !c.isActive {
		return "", false
	}
	id = c.activeID
	end := c.lastEventTime
	if end.IsZero() {
		end = c.cfg.Now()
	}
	c.endLocked(end, reason)
	return id, true
}

// startLocked opens a new session. Caller MUST hold c.mu.
func (c *Cutter) startLocked(now time.Time, bundleID string) {
	c.activeID = "sess_" + randomHex(12)
	c.sessionStart = now
	c.isActive = true
	c.lastEventTime = now
	c.lastBundleID = bundleID
	c.appSwitchedAt = time.Time{}
	c.recentSwitches = c.recentSwitches[:0]
	if c.OnStart != nil {
		// Caller hooks may be slow; release lock before invoking. Snapshot
		// values first.
		id := c.activeID
		started := c.sessionStart
		c.mu.Unlock()
		c.OnStart(id, started)
		c.mu.Lock()
	}
}

// endLocked closes the active session. Caller MUST hold c.mu.
func (c *Cutter) endLocked(endTime time.Time, reason EndReason) {
	id := c.activeID
	started := c.sessionStart
	c.activeID = ""
	c.sessionStart = time.Time{}
	c.isActive = false
	c.lastBundleID = ""
	c.appSwitchedAt = time.Time{}
	c.recentSwitches = c.recentSwitches[:0]
	if c.OnEnd != nil {
		c.mu.Unlock()
		c.OnEnd(id, started, endTime, reason)
		c.mu.Lock()
	}
}

// isFrequentSwitchingLocked returns true when ≥2 distinct apps have been
// focused in the last RecentSwitchWindow. Caller MUST hold c.mu.
func (c *Cutter) isFrequentSwitchingLocked(now time.Time) bool {
	cutoff := now.Add(-c.cfg.RecentSwitchWindow)
	seen := make(map[string]struct{})
	// Trim entries older than cutoff. Keep recentSwitches bounded by the
	// natural pruning here — no separate sweeper.
	pruned := c.recentSwitches[:0]
	for _, e := range c.recentSwitches {
		if e.at.Before(cutoff) {
			continue
		}
		pruned = append(pruned, e)
		if e.bundleID != "" {
			seen[e.bundleID] = struct{}{}
		}
	}
	c.recentSwitches = pruned
	return len(seen) >= 2
}

// bundleIDFromEvent extracts the bundle id from an event's metadata when
// present. Foreground source provides this directly; clipboard /
// screenshot include foreground_bundle_id; other sources may omit it.
func bundleIDFromEvent(ev ambient.RawEvent) string {
	if ev.Metadata == nil {
		return ""
	}
	if v, ok := ev.Metadata["bundle_id"].(string); ok {
		return v
	}
	if v, ok := ev.Metadata["foreground_bundle_id"].(string); ok {
		return v
	}
	return ""
}

// randomHex returns 2*n hex characters. Used for session id generation.
func randomHex(n int) string {
	buf := make([]byte, n)
	_, _ = rand.Read(buf)
	return hex.EncodeToString(buf)[:n]
}

// Compile-time assertion: *Cutter satisfies the ambient.Cutter interface
// so the substrate Runner can hold one without conversion.
var _ ambient.Cutter = (*Cutter)(nil)
