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
// Optional handlers: webhook POST (JSON) and SMTP email. Every handler
// runs with a context bounded by Config.DeliveryTimeout and cancelled
// when Close's deadline passes. Outbound sends go through kit's network
// policy: a context carrying kit's offline mark is refused.
package security

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"net/smtp"
	"net/url"
	"strconv"
	"sync"
	"sync/atomic"
	"time"

	"github.com/google/uuid"
	"hop.top/kit/go/core/netpolicy"
	kitbus "hop.top/kit/go/runtime/bus"
	webhooksink "hop.top/kit/go/runtime/notify/sinks/webhook"

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

// AlertHandler processes a security alert. Each call runs on its own
// goroutine with a context that ends after Config.DeliveryTimeout, or
// when Close's deadline passes; implementations must return once it
// ends.
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
	// DeliveryTimeout bounds each alert handler call (one webhook POST,
	// one SMTP session). Default: 10s.
	DeliveryTimeout time.Duration
}

const (
	defaultAuditQueueSize  = 1024
	defaultDeliveryTimeout = 10 * time.Second
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
		DeliveryTimeout:      defaultDeliveryTimeout,
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
	// sends is the handlers' lifetime: Close cancels it once its
	// deadline passes (or the handlers are done), which ends every send
	// still in flight.
	sends     context.Context
	stopSends context.CancelFunc
	timeout   time.Duration // per handler call
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
	e.sends, e.stopSends = context.WithCancel(context.Background()) // #nosec G118 -- stopSends is stored and invoked by Close
	e.timeout = cfg.DeliveryTimeout
	if e.timeout <= 0 {
		e.timeout = defaultDeliveryTimeout
	}
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
		e.handlers = append(e.handlers, webhookHandler(cfg.WebhookURL, newWebhookClient(http.DefaultTransport)))
	}
	if cfg.SMTP.Host != "" && cfg.SMTP.To != "" {
		e.handlers = append(e.handlers, smtpHandler(cfg.SMTP, nil))
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
// no audit write happens after Close returns. Handler sends still in
// flight at that point are cancelled too. Idempotent; later calls
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
		// Every return path ends the sends still in flight.
		defer e.stopSends()

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
	// The detached context keeps the caller's values (kit's offline
	// mark among them) and drops its cancellation.
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
			sendCtx, cancel := context.WithTimeout(detached, e.timeout)
			defer cancel()
			stop := context.AfterFunc(e.sends, cancel)
			defer stop()
			handler(sendCtx, a)
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

// newWebhookClient returns the client webhook alerts are posted with.
// base (nil means http.DefaultTransport) is wrapped in kit's offline
// guard explicitly, so --offline holds whether or not netpolicy.Install
// ran. Redirects are not followed: the alert goes to the configured URL
// or nowhere, and a 3xx is reported like any other non-2xx answer. The
// per-call deadline comes from the handler context.
func newWebhookClient(base http.RoundTripper) *http.Client {
	return &http.Client{
		Transport: netpolicy.Guard(base),
		CheckRedirect: func(*http.Request, []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
}

// alertTemplate renders the Alert carried as a bus event's payload as
// the webhook body, keeping the wire format the plain Alert JSON.
type alertTemplate struct{}

func (alertTemplate) Render(e kitbus.Event) ([]byte, string, error) {
	body, err := json.Marshal(e.Payload)
	if err != nil {
		return nil, "", fmt.Errorf("security: marshal alert: %w", err)
	}
	return body, "application/json", nil
}

// webhookHandler returns an AlertHandler that POSTs alert JSON to
// target through kit's webhook sink.
func webhookHandler(target string, client *http.Client) AlertHandler {
	sink := webhooksink.New(target,
		webhooksink.WithHTTPClient(client),
		webhooksink.WithTemplate(alertTemplate{}),
	)
	// The URL is a secret (it often embeds a token): log its origin only.
	shown := "webhook"
	if u, err := url.Parse(target); err == nil && u.Host != "" {
		shown = u.Scheme + "://" + u.Host
	}
	return func(ctx context.Context, a Alert) {
		err := sink.Drain(ctx, kitbus.Event{Topic: kitbus.Topic(a.Kind), Source: "security", Timestamp: a.OccuredAt, Payload: a})
		if err == nil {
			return
		}
		var ue *url.Error
		if errors.As(err, &ue) {
			ue.URL = shown
		}
		slog.Warn("security: webhook delivery error", "endpoint", shown, "err", err)
	}
}

// smtpHandler returns an AlertHandler that sends an email via SMTP.
// dial (nil means a plain net.Dialer) is wrapped in kit's offline guard.
func smtpHandler(cfg SMTPConfig, dial netpolicy.DialFunc) AlertHandler {
	dial = netpolicy.GuardDial(dial)
	return func(ctx context.Context, a Alert) {
		subject := fmt.Sprintf("[Security Alert] %s: %s", a.Kind, a.Level)
		body := fmt.Sprintf("Subject: %s\r\nFrom: %s\r\nTo: %s\r\n\r\n%s\r\n\r\nPrincipal: %s\nCount: %d\nTime: %s\n",
			subject, cfg.From, cfg.To,
			a.Message,
			a.Principal,
			a.Count,
			a.OccuredAt.Format(time.RFC3339),
		)
		if err := sendMail(ctx, dial, cfg, []byte(body)); err != nil {
			slog.Warn("security: smtp delivery error", "err", err)
		}
	}
}

// sendMail is smtp.SendMail bounded by ctx: net/smtp has no context
// support (and kit's SMTP mailer honours ctx only while dialing), so
// ctx's deadline is set on the connection and its cancellation closes
// it, ending a conversation the server stalls at any step. STARTTLS is
// used when the server offers it, as smtp.SendMail does.
func sendMail(ctx context.Context, dial netpolicy.DialFunc, cfg SMTPConfig, msg []byte) (err error) {
	conn, err := dial(ctx, "tcp", net.JoinHostPort(cfg.Host, strconv.Itoa(cfg.Port)))
	if err != nil {
		return err
	}
	if dl, ok := ctx.Deadline(); ok {
		_ = conn.SetDeadline(dl)
	}
	stop := context.AfterFunc(ctx, func() { _ = conn.Close() })
	defer func() {
		stop()
		if err != nil && ctx.Err() != nil {
			err = fmt.Errorf("%w: %w", ctx.Err(), err)
		}
	}()

	c, err := smtp.NewClient(conn, cfg.Host)
	if err != nil {
		_ = conn.Close()
		return err
	}
	defer func() { _ = c.Close() }()
	if ok, _ := c.Extension("STARTTLS"); ok {
		if err := c.StartTLS(&tls.Config{ServerName: cfg.Host, MinVersion: tls.VersionTLS12}); err != nil {
			return err
		}
	}
	if err := c.Mail(cfg.From); err != nil {
		return err
	}
	if err := c.Rcpt(cfg.To); err != nil {
		return err
	}
	w, err := c.Data()
	if err != nil {
		return err
	}
	if _, err := w.Write(msg); err != nil {
		_ = w.Close()
		return err
	}
	if err := w.Close(); err != nil {
		return err
	}
	return c.Quit()
}
