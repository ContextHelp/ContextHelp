package sharing_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ideacrafterslabs/ctxt/internal/sharing"
	"github.com/ideacrafterslabs/ctxt/internal/storage"
)

var receiptNow = time.Date(2026, 10, 15, 12, 0, 0, 0, time.UTC)

func colleagueViewer(t *testing.T, deny ...string) sharing.Viewer {
	t.Helper()
	tpl, _ := sharing.Template(sharing.TemplateColleagues)
	v, err := sharing.Compile(sharing.Input{
		PrincipalID: "prn_a", IdentityID: "idn_1", DenyClasses: deny,
		Grants: []sharing.Grant{{ID: "g1", Audience: "colleagues", Policy: tpl}},
	})
	require.NoError(t, err)
	return v
}

func newReceipt(t *testing.T, v sharing.Viewer) *sharing.Receipt {
	t.Helper()
	return sharing.NewReceipt(sharing.Operation{
		Method: "GET", Route: "GET /objects", Principal: "prn_a", Identity: "idn_1", AuthMethod: "api_key",
		Params: map[string][]string{"query.limit": {"10"}}, Viewer: v,
	})
}

// finish records the receipt through a real writer and returns what the
// store was handed.
func finish(t *testing.T, r *sharing.Receipt) []*storage.AccessEntry {
	t.Helper()
	store := &memAccessLog{}
	w := sharing.NewAuditWriter(store, sharing.AuditOptions{Interval: time.Hour})
	r.Finish(w)
	require.NoError(t, w.Close(context.Background()))
	var out []*storage.AccessEntry
	for _, b := range store.batches {
		out = append(out, b...)
	}
	for _, e := range out {
		require.NoError(t, storage.ValidateAccessEntry(e), "a receipt is valid by construction")
	}
	assert.Zero(t, w.Dropped(), "the writer dropped a receipt")
	return out
}

func checkIDs(e *storage.AccessEntry) []string {
	var ids []string
	for _, c := range e.Checks {
		ids = append(ids, c.Check)
	}
	return ids
}

// An admitted request leaves the checks it met, in the order they ran,
// every one PASS or NOT_APPLICABLE with a reason: authn, scope, route
// access, then the share policy, the ceiling and the backstop once the
// handler has run.
func TestReceiptAllowedTrail(t *testing.T) {
	v := colleagueViewer(t, "personal")
	r := newReceipt(t, v)
	l := sharing.NewLedger()
	l.AddObject("o1")
	l.MarkScoped()
	r.SetLedger(l)
	r.Pass(sharing.CheckScope, sharing.ReasonScopeHeld)
	r.Pass(sharing.CheckRouteAccess, sharing.ReasonCapabilityHeld)
	r.SettlePolicy()
	r.Pass(sharing.CheckBackstop, sharing.ReasonClean)

	got := finish(t, r)
	require.Len(t, got, 1)
	e := got[0]
	assert.Equal(t, storage.AccessDecisionAllowed, e.Decision)
	assert.Equal(t, []string{"authn.api_key", "scope", "route_access", "share_policy", "deny_class", "backstop"}, checkIDs(e))
	for _, c := range e.Checks {
		assert.Equal(t, storage.AccessOutcomePass, c.Outcome, c.Check)
		assert.NotEmpty(t, c.Reason, c.Check)
	}
	assert.Equal(t, "prn_a", e.PrincipalID)
	assert.Equal(t, "idn_1", e.IdentityID)
	assert.Equal(t, 1, e.ObjectCount)
	assert.Equal(t, v.PolicyKey(), e.PolicyVersions)
	assert.True(t, strings.HasPrefix(e.OperationDigest, storage.DigestPrefix))
}

// A refusal ends the trail at the check that refused: the checks after
// it never ran and are absent, never PASS.
func TestReceiptRefusalStopsTheTrail(t *testing.T) {
	r := newReceipt(t, colleagueViewer(t))
	r.Refuse(sharing.CheckScope, sharing.ReasonScopeMissing)

	got := finish(t, r)
	require.Len(t, got, 1)
	e := got[0]
	assert.Equal(t, storage.AccessDecisionDenied, e.Decision)
	assert.Equal(t, []storage.AccessCheck{
		{Check: "authn.api_key", Outcome: storage.AccessOutcomePass, Reason: "authenticated"},
		{Check: "scope", Outcome: storage.AccessOutcomeFail, Reason: "scope_missing"},
	}, e.Checks)
}

// Checks that ran and had nothing to decide are NOT_APPLICABLE, not
// PASS: no scoped read means no share policy and no ceiling to apply, no
// ceiling means nothing for the deny-class check to do.
func TestReceiptNothingToDecideIsNotPass(t *testing.T) {
	r := newReceipt(t, colleagueViewer(t))
	r.SettlePolicy() // handler made no scoped read
	e := finish(t, r)[0]
	by := map[string]storage.AccessCheck{}
	for _, c := range e.Checks {
		by[c.Check] = c
	}
	assert.Equal(t, storage.AccessCheck{Check: "share_policy", Outcome: storage.AccessOutcomeNotApplicable, Reason: "no_scoped_read"}, by["share_policy"])
	assert.Equal(t, storage.AccessCheck{Check: "deny_class", Outcome: storage.AccessOutcomeNotApplicable, Reason: "no_scoped_read"}, by["deny_class"])
	assert.Equal(t, storage.AccessDecisionAllowed, e.Decision)

	l := sharing.NewLedger()
	l.MarkScoped()
	r2 := newReceipt(t, colleagueViewer(t))
	r2.SetLedger(l)
	r2.SettlePolicy()
	e2 := finish(t, r2)[0]
	assert.Equal(t, storage.AccessCheck{Check: "share_policy", Outcome: storage.AccessOutcomePass, Reason: "rows_scoped"}, e2.Checks[1])
	assert.Equal(t, storage.AccessCheck{Check: "deny_class", Outcome: storage.AccessOutcomeNotApplicable, Reason: "no_ceiling"}, e2.Checks[2])
}

// A viewer no grant applies to is refused by the share policy once the
// handler has read through it.
func TestReceiptNoGrantIsDenied(t *testing.T) {
	v, err := sharing.Compile(sharing.Input{PrincipalID: "prn_a"})
	require.NoError(t, err)
	l := sharing.NewLedger()
	l.MarkScoped()
	r := newReceipt(t, v)
	r.SetLedger(l)
	r.SettlePolicy()
	e := finish(t, r)[0]
	assert.Equal(t, storage.AccessDecisionDenied, e.Decision)
	assert.Equal(t, storage.AccessCheck{Check: "share_policy", Outcome: storage.AccessOutcomeFail, Reason: "no_applicable_grant"}, e.Checks[1])
}

// The backstop refusing, failing to read the response, and stripping a
// hit are three different records.
func TestReceiptBackstopRecords(t *testing.T) {
	cases := []struct {
		name     string
		act      func(*sharing.Receipt)
		decision storage.AccessDecision
		want     storage.AccessCheck
	}{
		{"refused", func(r *sharing.Receipt) { r.Refuse(sharing.CheckBackstop, sharing.ReasonHitRefused) },
			storage.AccessDecisionDenied, storage.AccessCheck{Check: "backstop", Outcome: storage.AccessOutcomeFail, Reason: "hit_refused"}},
		{"unreadable", func(r *sharing.Receipt) { r.Defer(sharing.CheckBackstop, sharing.ReasonResponseUnreadable) },
			storage.AccessDecisionDeferred, storage.AccessCheck{Check: "backstop", Outcome: storage.AccessOutcomeUnknown, Reason: "response_unreadable"}},
		{"stripped", func(r *sharing.Receipt) { r.Fail(sharing.CheckBackstop, sharing.ReasonHitStripped) },
			storage.AccessDecisionAllowed, storage.AccessCheck{Check: "backstop", Outcome: storage.AccessOutcomeFail, Reason: "hit_stripped"}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			r := newReceipt(t, colleagueViewer(t))
			c.act(r)
			e := finish(t, r)[0]
			assert.Equal(t, c.decision, e.Decision)
			assert.Equal(t, c.want, e.Checks[len(e.Checks)-1])
		})
	}
}

// A stream checks every message: a clean message must not hide a hit
// that came later, and a hit stays whatever follows.
func TestReceiptBackstopKeepsTheWorstResult(t *testing.T) {
	r := newReceipt(t, colleagueViewer(t))
	r.Pass(sharing.CheckBackstop, sharing.ReasonClean)
	r.Fail(sharing.CheckBackstop, sharing.ReasonHitStripped)
	r.Pass(sharing.CheckBackstop, sharing.ReasonClean)
	e := finish(t, r)[0]
	var backstop []storage.AccessCheck
	for _, c := range e.Checks {
		if c.Check == sharing.CheckBackstop {
			backstop = append(backstop, c)
		}
	}
	assert.Equal(t, []storage.AccessCheck{{Check: "backstop", Outcome: storage.AccessOutcomeFail, Reason: "hit_stripped"}}, backstop)
}

// A stream checks every message, so a check id can be hit more than
// once with different results, in either order. The decision comes from
// the trail: a check that could not decide (UNKNOWN) outranks a stripped
// hit (FAIL) on the same id, a refusal outranks both, and the entry is
// valid whichever order they arrive in.
func TestReceiptSameCheckFailAndDefer(t *testing.T) {
	unknown := storage.AccessCheck{Check: "backstop", Outcome: storage.AccessOutcomeUnknown, Reason: "response_unreadable"}
	refused := storage.AccessCheck{Check: "backstop", Outcome: storage.AccessOutcomeFail, Reason: "hit_refused"}
	cases := []struct {
		name     string
		act      func(*sharing.Receipt)
		decision storage.AccessDecision
		want     storage.AccessCheck
	}{
		{"fail then defer", func(r *sharing.Receipt) {
			r.Fail(sharing.CheckBackstop, sharing.ReasonHitStripped)
			r.Defer(sharing.CheckBackstop, sharing.ReasonResponseUnreadable)
		}, storage.AccessDecisionDeferred, unknown},
		{"defer then fail", func(r *sharing.Receipt) {
			r.Defer(sharing.CheckBackstop, sharing.ReasonResponseUnreadable)
			r.Fail(sharing.CheckBackstop, sharing.ReasonHitStripped)
		}, storage.AccessDecisionDeferred, unknown},
		{"defer then refuse", func(r *sharing.Receipt) {
			r.Defer(sharing.CheckBackstop, sharing.ReasonResponseUnreadable)
			r.Refuse(sharing.CheckBackstop, sharing.ReasonHitRefused)
		}, storage.AccessDecisionDenied, refused},
		{"refuse then defer", func(r *sharing.Receipt) {
			r.Refuse(sharing.CheckBackstop, sharing.ReasonHitRefused)
			r.Defer(sharing.CheckBackstop, sharing.ReasonResponseUnreadable)
		}, storage.AccessDecisionDenied, refused},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			r := newReceipt(t, colleagueViewer(t))
			c.act(r)
			e := finish(t, r)[0]
			assert.Equal(t, c.decision, e.Decision)
			var backstop []storage.AccessCheck
			for _, ck := range e.Checks {
				if ck.Check == sharing.CheckBackstop {
					backstop = append(backstop, ck)
				}
			}
			assert.Equal(t, []storage.AccessCheck{c.want}, backstop)
		})
	}
}

// A stream settles its share policy as NOT_APPLICABLE and never
// refuses on it, even for a viewer no grant applies to.
func TestReceiptSettleStream(t *testing.T) {
	r := newReceipt(t, sharing.Viewer{})
	l := sharing.NewLedger()
	l.MarkScoped()
	r.SetLedger(l)
	r.SettleStream()
	e := finish(t, r)[0]
	assert.Equal(t, storage.AccessDecisionAllowed, e.Decision)
	assert.Contains(t, e.Checks, storage.AccessCheck{Check: "share_policy", Outcome: storage.AccessOutcomeNotApplicable, Reason: "stream_filtered"})
}

// A policy settled after a deferral on the same check cannot leave a
// refusal without its FAIL.
func TestReceiptRefusalAlwaysHasItsFail(t *testing.T) {
	r := newReceipt(t, sharing.Viewer{})
	l := sharing.NewLedger()
	l.MarkScoped()
	r.SetLedger(l)
	r.Defer(sharing.CheckSharePolicy, sharing.ReasonResponseUnreadable)
	r.SettlePolicy()
	e := finish(t, r)[0]
	assert.Equal(t, storage.AccessDecisionDenied, e.Decision)
}

// The entry is stamped with the time the request began, however long it
// ran before the transport finished the receipt: a stream's entry shows
// when the stream opened, not when it was closed.
func TestReceiptAtIsTheStart(t *testing.T) {
	r := sharing.NewReceipt(sharing.Operation{
		Method: "GET", Route: "GET /events", Principal: "prn_a", AuthMethod: "api_key",
		Viewer: colleagueViewer(t), At: receiptNow,
	})
	got := finish(t, r)
	require.Len(t, got, 1)
	assert.Equal(t, receiptNow, got[0].At)

	started := time.Now()
	r = sharing.NewReceipt(sharing.Operation{Method: "GET", Route: "GET /events", Principal: "prn_a", Viewer: colleagueViewer(t)})
	time.Sleep(20 * time.Millisecond)
	got = finish(t, r)
	require.Len(t, got, 1)
	assert.WithinDuration(t, started, got[0].At, 15*time.Millisecond, "the start, not the finish")
}

// One receipt, one entry, however many times the transport finishes it.
func TestReceiptFinishesOnce(t *testing.T) {
	store := &memAccessLog{}
	w := sharing.NewAuditWriter(store, sharing.AuditOptions{Interval: time.Hour})
	r := newReceipt(t, colleagueViewer(t))
	r.Finish(w)
	r.Finish(w)
	r.Refuse(sharing.CheckScope, sharing.ReasonScopeMissing)
	r.Finish(w)
	require.NoError(t, w.Close(context.Background()))
	_, n := store.count()
	assert.Equal(t, 1, n)
}

// A request nobody is accountable for, and a discarded one, leave no
// entry: an unauthenticated flood must not grow the table.
func TestReceiptUnattributedOrDiscardedIsNotRecorded(t *testing.T) {
	anon := sharing.NewReceipt(sharing.Operation{Method: "GET", Route: "GET /objects", Viewer: colleagueViewer(t)})
	anon.Refuse(sharing.CheckScope, sharing.ReasonScopeMissing)
	assert.Empty(t, finish(t, anon))

	d := newReceipt(t, colleagueViewer(t))
	d.Discard()
	assert.Empty(t, finish(t, d))

	var nilReceipt *sharing.Receipt
	nilReceipt.Pass(sharing.CheckScope, sharing.ReasonScopeHeld) // nil-safe
	nilReceipt.Finish(nil)
}

// The owner previewing a viewer is logged as the owner, with the
// previewed viewer's versions after the preview marker.
func TestReceiptPreviewIsTheOwnersRequest(t *testing.T) {
	v := colleagueViewer(t)
	r := sharing.NewReceipt(sharing.Operation{
		Method: "GET", Route: "GET /objects", Principal: storage.OwnerPrincipal, Identity: "idn_owner", AuthMethod: "api_key",
		Viewer: sharing.Unrestricted,
	})
	r.SetPreview(sharing.Preview{Target: "prn_a", Principal: storage.OwnerPrincipal, Identity: "idn_owner"}, v)
	e := finish(t, r)[0]
	assert.Equal(t, storage.OwnerPrincipal, e.PrincipalID)
	assert.Equal(t, "idn_owner", e.IdentityID)
	assert.Equal(t, "preview=prn_a;"+v.PolicyKey(), e.PolicyVersions)
	assert.Equal(t, []string{"authn.api_key", "view_as"}, checkIDs(e))
	assert.Equal(t, storage.AccessCheck{Check: "view_as", Outcome: storage.AccessOutcomePass, Reason: "preview_resolved"}, e.Checks[1])
}

// The digest is a versioned hash of the operation: the same whatever
// order the params arrive in, and different when any consequential
// part differs. It holds none of the values.
func TestOperationDigest(t *testing.T) {
	base := map[string][]string{"query.q": {"sarah"}, "query.tag": {"b", "a"}, "path.id": {"obj_1"}}
	d := sharing.OperationDigest("GET", "/objects/{id}", base)
	assert.True(t, strings.HasPrefix(d, storage.DigestPrefix), d)
	assert.Regexp(t, `^v1:sha256:[0-9a-f]{64}$`, d)
	assert.NotContains(t, d, "sarah")

	reordered := map[string][]string{"path.id": {"obj_1"}, "query.tag": {"a", "b"}, "query.q": {"sarah"}}
	assert.Equal(t, d, sharing.OperationDigest("GET", "/objects/{id}", reordered), "param order is not consequential")

	for name, other := range map[string]string{
		"value":  sharing.OperationDigest("GET", "/objects/{id}", map[string][]string{"query.q": {"sara"}, "query.tag": {"a", "b"}, "path.id": {"obj_1"}}),
		"key":    sharing.OperationDigest("GET", "/objects/{id}", map[string][]string{"query.query": {"sarah"}, "query.tag": {"a", "b"}, "path.id": {"obj_1"}}),
		"extra":  sharing.OperationDigest("GET", "/objects/{id}", map[string][]string{"query.q": {"sarah"}, "query.tag": {"a", "b"}, "path.id": {"obj_1"}, "query.limit": {"1"}}),
		"method": sharing.OperationDigest("POST", "/objects/{id}", base),
		"route":  sharing.OperationDigest("GET", "/objects", base),
		"path":   sharing.OperationDigest("GET", "/objects/{id}", map[string][]string{"query.q": {"sarah"}, "query.tag": {"a", "b"}, "path.id": {"obj_2"}}),
	} {
		assert.NotEqual(t, d, other, "a different %s must change the digest", name)
	}

	// Values cannot be moved between keys or merged: the encoding is
	// unambiguous.
	assert.NotEqual(t,
		sharing.OperationDigest("GET", "/x", map[string][]string{"a": {"b=c"}}),
		sharing.OperationDigest("GET", "/x", map[string][]string{"a=b": {"c"}}))
	assert.NotEqual(t,
		sharing.OperationDigest("GET", "/x", map[string][]string{"a": {"1", "2"}}),
		sharing.OperationDigest("GET", "/x", map[string][]string{"a": {"1,2"}}))
}
