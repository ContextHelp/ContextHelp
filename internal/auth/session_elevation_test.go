package auth_test

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ideacrafterslabs/ctxt/internal/auth"
	"github.com/ideacrafterslabs/ctxt/internal/config"
	"github.com/ideacrafterslabs/ctxt/internal/storage"
)

// elevationLog records the elevation events Sessions reports.
type elevationLog struct {
	mu     sync.Mutex
	events []auth.ElevationEvent
}

func (l *elevationLog) record(_ context.Context, e auth.ElevationEvent) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.events = append(l.events, e)
}

func (l *elevationLog) all() []auth.ElevationEvent {
	l.mu.Lock()
	defer l.mu.Unlock()
	return append([]auth.ElevationEvent(nil), l.events...)
}

// elevationFixture is the session fixture with an elevation log and a
// browser session of the person, signed in through a link identity.
type elevationFixture struct {
	*fixture
	log    *elevationLog
	secret string
	// session is the person's session principal as minted.
	session *auth.Principal
}

func newElevationFixture(t *testing.T, idle time.Duration) *elevationFixture {
	t.Helper()
	f := &elevationFixture{fixture: newFixture(t), log: &elevationLog{}}
	var err error
	f.sessions, err = auth.NewSessions(f.drv.UISessions(), f.drv.Auth(), f.keys, auth.SessionOptions{
		Now: f.clock.now, ElevationIdle: idle, OnElevation: f.log.record,
	})
	require.NoError(t, err)
	idn := &storage.Identity{PrincipalID: f.person.ID, Method: storage.IdentityMethodLink, Subject: "link-alice", CreatedAt: f.clock.t}
	require.NoError(t, f.drv.Auth().Identities().Create(context.Background(), idn))
	m, err := f.sessions.MintFor(context.Background(), f.person.ID, idn.ID, auth.ClientInfo{UserAgent: "test"})
	require.NoError(t, err)
	f.secret, f.session = m.Secret, m.Principal
	return f
}

// request authenticates the person's cookie as one request would.
func (f *elevationFixture) request() *auth.Principal {
	f.t.Helper()
	p, _, err := f.sessions.Authenticate(context.Background(), f.secret)
	require.NoError(f.t, err)
	return p
}

// row reads the person's session row.
func (f *elevationFixture) row() *storage.UISession {
	f.t.Helper()
	s, err := f.drv.UISessions().Get(context.Background(), f.session.Meta[auth.MetaSessionID])
	require.NoError(f.t, err)
	return s
}

// An elevated session names its grant on every request and refreshes
// the elevation's last activity; the session itself is unchanged.
func TestSessions_ElevationNamesTheGrantOnEveryRequest(t *testing.T) {
	f := newElevationFixture(t, 0)
	assert.Empty(t, f.request().Meta[auth.MetaStewardGrantID], "a new session is the ordinary view")

	at, err := f.sessions.Elevate(context.Background(), f.request(), "stg_matter")
	require.NoError(t, err)
	assert.Equal(t, f.clock.t, at)
	events := f.log.all()
	require.Len(t, events, 1)
	assert.Equal(t, auth.ElevationEvent{
		Started: true, PrincipalID: f.person.ID, SessionID: f.session.Meta[auth.MetaSessionID], GrantID: "stg_matter", At: f.clock.t,
	}, events[0])

	for i := 0; i < 3; i++ {
		f.clock.advance(10 * time.Minute)
		p := f.request()
		assert.Equal(t, "stg_matter", p.Meta[auth.MetaStewardGrantID], "request %d", i)
		assert.Equal(t, f.person.ID, p.ID)
		require.NotNil(t, f.row().StewardActiveAt)
		assert.Equal(t, f.clock.t, *f.row().StewardActiveAt, "every request refreshes the elevation")
	}
	assert.Len(t, f.log.all(), 1, "a refresh is no event")
}

// Idle past steward.elevation_idle ends the elevation: the request is
// the ordinary view, not a refusal, the row is cleared and the end is
// reported; the session lives on.
func TestSessions_ElevationEndsWhenIdle(t *testing.T) {
	for name, idle := range map[string]time.Duration{"default": 0, "configured": 5 * time.Minute} {
		t.Run(name, func(t *testing.T) {
			f := newElevationFixture(t, idle)
			want := idle
			if want == 0 {
				want = config.DefaultStewardElevationIdle
			}
			_, err := f.sessions.Elevate(context.Background(), f.request(), "stg_matter")
			require.NoError(t, err)
			f.clock.advance(want - time.Millisecond)
			assert.Equal(t, "stg_matter", f.request().Meta[auth.MetaStewardGrantID], "just inside the idle window")

			f.clock.advance(want)
			p := f.request()
			assert.Empty(t, p.Meta[auth.MetaStewardGrantID], "idle: the ordinary view")
			assert.Equal(t, f.person.ID, p.ID, "the session survives")
			assert.Empty(t, f.row().StewardGrantID, "the elevation is cleared")
			assert.Nil(t, f.row().StewardActiveAt)
			events := f.log.all()
			require.Len(t, events, 2)
			assert.Equal(t, auth.ElevationEvent{
				PrincipalID: f.person.ID, SessionID: f.session.Meta[auth.MetaSessionID], GrantID: "stg_matter",
				Reason: auth.ElevationEndedIdle, At: f.clock.t,
			}, events[1])
			assert.Empty(t, f.request().Meta[auth.MetaStewardGrantID], "it stays ended")
			assert.Len(t, f.log.all(), 2)
		})
	}
}

// Ending an elevation clears it at once; ending one that is not there
// reports false and records nothing.
func TestSessions_EndElevation(t *testing.T) {
	f := newElevationFixture(t, 0)
	_, err := f.sessions.Elevate(context.Background(), f.request(), "stg_matter")
	require.NoError(t, err)
	p := f.request()

	ended, err := f.sessions.EndElevation(context.Background(), p, "stg_other", auth.ElevationEndedByCaller)
	require.NoError(t, err)
	assert.False(t, ended, "another grant's elevation is not this one")
	assert.Equal(t, "stg_matter", f.request().Meta[auth.MetaStewardGrantID])

	ended, err = f.sessions.EndElevation(context.Background(), p, "stg_matter", auth.ElevationEndedByCaller)
	require.NoError(t, err)
	assert.True(t, ended)
	assert.Empty(t, f.request().Meta[auth.MetaStewardGrantID])
	events := f.log.all()
	require.Len(t, events, 2)
	assert.False(t, events[1].Started)
	assert.Equal(t, auth.ElevationEndedByCaller, events[1].Reason)

	ended, err = f.sessions.EndElevation(context.Background(), p, "stg_matter", auth.ElevationEndedByCaller)
	require.NoError(t, err)
	assert.False(t, ended)
	assert.Len(t, f.log.all(), 2)
}

// Only a web UI browser session elevates: an API-key principal, and a
// principal with no session at all, are refused and nothing is stored.
func TestSessions_OnlyABrowserSessionElevates(t *testing.T) {
	f := newElevationFixture(t, 0)
	for name, p := range map[string]*auth.Principal{
		"person key": f.principalOf(f.personKey),
		"owner key":  f.principalOf(f.ownerKey),
		"no session": {ID: f.person.ID, Kind: storage.PrincipalKindPerson, Meta: map[string]string{auth.MetaSessionID: f.session.Meta[auth.MetaSessionID]}},
	} {
		_, err := f.sessions.Elevate(context.Background(), p, "stg_matter")
		require.ErrorIs(t, err, auth.ErrElevationNeedsSession, name)
		_, err = f.sessions.EndElevation(context.Background(), p, "stg_matter", auth.ElevationEndedByCaller)
		require.ErrorIs(t, err, auth.ErrElevationNeedsSession, name)
	}
	assert.Empty(t, f.row().StewardGrantID)
	assert.Empty(t, f.log.all())
}

// A session holds one elevation at a time: elevating into a second
// grant is refused and the first one stays.
func TestSessions_OneElevationAtATime(t *testing.T) {
	f := newElevationFixture(t, 0)
	_, err := f.sessions.Elevate(context.Background(), f.request(), "stg_first")
	require.NoError(t, err)
	_, err = f.sessions.Elevate(context.Background(), f.request(), "stg_second")
	require.ErrorIs(t, err, storage.ErrSessionElevated)
	assert.Equal(t, "stg_first", f.request().Meta[auth.MetaStewardGrantID])
	assert.Len(t, f.log.all(), 1)
}
