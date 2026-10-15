package storagetest

import (
	"context"
	"encoding/json"
	"errors"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/ideacrafterslabs/ctxt/internal/storage"
)

// sharingT0 is the conformance clock origin, on a millisecond boundary:
// the SQLite driver stores Unix milliseconds.
var sharingT0 = time.Date(2026, 10, 15, 12, 0, 0, 0, time.UTC)

// Routes the access-log cases log.
const (
	routeObjects = "GET /objects"
	routeFind    = "POST /find"
)

// SharingConformance pins the sharing registry contract every driver
// implements (ADR-080): audiences and memberships with expiry, versioned
// policies, grants with expiry and revocation, the active-grant join a
// viewer compiles from, the per-principal ceiling, the policy an
// identity narrows to, and the batched read audit with retention
// pruning.
func SharingConformance(t *testing.T, drv storage.StorageDriver) {
	t.Helper()
	st := drv.Sharing()
	if st == nil {
		t.Fatal("Sharing() returned nil")
	}
	for _, c := range []struct {
		name string
		fn   func(*testing.T, storage.StorageDriver)
	}{
		{"audience create and get", sharingConfAudienceCreate},
		{"audience list by name", sharingConfAudienceList},
		{"audience create refusals", sharingConfAudienceRefusals},
		{"audience members oldest first", sharingConfMembers},
		{"audience memberships honour expiry", sharingConfMemberships},
		{"audience member replace and remove", sharingConfMemberReplace},
		{"audience member refusals", sharingConfMemberRefusals},
		{"policy create and get", sharingConfPolicyCreate},
		{"policy update bumps version", sharingConfPolicyUpdate},
		{"policy list by name", sharingConfPolicyList},
		{"policy refusals", sharingConfPolicyRefusals},
		{"grant create, get and list", sharingConfGrants},
		{"grant revoke keeps the first revocation", sharingConfGrantRevoke},
		{"grant refusals", sharingConfGrantRefusals},
		{"active grants honour membership, grant expiry and revocation", sharingConfActiveGrants},
		{"active grants carry the policy at its current version", sharingConfActiveGrantPolicy},
		{"ceiling read and write", sharingConfCeiling},
		{"ceiling refusals", sharingConfCeilingRefusals},
		{"identity policy binding", sharingConfIdentityPolicy},
		{"identity policy refusals", sharingConfIdentityPolicyRefusals},
		{"access log append and query by principal", sharingConfAccessLogPrincipal},
		{"access log query by object and window", sharingConfAccessLogObject},
		{"access log prune keeps entries and counts", sharingConfAccessLogPrune},
		{"access log refusals", sharingConfAccessLogRefusals},
	} {
		t.Run(c.name, func(t *testing.T) { c.fn(t, drv) })
	}
}

// sharingFixture is one audience, one policy and one principal, named
// after the test so the cases never collide.
type sharingFixture struct {
	audience  *storage.Audience
	policy    *storage.SharePolicy
	principal *storage.Principal
}

func newSharingFixture(t *testing.T, drv storage.StorageDriver, tag string) sharingFixture {
	t.Helper()
	ctx := context.Background()
	a := &storage.Audience{Name: "aud-" + tag, CreatedAt: sharingT0}
	if err := drv.Sharing().Audiences().Create(ctx, a); err != nil {
		t.Fatalf("create audience: %v", err)
	}
	p := &storage.SharePolicy{Name: "pol-" + tag, Spec: json.RawMessage(`{"data":"type==contact"}`), UpdatedAt: sharingT0}
	if err := drv.Sharing().Policies().Create(ctx, p); err != nil {
		t.Fatalf("create policy: %v", err)
	}
	return sharingFixture{audience: a, policy: p, principal: newPrincipal(t, drv.Auth(), "prn_share_"+tag, sharingT0)}
}

func (f sharingFixture) member(t *testing.T, drv storage.StorageDriver, principalID string, expires *time.Time, at time.Time) {
	t.Helper()
	m := &storage.AudienceMember{AudienceID: f.audience.ID, PrincipalID: principalID, ExpiresAt: expires, AddedBy: storage.OwnerPrincipal, AddedAt: at}
	if err := drv.Sharing().Audiences().AddMember(context.Background(), m); err != nil {
		t.Fatalf("add member %s: %v", principalID, err)
	}
}

func (f sharingFixture) grant(t *testing.T, drv storage.StorageDriver, id string, expires *time.Time, at time.Time) *storage.ShareGrant {
	t.Helper()
	g := &storage.ShareGrant{ID: id, AudienceID: f.audience.ID, PolicyID: f.policy.ID, ExpiresAt: expires, CreatedBy: storage.OwnerPrincipal, CreatedAt: at}
	if err := drv.Sharing().Grants().Create(context.Background(), g); err != nil {
		t.Fatalf("create grant %s: %v", id, err)
	}
	return g
}

// memberships returns how many memberships principal holds at now.
func memberships(t *testing.T, drv storage.StorageDriver, principal string, now time.Time) []*storage.AudienceMember {
	t.Helper()
	ms, err := drv.Sharing().Audiences().ListMemberships(context.Background(), principal, now)
	if err != nil {
		t.Fatalf("ListMemberships(%s, %s): %v", principal, now, err)
	}
	return ms
}

// activeGrantIDs returns the IDs of the grants in force for principal at now.
func activeGrantIDs(t *testing.T, drv storage.StorageDriver, principal string, now time.Time) []string {
	t.Helper()
	gs, err := drv.Sharing().Grants().ListActiveForPrincipal(context.Background(), principal, now)
	if err != nil {
		t.Fatalf("ListActiveForPrincipal(%s, %s): %v", principal, now, err)
	}
	out := make([]string, len(gs))
	for i, g := range gs {
		out[i] = g.Grant.ID
	}
	return out
}

// namesAmong returns, in list order, the names of list that are in want.
func namesAmong(names []string, want ...string) []string {
	out := make([]string, 0, len(want))
	for _, n := range names {
		if slices.Contains(want, n) {
			out = append(out, n)
		}
	}
	return out
}

func sharingConfAudienceCreate(t *testing.T, drv storage.StorageDriver) {
	ctx := context.Background()
	st := drv.Sharing().Audiences()
	a := &storage.Audience{Name: "  colleagues  ", CreatedAt: sharingT0}
	if err := st.Create(ctx, a); err != nil {
		t.Fatalf("Create: %v", err)
	}
	if !strings.HasPrefix(a.ID, "aud_") || a.Name != "colleagues" || !a.CreatedAt.Equal(sharingT0) {
		t.Fatalf("Create left %+v, want an aud_ id, a trimmed name and the created time", a)
	}
	got, err := st.Get(ctx, a.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.ID != a.ID || got.Name != a.Name || !got.CreatedAt.Equal(a.CreatedAt) {
		t.Fatalf("Get = %+v, want %+v", got, a)
	}
	byName, err := st.GetByName(ctx, "colleagues")
	if err != nil || byName.ID != a.ID {
		t.Fatalf("GetByName = %+v, %v; want %s", byName, err, a.ID)
	}
	defaulted := &storage.Audience{Name: "defaulted"}
	if err := st.Create(ctx, defaulted); err != nil || defaulted.CreatedAt.IsZero() {
		t.Fatalf("Create(no time) = %v, left %+v; want now filled in", err, defaulted)
	}
	if _, err := st.Get(ctx, "aud_missing"); !errors.Is(err, storage.ErrNotFound) {
		t.Fatalf("Get(missing) = %v, want ErrNotFound", err)
	}
	if _, err := st.GetByName(ctx, "nobody"); !errors.Is(err, storage.ErrNotFound) {
		t.Fatalf("GetByName(missing) = %v, want ErrNotFound", err)
	}
}

func sharingConfAudienceList(t *testing.T, drv storage.StorageDriver) {
	ctx := context.Background()
	st := drv.Sharing().Audiences()
	for _, name := range []string{"list-zeta", "list-alpha"} {
		if err := st.Create(ctx, &storage.Audience{Name: name, CreatedAt: sharingT0}); err != nil {
			t.Fatalf("Create %s: %v", name, err)
		}
	}
	list, err := st.List(ctx)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	names := make([]string, len(list))
	for i, a := range list {
		names[i] = a.Name
	}
	if got := namesAmong(names, "list-alpha", "list-zeta"); !slices.Equal(got, []string{"list-alpha", "list-zeta"}) {
		t.Fatalf("List names = %v, want by name", got)
	}
}

func sharingConfAudienceRefusals(t *testing.T, drv storage.StorageDriver) {
	ctx := context.Background()
	st := drv.Sharing().Audiences()
	if err := st.Create(ctx, &storage.Audience{Name: "dup-aud"}); err != nil {
		t.Fatalf("Create: %v", err)
	}
	if err := st.Create(ctx, &storage.Audience{Name: "dup-aud"}); !errors.Is(err, storage.ErrAlreadyExists) {
		t.Fatalf("Create(taken name) = %v, want ErrAlreadyExists", err)
	}
	for _, name := range []string{"", "Bad Name", "-leading"} {
		if err := st.Create(ctx, &storage.Audience{Name: name}); err == nil || errors.Is(err, storage.ErrAlreadyExists) {
			t.Fatalf("Create(name %q) = %v, want a validation error", name, err)
		}
	}
	if err := st.Create(ctx, nil); err == nil {
		t.Fatal("Create(nil) must fail")
	}
}

func sharingConfMembers(t *testing.T, drv storage.StorageDriver) {
	ctx := context.Background()
	f := newSharingFixture(t, drv, "members")
	other := newPrincipal(t, drv.Auth(), "prn_share_members_other", sharingT0)
	later := sharingT0.Add(time.Hour)
	f.member(t, drv, f.principal.ID, nil, sharingT0)
	f.member(t, drv, other.ID, &later, sharingT0.Add(time.Minute))

	members, err := drv.Sharing().Audiences().ListMembers(ctx, f.audience.ID)
	if err != nil {
		t.Fatalf("ListMembers: %v", err)
	}
	if len(members) != 2 || members[0].PrincipalID != f.principal.ID || members[1].PrincipalID != other.ID {
		t.Fatalf("ListMembers = %+v, want the two members oldest first", members)
	}
	if members[0].ExpiresAt != nil || members[1].ExpiresAt == nil || !members[1].ExpiresAt.Equal(later) {
		t.Fatalf("ListMembers expiries = %v, %v; want nil and %s", members[0].ExpiresAt, members[1].ExpiresAt, later)
	}
	if members[0].AddedBy != storage.OwnerPrincipal || !members[0].AddedAt.Equal(sharingT0) {
		t.Fatalf("member[0] = %+v, want added by owner at t0", members[0])
	}
}

func sharingConfMemberships(t *testing.T, drv storage.StorageDriver) {
	f := newSharingFixture(t, drv, "memberships")
	other := newPrincipal(t, drv.Auth(), "prn_share_memberships_other", sharingT0)
	later := sharingT0.Add(time.Hour)
	f.member(t, drv, f.principal.ID, nil, sharingT0)
	f.member(t, drv, other.ID, &later, sharingT0)

	// Before the hour both principals are in; at and after it only the
	// open-ended one.
	for _, c := range []struct {
		principal string
		now       time.Time
		want      int
	}{
		{f.principal.ID, sharingT0, 1},
		{f.principal.ID, later.Add(time.Hour), 1},
		{other.ID, sharingT0.Add(30 * time.Minute), 1},
		{other.ID, later, 0},
		{other.ID, later.Add(time.Second), 0},
		{"prn_share_nobody", sharingT0, 0},
	} {
		if got := memberships(t, drv, c.principal, c.now); len(got) != c.want {
			t.Fatalf("ListMemberships(%s, %s) = %d memberships, want %d", c.principal, c.now, len(got), c.want)
		}
	}
}

func sharingConfMemberReplace(t *testing.T, drv storage.StorageDriver) {
	ctx := context.Background()
	f := newSharingFixture(t, drv, "member-replace")
	st := drv.Sharing().Audiences()
	later := sharingT0.Add(time.Hour)
	f.member(t, drv, f.principal.ID, &later, sharingT0)

	// Re-adding replaces the membership's expiry, adder and time.
	if err := st.AddMember(ctx, &storage.AudienceMember{AudienceID: f.audience.ID, PrincipalID: f.principal.ID, AddedBy: "prn_share_member-replace", AddedAt: sharingT0.Add(2 * time.Minute)}); err != nil {
		t.Fatalf("AddMember(replace): %v", err)
	}
	ms := memberships(t, drv, f.principal.ID, later.Add(time.Hour))
	if len(ms) != 1 || ms[0].ExpiresAt != nil || ms[0].AddedBy != "prn_share_member-replace" {
		t.Fatalf("ListMemberships after replace = %+v; want one open-ended membership added by the fixture principal", ms)
	}

	if err := st.RemoveMember(ctx, f.audience.ID, f.principal.ID); err != nil {
		t.Fatalf("RemoveMember: %v", err)
	}
	if err := st.RemoveMember(ctx, f.audience.ID, f.principal.ID); !errors.Is(err, storage.ErrNotFound) {
		t.Fatalf("RemoveMember(again) = %v, want ErrNotFound", err)
	}
	if ms := memberships(t, drv, f.principal.ID, sharingT0); len(ms) != 0 {
		t.Fatalf("ListMemberships after remove = %+v, want none", ms)
	}
}

func sharingConfMemberRefusals(t *testing.T, drv storage.StorageDriver) {
	ctx := context.Background()
	f := newSharingFixture(t, drv, "member-refusals")
	st := drv.Sharing().Audiences()
	if err := st.AddMember(ctx, &storage.AudienceMember{AudienceID: "aud_missing", PrincipalID: f.principal.ID, AddedBy: storage.OwnerPrincipal}); !errors.Is(err, storage.ErrNotFound) {
		t.Fatalf("AddMember(unknown audience) = %v, want ErrNotFound", err)
	}
	if err := st.AddMember(ctx, &storage.AudienceMember{AudienceID: f.audience.ID, PrincipalID: "prn_missing", AddedBy: storage.OwnerPrincipal}); !errors.Is(err, storage.ErrNotFound) {
		t.Fatalf("AddMember(unknown principal) = %v, want ErrNotFound", err)
	}
	if err := st.AddMember(ctx, &storage.AudienceMember{AudienceID: f.audience.ID, PrincipalID: f.principal.ID}); err == nil || errors.Is(err, storage.ErrNotFound) {
		t.Fatalf("AddMember(no added_by) = %v, want a validation error", err)
	}
	if err := st.AddMember(ctx, nil); err == nil {
		t.Fatal("AddMember(nil) must fail")
	}
	members, err := st.ListMembers(ctx, "aud_missing")
	if err != nil || len(members) != 0 {
		t.Fatalf("ListMembers(unknown) = %v, %v; want none", members, err)
	}
}

func sharingConfPolicyCreate(t *testing.T, drv storage.StorageDriver) {
	ctx := context.Background()
	st := drv.Sharing().Policies()
	p := &storage.SharePolicy{Name: "conf-network", Spec: json.RawMessage(`{"data": "type==contact", "fields": {"contact": ["title"]}}`), Version: 7, UpdatedAt: sharingT0}
	if err := st.Create(ctx, p); err != nil {
		t.Fatalf("Create: %v", err)
	}
	if !strings.HasPrefix(p.ID, "pol_") || p.Version != 1 || !p.UpdatedAt.Equal(sharingT0) {
		t.Fatalf("Create left %+v, want a pol_ id, version 1 and the updated time", p)
	}
	got, err := st.Get(ctx, p.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.Name != p.Name || got.Version != 1 || !got.UpdatedAt.Equal(p.UpdatedAt) {
		t.Fatalf("Get = %+v, want %+v", got, p)
	}
	var spec map[string]any
	if err := json.Unmarshal(got.Spec, &spec); err != nil || spec["data"] != "type==contact" {
		t.Fatalf("Get spec = %s, %v; want the stored object", got.Spec, err)
	}
	byName, err := st.GetByName(ctx, "conf-network")
	if err != nil || byName.ID != p.ID {
		t.Fatalf("GetByName = %+v, %v", byName, err)
	}
	defaulted := &storage.SharePolicy{Name: "conf-defaulted", Spec: json.RawMessage(`{}`)}
	if err := st.Create(ctx, defaulted); err != nil || defaulted.UpdatedAt.IsZero() {
		t.Fatalf("Create(no time) = %v, left %+v; want now filled in", err, defaulted)
	}
	if _, err := st.Get(ctx, "pol_missing"); !errors.Is(err, storage.ErrNotFound) {
		t.Fatalf("Get(missing) = %v, want ErrNotFound", err)
	}
}

func sharingConfPolicyUpdate(t *testing.T, drv storage.StorageDriver) {
	ctx := context.Background()
	st := drv.Sharing().Policies()
	p := &storage.SharePolicy{Name: "conf-update", Spec: json.RawMessage(`{"data":"type==contact"}`), UpdatedAt: sharingT0}
	if err := st.Create(ctx, p); err != nil {
		t.Fatalf("Create: %v", err)
	}
	at := sharingT0.Add(time.Hour)
	updated, err := st.Update(ctx, p.ID, json.RawMessage(`{"data":"type==note"}`), at)
	if err != nil {
		t.Fatalf("Update: %v", err)
	}
	if updated.Version != 2 || !updated.UpdatedAt.Equal(at) || !strings.Contains(string(updated.Spec), "type==note") {
		t.Fatalf("Update = %+v, want version 2 at %s with the new spec", updated, at)
	}
	again, err := st.Update(ctx, p.ID, json.RawMessage(`{"data":"type==task"}`), at.Add(time.Minute))
	if err != nil || again.Version != 3 {
		t.Fatalf("second Update = %+v, %v; want version 3", again, err)
	}
	if got, _ := st.Get(ctx, p.ID); got.Version != 3 {
		t.Fatalf("Get after updates = version %d, want 3", got.Version)
	}
}

func sharingConfPolicyList(t *testing.T, drv storage.StorageDriver) {
	ctx := context.Background()
	st := drv.Sharing().Policies()
	for _, name := range []string{"plist-zeta", "plist-alpha"} {
		if err := st.Create(ctx, &storage.SharePolicy{Name: name, Spec: json.RawMessage(`{}`)}); err != nil {
			t.Fatalf("Create %s: %v", name, err)
		}
	}
	list, err := st.List(ctx)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	names := make([]string, len(list))
	for i, p := range list {
		names[i] = p.Name
	}
	if got := namesAmong(names, "plist-alpha", "plist-zeta"); !slices.Equal(got, []string{"plist-alpha", "plist-zeta"}) {
		t.Fatalf("List names = %v, want by name", got)
	}
}

func sharingConfPolicyRefusals(t *testing.T, drv storage.StorageDriver) {
	ctx := context.Background()
	st := drv.Sharing().Policies()
	if err := st.Create(ctx, &storage.SharePolicy{Name: "dup-pol", Spec: json.RawMessage(`{}`)}); err != nil {
		t.Fatalf("Create: %v", err)
	}
	if err := st.Create(ctx, &storage.SharePolicy{Name: "dup-pol", Spec: json.RawMessage(`{}`)}); !errors.Is(err, storage.ErrAlreadyExists) {
		t.Fatalf("Create(taken name) = %v, want ErrAlreadyExists", err)
	}
	for name, spec := range map[string]string{"array": `[]`, "string": `"x"`, "empty": ``, "broken": `{`} {
		if err := st.Create(ctx, &storage.SharePolicy{Name: "spec-" + name, Spec: json.RawMessage(spec)}); err == nil {
			t.Fatalf("Create(spec %s) must fail", name)
		}
	}
	if err := st.Create(ctx, &storage.SharePolicy{Name: "Bad Name", Spec: json.RawMessage(`{}`)}); err == nil {
		t.Fatal("Create(bad name) must fail")
	}
	if _, err := st.Update(ctx, "pol_missing", json.RawMessage(`{}`), sharingT0); !errors.Is(err, storage.ErrNotFound) {
		t.Fatalf("Update(missing) = %v, want ErrNotFound", err)
	}
	p, _ := st.GetByName(ctx, "dup-pol")
	if _, err := st.Update(ctx, p.ID, json.RawMessage(`[]`), sharingT0); err == nil {
		t.Fatal("Update(array spec) must fail")
	}
	if got, _ := st.Get(ctx, p.ID); got.Version != 1 {
		t.Fatalf("refused Update bumped the version to %d", got.Version)
	}
}

func sharingConfGrants(t *testing.T, drv storage.StorageDriver) {
	ctx := context.Background()
	f := newSharingFixture(t, drv, "grants")
	st := drv.Sharing().Grants()
	later := sharingT0.Add(time.Hour)
	g1 := f.grant(t, drv, "", nil, sharingT0)
	g2 := f.grant(t, drv, "grt_conf_second", &later, sharingT0.Add(time.Minute))
	if !strings.HasPrefix(g1.ID, "grt_") || g1.RevokedAt != nil {
		t.Fatalf("Create left %+v, want a grt_ id and no revocation", g1)
	}
	got, err := st.Get(ctx, g2.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.AudienceID != f.audience.ID || got.PolicyID != f.policy.ID || got.CreatedBy != storage.OwnerPrincipal {
		t.Fatalf("Get = %+v, want the stored grant", got)
	}
	if got.ExpiresAt == nil || !got.ExpiresAt.Equal(later) || !got.CreatedAt.Equal(sharingT0.Add(time.Minute)) {
		t.Fatalf("Get times = %v, %s; want expiry %s and creation t0+1m", got.ExpiresAt, got.CreatedAt, later)
	}
	list, err := st.List(ctx, storage.GrantFilter{AudienceID: f.audience.ID})
	if err != nil || len(list) != 2 || list[0].ID != g1.ID || list[1].ID != g2.ID {
		t.Fatalf("List(audience) = %+v, %v; want both grants oldest first", list, err)
	}
	if list, err := st.List(ctx, storage.GrantFilter{PolicyID: "pol_other"}); err != nil || len(list) != 0 {
		t.Fatalf("List(other policy) = %+v, %v; want none", list, err)
	}
	if _, err := st.Get(ctx, "grt_missing"); !errors.Is(err, storage.ErrNotFound) {
		t.Fatalf("Get(missing) = %v, want ErrNotFound", err)
	}
}

func sharingConfGrantRevoke(t *testing.T, drv storage.StorageDriver) {
	ctx := context.Background()
	f := newSharingFixture(t, drv, "revoke")
	st := drv.Sharing().Grants()
	g1 := f.grant(t, drv, "grt_revoke_first", nil, sharingT0)
	g2 := f.grant(t, drv, "grt_revoke_second", nil, sharingT0.Add(time.Minute))

	at := sharingT0.Add(2 * time.Hour)
	if err := st.Revoke(ctx, g1.ID, at); err != nil {
		t.Fatalf("Revoke: %v", err)
	}
	if err := st.Revoke(ctx, g1.ID, at.Add(time.Hour)); err != nil {
		t.Fatalf("Revoke(again): %v", err)
	}
	got, _ := st.Get(ctx, g1.ID)
	if got.RevokedAt == nil || !got.RevokedAt.Equal(at) {
		t.Fatalf("revoked_at = %v, want the first revocation %s", got.RevokedAt, at)
	}
	list, err := st.List(ctx, storage.GrantFilter{AudienceID: f.audience.ID})
	if err != nil || len(list) != 1 || list[0].ID != g2.ID {
		t.Fatalf("List after revoke = %+v, %v; want the live grant only", list, err)
	}
	list, err = st.List(ctx, storage.GrantFilter{AudienceID: f.audience.ID, IncludeRevoked: true})
	if err != nil || len(list) != 2 {
		t.Fatalf("List(include revoked) = %+v, %v; want both", list, err)
	}
	if err := st.Revoke(ctx, "grt_missing", at); !errors.Is(err, storage.ErrNotFound) {
		t.Fatalf("Revoke(missing) = %v, want ErrNotFound", err)
	}
}

func sharingConfGrantRefusals(t *testing.T, drv storage.StorageDriver) {
	ctx := context.Background()
	f := newSharingFixture(t, drv, "grant-refusals")
	st := drv.Sharing().Grants()
	if err := st.Create(ctx, &storage.ShareGrant{AudienceID: "aud_missing", PolicyID: f.policy.ID, CreatedBy: storage.OwnerPrincipal}); !errors.Is(err, storage.ErrNotFound) {
		t.Fatalf("Create(unknown audience) = %v, want ErrNotFound", err)
	}
	if err := st.Create(ctx, &storage.ShareGrant{AudienceID: f.audience.ID, PolicyID: "pol_missing", CreatedBy: storage.OwnerPrincipal}); !errors.Is(err, storage.ErrNotFound) {
		t.Fatalf("Create(unknown policy) = %v, want ErrNotFound", err)
	}
	if err := st.Create(ctx, &storage.ShareGrant{AudienceID: f.audience.ID, PolicyID: f.policy.ID}); err == nil || errors.Is(err, storage.ErrNotFound) {
		t.Fatalf("Create(no created_by) = %v, want a validation error", err)
	}
	g := f.grant(t, drv, "grt_conf_taken", nil, sharingT0)
	if err := st.Create(ctx, &storage.ShareGrant{ID: g.ID, AudienceID: f.audience.ID, PolicyID: f.policy.ID, CreatedBy: storage.OwnerPrincipal}); !errors.Is(err, storage.ErrAlreadyExists) {
		t.Fatalf("Create(taken id) = %v, want ErrAlreadyExists", err)
	}
}

func sharingConfActiveGrants(t *testing.T, drv storage.StorageDriver) {
	ctx := context.Background()
	f := newSharingFixture(t, drv, "active")
	grantEnd := sharingT0.Add(time.Hour)
	memberEnd := sharingT0.Add(2 * time.Hour)

	// No membership: nothing, whatever the grants say.
	open := f.grant(t, drv, "grt_active_open", nil, sharingT0)
	if got := activeGrantIDs(t, drv, f.principal.ID, sharingT0); len(got) != 0 {
		t.Fatalf("ListActiveForPrincipal(no membership) = %v; want none", got)
	}

	f.member(t, drv, f.principal.ID, &memberEnd, sharingT0)
	expiring := f.grant(t, drv, "grt_active_expiring", &grantEnd, sharingT0.Add(time.Minute))
	revoked := f.grant(t, drv, "grt_active_revoked", nil, sharingT0.Add(2*time.Minute))
	if err := drv.Sharing().Grants().Revoke(ctx, revoked.ID, sharingT0.Add(3*time.Minute)); err != nil {
		t.Fatalf("Revoke: %v", err)
	}
	for _, c := range []struct {
		name string
		now  time.Time
		want []string
	}{
		{"before the grant expiry", sharingT0.Add(30 * time.Minute), []string{open.ID, expiring.ID}},
		{"at the grant expiry", grantEnd, []string{open.ID}},
		{"after the grant expiry", grantEnd.Add(time.Minute), []string{open.ID}},
		{"at the membership expiry", memberEnd, nil},
	} {
		if got := activeGrantIDs(t, drv, f.principal.ID, c.now); !slices.Equal(got, c.want) {
			t.Fatalf("%s: active grants = %v, want %v", c.name, got, c.want)
		}
	}
}

func sharingConfActiveGrantPolicy(t *testing.T, drv storage.StorageDriver) {
	ctx := context.Background()
	f := newSharingFixture(t, drv, "active-policy")
	st := drv.Sharing().Grants()
	f.member(t, drv, f.principal.ID, nil, sharingT0)
	f.grant(t, drv, "grt_active_policy", nil, sharingT0)

	got, err := st.ListActiveForPrincipal(ctx, f.principal.ID, sharingT0)
	if err != nil || len(got) != 1 {
		t.Fatalf("ListActiveForPrincipal = %+v, %v; want one", got, err)
	}
	if got[0].Audience.Name != f.audience.Name || got[0].Policy.Name != f.policy.Name || got[0].Policy.Version != 1 {
		t.Fatalf("active grant = %+v, want its audience and policy v1", got[0])
	}
	if !strings.Contains(string(got[0].Policy.Spec), "type==contact") {
		t.Fatalf("active grant spec = %s, want the stored spec", got[0].Policy.Spec)
	}
	// An edit is seen on the next compile.
	if _, err := drv.Sharing().Policies().Update(ctx, f.policy.ID, json.RawMessage(`{"data":"type==note"}`), sharingT0.Add(time.Minute)); err != nil {
		t.Fatalf("Update policy: %v", err)
	}
	got, _ = st.ListActiveForPrincipal(ctx, f.principal.ID, sharingT0)
	if got[0].Policy.Version != 2 || !strings.Contains(string(got[0].Policy.Spec), "type==note") {
		t.Fatalf("active grant after edit = %+v, want policy v2 with the new spec", got[0].Policy)
	}
}

func sharingConfCeiling(t *testing.T, drv storage.StorageDriver) {
	ctx := context.Background()
	f := newSharingFixture(t, drv, "ceiling")
	st := drv.Sharing()
	got, err := st.DenyClasses(ctx, f.principal.ID)
	if err != nil || got != nil {
		t.Fatalf("DenyClasses(new) = %v, %v; want none", got, err)
	}
	if err := st.SetDenyClasses(ctx, f.principal.ID, []string{"personal", "financial", "personal"}); err != nil {
		t.Fatalf("SetDenyClasses: %v", err)
	}
	got, err = st.DenyClasses(ctx, f.principal.ID)
	if err != nil || !slices.Equal(got, []string{"financial", "personal"}) {
		t.Fatalf("DenyClasses = %v, %v; want financial, personal", got, err)
	}
	if err := st.SetDenyClasses(ctx, f.principal.ID, nil); err != nil {
		t.Fatalf("SetDenyClasses(nil): %v", err)
	}
	if got, _ = st.DenyClasses(ctx, f.principal.ID); got != nil {
		t.Fatalf("DenyClasses after clear = %v, want none", got)
	}
}

func sharingConfCeilingRefusals(t *testing.T, drv storage.StorageDriver) {
	ctx := context.Background()
	f := newSharingFixture(t, drv, "ceiling-refusals")
	st := drv.Sharing()
	if err := st.SetDenyClasses(ctx, f.principal.ID, []string{"Not Valid"}); err == nil {
		t.Fatal("SetDenyClasses(invalid class) must fail")
	}
	if _, err := st.DenyClasses(ctx, "prn_missing"); !errors.Is(err, storage.ErrNotFound) {
		t.Fatalf("DenyClasses(missing) = %v, want ErrNotFound", err)
	}
	if err := st.SetDenyClasses(ctx, "prn_missing", []string{"personal"}); !errors.Is(err, storage.ErrNotFound) {
		t.Fatalf("SetDenyClasses(missing) = %v, want ErrNotFound", err)
	}
}

func sharingConfIdentityPolicy(t *testing.T, drv storage.StorageDriver) {
	ctx := context.Background()
	f := newSharingFixture(t, drv, "identity")
	id := newIdentity(t, drv.Auth(), f.principal.ID, "key-identity-policy", sharingT0)
	st := drv.Sharing()
	got, err := st.IdentityPolicy(ctx, id.ID)
	if err != nil || got != "" {
		t.Fatalf("IdentityPolicy(unbound) = %q, %v; want empty", got, err)
	}
	if err := st.BindIdentityPolicy(ctx, id.ID, f.policy.ID); err != nil {
		t.Fatalf("BindIdentityPolicy: %v", err)
	}
	if got, _ = st.IdentityPolicy(ctx, id.ID); got != f.policy.ID {
		t.Fatalf("IdentityPolicy = %q, want %s", got, f.policy.ID)
	}
	if err := st.BindIdentityPolicy(ctx, id.ID, ""); err != nil {
		t.Fatalf("BindIdentityPolicy(clear): %v", err)
	}
	if got, _ = st.IdentityPolicy(ctx, id.ID); got != "" {
		t.Fatalf("IdentityPolicy after clear = %q, want empty", got)
	}
}

func sharingConfIdentityPolicyRefusals(t *testing.T, drv storage.StorageDriver) {
	ctx := context.Background()
	f := newSharingFixture(t, drv, "identity-refusals")
	id := newIdentity(t, drv.Auth(), f.principal.ID, "key-identity-refusals", sharingT0)
	st := drv.Sharing()
	if err := st.BindIdentityPolicy(ctx, id.ID, "pol_missing"); !errors.Is(err, storage.ErrNotFound) {
		t.Fatalf("BindIdentityPolicy(unknown policy) = %v, want ErrNotFound", err)
	}
	if err := st.BindIdentityPolicy(ctx, "idn_missing", f.policy.ID); !errors.Is(err, storage.ErrNotFound) {
		t.Fatalf("BindIdentityPolicy(unknown identity) = %v, want ErrNotFound", err)
	}
	if _, err := st.IdentityPolicy(ctx, "idn_missing"); !errors.Is(err, storage.ErrNotFound) {
		t.Fatalf("IdentityPolicy(missing) = %v, want ErrNotFound", err)
	}
}

// accessLogEntries is the batch the access-log cases append under tag,
// so the cases never see each other's rows.
func accessLogEntries(tag string) []*storage.AccessEntry {
	return []*storage.AccessEntry{
		{At: sharingT0, PrincipalID: "prn_log_a_" + tag, IdentityID: "idn_log_a", Route: routeObjects, PolicyVersions: "v1", ObjectCount: 2, EntityCount: 1, ObjectIDs: []string{"obj-2-" + tag, "obj-1-" + tag, "obj-2-" + tag}},
		{At: sharingT0.Add(time.Minute), PrincipalID: "prn_log_a_" + tag, Route: routeFind, PolicyVersions: "v1", ObjectCount: 1, ObjectIDs: []string{"obj-3-" + tag}},
		{At: sharingT0.Add(2 * time.Minute), PrincipalID: "prn_log_b_" + tag, Route: routeObjects + "/obj-1", PolicyVersions: "v2", ObjectCount: 1, ObjectIDs: []string{"obj-1-" + tag}},
		{PrincipalID: "prn_log_c_" + tag, Route: "GET /stats"},
	}
}

func appendAccessLog(t *testing.T, drv storage.StorageDriver, tag string) []*storage.AccessEntry {
	t.Helper()
	entries := accessLogEntries(tag)
	if err := drv.Sharing().AccessLog().Append(context.Background(), entries); err != nil {
		t.Fatalf("Append: %v", err)
	}
	return entries
}

func queryAccessLog(t *testing.T, drv storage.StorageDriver, f storage.AccessFilter) []*storage.AccessEntry {
	t.Helper()
	got, err := drv.Sharing().AccessLog().Query(context.Background(), f)
	if err != nil {
		t.Fatalf("Query(%+v): %v", f, err)
	}
	return got
}

func sharingConfAccessLogPrincipal(t *testing.T, drv storage.StorageDriver) {
	entries := appendAccessLog(t, drv, "principal")
	for _, e := range entries {
		if !strings.HasPrefix(e.ID, "acc_") || e.At.IsZero() {
			t.Fatalf("Append left %+v, want an acc_ id and a time", e)
		}
	}
	if err := drv.Sharing().AccessLog().Append(context.Background(), nil); err != nil {
		t.Fatalf("Append(nil): %v", err)
	}
	byA := queryAccessLog(t, drv, storage.AccessFilter{PrincipalID: "prn_log_a_principal"})
	if len(byA) != 2 || byA[0].Route != routeFind || byA[1].Route != routeObjects {
		t.Fatalf("Query(principal) = %+v, want its two entries newest first", byA)
	}
	first := byA[1]
	if !slices.Equal(first.ObjectIDs, []string{"obj-1-principal", "obj-2-principal"}) || first.IdentityID != "idn_log_a" {
		t.Fatalf("Query entry = %+v, want its ids deduplicated and sorted, and its identity", first)
	}
	if first.ObjectCount != 2 || first.EntityCount != 1 || first.PolicyVersions != "v1" || !first.At.Equal(sharingT0) {
		t.Fatalf("Query entry = %+v, want its counts, versions and time", first)
	}
	if byA[0].IdentityID != "" {
		t.Fatalf("entry without identity read back %q", byA[0].IdentityID)
	}
	limited := queryAccessLog(t, drv, storage.AccessFilter{PrincipalID: "prn_log_a_principal", Limit: 1})
	if len(limited) != 1 || limited[0].Route != routeFind {
		t.Fatalf("Query(limit 1) = %+v, want the newest", limited)
	}
}

func sharingConfAccessLogObject(t *testing.T, drv storage.StorageDriver) {
	appendAccessLog(t, drv, "object")
	byObject := queryAccessLog(t, drv, storage.AccessFilter{ObjectID: "obj-1-object"})
	if len(byObject) != 2 || byObject[0].PrincipalID != "prn_log_b_object" || byObject[1].PrincipalID != "prn_log_a_object" {
		t.Fatalf("Query(object) = %+v, want both readers of obj-1 newest first", byObject)
	}
	both := queryAccessLog(t, drv, storage.AccessFilter{PrincipalID: "prn_log_a_object", ObjectID: "obj-1-object"})
	if len(both) != 1 || both[0].Route != routeObjects {
		t.Fatalf("Query(principal and object) = %+v", both)
	}
	window := queryAccessLog(t, drv, storage.AccessFilter{PrincipalID: "prn_log_a_object", Since: sharingT0.Add(time.Minute), Until: sharingT0.Add(2 * time.Minute)})
	if len(window) != 1 || window[0].Route != routeFind {
		t.Fatalf("Query(window) = %+v, want the entry at t0+1m only", window)
	}
}

func sharingConfAccessLogPrune(t *testing.T, drv storage.StorageDriver) {
	ctx := context.Background()
	appendAccessLog(t, drv, "prune")
	st := drv.Sharing().AccessLog()
	// Retention drops the ID lists of old entries; the entries and their
	// counts stay. The two entries before t0+2m hold three ids.
	n, err := st.Prune(ctx, sharingT0.Add(2*time.Minute))
	if err != nil || n < 3 {
		t.Fatalf("Prune = %d, %v; want at least the 3 ids of this case removed", n, err)
	}
	byA := queryAccessLog(t, drv, storage.AccessFilter{PrincipalID: "prn_log_a_prune"})
	if len(byA) != 2 || byA[1].ObjectIDs != nil || byA[1].ObjectCount != 2 {
		t.Fatalf("Query after prune = %+v, want entries kept with counts, without ids", byA)
	}
	byObject := queryAccessLog(t, drv, storage.AccessFilter{ObjectID: "obj-1-prune"})
	if len(byObject) != 1 || byObject[0].PrincipalID != "prn_log_b_prune" {
		t.Fatalf("Query(object) after prune = %+v, want the unpruned reader only", byObject)
	}
	if n, err := st.Prune(ctx, sharingT0.Add(2*time.Minute)); err != nil || n != 0 {
		t.Fatalf("Prune(again) = %d, %v; want nothing left", n, err)
	}
}

func sharingConfAccessLogRefusals(t *testing.T, drv storage.StorageDriver) {
	ctx := context.Background()
	st := drv.Sharing().AccessLog()
	for name, e := range map[string]*storage.AccessEntry{
		"no principal": {Route: routeObjects},
		"no route":     {PrincipalID: "prn_log_x"},
	} {
		if err := st.Append(ctx, []*storage.AccessEntry{e}); err == nil {
			t.Fatalf("Append(%s) must fail", name)
		}
	}
	if err := st.Append(ctx, []*storage.AccessEntry{nil}); err == nil {
		t.Fatal("Append(nil entry) must fail")
	}
	// A refused batch writes nothing.
	ok := &storage.AccessEntry{PrincipalID: "prn_log_batch", Route: routeObjects}
	if err := st.Append(ctx, []*storage.AccessEntry{ok, {Route: "GET /x"}}); err == nil {
		t.Fatal("Append(mixed batch) must fail")
	}
	if got := queryAccessLog(t, drv, storage.AccessFilter{PrincipalID: "prn_log_batch"}); len(got) != 0 {
		t.Fatalf("refused batch wrote %+v", got)
	}
}
