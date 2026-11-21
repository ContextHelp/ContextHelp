package http

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"

	"hop.top/kit/go/runtime/bus"

	"github.com/ideacrafterslabs/ctxt/internal/config"
	"github.com/ideacrafterslabs/ctxt/internal/events"
	"github.com/ideacrafterslabs/ctxt/internal/service"
	"github.com/ideacrafterslabs/ctxt/internal/sharing"
	"github.com/ideacrafterslabs/ctxt/internal/storage"
)

// sseBuffer is how many events one connection holds before it drops.
const sseBuffer = 64

// CodeEventIDExpired is the error code of the 410 a Last-Event-ID
// answers when the outbox no longer holds the rows after it (past
// server.events_retention), or the id is ahead of anything the
// instance wrote. The client resyncs from the API and reconnects
// without an id.
const CodeEventIDExpired = "EVENT_ID_EXPIRED"

// CodeInvalidEventID is the error code of the 400 a malformed
// Last-Event-ID (or last_event_id) answers.
const CodeInvalidEventID = "INVALID_EVENT_ID"

// Defaults for SSEConfig's zero fields.
const (
	defaultSSEReplayPage = 200
	defaultSSERecheckTTL = 2 * time.Second
)

// SSEConfig tunes GET /api/v1/events per router. The zero value is the
// production setting; tests set fields to make a replay span pages or
// every event re-check the caller, without touching shared state.
type SSEConfig struct {
	// ReplayPage is how many outbox rows one read returns. <= 0 uses
	// the default (200).
	ReplayPage int
	// RecheckTTL is how long the stream trusts the caller's last check:
	// each event re-authenticates the credential and re-resolves the
	// viewer unless the last check is younger than this. A revoked key
	// or grant therefore takes effect within this long (and at the next
	// heartbeat at the latest). The check is a few indexed reads and a
	// cache lookup, so the TTL only bounds the cost of an event burst.
	// <= 0 uses the default (2s).
	RecheckTTL time.Duration
	// AfterPage, when set, runs after each page of rows has been
	// written to the stream.
	AfterPage func()
}

func (c SSEConfig) replayPage() int {
	if c.ReplayPage > 0 {
		return c.ReplayPage
	}
	return defaultSSEReplayPage
}

func (c SSEConfig) recheckTTL() time.Duration {
	if c.RecheckTTL > 0 {
		return c.RecheckTTL
	}
	return defaultSSERecheckTTL
}

// HandleSSE streams events as Server-Sent Events to the client. Each
// event is a CloudEvents JSON envelope in the SSE `data:` field.
// The connection is held open until the client disconnects; its bus
// subscription ends with it.
//
// Object events (ingested, updated, deleted, raw_stored) are read from
// the event outbox (ADR-075 §4), in seq order, and carry `id: <seq>`
// and the row's UUIDv7 as the envelope id. The bus only wakes the
// stream: one cursor reads the outbox, so a reconnect with
// Last-Event-ID replays the rows after it and then follows live with no
// gap and no duplicate. An id older than the retention window is
// answered 410 Gone (EVENT_ID_EXPIRED) before the stream starts. The
// other event types (jobs, upgrades, ...) are owner-only, live from the
// bus, and carry no id.
//
// The owner (and every caller on a private instance) gets every event.
// Any other principal gets the object events for objects its viewer may
// read, filtered per event (see viewerEvents), replayed rows included; a
// viewer that may read nothing gets an empty stream, never a 403, so the
// answer does not reveal whether anything exists. While the stream is
// idle the handler writes an SSE comment every server.events_heartbeat.
//
// The stream follows the caller: each event (at most every
// SSEConfig.RecheckTTL) and each heartbeat authenticates the credential again
// and resolves the viewer as it is now, so a revoked grant applies to
// the next event, and a revoked or expired key ends the stream with a
// final comment.
//
// Route: GET /api/v1/events
func HandleSSE(svc *service.Service, cfg SSEConfig) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		// Verify the client supports streaming.
		flusher, ok := w.(http.Flusher)
		if !ok {
			WriteError(w, http.StatusInternalServerError, "STREAMING_UNSUPPORTED", "streaming not supported")
			return
		}
		after, resume, err := lastEventID(r)
		if err != nil {
			WriteError(w, http.StatusBadRequest, CodeInvalidEventID, err.Error())
			return
		}

		// A caller with no viewer attached is the zero viewer: nothing
		// visible. Only an explicit Unrestricted viewer sees everything.
		viewer, _ := sharing.FromContext(r.Context())
		st := &sseStream{
			w:          w,
			flusher:    flusher,
			ctx:        r.Context(),
			outbox:     svc.Outbox(),
			callerView: newCallerView(r.Context(), viewer, cfg.recheckTTL()),
			filter:     newViewerEvents(svc),
			wake:       make(chan struct{}, 1),
			ch:         make(chan bus.Event, sseBuffer),
			cursor:     after,
			pageSize:   cfg.replayPage(),
			afterPage:  cfg.AfterPage,
		}
		// The owner's stream subscribes to everything. Anyone else
		// subscribes to object events only, and always, even with no
		// grant yet: a grant made later then applies without a reconnect.
		pattern := "ctxt.runtime.object.*"
		if viewer.IsUnrestricted() {
			pattern = "#"
		}
		// Subscribe before reading the outbox: a row committed from now
		// on wakes the stream, and a row committed before is read. The
		// handler only hands off and never returns an error: that would
		// veto the publish. Unsubscribe on disconnect, or the subscriber
		// outlives the connection.
		unsub := svc.Bus.Subscribe(pattern, st.onBus)
		defer unsub()

		first, ok := st.start(resume)
		if !ok {
			return
		}
		st.run(sseHeartbeat(svc.Cfg), first)
	}
}

// sseStream is one open GET /api/v1/events connection. Its methods run
// on the connection's goroutine, except onBus, which runs on the
// publisher's.
type sseStream struct {
	w          http.ResponseWriter
	flusher    http.Flusher
	ctx        context.Context
	outbox     storage.OutboxStore // nil: object events come from the bus
	callerView *callerView
	filter     *viewerEvents
	wake       chan struct{}
	ch         chan bus.Event
	// cursor is the seq of the last outbox row read; only this
	// connection's goroutine moves it.
	cursor int64
	// pageSize is how many outbox rows one read returns; afterPage, when
	// set, runs after each page is written.
	pageSize  int
	afterPage func()
}

// onBus is the bus handler: an outbox topic only wakes the stream, any
// other event is handed to the loop.
func (st *sseStream) onBus(_ context.Context, e bus.Event) error {
	if st.outbox != nil && events.OutboxTopics[e.Topic] {
		select {
		case st.wake <- struct{}{}:
		default: // a wake-up is already pending
		}
		return nil
	}
	select {
	case st.ch <- e:
	default:
		// Drop on full buffer rather than blocking the bus.
	}
	return nil
}

// start settles where the stream begins and sends the headers. A plain
// connect starts at the outbox head. A resume reads its first page
// before any header goes out, so a stale id is still a 410. ok is false
// when the request was answered with an error.
func (st *sseStream) start(resume bool) (first []storage.OutboxRow, ok bool) {
	if st.outbox != nil {
		var err error
		if first, err = st.position(resume); err != nil {
			st.refuse(err)
			return nil, false
		}
	}
	h := st.w.Header()
	h.Set("Content-Type", "text/event-stream")
	h.Set("Cache-Control", "no-cache")
	h.Set("Connection", "keep-alive")
	h.Set("X-Accel-Buffering", "no")
	st.w.WriteHeader(http.StatusOK)
	// Send the headers now: until they arrive, a browser EventSource
	// stays "connecting" and never fires open.
	st.flusher.Flush()
	return first, true
}

// errEventIDGone is position's refusal of a cursor the outbox cannot
// resume from.
type errEventIDGone struct {
	msg          string
	oldest, head int64
}

func (e *errEventIDGone) Error() string { return e.msg }

func (st *sseStream) position(resume bool) ([]storage.OutboxRow, error) {
	head, err := st.outbox.Head(st.ctx)
	if err != nil {
		return nil, err
	}
	if !resume {
		// A plain connect follows live: nothing before now.
		st.cursor = head
		return nil, nil
	}
	if st.cursor > head {
		return nil, &errEventIDGone{msg: "the event id is ahead of the newest event this instance wrote", head: head}
	}
	rows, err := st.outbox.Since(st.ctx, st.cursor, st.pageSize)
	var exp *storage.OutboxExpiredError
	if errors.As(err, &exp) {
		return nil, &errEventIDGone{msg: "the event id is older than the retained events", oldest: exp.Oldest, head: head}
	}
	return rows, err
}

func (st *sseStream) refuse(err error) {
	var gone *errEventIDGone
	if errors.As(err, &gone) {
		writeEventIDGone(st.w, gone.msg, gone.oldest, gone.head)
		return
	}
	WriteError(st.w, http.StatusInternalServerError, "INTERNAL_ERROR", "event outbox unavailable")
}

// run writes the first page, drains what was committed since the
// subscription, then streams until the client disconnects or the caller
// stops authenticating.
func (st *sseStream) run(every time.Duration, first []storage.OutboxRow) {
	heartbeat := time.NewTicker(every)
	defer heartbeat.Stop()

	// The first page was read before the headers went out; write it.
	if len(first) > 0 && !st.page(first) {
		return
	}
	// More than a page, and rows committed since the subscription, are
	// read at once.
	if !st.drain() {
		return
	}
	for {
		select {
		case <-st.ctx.Done():
			return
		case <-heartbeat.C:
			// Re-check the caller on every tick, so a revoked key
			// ends an idle stream too.
			if _, ok := st.callerView.current(true); !ok {
				closeStream(st.w, st.flusher)
				return
			}
			// A tick also reads the outbox, in case a wake-up was lost
			// (a row written by another process).
			if !st.drain() {
				return
			}
			fmt.Fprint(st.w, ": heartbeat\n\n")
			st.flusher.Flush()
		case <-st.wake:
			if !st.drain() {
				return
			}
		case e := <-st.ch:
			if !st.write(e, 0, "") {
				return
			}
		}
	}
}

// write sends one event to this viewer, or drops it. It reports false
// when the caller no longer authenticates and the stream must end. seq
// is the outbox row's seq, 0 for a bus event; id replaces the envelope
// id when set.
func (st *sseStream) write(e bus.Event, seq int64, id string) bool {
	v, ok := st.callerView.current(false)
	if !ok {
		closeStream(st.w, st.flusher)
		return false
	}
	var ce events.Event
	var err error
	if v.IsUnrestricted() {
		ce, err = events.CloudEvent(e)
	} else {
		var send bool
		ce, send, err = st.filter.project(sharing.WithViewer(st.ctx, v), e)
		if !send && err == nil {
			return true
		}
	}
	if err != nil {
		slog.Warn("events: skipping event", "topic", e.Topic, "err", err)
		return true
	}
	if id != "" {
		ce.ID = id
	}
	data, err := json.Marshal(ce)
	if err != nil {
		return true
	}
	if seq > 0 {
		fmt.Fprintf(st.w, "id: %d\n", seq)
	}
	fmt.Fprintf(st.w, "data: %s\n\n", data)
	st.flusher.Flush()
	return true
}

// page writes a page of outbox rows and advances the cursor past each,
// sent or dropped.
func (st *sseStream) page(rows []storage.OutboxRow) bool {
	for _, row := range rows {
		st.cursor = row.Seq
		e := bus.Event{
			Topic:     bus.Topic(row.Topic),
			Source:    row.Source,
			Timestamp: row.OccurredAt,
			Payload:   json.RawMessage(row.Payload),
		}
		if !st.write(e, row.Seq, row.ID) {
			return false
		}
	}
	if len(rows) > 0 && st.afterPage != nil {
		st.afterPage()
	}
	return true
}

// drain reads the outbox from the cursor to its end, a page at a time.
// Only this goroutine moves the cursor, so a row is written once and in
// order, whether it arrived before the connection (a replay) or after.
func (st *sseStream) drain() bool {
	if st.outbox == nil {
		return true
	}
	for {
		rows, err := st.outbox.Since(st.ctx, st.cursor, st.pageSize)
		if err != nil {
			if st.ctx.Err() == nil {
				slog.Warn("events: outbox read", "err", err)
				fmt.Fprint(st.w, ": closed: events no longer retained\n\n")
				st.flusher.Flush()
			}
			return false
		}
		if !st.page(rows) {
			return false
		}
		if len(rows) < st.pageSize {
			return true
		}
	}
}

// lastEventID is the id a reconnecting client resumes after: the
// Last-Event-ID header an EventSource sends, or the last_event_id query
// parameter for clients that cannot set headers (the header wins).
// resume is false when neither is present. An id is a decimal seq; "0"
// replays everything retained.
func lastEventID(r *http.Request) (after int64, resume bool, err error) {
	raw := strings.TrimSpace(r.Header.Get("Last-Event-ID"))
	if raw == "" {
		raw = strings.TrimSpace(r.URL.Query().Get("last_event_id"))
	}
	if raw == "" {
		return 0, false, nil
	}
	n, perr := strconv.ParseInt(raw, 10, 64)
	if perr != nil || n < 0 {
		return 0, false, fmt.Errorf("Last-Event-ID must be an event id from this stream (a non-negative integer)")
	}
	return n, true, nil
}

// writeEventIDGone answers a Last-Event-ID the outbox cannot resume
// from: 410 Gone, with the oldest retained id and the newest id in the
// details.
func writeEventIDGone(w http.ResponseWriter, msg string, oldest, head int64) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusGone)
	_ = json.NewEncoder(w).Encode(ErrorEnvelope{Error: ErrorBody{
		Code:    CodeEventIDExpired,
		Message: msg + "; resync from the API and reconnect without Last-Event-ID",
		Details: map[string]any{"oldest_retained": oldest, "newest": head},
	}})
}

// sseHeartbeat is the interval between heartbeat comments:
// server.events_heartbeat, or the default when it is unset. A negative
// value is treated as unset too, so the ticker is always valid.
func sseHeartbeat(cfg config.Config) time.Duration {
	if d := cfg.Server.EventsHeartbeat; d > 0 {
		return d
	}
	return config.DefaultEventsHeartbeat
}

// closeStream ends a stream whose caller no longer authenticates, with a
// final comment line so the client sees why. An EventSource reconnects
// and is then answered 401.
func closeStream(w http.ResponseWriter, f http.Flusher) {
	fmt.Fprint(w, ": closed: credential no longer valid\n\n")
	f.Flush()
}

// callerView is the caller's current viewer for one stream.
type callerView struct {
	ctx     context.Context
	recheck Recheck
	v       sharing.Viewer
	at      time.Time
	ttl     time.Duration // how long a check is trusted
}

func newCallerView(ctx context.Context, v sharing.Viewer, ttl time.Duration) *callerView {
	return &callerView{ctx: ctx, recheck: recheckFrom(ctx), v: v, at: time.Now(), ttl: ttl}
}

// current returns the caller's viewer, checking again when force is set
// or the last check is older than its TTL. false means the caller
// no longer authenticates, or its viewer cannot be resolved (fail
// closed): the stream must end. With no Recheck (a private instance)
// the viewer never changes.
func (c *callerView) current(force bool) (sharing.Viewer, bool) {
	if c.recheck == nil {
		return c.v, true
	}
	if !force && time.Since(c.at) < c.ttl {
		return c.v, true
	}
	v, err := c.recheck(c.ctx)
	if err != nil {
		return sharing.Viewer{}, false
	}
	c.v, c.at = v, time.Now()
	return v, true
}
