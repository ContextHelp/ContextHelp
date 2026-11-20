package http

import (
	"encoding/json"
	"net/http"
	"strconv"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ideacrafterslabs/ctxt/internal/events"
)

// nextID returns the next frame and the id: line that came with it.
func (s *stream) nextID(t *testing.T) (events.Event, string) {
	t.Helper()
	ev := s.next(t)
	return ev, <-s.sseIDs
}

func (e *sseEnv) updated(t *testing.T, id string) int64 {
	t.Helper()
	return e.publish(t, events.TopicObjectUpdated, "worker.pool", events.ObjectUpdatedPayload{ObjectID: id, Type: "note", Fields: []string{"tags"}})
}

func (s *stream) seqs(t *testing.T, n int) []int64 {
	t.Helper()
	var out []int64
	for i := 0; i < n; i++ {
		_, id := s.nextID(t)
		v, err := strconv.ParseInt(id, 10, 64)
		require.NoError(t, err, "frame %d has no numeric id: %q", i, id)
		out = append(out, v)
	}
	return out
}

// Each object event carries the outbox seq as the SSE id and the row's
// UUIDv7 as the envelope id; a bus-only event carries no id.
func TestSSEFramesCarryOutboxIDs(t *testing.T) {
	e := newSSEEnv(t)
	own := e.open(t, capOwnerToken)
	s1 := e.updated(t, e.note)
	s2 := e.updated(t, e.contact)
	e.publish(t, events.TopicJobCompleted, "worker.pool", events.JobCompletedPayload{JobID: "j"})

	ev1, id1 := own.nextID(t)
	ev2, id2 := own.nextID(t)
	assert.Equal(t, strconv.FormatInt(s1, 10), id1)
	assert.Equal(t, strconv.FormatInt(s2, 10), id2)
	assert.Greater(t, s2, s1)
	rows, err := e.svc.Outbox().Since(t.Context(), 0, 0)
	require.NoError(t, err)
	require.Len(t, rows, 2)
	assert.Equal(t, rows[0].ID, ev1.ID, "envelope id is the row's id")
	assert.Equal(t, rows[1].ID, ev2.ID)
	job, jid := own.nextID(t)
	assert.Equal(t, string(events.TopicJobCompleted), job.Type)
	assert.Empty(t, jid, "a bus-only event has no id")
}

// A plain connect follows live: rows from before it are not replayed.
func TestSSEPlainConnectDoesNotReplay(t *testing.T) {
	e := newSSEEnv(t)
	e.updated(t, e.note)
	own := e.open(t, capOwnerToken)
	assert.Empty(t, own.quiet(200*time.Millisecond))
	live := e.updated(t, e.contact)
	_, id := own.nextID(t)
	assert.Equal(t, strconv.FormatInt(live, 10), id)
}

// Last-Event-ID replays the rows after it, then follows live; the
// header wins over the query parameter.
func TestSSELastEventIDResumes(t *testing.T) {
	e := newSSEEnv(t)
	var seqs []int64
	for i := 0; i < 5; i++ {
		seqs = append(seqs, e.updated(t, e.note))
	}
	for name, open := range map[string]func() *stream{
		"header": func() *stream { return e.openAt(t, capOwnerToken, strconv.FormatInt(seqs[1], 10), "") },
		"query":  func() *stream { return e.openAt(t, capOwnerToken, "", strconv.FormatInt(seqs[1], 10)) },
		"header wins": func() *stream {
			return e.openAt(t, capOwnerToken, strconv.FormatInt(seqs[1], 10), strconv.FormatInt(seqs[3], 10))
		},
	} {
		t.Run(name, func(t *testing.T) {
			s := open()
			require.Equal(t, http.StatusOK, s.status)
			assert.Equal(t, seqs[2:], s.seqs(t, 3))
			assert.Empty(t, s.quiet(150*time.Millisecond), "nothing beyond the head")
		})
	}
	t.Run("then live", func(t *testing.T) {
		s := e.openAt(t, capOwnerToken, strconv.FormatInt(seqs[3], 10), "")
		assert.Equal(t, seqs[4:], s.seqs(t, 1))
		live := e.updated(t, e.contact)
		assert.Equal(t, []int64{live}, s.seqs(t, 1))
	})
	t.Run("zero replays everything retained", func(t *testing.T) {
		s := e.openAt(t, capOwnerToken, "0", "")
		assert.Equal(t, seqs, s.seqs(t, 5))
	})
	t.Run("at the head replays nothing", func(t *testing.T) {
		head, err := e.svc.Outbox().Head(t.Context())
		require.NoError(t, err)
		s := e.openAt(t, capOwnerToken, strconv.FormatInt(head, 10), "")
		require.Equal(t, http.StatusOK, s.status)
		assert.Empty(t, s.quiet(150*time.Millisecond))
	})
}

// An id past retention is 410 EVENT_ID_EXPIRED before any stream
// starts, with the oldest retained id; an id ahead of the head is 410
// too; a malformed id is 400.
func TestSSELastEventIDErrors(t *testing.T) {
	e := newSSEEnv(t)
	var seqs []int64
	for i := 0; i < 4; i++ {
		seqs = append(seqs, e.updated(t, e.note))
	}
	_, err := e.svc.Outbox().Prune(t.Context(), time.Now().Add(time.Hour))
	require.NoError(t, err)

	decode := func(t *testing.T, s *stream) (string, map[string]any) {
		var env struct {
			Error struct {
				Code    string         `json:"code"`
				Details map[string]any `json:"details"`
			} `json:"error"`
		}
		require.NoError(t, json.Unmarshal([]byte(s.body), &env), s.body)
		return env.Error.Code, env.Error.Details
	}

	t.Run("expired", func(t *testing.T) {
		s := e.openAt(t, capOwnerToken, strconv.FormatInt(seqs[0], 10), "")
		require.Equal(t, http.StatusGone, s.status)
		assert.Contains(t, s.ctype, "application/json")
		code, d := decode(t, s)
		assert.Equal(t, CodeEventIDExpired, code)
		assert.EqualValues(t, seqs[3], d["oldest_retained"])
		assert.EqualValues(t, seqs[3], d["newest"])
		assert.EqualValues(t, 0, e.bus.active.Load(), "the refused request holds no subscription")
	})
	t.Run("zero is expired once rows were pruned", func(t *testing.T) {
		assert.Equal(t, http.StatusGone, e.openAt(t, capOwnerToken, "0", "").status)
	})
	t.Run("the oldest retained row minus one resumes", func(t *testing.T) {
		s := e.openAt(t, capOwnerToken, strconv.FormatInt(seqs[3]-1, 10), "")
		require.Equal(t, http.StatusOK, s.status)
		assert.Equal(t, seqs[3:], s.seqs(t, 1))
	})
	t.Run("ahead of the head", func(t *testing.T) {
		s := e.openAt(t, capOwnerToken, strconv.FormatInt(seqs[3]+100, 10), "")
		require.Equal(t, http.StatusGone, s.status)
		code, _ := decode(t, s)
		assert.Equal(t, CodeEventIDExpired, code)
	})
	t.Run("malformed", func(t *testing.T) {
		for _, bad := range []string{"abc", "-1", "1.5"} {
			s := e.openAt(t, capOwnerToken, bad, "")
			require.Equal(t, http.StatusBadRequest, s.status, bad)
			code, _ := decode(t, s)
			assert.Equal(t, CodeInvalidEventID, code)
		}
	})
}

// The viewer filter and the caller re-check apply to replayed rows as
// to live ones: hidden objects never reach a non-owner, readable ones
// arrive projected, and a revoked grant empties the replay.
func TestSSEReplayAppliesTheViewerFilter(t *testing.T) {
	e := newSSEEnv(t)
	e.publish(t, events.TopicObjectIngested, "worker.pool", events.ObjectIngestedPayload{ObjectID: e.note, Type: "note", Source: "secret-src"})
	e.publish(t, events.TopicObjectUpdated, "worker.pool", events.ObjectUpdatedPayload{ObjectID: e.note, Type: "note", Fields: []string{"tags"}})
	e.publish(t, events.TopicJobCompleted, "worker.pool", events.JobCompletedPayload{JobID: "j"})
	readable := e.publish(t, events.TopicObjectUpdated, "worker.pool", events.ObjectUpdatedPayload{ObjectID: e.contact, Type: "contact", Fields: []string{"summary"}})

	col := e.openAt(t, capColleagueToken, "0", "")
	require.Equal(t, http.StatusOK, col.status)
	ev, id := col.nextID(t)
	assert.Equal(t, strconv.FormatInt(readable, 10), id)
	var p map[string]any
	require.NoError(t, json.Unmarshal(ev.Data, &p))
	assert.Equal(t, e.contact, p["object_id"])
	assert.NotContains(t, p, "fields", "the viewer gets no field list")
	assert.Empty(t, col.quiet(250*time.Millisecond), "the hidden object's rows never arrive")

	t.Run("no grant", func(t *testing.T) {
		s := e.openAt(t, capStrangerToken, "0", "")
		require.Equal(t, http.StatusOK, s.status)
		assert.Empty(t, s.quiet(250*time.Millisecond))
	})
	t.Run("revoked grant", func(t *testing.T) {
		require.NoError(t, e.driver.Sharing().Grants().Revoke(ownerCtx(), e.sourcerGrant.ID, time.Now()))
		e.publish(t, events.TopicObjectIngested, "worker.pool", events.ObjectIngestedPayload{ObjectID: capSourced, Type: "note", Source: "gh-stars"})
		s := e.openAt(t, capSourcerToken, "0", "")
		assert.Empty(t, s.quiet(250*time.Millisecond), "a revoked grant replays nothing")
	})
	t.Run("revoked key ends a replay", func(t *testing.T) {
		e.revoke(capSourcerToken)
		s := e.openAt(t, capSourcerToken, "0", "")
		assert.Equal(t, http.StatusUnauthorized, s.status)
	})
}

// A delete in a replay follows the "seen on this connection" rule: it is
// delivered for an object the replay itself just delivered, and dropped
// for one the connection never saw (the object is gone, so the viewer
// cannot be judged to have been able to read it).
func TestSSEReplayDeleteRule(t *testing.T) {
	e := newSSEEnv(t)
	// contact: ingested then deleted, but still stored: the replay reads
	// it, so the viewer sees it exist, then see it deleted.
	e.publish(t, events.TopicObjectIngested, "worker.pool", events.ObjectIngestedPayload{ObjectID: e.contact, Type: "contact"})
	e.publish(t, events.TopicObjectDeleted, "service.objects", events.ObjectDeletedPayload{ObjectID: e.contact})
	// gone: only its delete is in the window.
	e.publish(t, events.TopicObjectDeleted, "service.objects", events.ObjectDeletedPayload{ObjectID: "cap-gone"})

	col := e.openAt(t, capColleagueToken, "0", "")
	assert.Equal(t, string(events.TopicObjectIngested), col.next(t).Type)
	assert.Equal(t, string(events.TopicObjectDeleted), col.next(t).Type)
	assert.Empty(t, col.quiet(250*time.Millisecond), "a delete for an object the connection never saw")
}

// Rows written while the replay runs arrive once, in order, between the
// replay and the live tail: no gap and no duplicate at the switch.
func TestSSEReplayToLiveSwitchHasNoGapOrDuplicate(t *testing.T) {
	e := newSSEEnv(t)
	oldPage := sseReplayPage
	sseReplayPage = 3
	t.Cleanup(func() { sseReplayPage = oldPage; sseAfterPage = nil })

	var want []int64
	for i := 0; i < 8; i++ {
		want = append(want, e.updated(t, e.note))
	}
	// During the replay, after each of its first three pages, another
	// row is written (and its bus wake-up published).
	pages := 0
	sseAfterPage = func() {
		pages++
		if pages <= 3 {
			want = append(want, e.updated(t, e.contact))
		}
	}

	s := e.openAt(t, capOwnerToken, "0", "")
	require.Equal(t, http.StatusOK, s.status)
	total := 8 + 3
	got := s.seqs(t, total)
	assert.Equal(t, want, got, "every row once, in seq order")
	// ... and the tail is live from there.
	live := e.updated(t, e.note)
	assert.Equal(t, []int64{live}, s.seqs(t, 1))
	assert.Empty(t, s.quiet(200*time.Millisecond), "nothing delivered twice")
}

// Rows written between the 410 check and the first read are not lost:
// the subscription predates the read.
func TestSSEConcurrentWritersLoseNothing(t *testing.T) {
	e := newSSEEnv(t)
	first := e.updated(t, e.note)
	s := e.openAt(t, capOwnerToken, strconv.FormatInt(first, 10), "")
	done := make(chan []int64)
	go func() {
		var seqs []int64
		for i := 0; i < 25; i++ {
			seqs = append(seqs, e.updated(t, e.contact))
		}
		done <- seqs
	}()
	want := <-done
	assert.Equal(t, want, s.seqs(t, len(want)))
}
