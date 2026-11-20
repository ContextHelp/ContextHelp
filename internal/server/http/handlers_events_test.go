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
// counting bus under the service and a short heartbeat.
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
		open = append(open, e.open(t, capOwnerToken))
	}
	e.waitSubscribed(t, base+n)
	for _, s := range open {
		s.cancel()
	}
	require.Eventually(t, func() bool { return e.bus.active.Load() == base }, 3*time.Second, 10*time.Millisecond,
		"subscribers after disconnect: %d, before: %d", e.bus.active.Load(), base)
}
