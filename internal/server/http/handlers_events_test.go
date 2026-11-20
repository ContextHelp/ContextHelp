package http

import (
	"bufio"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"hop.top/kit/go/runtime/bus"

	"github.com/ideacrafterslabs/ctxt/internal/events"
)

// countingBus counts the subscriptions a bus holds now.
type countingBus struct {
	bus.Bus
	active atomic.Int64
}

func (c *countingBus) Subscribe(p string, h bus.Handler) bus.Unsubscribe {
	c.active.Add(1)
	un := c.Bus.Subscribe(p, h)
	var once atomic.Bool
	return func() {
		if once.CompareAndSwap(false, true) {
			c.active.Add(-1)
		}
		un()
	}
}

func (c *countingBus) SubscribeAsync(p string, h bus.AsyncHandler) bus.Unsubscribe {
	c.active.Add(1)
	un := c.Bus.SubscribeAsync(p, h)
	var once atomic.Bool
	return func() {
		if once.CompareAndSwap(false, true) {
			c.active.Add(-1)
		}
		un()
	}
}

// sseEnv is the capability fixture behind a real listener, with a
// counting bus under the service.
type sseEnv struct {
	*capabilityEnv
	srv *httptest.Server
	bus *countingBus
}

func newSSEEnv(t *testing.T) *sseEnv {
	t.Helper()
	e := newCapabilityEnv(t)
	cb := &countingBus{Bus: e.svc.Bus}
	e.svc.Bus = cb
	srv := httptest.NewServer(e.router)
	t.Cleanup(srv.Close)
	return &sseEnv{capabilityEnv: e, srv: srv, bus: cb}
}

// stream is one open GET /api/v1/events connection.
type stream struct {
	status int
	ctype  string
	frames chan events.Event
	lines  chan string // comment lines
	cancel context.CancelFunc
}

func (e *sseEnv) open(t *testing.T, token string) *stream {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, e.srv.URL+"/api/v1/events", nil)
	require.NoError(t, err)
	req.Header.Set("Authorization", "Bearer "+token)
	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	s := &stream{
		status: resp.StatusCode,
		ctype:  resp.Header.Get("Content-Type"),
		frames: make(chan events.Event, 64),
		lines:  make(chan string, 64),
		cancel: cancel,
	}
	go func() {
		defer resp.Body.Close()
		sc := bufio.NewScanner(resp.Body)
		for sc.Scan() {
			line := sc.Text()
			switch {
			case strings.HasPrefix(line, "data: "):
				var ev events.Event
				if json.Unmarshal([]byte(strings.TrimPrefix(line, "data: ")), &ev) == nil {
					s.frames <- ev
				}
			case strings.HasPrefix(line, ":"):
				select {
				case s.lines <- line:
				default:
				}
			}
		}
	}()
	t.Cleanup(cancel)
	return s
}

// next returns the next frame or fails after the wait.
func (s *stream) next(t *testing.T) events.Event {
	t.Helper()
	select {
	case ev := <-s.frames:
		return ev
	case <-time.After(3 * time.Second):
		t.Fatal("no event frame")
		return events.Event{}
	}
}

// quiet reports that no frame arrives within d.
func (s *stream) quiet(d time.Duration) []events.Event {
	var got []events.Event
	deadline := time.After(d)
	for {
		select {
		case ev := <-s.frames:
			got = append(got, ev)
		case <-deadline:
			return got
		}
	}
}

func (e *sseEnv) waitSubscribed(t *testing.T, n int64) {
	t.Helper()
	require.Eventually(t, func() bool { return e.bus.active.Load() >= n }, 3*time.Second, 5*time.Millisecond)
}

func (e *sseEnv) publish(t *testing.T, topic bus.Topic, source string, payload any) {
	t.Helper()
	require.NoError(t, e.svc.Bus.Publish(context.Background(), bus.NewEvent(topic, source, payload)))
}

// Every connection's bus subscription ends with the connection: N
// connects and disconnects leave the subscriber count where it began.
func TestSSEUnsubscribesOnDisconnect(t *testing.T) {
	e := newSSEEnv(t)
	base := e.bus.active.Load()

	const n = 6
	var open []*stream
	for i := 0; i < n; i++ {
		token := capOwnerToken
		if i%2 == 1 {
			token = capColleagueToken
		}
		open = append(open, e.open(t, token))
	}
	e.waitSubscribed(t, base+n)
	for _, s := range open {
		s.cancel()
	}
	require.Eventually(t, func() bool { return e.bus.active.Load() == base }, 3*time.Second, 10*time.Millisecond,
		"subscribers after disconnect: %d, before: %d", e.bus.active.Load(), base)
}

// A non-owner key receives only the object events for objects its
// viewer reads; a hidden object's ingest, update and delete, and every
// event that is not about an object, never reach it.
func TestSSEColleagueGetsOnlyReadableObjectEvents(t *testing.T) {
	e := newSSEEnv(t)
	col := e.open(t, capColleagueToken)
	require.Equal(t, http.StatusOK, col.status)
	require.Contains(t, col.ctype, "text/event-stream")
	e.waitSubscribed(t, 1)

	e.publish(t, events.TopicObjectIngested, "worker.pool", events.ObjectIngestedPayload{ObjectID: e.note, Type: "note", Source: "secret-src"})
	e.publish(t, events.TopicObjectUpdated, "worker.pool", events.ObjectUpdatedPayload{ObjectID: e.note, Type: "note", Fields: []string{"tags"}})
	e.publish(t, events.TopicObjectDeleted, "service.objects", events.ObjectDeletedPayload{ObjectID: e.note})
	e.publish(t, events.TopicJobCompleted, "worker.pool", events.JobCompletedPayload{JobID: "job-9"})
	e.publish(t, events.TopicObjectIngested, "worker.pool", events.ObjectIngestedPayload{ObjectID: e.contact, Type: "contact", Source: "cli", SourceKey: "k-1"})

	ev := col.next(t)
	assert.Equal(t, string(events.TopicObjectIngested), ev.Type)
	assert.Contains(t, string(ev.Data), e.contact)
	rest := col.quiet(300 * time.Millisecond)
	assert.Empty(t, rest, "frames after the readable one")
	assert.NotContains(t, string(ev.Data), e.note)
	assert.NotContains(t, string(ev.Data), "k-1", "source_key is not part of what a viewer reads")
}

// An update carries the object and its type, not the list of changed
// fields, which can name a field the viewer's policy withholds.
func TestSSEUpdateCarriesNoFieldListForAViewer(t *testing.T) {
	e := newSSEEnv(t)
	col := e.open(t, capColleagueToken)
	e.waitSubscribed(t, 1)
	e.publish(t, events.TopicObjectUpdated, "worker.pool", events.ObjectUpdatedPayload{ObjectID: e.contact, Type: "contact", Fields: []string{"tags", "summary"}})

	ev := col.next(t)
	var p map[string]any
	require.NoError(t, json.Unmarshal(ev.Data, &p))
	assert.Equal(t, e.contact, p["object_id"])
	assert.NotContains(t, p, "fields")
}

// A delete passes only for an object the connection already saw: the
// viewer knew it existed. A delete of any other ID is dropped.
func TestSSEDeleteOnlyForObjectsTheViewerSaw(t *testing.T) {
	e := newSSEEnv(t)
	col := e.open(t, capColleagueToken)
	e.waitSubscribed(t, 1)

	// Never seen on this connection: dropped, readable or not.
	e.publish(t, events.TopicObjectDeleted, "service.objects", events.ObjectDeletedPayload{ObjectID: e.contact})
	e.publish(t, events.TopicObjectDeleted, "service.objects", events.ObjectDeletedPayload{ObjectID: e.note})
	assert.Empty(t, col.quiet(300*time.Millisecond), "deletes of unseen objects")

	// Seen, then deleted: delivered once.
	e.publish(t, events.TopicObjectIngested, "worker.pool", events.ObjectIngestedPayload{ObjectID: e.contact, Type: "contact"})
	assert.Equal(t, string(events.TopicObjectIngested), col.next(t).Type)
	e.publish(t, events.TopicObjectDeleted, "service.objects", events.ObjectDeletedPayload{ObjectID: e.contact})
	del := col.next(t)
	assert.Equal(t, string(events.TopicObjectDeleted), del.Type)
	assert.Contains(t, string(del.Data), e.contact)
	e.publish(t, events.TopicObjectDeleted, "service.objects", events.ObjectDeletedPayload{ObjectID: e.contact})
	assert.Empty(t, col.quiet(200*time.Millisecond), "a second delete of the same object")
}

// A caller with no grant gets 200 and an empty stream, not a 403 that
// would say the route exists for others; it holds no subscription.
func TestSSENoGrantGetsAnEmptyStream(t *testing.T) {
	e := newSSEEnv(t)
	base := e.bus.active.Load()
	s := e.open(t, capStrangerToken)
	require.Equal(t, http.StatusOK, s.status)
	require.Contains(t, s.ctype, "text/event-stream")

	e.publish(t, events.TopicObjectIngested, "worker.pool", events.ObjectIngestedPayload{ObjectID: e.contact, Type: "contact"})
	e.publish(t, events.TopicJobCompleted, "worker.pool", events.JobCompletedPayload{JobID: "j"})
	assert.Empty(t, s.quiet(300*time.Millisecond))
	assert.Equal(t, base, e.bus.active.Load(), "a viewer that reads nothing needs no subscription")
}

// The owner's stream is unchanged: every event, as a CloudEvent with
// the publisher's whole payload, jobs and all.
func TestSSEOwnerGetsEveryEventUnchanged(t *testing.T) {
	e := newSSEEnv(t)
	own := e.open(t, capOwnerToken)
	e.waitSubscribed(t, 1)

	e.publish(t, events.TopicObjectUpdated, "worker.pool", events.ObjectUpdatedPayload{ObjectID: e.note, Type: "note", Fields: []string{"tags"}})
	e.publish(t, events.TopicJobCompleted, "worker.pool", events.JobCompletedPayload{JobID: "job-1", ObjectCount: 1})
	e.publish(t, events.TopicObjectDeleted, "service.objects", events.ObjectDeletedPayload{ObjectID: "never-seen"})

	up := own.next(t)
	assert.Equal(t, "1.0", up.SpecVersion)
	assert.Equal(t, string(events.TopicObjectUpdated), up.Type)
	assert.Equal(t, "worker.pool", up.Source)
	assert.NotEmpty(t, up.ID)
	assert.JSONEq(t, `{"object_id":"`+e.note+`","type":"note","fields":["tags"]}`, string(up.Data))
	assert.Equal(t, string(events.TopicJobCompleted), own.next(t).Type)
	assert.Equal(t, string(events.TopicObjectDeleted), own.next(t).Type)
}
