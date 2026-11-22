package http

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/ideacrafterslabs/ctxt/internal/approval"
	authn "github.com/ideacrafterslabs/ctxt/internal/auth"
	"github.com/ideacrafterslabs/ctxt/internal/jobs"
	"github.com/ideacrafterslabs/ctxt/internal/pipeline/builtins"
	"github.com/ideacrafterslabs/ctxt/internal/search"
	"github.com/ideacrafterslabs/ctxt/internal/security"
	"github.com/ideacrafterslabs/ctxt/internal/service"
	"github.com/ideacrafterslabs/ctxt/internal/sharing"
	"github.com/ideacrafterslabs/ctxt/internal/steward"
	"github.com/ideacrafterslabs/ctxt/internal/storage"
)

// The steward flow over the API (ADR-083 §2, §3): a router wired the
// way the server wires it, over a store a test chooses (SQLite, or
// Postgres under the integration tag), with browser sessions for its
// people and API keys for the owner and a device.

// Callers of the steward flow fixture.
const (
	sfOwner  = "owner"
	sfDevice = "device"
	// The people, each signed in with a browser session.
	sfSteward   = "prn_sf_steward"   // reader + steward-requester: the one who reads
	sfRequester = "prn_sf_requester" // reader + steward-requester
	sfApprover  = "prn_sf_approver"  // reader + steward-approver
	sfDual      = "prn_sf_dual"      // reader + both steward roles
	sfOther     = "prn_sf_other"     // reader + steward-requester, party to no grant
	sfPlain     = "prn_sf_plain"     // reader
)

// sfReason is long enough for steward.min_reason_chars.
const sfReason = "Legal hold for matter 2026-117: opposing counsel asked for the supplier contacts"

// sfScope opens contacts, titles only.
const sfScope = `{"data":"type==contact","fields":{"contact":["title"]}}`

// alertLog records the security alerts a router's emitter dispatched.
type alertLog struct {
	mu   sync.Mutex
	list []security.Alert
}

func (l *alertLog) record(_ context.Context, a security.Alert) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.list = append(l.list, a)
}

// kinds waits until want's alerts arrived (handlers run on their own
// goroutines) and returns every alert kind recorded, sorted.
func (l *alertLog) kinds(t *testing.T, want int) []string {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for {
		l.mu.Lock()
		n := len(l.list)
		l.mu.Unlock()
		if n >= want || time.Now().After(deadline) {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	time.Sleep(30 * time.Millisecond)
	l.mu.Lock()
	defer l.mu.Unlock()
	out := make([]string, len(l.list))
	for i, a := range l.list {
		out[i] = a.Kind
	}
	sort.Strings(out)
	return out
}

// of returns the recorded alerts of kind.
func (l *alertLog) of(kind string) []security.Alert {
	l.mu.Lock()
	defer l.mu.Unlock()
	var out []security.Alert
	for _, a := range l.list {
		if a.Kind == kind {
			out = append(out, a)
		}
	}
	return out
}

// stewardFlow is the fixture.
type stewardFlow struct {
	t        *testing.T
	drv      storage.StorageDriver
	clock    *testClock
	settings *sharing.StewardSettings
	sessions *authn.Sessions
	alerts   *alertLog
	// audit is the read audit the router records into, as the server
	// wires it: elevated requests commit their entries through it.
	// auditDown makes its steward appends fail, an audit store outage.
	audit     *sharing.AuditWriter
	auditDown atomic.Bool
	ts        *httptest.Server
	svc       *service.Service
	keyring   *authn.Keys
	// keys holds the API keys of sfOwner and sfDevice; cookies the
	// browser session secret of each person.
	keys    map[string]string
	cookies map[string]string
	// contact is an object the grant opens; note one it does not.
	contact, note string
}

func newStewardFlow(t *testing.T, newDriver func(*testing.T) storage.StorageDriver) *stewardFlow {
	t.Helper()
	ctx := ownerCtx()
	drv := newDriver(t)
	f := &stewardFlow{
		t: t, drv: drv, clock: &testClock{t: time.Now().UTC().Truncate(time.Millisecond)},
		settings: &sharing.StewardSettings{Enabled: true, MaxDuration: 14 * 24 * time.Hour},
		alerts:   &alertLog{}, keys: map[string]string{}, cookies: map[string]string{},
	}
	settings := func() sharing.StewardSettings { return *f.settings }
	sec := security.New(security.DefaultConfig(), nil)
	t.Cleanup(func() { _ = sec.Close(context.Background()) })
	sec.AddHandler(f.alerts.record)

	svc := service.New(drv, jobs.NewQueue(drv.Jobs()), builtins.Registry(), search.NewEngine(drv), "", nil)
	svc.Cfg.Server.EventsHeartbeat = 300 * time.Millisecond
	f.svc = svc
	svc.Viewers().WithClock(f.clock.now).WithSteward(steward.ResolverOptions(settings))
	keys := authn.NewKeys(authn.NewLocalKeys(drv.Auth().KV(), f.clock.now), drv.Auth(), f.clock.now)
	f.keyring = keys
	var err error
	// Sessions idle for days, so a test can move the clock past a
	// grant's window and still call as the same browser.
	f.sessions, err = authn.NewSessions(drv.UISessions(), drv.Auth(), keys, authn.SessionOptions{
		Now: f.clock.now, IdleTTL: 6 * 24 * time.Hour, OnElevation: steward.ElevationEvents(sec),
	})
	require.NoError(t, err)

	reg := approval.NewRegistry()
	kind, err := steward.Kind(steward.KindOptions{
		Settings: settings, MinReasonChars: func() int { return 30 }, Sharing: drv.Sharing(), Auth: drv.Auth(), Audit: drv.AuditLog(), Now: f.clock.now,
	})
	require.NoError(t, err)
	require.NoError(t, reg.Register(kind))
	approvals, err := approval.NewService(drv.Approvals(), drv.AuditLog(), reg, approval.Options{
		PendingTTL: 72 * time.Hour, Now: f.clock.now, Notify: steward.ApprovalNotices(sec),
	})
	require.NoError(t, err)
	stewards, err := steward.NewService(steward.ServiceOptions{
		Sharing: drv.Sharing(), Approvals: drv.Approvals(), Audit: drv.AuditLog(), Settings: settings, Security: sec, Now: f.clock.now,
	})
	require.NoError(t, err)

	for id, roles := range map[string][]string{
		sfSteward:   {authn.RoleReader, authn.RoleStewardRequester},
		sfRequester: {authn.RoleReader, authn.RoleStewardRequester},
		sfApprover:  {authn.RoleReader, authn.RoleStewardApprover},
		sfDual:      {authn.RoleReader, authn.RoleStewardRequester, authn.RoleStewardApprover},
		sfOther:     {authn.RoleReader, authn.RoleStewardRequester},
		sfPlain:     {authn.RoleReader},
	} {
		require.NoError(t, drv.Auth().Principals().Create(ctx, &storage.Principal{ID: id, Kind: storage.PrincipalKindPerson, DisplayName: id, Roles: roles}))
		idn := &storage.Identity{PrincipalID: id, Method: storage.IdentityMethodLink, Subject: "link-" + id}
		require.NoError(t, drv.Auth().Identities().Create(ctx, idn))
		m, err := f.sessions.MintFor(ctx, id, idn.ID, authn.ClientInfo{UserAgent: "test"})
		require.NoError(t, err)
		f.cookies[id] = m.Secret
	}
	// A device holding the steward-requester role: an API credential,
	// which never elevates.
	require.NoError(t, drv.Auth().Principals().Create(ctx, &storage.Principal{
		ID: "prn_sf_device", Kind: storage.PrincipalKindDevice, DisplayName: "device", Roles: []string{authn.RoleReader, authn.RoleStewardRequester},
	}))
	for who, principal := range map[string]string{sfOwner: storage.OwnerPrincipal, sfDevice: "prn_sf_device"} {
		secret, _, err := keys.Mint(ctx, authn.MintKey{PrincipalID: principal, Name: who})
		require.NoError(t, err)
		f.keys[who] = secret
	}

	now := f.clock.now()
	contact := &storage.KnowledgeObject{ID: "sf-contact", Type: "contact", Status: storage.ObjectStatusActive,
		RawContent: "Sarah Lee | supplier", ContentHash: "sha256:sf-contact", CreatedAt: now, UpdatedAt: now}
	note := &storage.KnowledgeObject{ID: "sf-note", Type: "note", Status: storage.ObjectStatusActive,
		RawContent: "a private note", ContentHash: "sha256:sf-note", CreatedAt: now, UpdatedAt: now}
	for _, o := range []*storage.KnowledgeObject{contact, note} {
		require.NoError(t, drv.Objects().Create(ctx, o))
	}
	f.contact, f.note = contact.ID, note.ID

	f.audit = sharing.NewAuditWriter(switchedAccessLog{AccessLogStore: drv.Sharing().AccessLog(), down: &f.auditDown}, sharing.AuditOptions{})
	t.Cleanup(func() { _ = f.audit.Close(context.Background()) })
	f.ts = httptest.NewServer(NewRouterWithConfig(svc, RouterConfig{
		Auth:          authn.NewChain(authn.NewAPIKeyMethod(keys, nil), f.sessions.Method()),
		Sessions:      f.sessions,
		Security:      sec,
		Registry:      drv.Auth(),
		Keys:          keys,
		Sharing:       drv.Sharing(),
		AuditLog:      drv.AuditLog(),
		Approvals:     approvals,
		Stewards:      stewards,
		Audit:         f.audit,
		StrictSharing: true,
		SSE:           SSEConfig{RecheckTTL: time.Nanosecond},
		Access:        stubAccess{},
	}))
	t.Cleanup(f.ts.Close)
	return f
}

// stewardsOn returns rc wired for steward access, switched on, over
// svc's store: a test that walks every route reaches the steward
// routes' own checks (off, they are absent and answer 404 first).
func stewardsOn(t *testing.T, svc *service.Service, rc RouterConfig) RouterConfig {
	t.Helper()
	drv := svc.Store(ownerCtx())
	settings := func() sharing.StewardSettings { return sharing.StewardSettings{Enabled: true} }
	reg := approval.NewRegistry()
	kind, err := steward.Kind(steward.KindOptions{Settings: settings, Sharing: drv.Sharing(), Auth: drv.Auth(), Audit: drv.AuditLog()})
	require.NoError(t, err)
	require.NoError(t, reg.Register(kind))
	rc.Approvals, err = approval.NewService(drv.Approvals(), drv.AuditLog(), reg, approval.Options{PendingTTL: time.Hour})
	require.NoError(t, err)
	rc.Stewards, err = steward.NewService(steward.ServiceOptions{
		Sharing: drv.Sharing(), Approvals: drv.Approvals(), Audit: drv.AuditLog(), Settings: settings,
	})
	require.NoError(t, err)
	return rc
}

// do sends method path as who: a person through its browser session,
// as the web UI sends it, or sfOwner and sfDevice with their API key.
// opts adjust the request last.
func (f *stewardFlow) do(who, method, path string, body any, opts ...reqOpt) *http.Response {
	f.t.Helper()
	var rd io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		require.NoError(f.t, err)
		rd = bytes.NewReader(b)
	}
	req, err := http.NewRequest(method, f.ts.URL+path, rd)
	require.NoError(f.t, err)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if key, ok := f.keys[who]; ok {
		req.Header.Set("Authorization", "Bearer "+key)
	} else {
		req.AddCookie(&http.Cookie{Name: SessionCookieName(strings.TrimPrefix(f.ts.URL, "http://")), Value: f.cookies[who]})
		fromUI(req)
	}
	for _, o := range opts {
		o(req)
	}
	resp, err := http.DefaultClient.Do(req)
	require.NoError(f.t, err)
	f.t.Cleanup(func() { resp.Body.Close() })
	return resp
}

// filing is a valid filing body for the steward over sfScope, two days
// from now.
func (f *stewardFlow) filing() map[string]any {
	now := f.clock.now()
	return map[string]any{
		"steward_id": sfSteward, "reason": sfReason, "matter_ref": "CASE-117",
		"scope": json.RawMessage(sfScope), "not_before": now, "expires_at": now.Add(48 * time.Hour),
	}
}

// file files body as who and returns the pending request.
func (f *stewardFlow) file(who string, body map[string]any) approvalRecord {
	f.t.Helper()
	resp := f.do(who, http.MethodPost, "/api/v1/steward/grants", body)
	require.Equal(f.t, http.StatusAccepted, resp.StatusCode, errBody(f.t, resp))
	rec := decodeBody[approvalRecord](f.t, resp)
	require.Equal(f.t, "/api/v1/approvals/"+rec.ID, resp.Header.Get("Location"))
	return rec
}

// approve approves request rec as who.
func (f *stewardFlow) approve(who string, rec approvalRecord) *http.Response {
	f.t.Helper()
	return f.do(who, http.MethodPost, "/api/v1/approvals/"+rec.ID+"/approve", map[string]string{"payload_digest": rec.PayloadDigest})
}

// grant files the requester's request and has the approver approve it:
// the id of the grant it wrote.
func (f *stewardFlow) grant() string {
	f.t.Helper()
	resp := f.approve(sfApprover, f.file(sfRequester, f.filing()))
	require.Equal(f.t, http.StatusOK, resp.StatusCode, errBody(f.t, resp))
	d := decodeBody[decisionResponse](f.t, resp)
	require.Equal(f.t, approval.StateApproved, d.State)
	require.NotEmpty(f.t, d.ResultRef)
	return d.ResultRef
}

// objects lists /objects as who: the status and the ids answered.
func (f *stewardFlow) objects(who string, opts ...reqOpt) (int, []string) {
	f.t.Helper()
	resp := f.do(who, http.MethodGet, "/api/v1/objects", nil, opts...)
	if resp.StatusCode != http.StatusOK {
		return resp.StatusCode, nil
	}
	var list struct {
		Data []struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	require.NoError(f.t, json.NewDecoder(resp.Body).Decode(&list))
	ids := []string{}
	for _, o := range list.Data {
		ids = append(ids, o.ID)
	}
	return resp.StatusCode, ids
}

// elevate elevates who's browser session into grant.
func (f *stewardFlow) elevate(who, grant string) *http.Response {
	f.t.Helper()
	return f.do(who, http.MethodPost, "/api/v1/steward/grants/"+grant+"/elevate", nil)
}

// mustRead returns resp's body.
func mustRead(t *testing.T, resp *http.Response) []byte {
	t.Helper()
	b, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	return b
}

// session reads who's browser session row.
func (f *stewardFlow) session(who string) *storage.UISession {
	f.t.Helper()
	list, err := f.drv.UISessions().List(context.Background(), storage.UISessionFilter{PrincipalID: who})
	require.NoError(f.t, err)
	require.Len(f.t, list, 1)
	return list[0]
}

// switchedAccessLog is an access log whose steward appends fail while
// down is set.
type switchedAccessLog struct {
	storage.AccessLogStore
	down *atomic.Bool
}

func (s switchedAccessLog) AppendSteward(ctx context.Context, e *storage.AccessEntry) error {
	if s.down.Load() {
		return errAuditDown
	}
	return s.AccessLogStore.AppendSteward(ctx, e)
}

// stubAccess is an authority receiver no test calls through: the
// elevated-session refusal ahead of the guard is what is under test.
type stubAccess struct{ AccessReceiver }

// mintKey mints an API key for principal narrowed to scopes and returns
// the request option that sends it.
func (f *stewardFlow) mintKey(principal string, scopes ...authn.Scope) reqOpt {
	f.t.Helper()
	secret, _, err := f.keyring.Mint(ownerCtx(), authn.MintKey{PrincipalID: principal, Name: "narrowed", Scopes: scopes})
	require.NoError(f.t, err)
	return withBearer(secret)
}
