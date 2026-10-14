// Package security implements security event emission and alerting hooks.
//
// SecurityEventEmitter tracks rate-limited security events (auth failures,
// ACL denials, quota exhaustion, signing failures, worker crash loops) and
// dispatches them to one or more AlertHandlers.
//
// Default handler: logs the alert and queues an AuditStore row with
// event_class=security. A single worker owned by the emitter writes the
// queued rows; the queue is bounded and drops (counted, rate-limited
// warning) when full, so recording never blocks the request path. Close
// stops intake and drains the queue; call it before closing the store.
// Optional handlers: webhook POST (JSON) and SMTP email.
package security

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"net/smtp"
	"sync"
	"sync/atomic"
	"time"

	"github.com/google/uuid"
	"github.com/ideacrafterslabs/ctxt/internal/storage"
)

// Event kinds.
const (
	EventAuthFailure     = "security.auth_failure"
	EventACLDenial       = "security.acl_denial"
	EventQuotaExhausted  = "security.quota_exhausted"
	EventSigningFailure  = "security.signing_failure"
	EventWorkerCrashLoop = "security.worker_crash_loop"
)

// AlertLevel classifies the urgency of a security alert.
type AlertLevel string

const (
	AlertLevelWarn     AlertLevel = "warn"
	AlertLevelCritical AlertLevel = "critical"
)

// Alert is the payload passed to every registered AlertHandler.
type Alert struct {
	ID        string            `json:"id"`
	Kind      string            `json:"kind"`
	Level     AlertLevel        `json:"level"`
	Principal string            `json:"principal,omitempty"`
	Message   string            `json:"message"`
	Count     int               `json:"count"`
	Meta      map[string]string `json:"meta,omitempty"`
	OccuredAt time.Time         `json:"occurred_at"`
}

// AlertHandler processes a security alert.
// Implementations MUST be non-blocking (complete quickly or offload to goroutine).
type AlertHandler func(ctx context.Context, a Alert)

// Config holds tunable parameters for the emitter.
type Config struct {
	// AuthFailureThreshold is the max auth failures per principal within the
	// sliding window before an alert fires. Default: 3.
	AuthFailureThreshold int
	// ACLDenialThreshold is the max ACL denials per principal within the
	// sliding window before an alert fires. Default: 10.
	ACLDenialThreshold int
	// WindowDuration is the sliding window length. Default: 60s.
	WindowDuration time.Duration
	// WebhookURL is an optional endpoint for webhook alerts (empty = disabled).
	WebhookURL string
	// SMTP holds optional SMTP config (zero value = disabled).
	SMTP SMTPConfig
	// AuditQueueSize bounds the audit rows waiting for the writer.
	// When full, further rows are dropped and counted. Default: 1024.
	AuditQueueSize int
}

const (
	defaultAuditQueueSize = 1024
	// dropWarnInterval rate-limits the "audit queue full" warning.
	dropWarnInterval = 10 * time.Second
)

// SMTPConfig holds SMTP delivery settings.
type SMTPConfig struct {
	Host string
	Port int
	From string
	To   string
}

// DefaultConfig returns sensible production defaults.
func DefaultConfig() Config {
	return Config{
		AuthFailureThreshold: 3,
		ACLDenialThreshold:   10,
		WindowDuration:       60 * time.Second,
		AuditQueueSize:       defaultAuditQueueSize,
	}
}

// slidingCounter tracks event timestamps in a fixed sliding window.
// goroutine-safe.
type slidingCounter struct {
	mu     sync.Mutex
	window time.Duration
	events []time.Time
}

func newSlidingCounter(window time.Duration) *slidingCounter {
	return &slidingCounter{window: window}
}

// Add records one event at now and returns the current count within the window.
func (c *slidingCounter) Add(now time.Time) int {
	c.mu.Lock()
	defer c.mu.Unlock()

	cutoff := now.Add(-c.window)
	// Evict old entries.
	j := 0
	for _, t := range c.events {
		if t.After(cutoff) {
			c.events[j] = t
			j++
		}
	}
	c.events = c.events[:j]
	c.events = append(c.events, now)
	return len(c.events)
}

// Count returns the current count without adding.
func (c *slidingCounter) Count(now time.Time) int {
	c.mu.Lock()
	defer c.mu.Unlock()

	cutoff := now.Add(-c.window)
	n := 0
	for _, t := range c.events {
		if t.After(cutoff) {
			n++
		}
	}
	return n
}

// Emitter tracks security events and dispatches alerts when thresholds are exceeded.
type Emitter struct {
	cfg      Config
	audit    storage.AuditStore // may be nil
	handlers []AlertHandler

	mu       sync.Mutex
	counters map[string]*slidingCounter // key: "kind:principal"

	// intake guards closed, queue sends and inflight.Add: dispatch holds
	// it shared, Close exclusively, so nothing is queued or started
	// after Close has stopped intake.
	intake   sync.RWMutex
	closed   bool
	queue    chan auditJob  // nil when audit is nil
	inflight sync.WaitGroup // optional handler goroutines
	// workerDone closes when the audit writer exits.
	workerDone chan struct{}
	// abort ends the drain early: the in-flight write's context is
	// cancelled and the remaining rows are discarded.
	abort     context.Context
	abortFunc context.CancelFunc
	closeOnce sync.Once
	closeErr  error

	dropped      atomic.Uint64
	lastDropWarn atomic.Int64 // unix nanos
}

// auditJob is one queued audit row with its originating context
// (cancellation already detached).
type auditJob struct {
	ctx   context.Context
	alert Alert
}

// New creates an Emitter with the provided config and audit store.
// audit may be nil (alerts are still logged and dispatched; no rows are
// written and no writer goroutine starts). Call Close when done.
func New(cfg Config, audit storage.AuditStore) *Emitter {
	e := &Emitter{
		cfg:        cfg,
		audit:      audit,
		counters:   make(map[string]*slidingCounter),
		workerDone: make(chan struct{}),
	}
	e.abort, e.abortFunc = context.WithCancel(context.Background()) // #nosec G118 -- abortFunc is stored and invoked by Close
	if audit != nil {
		size := cfg.AuditQueueSize
		if size <= 0 {
			size = defaultAuditQueueSize
		}
		e.queue = make(chan auditJob, size)
		go e.writeAuditRows()
	} else {
		close(e.workerDone)
	}
	if cfg.WebhookURL != "" {
		e.handlers = append(e.handlers, webhookHandler(cfg.WebhookURL))
	}
	if cfg.SMTP.Host != "" && cfg.SMTP.To != "" {
		e.handlers = append(e.handlers, smtpHandler(cfg.SMTP))
	}
	return e
}

// AddHandler appends a custom AlertHandler. Call it before the emitter
// is shared: handlers are not guarded against concurrent dispatch.
func (e *Emitter) AddHandler(h AlertHandler) {
	e.handlers = append(e.handlers, h)
}

// Dropped returns how many audit rows were discarded because the queue
// was full, or because Close's deadline cut the drain short.
func (e *Emitter) Dropped() uint64 {
	return e.dropped.Load()
}

// Close stops intake (later alerts are logged but neither queued nor
// dispatched), then waits for the queued audit rows and in-flight
// handlers to finish. If ctx ends first, the in-flight write's context
// is cancelled, the remaining rows are discarded (and counted as
// dropped), and Close returns ctx's error once the writer has exited:
// no audit write happens after Close returns. Idempotent; later calls
// return the first call's result.
func (e *Emitter) Close(ctx context.Context) error {
	e.closeOnce.Do(func() {
		e.intake.Lock()
		e.closed = true
		if e.queue != nil {
			close(e.queue)
		}
		e.intake.Unlock()

		handlersDone := make(chan struct{})
		go func() {
			e.inflight.Wait()
			close(handlersDone)
		}()

		select {
		case <-e.workerDone:
		case <-ctx.Done():
			e.abortFunc()
			<-e.workerDone
			e.closeErr = fmt.Errorf("security: drain audit queue: %w", ctx.Err())
		}
		e.abortFunc()
		if e.closeErr != nil {
			return
		}
		select {
		case <-handlersDone:
		case <-ctx.Done():
			e.closeErr = fmt.Errorf("security: wait for alert handlers: %w", ctx.Err())
		}
	})
	return e.closeErr
}

// RecordAuthFailure records one authentication failure for principal.
// Fires an alert when the sliding-window count exceeds AuthFailureThreshold.
func (e *Emitter) RecordAuthFailure(ctx context.Context, principal string) {
	count := e.count(EventAuthFailure, principal)
	if count >= e.cfg.AuthFailureThreshold {
		e.dispatch(ctx, Alert{
			ID:        uuid.New().String(),
			Kind:      EventAuthFailure,
			Level:     AlertLevelCritical,
			Principal: principal,
			Message:   fmt.Sprintf("auth failure threshold exceeded: %d failures in %s", count, e.cfg.WindowDuration),
			Count:     count,
			OccuredAt: time.Now().UTC(),
		})
	}
}

// RecordACLDenial records one ACL denial for principal.
// Fires an alert when the sliding-window count exceeds ACLDenialThreshold.
func (e *Emitter) RecordACLDenial(ctx context.Context, principal string) {
	count := e.count(EventACLDenial, principal)
	if count >= e.cfg.ACLDenialThreshold {
		e.dispatch(ctx, Alert{
			ID:        uuid.New().String(),
			Kind:      EventACLDenial,
			Level:     AlertLevelWarn,
			Principal: principal,
			Message:   fmt.Sprintf("ACL denial threshold exceeded: %d denials in %s", count, e.cfg.WindowDuration),
			Count:     count,
			OccuredAt: time.Now().UTC(),
		})
	}
}

// RecordQuotaExhausted fires an immediate alert for quota exhaustion.
// No threshold — every occurrence is significant.
func (e *Emitter) RecordQuotaExhausted(ctx context.Context, principal, registry string) {
	e.dispatch(ctx, Alert{
		ID:        uuid.New().String(),
		Kind:      EventQuotaExhausted,
		Level:     AlertLevelWarn,
		Principal: principal,
		Message:   fmt.Sprintf("quota exhausted for registry %q", registry),
		Count:     1,
		Meta:      map[string]string{"registry": registry},
		OccuredAt: time.Now().UTC(),
	})
}

// RecordSigningFailure fires an immediate alert for a signing verification failure.
func (e *Emitter) RecordSigningFailure(ctx context.Context, detail string) {
	e.dispatch(ctx, Alert{
		ID:        uuid.New().String(),
		Kind:      EventSigningFailure,
		Level:     AlertLevelCritical,
		Message:   fmt.Sprintf("signing verification failed: %s", detail),
		Count:     1,
		Meta:      map[string]string{"detail": detail},
		OccuredAt: time.Now().UTC(),
	})
}

// RecordWorkerCrashLoop fires an immediate alert for a worker crash loop.
func (e *Emitter) RecordWorkerCrashLoop(ctx context.Context, workerID, reason string) {
	e.dispatch(ctx, Alert{
		ID:        uuid.New().String(),
		Kind:      EventWorkerCrashLoop,
		Level:     AlertLevelCritical,
		Message:   fmt.Sprintf("worker crash loop detected: worker=%s reason=%s", workerID, reason),
		Count:     1,
		Meta:      map[string]string{"worker_id": workerID, "reason": reason},
		OccuredAt: time.Now().UTC(),
	})
}

// --- internal ---

func (e *Emitter) count(kind, principal string) int {
	key := kind + ":" + principal
	now := time.Now()

	e.mu.Lock()
	c, ok := e.counters[key]
	if !ok {
		c = newSlidingCounter(e.cfg.WindowDuration)
		e.counters[key] = c
	}
	e.mu.Unlock()

	return c.Add(now)
}

// dispatch logs the alert, queues its audit row and starts the optional
// handlers, without blocking: a full queue drops the row. The context
// passed on is detached from the caller's cancellation so the work can
// outlive the request; Close bounds it instead.
func (e *Emitter) dispatch(ctx context.Context, a Alert) {
	slog.WarnContext(ctx, "security alert",
		"kind", a.Kind,
		"level", string(a.Level),
		"principal", a.Principal,
		"message", a.Message,
		"count", a.Count,
	)
	detached := context.WithoutCancel(ctx)

	e.intake.RLock()
	defer e.intake.RUnlock()
	if e.closed {
		return
	}
	if e.queue != nil {
		select {
		case e.queue <- auditJob{ctx: detached, alert: a}:
		default:
			e.noteDropped(ctx, 1)
		}
	}
	for _, h := range e.handlers {
		e.inflight.Add(1)
		go func(handler AlertHandler) {
			defer e.inflight.Done()
			handler(detached, a)
		}(h)
	}
}

// noteDropped counts n discarded audit rows and warns at most once per
// dropWarnInterval.
func (e *Emitter) noteDropped(ctx context.Context, n uint64) {
	total := e.dropped.Add(n)
	now := time.Now().UnixNano()
	last := e.lastDropWarn.Load()
	if now-last < int64(dropWarnInterval) || !e.lastDropWarn.CompareAndSwap(last, now) {
		return
	}
	slog.WarnContext(ctx, "security: audit rows dropped", "dropped_total", total)
}

// writeAuditRows is the single audit writer: it appends queued rows
// until Close closes the queue, or discards them once abort fires.
func (e *Emitter) writeAuditRows() {
	defer close(e.workerDone)
	for job := range e.queue {
		if e.abort.Err() != nil {
			e.noteDropped(job.ctx, 1)
			continue
		}
		ctx, cancel := context.WithCancel(job.ctx)
		stop := context.AfterFunc(e.abort, cancel)
		e.writeAuditRow(ctx, job.alert)
		stop()
		cancel()
	}
}

// writeAuditRow appends a to the audit log (event_class=security).
func (e *Emitter) writeAuditRow(ctx context.Context, a Alert) {
	payload := map[string]any{
		"event_class": "security",
		"kind":        a.Kind,
		"level":       string(a.Level),
		"message":     a.Message,
		"count":       a.Count,
	}
	if a.Principal != "" {
		payload["principal"] = a.Principal
	}
	for k, v := range a.Meta {
		payload[k] = v
	}
	entry := &storage.AuditEntry{
		ID:        a.ID,
		EventType: a.Kind,
		ObjectID:  "",
		Actor:     a.Principal,
		Payload:   payload,
		CreatedAt: a.OccuredAt,
	}
	// Best-effort: callers never see the error; the operator does.
	if err := e.audit.Append(ctx, entry); err != nil {
		slog.WarnContext(ctx, "security: audit append failed", "kind", a.Kind, "err", err)
	}
}

// webhookHandler returns an AlertHandler that POSTs alert JSON to url.
func webhookHandler(url string) AlertHandler {
	return func(ctx context.Context, a Alert) {
		body, err := json.Marshal(a)
		if err != nil {
			slog.Warn("security: webhook marshal error", "err", err)
			return
		}
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
		if err != nil {
			slog.Warn("security: webhook request error", "err", err)
			return
		}
		req.Header.Set("Content-Type", "application/json")
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			slog.Warn("security: webhook delivery error", "err", err)
			return
		}
		_ = resp.Body.Close()
		if resp.StatusCode >= 400 {
			slog.Warn("security: webhook non-2xx response", "status", resp.StatusCode)
		}
	}
}

// smtpHandler returns an AlertHandler that sends an email via SMTP.
func smtpHandler(cfg SMTPConfig) AlertHandler {
	return func(ctx context.Context, a Alert) {
		addr := fmt.Sprintf("%s:%d", cfg.Host, cfg.Port)
		subject := fmt.Sprintf("[Security Alert] %s: %s", a.Kind, a.Level)
		body := fmt.Sprintf("Subject: %s\r\nFrom: %s\r\nTo: %s\r\n\r\n%s\r\n\r\nPrincipal: %s\nCount: %d\nTime: %s\n",
			subject, cfg.From, cfg.To,
			a.Message,
			a.Principal,
			a.Count,
			a.OccuredAt.Format(time.RFC3339),
		)
		err := smtp.SendMail(addr, nil, cfg.From, []string{cfg.To}, []byte(body))
		if err != nil {
			slog.Warn("security: smtp delivery error", "err", err)
		}
	}
}
