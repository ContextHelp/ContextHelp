package sharing_test

import (
	"context"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/ideacrafterslabs/ctxt/internal/search"
	"github.com/ideacrafterslabs/ctxt/internal/sharing"
	"github.com/ideacrafterslabs/ctxt/internal/storage"
	"github.com/ideacrafterslabs/ctxt/internal/storageutil"
)

// t0 is the fixture clock, on a millisecond boundary.
var t0 = time.Date(2026, 10, 15, 12, 0, 0, 0, time.UTC)

// colleagues and partners are the ADR-080 §2 templates at version 1.
func colleagues() sharing.Policy {
	p, _ := sharing.Template(sharing.TemplateColleagues)
	p.Version = 1
	return p
}

func partners() sharing.Policy {
	p, _ := sharing.Template(sharing.TemplatePartners)
	p.Version = 1
	return p
}

// seedCorpus writes the objects every predicate case selects from and
// returns their IDs by name.
//
//	c1 contact, tag work            visible to colleagues and partners
//	c2 contact, tag family          partners only (colleagues want work or client)
//	c3 contact, tag client, personal excluded by both (exclude_classes)
//	c4 contact, tag work, financial visible to both unless a ceiling denies financial
//	n1 note                         neither
func seedCorpus(t *testing.T, drv storage.StorageDriver) map[string]string {
	t.Helper()
	ctx := context.Background()
	ids := map[string]string{}
	for _, o := range []struct {
		name, typ, tag string
		classes        []string
	}{
		{"c1", "contact", "work", nil},
		{"c2", "contact", "family", nil},
		{"c3", "contact", "client", []string{"personal"}},
		{"c4", "contact", "work", []string{"financial"}},
		{"n1", "note", "work", nil},
	} {
		obj := &storage.KnowledgeObject{
			ID: "obj-" + o.name, Type: o.typ, RawContent: o.name + " body", Status: storage.ObjectStatusActive,
			Tags: []storage.Tag{{Label: o.tag}}, Classes: o.classes, ContentHash: "sha256:" + o.name,
			Metadata: map[string]any{"city": "Lisbon"}, CreatedAt: t0, UpdatedAt: t0,
		}
		if err := drv.Objects().Create(ctx, obj); err != nil {
			t.Fatalf("seed %s: %v", o.name, err)
		}
		ids[o.name] = obj.ID
	}
	return ids
}

// visible runs v's SQLite predicate through ListBySQL and returns the
// matched object IDs, sorted.
func visible(t *testing.T, drv storage.StorageDriver, v sharing.Viewer) []string {
	t.Helper()
	sql, args := v.Predicate(search.DialectSQLite)
	objs, total, err := drv.Objects().ListBySQL(context.Background(), sql, args, storage.ObjectFilter{Status: storage.ObjectStatusAll})
	if err != nil {
		t.Fatalf("ListBySQL(%q): %v", sql, err)
	}
	if total != len(objs) {
		t.Fatalf("ListBySQL total %d, rows %d", total, len(objs))
	}
	out := make([]string, len(objs))
	for i, o := range objs {
		out[i] = o.ID
	}
	slices.Sort(out)
	return out
}

func want(names ...string) []string {
	out := make([]string, len(names))
	for i, n := range names {
		out[i] = "obj-" + n
	}
	slices.Sort(out)
	return out
}

// The pure compile cases: what each shape of Input yields, run against
// a seeded SQLite store so the SQL is proven, not inspected.
func TestCompile_Predicates(t *testing.T) {
	drv := storageutil.NewTestDriver(t)
	seedCorpus(t, drv)
	col := sharing.Grant{ID: "grt_1", Audience: "colleagues", Policy: colleagues()}
	par := sharing.Grant{ID: "grt_2", Audience: "partners", Policy: partners()}
	for _, tc := range []struct {
		name string
		in   sharing.Input
		want []string
	}{
		{"colleagues alone", sharing.Input{PrincipalID: "p", Grants: []sharing.Grant{col}}, want("c1", "c4")},
		{"partners alone", sharing.Input{PrincipalID: "p", Grants: []sharing.Grant{par}}, want("c1", "c2", "c4")},
		{"union of two grants", sharing.Input{PrincipalID: "p", Grants: []sharing.Grant{col, par}}, want("c1", "c2", "c4")},
		{"ceiling removes a class a grant allows", sharing.Input{PrincipalID: "p", Grants: []sharing.Grant{col, par}, DenyClasses: []string{"financial"}}, want("c1", "c2")},
		{"ceiling on a class no grant excludes", sharing.Input{PrincipalID: "p", Grants: []sharing.Grant{par}, DenyClasses: []string{"financial", "personal"}}, want("c1", "c2")},
		{"key policy narrows the grants", sharing.Input{PrincipalID: "p", Grants: []sharing.Grant{par}, KeyPolicy: ptr(colleagues())}, want("c1", "c4")},
		{"key policy narrows the union", sharing.Input{PrincipalID: "p", Grants: []sharing.Grant{col, par}, KeyPolicy: ptr(colleagues())}, want("c1", "c4")},
		{"no grants match nothing", sharing.Input{PrincipalID: "p"}, want()},
		{"no grants with a key policy still match nothing", sharing.Input{PrincipalID: "p", KeyPolicy: ptr(partners())}, want()},
	} {
		t.Run(tc.name, func(t *testing.T) {
			v, err := sharing.Compile(tc.in)
			if err != nil {
				t.Fatalf("Compile: %v", err)
			}
			if v.IsUnrestricted() || v.PrincipalID() != "p" {
				t.Fatalf("Compile = %+v, want a restricted viewer for p", v)
			}
			if got := visible(t, drv, v); !slices.Equal(got, tc.want) {
				sql, args := v.Predicate(search.DialectSQLite)
				t.Fatalf("visible = %v, want %v\nsql: %s\nargs: %v", got, tc.want, sql, args)
			}
			if v.MatchesNothing() != (len(tc.want) == 0) {
				t.Errorf("MatchesNothing = %v with %d visible", v.MatchesNothing(), len(tc.want))
			}
			if _, args := v.Predicate(search.DialectPostgres); v.MatchesNothing() != (args == nil) {
				sql, _ := v.Predicate(search.DialectPostgres)
				t.Errorf("postgres predicate = %q, %v", sql, args)
			}
		})
	}
}

func ptr(p sharing.Policy) *sharing.Policy { return &p }

// Both dialects compile the same AST; the Postgres text binds $N
// placeholders in argument order.
func TestCompile_Dialects(t *testing.T) {
	v, err := sharing.Compile(sharing.Input{PrincipalID: "p", Grants: []sharing.Grant{{ID: "g", Policy: colleagues()}}, DenyClasses: []string{"financial"}})
	if err != nil {
		t.Fatal(err)
	}
	sq, sargs := v.Predicate(search.DialectSQLite)
	pg, pargs := v.Predicate(search.DialectPostgres)
	if strings.Count(sq, "?") != len(sargs) || len(sargs) != len(pargs) {
		t.Fatalf("sqlite %q binds %d of %d args; postgres %d", sq, strings.Count(sq, "?"), len(sargs), len(pargs))
	}
	if !strings.Contains(pg, "$"+itoa(len(pargs))) || strings.Contains(pg, "?") {
		t.Errorf("postgres predicate %q not rebound", pg)
	}
	if !strings.Contains(sq, "json_each(classes)") || !strings.Contains(pg, "jsonb_array_elements_text(classes)") {
		t.Errorf("class exclusion missing: sqlite %q, postgres %q", sq, pg)
	}
	if v.PredicateNode() == nil {
		t.Error("PredicateNode = nil for a viewer with a grant")
	}
	// Predicate hands out a copy of the arguments.
	sargs[0] = "mutated"
	if _, again := v.Predicate(search.DialectSQLite); again[0] == "mutated" {
		t.Error("Predicate shares its argument slice")
	}
}

// The render plan unions fields and capabilities across grants, keeps
// the ceiling and intersects masks.
func TestCompile_RenderPlanUnion(t *testing.T) {
	col := sharing.Grant{ID: "grt_1", Audience: "colleagues", Policy: colleagues()}
	par := sharing.Grant{ID: "grt_2", Audience: "partners", Policy: partners()}
	v, err := sharing.Compile(sharing.Input{PrincipalID: "p", Grants: []sharing.Grant{par, col}, DenyClasses: []string{"personal"}})
	if err != nil {
		t.Fatal(err)
	}
	plan := v.Plan()
	if len(plan.Grants) != 2 || plan.Grants[0].ID != "grt_1" || plan.Grants[1].Audience != "partners" || plan.Grants[0].Version != 1 {
		t.Fatalf("plan grants = %+v, want both by id", plan.Grants)
	}
	if got := plan.FieldsFor("contact"); strings.Join(got, ",") != "title,org,role,tags,city,email,phone" {
		t.Errorf("contact fields = %v, want the union in grant order", got)
	}
	if got := plan.FieldsFor("note"); strings.Join(got, ",") != "title,summary,tags,created_at" {
		t.Errorf("note fields = %v, want the * entry", got)
	}
	if !slices.Equal(plan.DenyClasses, []string{"personal"}) {
		t.Errorf("deny classes = %v", plan.DenyClasses)
	}
	if got := plan.Capabilities; !got.Entities || !got.MCP || got.Export || got.Events || strings.Join(got.Search, ",") != "fts,vector,graph" {
		t.Errorf("capabilities = %+v, want the union", got)
	}
	if plan.Masked != nil {
		t.Errorf("masked = %v, want none: partners mask nothing", plan.Masked)
	}
	if !slices.Equal(plan.Grants[0].ExcludeClasses, []string{"personal"}) || plan.Grants[0].Data == nil {
		t.Errorf("grant plan = %+v, want its exclusions and data", plan.Grants[0])
	}
}

// Alone, colleagues mask three categories; a key policy narrows the
// plan: fields intersect per type, capabilities AND.
func TestCompile_RenderPlanNarrowing(t *testing.T) {
	col := sharing.Grant{ID: "grt_1", Audience: "colleagues", Policy: colleagues()}
	par := sharing.Grant{ID: "grt_2", Audience: "partners", Policy: partners()}
	v, err := sharing.Compile(sharing.Input{PrincipalID: "p", Grants: []sharing.Grant{col}})
	if err != nil {
		t.Fatal(err)
	}
	if got := v.Plan().Masked; strings.Join(got, ",") != "address,email,phone" {
		t.Errorf("masked = %v", got)
	}

	v, err = sharing.Compile(sharing.Input{PrincipalID: "p", Grants: []sharing.Grant{par}, KeyPolicy: ptr(colleagues())})
	if err != nil {
		t.Fatal(err)
	}
	plan := v.Plan()
	if got := plan.FieldsFor("contact"); strings.Join(got, ",") != "title,org,role,tags,city" {
		t.Errorf("narrowed contact fields = %v", got)
	}
	if got := plan.FieldsFor("note"); got != nil {
		t.Errorf("narrowed note fields = %v, want none: partners render no other type", got)
	}
	if got := plan.Capabilities; strings.Join(got.Search, ",") != "fts,vector" || !got.Entities || !got.MCP {
		t.Errorf("narrowed capabilities = %+v", got)
	}
}

// The policy key names the principal, the ceiling, the key policy and
// every grant with its policy version, so each change yields a new key.
func TestCompile_PolicyKey(t *testing.T) {
	col := sharing.Grant{ID: "grt_1", Audience: "colleagues", Policy: colleagues()}
	base := sharing.Input{PrincipalID: "p", Grants: []sharing.Grant{col}}
	key := func(in sharing.Input) string {
		v, err := sharing.Compile(in)
		if err != nil {
			t.Fatal(err)
		}
		return v.PolicyKey()
	}
	k0 := key(base)
	if k0 != "principal=p;deny=;grant=grt_1:colleagues-network@1" {
		t.Errorf("key = %q", k0)
	}
	bumped := col
	bumped.Policy.Version = 2
	for name, in := range map[string]sharing.Input{
		"policy version":  {PrincipalID: "p", Grants: []sharing.Grant{bumped}},
		"another grant":   {PrincipalID: "p", Grants: []sharing.Grant{col, {ID: "grt_2", Policy: partners()}}},
		"ceiling":         {PrincipalID: "p", Grants: []sharing.Grant{col}, DenyClasses: []string{"personal"}},
		"key policy":      {PrincipalID: "p", Grants: []sharing.Grant{col}, KeyPolicy: ptr(partners())},
		"other principal": {PrincipalID: "q", Grants: []sharing.Grant{col}},
		"no grants":       {PrincipalID: "p"},
	} {
		if k := key(in); k == k0 {
			t.Errorf("%s: key unchanged %q", name, k)
		}
	}
	if sharing.Unrestricted.PolicyKey() != "unrestricted" || (sharing.Viewer{}).PolicyKey() != "" {
		t.Error("sentinel keys")
	}
}

// The zero viewer and Unrestricted answer the accessors without a view.
func TestViewerSentinels(t *testing.T) {
	var zero sharing.Viewer
	if !zero.MatchesNothing() || zero.PredicateNode() != nil || zero.PrincipalID() != "" {
		t.Error("zero viewer must match nothing")
	}
	if sql, args := zero.Predicate(search.DialectSQLite); sql != sharing.MatchNothing || args != nil {
		t.Errorf("zero predicate = %q, %v", sql, args)
	}
	if sharing.Unrestricted.MatchesNothing() {
		t.Error("Unrestricted must not match nothing")
	}
	if sql, args := sharing.Unrestricted.Predicate(search.DialectPostgres); sql != "" || args != nil {
		t.Errorf("Unrestricted predicate = %q, %v, want none", sql, args)
	}
	if sharing.Unrestricted.Plan().Fields != nil {
		t.Error("Unrestricted has a plan")
	}
}

func TestCompile_Refusals(t *testing.T) {
	if _, err := sharing.Compile(sharing.Input{}); err == nil {
		t.Error("Compile without a principal must fail")
	}
	bad := colleagues()
	bad.Data = "related==x"
	if _, err := sharing.Compile(sharing.Input{PrincipalID: "p", Grants: []sharing.Grant{{ID: "g", Policy: bad}}}); err == nil {
		t.Error("Compile with an invalid grant policy must fail")
	}
	if _, err := sharing.Compile(sharing.Input{PrincipalID: "p", Grants: []sharing.Grant{{ID: "g", Policy: colleagues()}}, KeyPolicy: &bad}); err == nil {
		t.Error("Compile with an invalid key policy must fail")
	}
}

// storeFixture is a seeded SQLite store with one person, one API key,
// the two template policies and the two audiences, for the CompileFor
// cases.
type storeFixture struct {
	t         *testing.T
	drv       storage.StorageDriver
	st        storage.SharingStore
	person    *storage.Principal
	key       *storage.Identity
	policies  map[string]*storage.SharePolicy
	audiences map[string]*storage.Audience
}

func newStoreFixture(t *testing.T) *storeFixture {
	t.Helper()
	ctx := context.Background()
	f := &storeFixture{t: t, drv: storageutil.NewTestDriver(t), policies: map[string]*storage.SharePolicy{}, audiences: map[string]*storage.Audience{}}
	f.st = f.drv.Sharing()
	seedCorpus(t, f.drv)
	f.person = &storage.Principal{ID: "prn_compile", Kind: storage.PrincipalKindPerson, DisplayName: "Colleague", CreatedAt: t0}
	if err := f.drv.Auth().Principals().Create(ctx, f.person); err != nil {
		t.Fatal(err)
	}
	f.key = &storage.Identity{PrincipalID: f.person.ID, Method: storage.IdentityMethodAPIKey, Subject: "agent-key", CreatedAt: t0}
	if err := f.drv.Auth().Identities().Create(ctx, f.key); err != nil {
		t.Fatal(err)
	}
	for _, tpl := range sharing.Templates() {
		spec, err := tpl.Spec()
		if err != nil {
			t.Fatal(err)
		}
		row := &storage.SharePolicy{Name: tpl.Name, Spec: spec, UpdatedAt: t0}
		if err := f.st.Policies().Create(ctx, row); err != nil {
			t.Fatal(err)
		}
		f.policies[tpl.Name] = row
	}
	for _, name := range []string{"colleagues", "partners"} {
		a := &storage.Audience{Name: name, CreatedAt: t0}
		if err := f.st.Audiences().Create(ctx, a); err != nil {
			t.Fatal(err)
		}
		f.audiences[name] = a
	}
	return f
}

// compile compiles the person's viewer at now, under identity when set.
func (f *storeFixture) compile(identity string, now time.Time) sharing.Viewer {
	f.t.Helper()
	v, err := sharing.CompileFor(context.Background(), f.st, f.person.ID, identity, now)
	if err != nil {
		f.t.Fatalf("CompileFor: %v", err)
	}
	return v
}

// join puts the person in audience, expiring at expires when set.
func (f *storeFixture) join(audience string, expires *time.Time) {
	f.t.Helper()
	m := &storage.AudienceMember{AudienceID: f.audiences[audience].ID, PrincipalID: f.person.ID, ExpiresAt: expires, AddedBy: storage.OwnerPrincipal, AddedAt: t0}
	if err := f.st.Audiences().AddMember(context.Background(), m); err != nil {
		f.t.Fatal(err)
	}
}

// grant gives audience the template policy, expiring at expires when set.
func (f *storeFixture) grant(audience, policy string, expires *time.Time, at time.Time) *storage.ShareGrant {
	f.t.Helper()
	g := &storage.ShareGrant{AudienceID: f.audiences[audience].ID, PolicyID: f.policies[policy].ID, ExpiresAt: expires, CreatedBy: storage.OwnerPrincipal, CreatedAt: at}
	if err := f.st.Grants().Create(context.Background(), g); err != nil {
		f.t.Fatal(err)
	}
	return g
}

// assertVisible checks the person's view at now selects want.
func (f *storeFixture) assertVisible(name, identity string, now time.Time, wanted []string) {
	f.t.Helper()
	if got := visible(f.t, f.drv, f.compile(identity, now)); !slices.Equal(got, wanted) {
		f.t.Fatalf("%s: visible %v, want %v", name, got, wanted)
	}
}

// CompileFor reads the store: memberships, grants (expiry and
// revocation respected), the ceiling and the key binding, and the
// predicate runs against the same store.
func TestCompileFor_Store(t *testing.T) {
	ctx := context.Background()
	f := newStoreFixture(t)
	hour, later := t0.Add(time.Hour), t0.Add(2*time.Hour)

	// No membership: nothing, whatever the grants say.
	f.grant("colleagues", sharing.TemplateColleagues, &hour, t0)
	f.assertVisible("no membership", "", t0, want())

	// Colleague membership with the grant that expires in an hour.
	f.join("colleagues", nil)
	f.assertVisible("colleague before expiry", "", t0, want("c1", "c4"))
	if v := f.compile("", hour); !v.MatchesNothing() {
		t.Fatalf("expired grant still compiled: %q", v.PolicyKey())
	}

	// A partners grant through a membership that expires at two hours.
	f.join("partners", &later)
	partnersGrant := f.grant("partners", sharing.TemplatePartners, nil, t0.Add(time.Minute))
	f.assertVisible("both grants", "", t0, want("c1", "c2", "c4"))
	f.assertVisible("after both expiries", "", later, want())

	// The ceiling removes financial through either grant.
	if err := f.st.SetDenyClasses(ctx, f.person.ID, []string{"financial"}); err != nil {
		t.Fatal(err)
	}
	f.assertVisible("ceiling", "", t0, want("c1", "c2"))
	if err := f.st.SetDenyClasses(ctx, f.person.ID, nil); err != nil {
		t.Fatal(err)
	}

	// A revoked grant is gone.
	if err := f.st.Grants().Revoke(ctx, partnersGrant.ID, t0.Add(time.Minute)); err != nil {
		t.Fatal(err)
	}
	f.assertVisible("after revoke", "", t0, want("c1", "c4"))

	// An unknown principal is not found.
	if _, err := sharing.CompileFor(ctx, f.st, "prn_missing", "", t0); err == nil {
		t.Error("CompileFor(unknown principal) must fail")
	}
}

// The key binding narrows the view and the policy key follows every
// grant and policy version.
func TestCompileFor_KeyAndVersions(t *testing.T) {
	ctx := context.Background()
	f := newStoreFixture(t)
	f.join("colleagues", nil)
	f.join("partners", nil)
	colGrant := f.grant("colleagues", sharing.TemplateColleagues, nil, t0)
	parGrant := f.grant("partners", sharing.TemplatePartners, nil, t0.Add(time.Minute))

	k := f.compile("", t0).PolicyKey()
	if !strings.Contains(k, colGrant.ID+":colleagues-network@1") || !strings.Contains(k, parGrant.ID+":partners-network@1") {
		t.Errorf("policy key = %q", k)
	}

	// A key bound to the colleagues policy narrows the partners view.
	if err := f.st.BindIdentityPolicy(ctx, f.key.ID, f.policies[sharing.TemplateColleagues].ID); err != nil {
		t.Fatal(err)
	}
	f.assertVisible("key policy", f.key.ID, t0, want("c1", "c4"))
	if v := f.compile(f.key.ID, t0); v.IdentityID() != f.key.ID || !strings.Contains(v.PolicyKey(), "key=colleagues-network@1") {
		t.Errorf("key viewer = %s %q", v.IdentityID(), v.PolicyKey())
	}
	// Unbound, the same key sees the union again.
	if err := f.st.BindIdentityPolicy(ctx, f.key.ID, ""); err != nil {
		t.Fatal(err)
	}
	f.assertVisible("key unbound", f.key.ID, t0, want("c1", "c2", "c4"))

	// An edit bumps the policy version into the key.
	spec, _ := partners().Spec()
	if _, err := f.st.Policies().Update(ctx, f.policies[sharing.TemplatePartners].ID, spec, t0.Add(time.Minute)); err != nil {
		t.Fatal(err)
	}
	if k := f.compile("", t0).PolicyKey(); !strings.Contains(k, "partners-network@2") {
		t.Errorf("policy key after edit = %q", k)
	}
	// An unknown identity is not found.
	if _, err := sharing.CompileFor(ctx, f.st, f.person.ID, "idn_missing", t0); err == nil {
		t.Error("CompileFor(unknown identity) must fail")
	}
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var b []byte
	for n > 0 {
		b = append([]byte{byte('0' + n%10)}, b...)
		n /= 10
	}
	return string(b)
}
