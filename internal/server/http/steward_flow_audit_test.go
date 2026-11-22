package http

import (
	"bufio"
	"cmp"
	"context"
	"net/http"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	authn "github.com/ideacrafterslabs/ctxt/internal/auth"
	"github.com/ideacrafterslabs/ctxt/internal/events"
	"github.com/ideacrafterslabs/ctxt/internal/security"
	"github.com/ideacrafterslabs/ctxt/internal/sharing"
	"github.com/ideacrafterslabs/ctxt/internal/storage"
	"github.com/ideacrafterslabs/ctxt/internal/storageutil"
)

func TestStewardFlowAudit(t *testing.T) { runStewardFlowAuditSuite(t, storageutil.NewTestDriver) }

// runStewardFlowAuditSuite runs the elevated-read audit (ADR-083 §5)
// over a real browser session elevated through the elevate route, over
// the store newDriver opens.
func runStewardFlowAuditSuite(t *testing.T, newDriver func(*testing.T) storage.StorageDriver) {
	for name, fn := range map[string]func(*testing.T, *stewardFlow){
		"an elevated read is one chained entry":                  stewardAuditElevatedRead,
		"a revoked grant is refused, recorded and cleared":       stewardAuditRevoked,
		"an audit outage withholds and keeps elevation":          stewardAuditOutage,
		"a refused elevation leaves an entry and an event":       stewardAuditElevateRefused,
		"an API key naming a grant is recorded refused":          stewardAuditKeyElevate,
		"an elevated session never reaches the events":           stewardAuditEventsRefused,
		"an unknown grant is recorded, a malformed id is not":    stewardAuditElevateUnknown,
		"a second elevation is a recorded refusal":               stewardAuditElevateSecond,
		"an elevated session is refused at the authority routes": stewardAuditAuthorityRoutes,
		"a stream open before elevating closes on it":            stewardAuditStreamClosesOnElevation,
		"a stream never keeps an elevation alive":                stewardAuditStreamDoesNotRefresh,
	} {
		t.Run(name, func(t *testing.T) { fn(t, newStewardFlow(t, newDriver)) })
	}
}

// stewardLog lists the steward entries of grant in append order.
func (f *stewardFlow) stewardLog(grant string) []*storage.AccessEntry {
	f.t.Helper()
	got, err := f.drv.Sharing().AccessLog().Query(context.Background(), storage.AccessFilter{StewardGrantID: grant})
	require.NoError(f.t, err)
	for _, e := range got {
		require.NotNil(f.t, e.Chain, "steward entry %s is not chained", e.ID)
	}
	slices.SortFunc(got, func(a, b *storage.AccessEntry) int { return cmp.Compare(a.Chain.Seq, b.Chain.Seq) })
	return got
}

// verifyChain checks the instance's steward chain and returns its
// length.
func (f *stewardFlow) verifyChain() int64 {
	f.t.Helper()
	head, entries, err := f.drv.Sharing().AccessLog().StewardChain(context.Background())
	require.NoError(f.t, err)
	require.NoError(f.t, storage.VerifyAccessChain(head, entries, time.Time{}))
	return head.Seq
}

// refusals waits for n elevation refusals of grant and returns them.
func (f *stewardFlow) refusals(grant string, n int) []security.Alert {
	f.t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	var out []security.Alert
	for {
		out = out[:0]
		for _, a := range f.alerts.of(security.EventStewardElevationRefused) {
			if a.Meta["grant_id"] == grant {
				out = append(out, a)
			}
		}
		if len(out) >= n || time.Now().After(deadline) {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	time.Sleep(30 * time.Millisecond)
	return out
}

// A browser session elevated through POST /steward/grants/{id}/elevate
// reads under the grant, and the read is exactly one durable chained
// steward entry, committed before the answer arrived: the grant, the
// session's authentication, the steward_grant check passed, the object
// released.
func stewardAuditElevatedRead(t *testing.T, f *stewardFlow) {
	grant := f.grant()
	require.Equal(t, http.StatusOK, f.elevate(sfSteward, grant).StatusCode)
	assert.Empty(t, f.stewardLog(grant), "elevating is the ordinary view's request: no steward entry yet")

	code, ids := f.objects(sfSteward)
	require.Equal(t, http.StatusOK, code)
	require.Equal(t, []string{f.contact}, ids)
	got := f.stewardLog(grant)
	require.Len(t, got, 1, "one durable entry, there when the answer arrived")
	e := got[0]
	assert.Equal(t, grant, e.StewardGrantID)
	assert.Equal(t, sfSteward, e.PrincipalID)
	assert.Equal(t, "GET /objects", e.Route)
	assert.Equal(t, storage.AccessDecisionAllowed, e.Decision)
	assert.Equal(t, storage.AccessCheck{Check: "authn." + authn.MethodSession, Outcome: storage.AccessOutcomePass, Reason: sharing.ReasonAuthenticated}, e.Checks[0])
	assert.Equal(t, storage.AccessCheck{Check: sharing.CheckStewardGrant, Outcome: storage.AccessOutcomePass, Reason: sharing.StewardReasonGrantActive}, e.Checks[1])
	assert.Equal(t, []string{f.contact}, e.ObjectIDs)
	assert.Contains(t, e.PolicyVersions, "steward="+grant)
	assert.Equal(t, int64(1), f.verifyChain())
}

// Revoked between two requests of the elevated session: the second is
// refused with the grant's code, recorded as a denied entry under its
// route, raises one elevation refusal, and the elevation is cleared, so
// the session's next request is the ordinary view and leaves no
// steward entry.
func stewardAuditRevoked(t *testing.T, f *stewardFlow) {
	grant := f.grant()
	require.Equal(t, http.StatusOK, f.elevate(sfSteward, grant).StatusCode)
	_, ids := f.objects(sfSteward)
	require.Equal(t, []string{f.contact}, ids)
	resp := f.do(sfOwner, http.MethodPost, "/api/v1/steward/grants/"+grant+"/revoke", map[string]string{"reason": "matter closed"})
	require.Equal(t, http.StatusOK, resp.StatusCode, errBody(t, resp))

	resp = f.do(sfSteward, http.MethodGet, "/api/v1/objects", nil)
	require.Equal(t, http.StatusForbidden, resp.StatusCode)
	body := string(mustRead(t, resp))
	assert.Contains(t, body, "STEWARD_GRANT_REVOKED")
	assert.NotContains(t, body, f.contact)
	got := f.stewardLog(grant)
	require.Len(t, got, 2, "the read and the refusal")
	refused := got[1]
	assert.Equal(t, "GET /objects", refused.Route)
	assert.Equal(t, storage.AccessDecisionDenied, refused.Decision)
	assert.Equal(t, storage.AccessCheck{Check: sharing.CheckStewardGrant, Outcome: storage.AccessOutcomeFail, Reason: sharing.StewardReasonGrantRevoked}, refused.Checks[len(refused.Checks)-1])
	assert.Empty(t, refused.ObjectIDs)
	assert.Empty(t, f.session(sfSteward).StewardGrantID, "the refusal cleared the elevation")
	events := f.refusals(grant, 1)
	require.Len(t, events, 1, "one security event per refusal")
	assert.Equal(t, map[string]string{"grant_id": grant, "reason": sharing.StewardReasonGrantRevoked, "deferred": "false"}, events[0].Meta)

	code, ids := f.objects(sfSteward)
	require.Equal(t, http.StatusOK, code)
	assert.Empty(t, ids, "the ordinary view")
	assert.Len(t, f.stewardLog(grant), 2, "an ordinary request is no steward entry")
	assert.Equal(t, int64(2), f.verifyChain())
}

// While the audit store is down an elevated session gets 503
// AUDIT_UNAVAILABLE and nothing else, for a read and for a refusal
// alike, and keeps its elevation: a refusal that could not be recorded
// is deferred. Once the store is back, the refusal is recorded and the
// elevation cleared.
func stewardAuditOutage(t *testing.T, f *stewardFlow) {
	grant := f.grant()
	require.Equal(t, http.StatusOK, f.elevate(sfSteward, grant).StatusCode)

	f.auditDown.Store(true)
	resp := f.do(sfSteward, http.MethodGet, "/api/v1/objects", nil)
	require.Equal(t, http.StatusServiceUnavailable, resp.StatusCode)
	body := string(mustRead(t, resp))
	assert.Contains(t, body, sharing.CodeAuditUnavailable)
	assert.NotContains(t, body, f.contact)
	assert.Equal(t, grant, f.session(sfSteward).StewardGrantID, "an outage keeps the elevation")

	resp = f.do(sfOwner, http.MethodPost, "/api/v1/steward/grants/"+grant+"/revoke", map[string]string{"reason": "matter closed"})
	require.Equal(t, http.StatusOK, resp.StatusCode, errBody(t, resp))
	resp = f.do(sfSteward, http.MethodGet, "/api/v1/objects", nil)
	require.Equal(t, http.StatusServiceUnavailable, resp.StatusCode, "a refusal that cannot be recorded is not answered with its code either")
	assert.Contains(t, string(mustRead(t, resp)), sharing.CodeAuditUnavailable)
	assert.Equal(t, grant, f.session(sfSteward).StewardGrantID, "a refusal that could not be recorded is deferred: the elevation stays")
	events := f.refusals(grant, 1)
	require.Len(t, events, 1)
	assert.Equal(t, "true", events[0].Meta["deferred"])
	assert.Empty(t, f.stewardLog(grant), "nothing could be recorded")

	f.auditDown.Store(false)
	resp = f.do(sfSteward, http.MethodGet, "/api/v1/objects", nil)
	require.Equal(t, http.StatusForbidden, resp.StatusCode)
	assert.Equal(t, "STEWARD_GRANT_REVOKED", errCode(t, resp))
	got := f.stewardLog(grant)
	require.Len(t, got, 1)
	assert.Equal(t, storage.AccessDecisionDenied, got[0].Decision)
	assert.Empty(t, f.session(sfSteward).StewardGrantID, "recorded now: the elevation is cleared")
	events = f.refusals(grant, 2)
	require.Len(t, events, 2)
	assert.Equal(t, "false", events[1].Meta["deferred"])
	f.verifyChain()
}

// Elevating into a grant that fails a check is a refusal like any other:
// one denied entry under the elevate route, steward_grant failed with
// the resolution's code, committed before the answer, one security
// event, and the session stays unelevated.
func stewardAuditElevateRefused(t *testing.T, f *stewardFlow) {
	grant := f.grant()
	require.Equal(t, http.StatusOK, f.do(sfOwner, http.MethodPost, "/api/v1/steward/grants/"+grant+"/revoke", nil).StatusCode)

	resp := f.elevate(sfSteward, grant)
	require.Equal(t, http.StatusForbidden, resp.StatusCode)
	assert.Equal(t, "STEWARD_GRANT_REVOKED", errCode(t, resp))
	got := f.stewardLog(grant)
	require.Len(t, got, 1, "the refused elevation is one durable entry")
	e := got[0]
	assert.Equal(t, sfSteward, e.PrincipalID)
	assert.Equal(t, "POST /steward/grants/{id}/elevate", e.Route)
	assert.Equal(t, storage.AccessDecisionDenied, e.Decision)
	assert.Equal(t, storage.AccessCheck{Check: sharing.CheckStewardGrant, Outcome: storage.AccessOutcomeFail, Reason: sharing.StewardReasonGrantRevoked}, e.Checks[len(e.Checks)-1])
	assert.Empty(t, e.ObjectIDs)
	assert.Empty(t, f.session(sfSteward).StewardGrantID)
	events := f.refusals(grant, 1)
	require.Len(t, events, 1)
	assert.Equal(t, map[string]string{"grant_id": grant, "reason": sharing.StewardReasonGrantRevoked, "deferred": "false"}, events[0].Meta)
	assert.Equal(t, int64(1), f.verifyChain())

	// No record, no answer: an outage turns the refusal into 503.
	f.auditDown.Store(true)
	resp = f.elevate(sfSteward, grant)
	require.Equal(t, http.StatusServiceUnavailable, resp.StatusCode)
	assert.Equal(t, sharing.CodeAuditUnavailable, errCode(t, resp))
	f.auditDown.Store(false)
	assert.Len(t, f.stewardLog(grant), 1)
}

// An API credential never elevates, and naming a grant to try is
// recorded: a denied entry whose steward_grant check failed browser_only
// and an event, before the refusal is answered.
func stewardAuditKeyElevate(t *testing.T, f *stewardFlow) {
	grant := f.grant()
	resp := f.do(sfDevice, http.MethodPost, "/api/v1/steward/grants/"+grant+"/elevate", nil)
	require.Equal(t, http.StatusForbidden, resp.StatusCode)
	assert.Equal(t, codeElevationBrowserOnly, errCode(t, resp))
	got := f.stewardLog(grant)
	require.Len(t, got, 1)
	assert.Equal(t, storage.AccessDecisionDenied, got[0].Decision)
	assert.Equal(t, storage.AccessCheck{Check: sharing.CheckStewardGrant, Outcome: storage.AccessOutcomeFail, Reason: reasonBrowserOnly}, got[0].Checks[len(got[0].Checks)-1])
	events := f.refusals(grant, 1)
	require.Len(t, events, 1)
	assert.Equal(t, reasonBrowserOnly, events[0].Meta["reason"])
}

// A browser session elevated through the elevate route is refused at
// the event stream: 403 NOT_SHARED, nothing streamed, one denied entry
// whose route_access check failed steward_no_stream. Before elevating
// and after ending, the same session reaches the stream like any
// restricted viewer.
func stewardAuditEventsRefused(t *testing.T, f *stewardFlow) {
	grant := f.grant()
	events := func() (int, string) {
		resp := f.do(sfSteward, http.MethodGet, "/api/v1/events", nil, func(r *http.Request) {
			ctx, cancel := context.WithTimeout(r.Context(), 300*time.Millisecond)
			t.Cleanup(cancel)
			*r = *r.WithContext(ctx)
		})
		return resp.StatusCode, resp.Header.Get("Content-Type")
	}
	code, _ := events()
	assert.Equal(t, http.StatusOK, code, "the ordinary view keeps its stream")

	require.Equal(t, http.StatusOK, f.elevate(sfSteward, grant).StatusCode)
	resp := f.do(sfSteward, http.MethodGet, "/api/v1/events", nil, func(r *http.Request) {
		ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
		t.Cleanup(cancel)
		*r = *r.WithContext(ctx)
	})
	require.Equal(t, http.StatusForbidden, resp.StatusCode)
	assert.Equal(t, CodeNotShared, errCode(t, resp))
	got := f.stewardLog(grant)
	require.Len(t, got, 1)
	e := got[0]
	assert.Equal(t, "GET /events", e.Route)
	assert.Equal(t, storage.AccessDecisionDenied, e.Decision)
	assert.Equal(t, storage.AccessCheck{Check: sharing.CheckRouteAccess, Outcome: storage.AccessOutcomeFail, Reason: sharing.ReasonStewardNoStream}, e.Checks[len(e.Checks)-1])
	assert.Empty(t, e.ObjectIDs)
	assert.Equal(t, grant, f.session(sfSteward).StewardGrantID, "a refused stream is no refusal of the grant")

	require.Equal(t, http.StatusNoContent, f.do(sfSteward, http.MethodDelete, "/api/v1/steward/grants/"+grant+"/elevate", nil).StatusCode)
	code, _ = events()
	assert.Equal(t, http.StatusOK, code)
	// The end request itself ran under the elevation: the chain holds
	// the refused stream and that request, and the stream the ordinary
	// view opened adds none.
	assert.Len(t, f.stewardLog(grant), 2)
	assert.Equal(t, int64(2), f.verifyChain())
}

// openStream opens GET /events as who and returns the data frames it
// delivers and a channel that closes when the server ends the stream.
func (f *stewardFlow) openStream(who string) (frames <-chan string, done <-chan struct{}) {
	f.t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	f.t.Cleanup(cancel)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, f.ts.URL+"/api/v1/events", nil)
	require.NoError(f.t, err)
	req.AddCookie(&http.Cookie{Name: SessionCookieName(strings.TrimPrefix(f.ts.URL, "http://")), Value: f.cookies[who]})
	fromUI(req)
	resp, err := http.DefaultClient.Do(req)
	require.NoError(f.t, err)
	require.Equal(f.t, http.StatusOK, resp.StatusCode)
	out, fin := make(chan string, 16), make(chan struct{})
	go func() {
		defer close(fin)
		defer resp.Body.Close()
		sc := bufio.NewScanner(resp.Body)
		for sc.Scan() {
			if line := sc.Text(); strings.HasPrefix(line, "data: ") {
				out <- line
			}
		}
	}()
	return out, fin
}

// publishContactUpdate announces an update of the object only the grant
// opens.
func (f *stewardFlow) publishContactUpdate() {
	f.t.Helper()
	ctx := context.Background()
	ev := &storage.OutboxEvent{
		Topic: string(events.TopicObjectUpdated), Source: "service.objects", Origin: events.OriginAPI, ObjectID: f.contact,
		Payload: events.ObjectUpdatedPayload{ObjectID: f.contact, Type: "contact", Fields: []string{"tags"}}, OccurredAt: time.Now().UTC(),
	}
	require.NoError(f.t, f.svc.Outbox().Append(ctx, ev))
	require.NoError(f.t, events.Publish(ctx, f.svc.Bus, ev))
}

// A stream opened before the session elevated must not slide into the
// grant's view: it closes with nothing delivered, and the closure is
// recorded like any refused elevated stream (a chained entry whose
// steward_grant check failed steward_no_stream, and an event).
func stewardAuditStreamClosesOnElevation(t *testing.T, f *stewardFlow) {
	grant := f.grant()
	frames, done := f.openStream(sfSteward)
	require.Equal(t, http.StatusOK, f.elevate(sfSteward, grant).StatusCode)
	f.publishContactUpdate()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("the stream stayed open under an elevation")
	}
	assert.Empty(t, frames, "nothing the grant opens reaches the stream")
	got := f.stewardLog(grant)
	require.Len(t, got, 1)
	assert.Equal(t, "GET /events", got[0].Route)
	assert.Equal(t, storage.AccessDecisionDenied, got[0].Decision)
	assert.Equal(t, storage.AccessCheck{Check: sharing.CheckStewardGrant, Outcome: storage.AccessOutcomeFail, Reason: sharing.ReasonStewardNoStream}, got[0].Checks[len(got[0].Checks)-1])
	events := f.refusals(grant, 1)
	require.Len(t, events, 1)
	assert.Equal(t, sharing.ReasonStewardNoStream, events[0].Meta["reason"])
	assert.Equal(t, int64(1), f.verifyChain())
}

// Watching is no activity under an elevation: the stream's re-check
// neither refreshes nor ends it, so the idle timeout still runs while a
// stream is open, and the session's next ordinary request ends it.
func stewardAuditStreamDoesNotRefresh(t *testing.T, f *stewardFlow) {
	grant := f.grant()
	_, done := f.openStream(sfSteward)
	require.Equal(t, http.StatusOK, f.elevate(sfSteward, grant).StatusCode)
	at := f.session(sfSteward).StewardActiveAt
	require.NotNil(t, at)
	f.clock.advance(10 * time.Minute)
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("the stream stayed open under an elevation")
	}
	after := f.session(sfSteward).StewardActiveAt
	require.NotNil(t, after, "the stream's re-check does not end the elevation")
	assert.True(t, after.Equal(*at), "the stream's re-check refreshed the elevation: %s -> %s", at, after)

	f.clock.advance(6 * time.Minute) // 16 minutes since the elevation was last used
	code, ids := f.objects(sfSteward)
	require.Equal(t, http.StatusOK, code)
	assert.Empty(t, ids, "idle ended the elevation: the ordinary view")
	assert.Empty(t, f.session(sfSteward).StewardGrantID)
}

// Elevating into a grant that does not exist, or the caller cannot see,
// is recorded (grant_unknown) and answered 404 either way. An id of no
// grant's shape writes nothing.
func stewardAuditElevateUnknown(t *testing.T, f *stewardFlow) {
	unknown := storage.NewStewardGrantID()
	resp := f.elevate(sfSteward, unknown)
	require.Equal(t, http.StatusNotFound, resp.StatusCode)
	got := f.stewardLog(unknown)
	require.Len(t, got, 1)
	assert.Equal(t, storage.AccessDecisionDenied, got[0].Decision)
	assert.Equal(t, storage.AccessCheck{Check: sharing.CheckStewardGrant, Outcome: storage.AccessOutcomeFail, Reason: sharing.StewardReasonGrantUnknown}, got[0].Checks[len(got[0].Checks)-1])
	assert.Len(t, f.refusals(unknown, 1), 1)

	// A grant the caller may not see answers the same, and is recorded
	// the same.
	grant := f.grant()
	require.Equal(t, http.StatusNotFound, f.elevate(sfOther, grant).StatusCode)
	got = f.stewardLog(grant)
	require.Len(t, got, 1)
	assert.Equal(t, sfOther, got[0].PrincipalID)

	before := f.verifyChain()
	for _, bad := range []string{"stg_nothing", "stg_" + strings.Repeat("A", 200), "x"} {
		assert.Equal(t, http.StatusNotFound, f.elevate(sfSteward, bad).StatusCode, bad)
		assert.Empty(t, f.stewardLog(bad), bad)
	}
	assert.Equal(t, before, f.verifyChain(), "a malformed id wrote nothing")
}

// Asking a session already elevated into one grant for a second is a
// refused elevation: 409, recorded under the grant asked for with the
// reason elevation_active, and an event.
func stewardAuditElevateSecond(t *testing.T, f *stewardFlow) {
	first, second := f.grant(), f.grant()
	require.Equal(t, http.StatusOK, f.elevate(sfSteward, first).StatusCode)
	resp := f.elevate(sfSteward, second)
	require.Equal(t, http.StatusConflict, resp.StatusCode)
	got := f.stewardLog(second)
	require.Len(t, got, 1)
	assert.Equal(t, storage.AccessDecisionDenied, got[0].Decision)
	assert.Equal(t, storage.AccessCheck{Check: sharing.CheckStewardGrant, Outcome: storage.AccessOutcomeFail, Reason: reasonElevationActive}, got[0].Checks[len(got[0].Checks)-1])
	assert.Len(t, f.refusals(second, 1), 1)
	assert.Equal(t, first, f.session(sfSteward).StewardGrantID, "the first elevation stays")
}

// The authority's provisioning routes are never an elevation's: the
// request is recorded under its own route name, then refused, and an
// audit outage turns the refusal into 503.
func stewardAuditAuthorityRoutes(t *testing.T, f *stewardFlow) {
	grant := f.grant()
	require.Equal(t, http.StatusOK, f.elevate(sfSteward, grant).StatusCode)
	resp := f.do(sfSteward, http.MethodGet, "/bos/v1/profile", nil)
	require.Equal(t, http.StatusForbidden, resp.StatusCode)
	got := f.stewardLog(grant)
	require.Len(t, got, 1)
	assert.Equal(t, "GET /bos/v1/profile", got[0].Route)
	assert.Equal(t, storage.AccessDecisionDenied, got[0].Decision)
	assert.Equal(t, storage.AccessCheck{Check: sharing.CheckRouteAccess, Outcome: storage.AccessOutcomeFail, Reason: sharing.ReasonNotAuthority}, got[0].Checks[len(got[0].Checks)-1])

	f.auditDown.Store(true)
	resp = f.do(sfSteward, http.MethodGet, "/bos/v1/profile", nil)
	assert.Equal(t, http.StatusServiceUnavailable, resp.StatusCode)
	assert.Equal(t, sharing.CodeAuditUnavailable, errCode(t, resp))
}
