package steward_test

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ideacrafterslabs/ctxt/internal/approval"
	"github.com/ideacrafterslabs/ctxt/internal/sharing"
	"github.com/ideacrafterslabs/ctxt/internal/steward"
	"github.com/ideacrafterslabs/ctxt/internal/storage"
)

// grantAudience creates audience name holding a grant of the given
// policy specs (name, spec), and returns the audience and the policy
// rows.
func (f *fixture) grantAudience(name string, policies map[string]string) (*storage.Audience, map[string]*storage.SharePolicy) {
	f.t.Helper()
	ctx := f.t.Context()
	a := &storage.Audience{Name: name}
	require.NoError(f.t, f.sharing.Audiences().Create(ctx, a))
	rows := map[string]*storage.SharePolicy{}
	for pname, spec := range policies {
		row := &storage.SharePolicy{Name: pname, Spec: json.RawMessage(spec)}
		require.NoError(f.t, f.sharing.Policies().Create(ctx, row))
		require.NoError(f.t, f.sharing.Grants().Create(ctx, &storage.ShareGrant{AudienceID: a.ID, PolicyID: row.ID, CreatedBy: storage.OwnerPrincipal}))
		rows[pname] = row
	}
	return a, rows
}

const partnerPolicy = `{"data":"type==contact","exclude_classes":["personal"],"fields":{"contact":["title","org"]},"capabilities":{"search":["fts"],"entities":true,"mcp":true}}`

// as_audience compiles the policies currently granted to the audience
// into the frozen scope: the approvers see the compiled spec, with the
// audience recorded, and the steward's view is what the audience's
// members see (less what a steward scope never opens).
func TestAsAudienceCompilesIntoTheFrozenScope(t *testing.T) {
	f := newFixture(t)
	f.seedCorpus()
	f.grantAudience("partners", map[string]string{"partners-policy": partnerPolicy})

	req := f.request()
	req.Scope, req.AsAudience = nil, "partners"
	r := f.mustFile(req)
	p, err := steward.DecodePayload(r.Payload)
	require.NoError(t, err)
	assert.Equal(t, "partners", p.AsAudience)
	scope, err := sharing.ParseStewardScope(p.Scope)
	require.NoError(t, err)
	require.Len(t, scope.Specs, 1)
	assert.Equal(t, "type==contact", scope.Specs[0].Data)
	assert.False(t, scope.Specs[0].Capabilities.MCP, "mcp is not carried into a steward scope")
	assert.True(t, scope.Specs[0].Capabilities.Entities)
	assert.Equal(t, []string{"mcp"}, p.DroppedCapabilities, "the approvers see what the freeze dropped")

	_, err = f.approve(r, f.alice)
	require.NoError(t, err)
	g, err := f.sharing.StewardGrants().GetByRequest(t.Context(), r.ID)
	require.NoError(t, err)
	v, err := f.elevated(g)
	require.NoError(t, err)
	assert.Equal(t, want("c1", "c2", "c4"), f.visible(v), "what partners see: contacts, not the personal one")
	assert.False(t, v.Allows(storage.CapabilityMCP))
}

// An audience of several policies freezes one spec per policy, never a
// merge: the payload carries each, the capabilities dropped from any of
// them, and the steward's rows are the union the members see.
func TestAsAudienceFreezesEachPolicy(t *testing.T) {
	f := newFixture(t)
	f.seedCorpus()
	f.grantAudience("auditors", map[string]string{
		"a-contacts": `{"data":"type==contact","exclude_classes":["financial"],"fields":{"contact":["title"]},"capabilities":{"entities":true,"mcp":true}}`,
		"b-work":     `{"data":"tag==work","fields":{"*":["*"]},"capabilities":{"events":true,"export":true}}`,
	})
	req := f.request()
	req.Scope, req.AsAudience = nil, "auditors"
	r := f.mustFile(req)
	p, err := steward.DecodePayload(r.Payload)
	require.NoError(t, err)
	scope, err := sharing.ParseStewardScope(p.Scope)
	require.NoError(t, err)
	require.Len(t, scope.Specs, 2)
	assert.Equal(t, "type==contact", scope.Specs[0].Data)
	assert.Equal(t, []string{"financial"}, scope.Specs[0].ExcludeClasses, "each policy keeps its own exclusions")
	assert.Equal(t, map[string][]string{"contact": {"title"}}, scope.Specs[0].Fields)
	assert.Equal(t, "tag==work", scope.Specs[1].Data)
	assert.Empty(t, scope.Specs[1].ExcludeClasses)
	assert.Equal(t, []string{"events", "export", "mcp"}, p.DroppedCapabilities)

	_, err = f.approve(r, f.alice)
	require.NoError(t, err)
	g, err := f.sharing.StewardGrants().GetByRequest(t.Context(), r.ID)
	require.NoError(t, err)
	v, err := f.elevated(g)
	require.NoError(t, err)
	assert.Equal(t, want("c1", "c2", "c4", "n1"), f.visible(v), "contacts but the personal (withheld) one and the financial one through a-contacts; work through b-work")
	require.Len(t, v.Plan().Grants, 2, "one grant per spec")
	assert.Equal(t, v.Plan().Grants[0].ID, v.Plan().Grants[1].ID, "both the one steward grant's")
	assert.False(t, v.Allows(storage.CapabilityEvents) || v.Allows(storage.CapabilityExport) || v.Allows(storage.CapabilityMCP))
}

// colleaguesSpec is the colleagues-network template as a policy row
// stores it; it masks email, phone and address.
func colleaguesSpec(t *testing.T) string {
	t.Helper()
	p, ok := sharing.Template(sharing.TemplateColleagues)
	require.True(t, ok)
	raw, err := json.Marshal(p)
	require.NoError(t, err)
	return string(raw)
}

// payloadKeys decodes a frozen payload's top-level keys.
func payloadKeys(t *testing.T, raw json.RawMessage) map[string]any {
	t.Helper()
	var m map[string]any
	require.NoError(t, json.Unmarshal(raw, &m))
	return m
}

// An audience granted a policy that redacts (the colleagues-network
// template) files and freezes: the spec keeps the policy's redact as
// the members have it, and the payload says the masking is asked for
// but not applied, under the digest the approvers bind to. Masking is
// inert for members and steward alike; the elevated plan masks what
// the policy masks.
func TestAsAudienceFreezesARedactingPolicy(t *testing.T) {
	f := newFixture(t)
	f.seedCorpus()
	f.grantAudience("colleagues", map[string]string{sharing.TemplateColleagues: colleaguesSpec(t)})
	req := f.request()
	req.Scope, req.AsAudience = nil, "colleagues"
	r := f.mustFile(req)
	p, err := steward.DecodePayload(r.Payload)
	require.NoError(t, err)
	scope, err := sharing.ParseStewardScope(p.Scope)
	require.NoError(t, err)
	require.Len(t, scope.Specs, 1)
	assert.Equal(t, sharing.Redact{Mask: []string{"email", "phone", "address"}, Style: sharing.RedactCategory}, scope.Specs[0].Redact, "the template's redact, unchanged")
	assert.True(t, p.MaskingInert, "the approvers see that masking is asked for but not applied")
	assert.Equal(t, []string{"mcp"}, p.DroppedCapabilities)

	// The marker is under the payload digest, as the rest of it is.
	m := payloadKeys(t, r.Payload)
	assert.Equal(t, true, m["masking_inert"])
	canon, err := approval.Canonicalize(r.Payload)
	require.NoError(t, err)
	assert.Equal(t, r.PayloadDigest, approval.Digest(canon))
	delete(m, "masking_inert")
	stripped, err := json.Marshal(m)
	require.NoError(t, err)
	canon, err = approval.Canonicalize(stripped)
	require.NoError(t, err)
	assert.NotEqual(t, r.PayloadDigest, approval.Digest(canon), "the payload without the marker has another digest")

	_, err = f.approve(r, f.alice)
	require.NoError(t, err)
	g, err := f.sharing.StewardGrants().GetByRequest(t.Context(), r.ID)
	require.NoError(t, err)
	v, err := f.elevated(g)
	require.NoError(t, err)
	assert.Equal(t, want("c1", "c4"), f.visible(v), "what colleagues see: work and client contacts, not the personal one")
	assert.Equal(t, []string{"address", "email", "phone"}, v.Plan().Masked, "the elevated plan carries the policy's mask")
}

// The payload marks masking as not applied exactly when a frozen spec
// carries a redact: an audience policy that masks, alone or beside one
// that does not. An audience that masks nothing, and an explicit scope,
// carry no marker.
func TestPayloadMarksMaskingInertWhenASpecRedacts(t *testing.T) {
	for name, c := range map[string]struct {
		policies map[string]string
		want     bool
	}{
		"no policy redacts":  {map[string]string{"partners-policy": partnerPolicy}, false},
		"the policy redacts": {map[string]string{sharing.TemplateColleagues: colleaguesSpec(t)}, true},
		"one of two redacts": {map[string]string{sharing.TemplateColleagues: colleaguesSpec(t), "partners-policy": partnerPolicy}, true},
	} {
		t.Run(name, func(t *testing.T) {
			f := newFixture(t)
			f.grantAudience("audience", c.policies)
			req := f.request()
			req.Scope, req.AsAudience = nil, "audience"
			r := f.mustFile(req)
			p, err := steward.DecodePayload(r.Payload)
			require.NoError(t, err)
			assert.Equal(t, c.want, p.MaskingInert)
			_, has := payloadKeys(t, r.Payload)["masking_inert"]
			assert.Equal(t, c.want, has, "the key is present only when set")
		})
	}
	f := newFixture(t)
	r := f.mustFile(f.request())
	p, err := steward.DecodePayload(r.Payload)
	require.NoError(t, err)
	assert.False(t, p.MaskingInert, "an explicit scope carries no redact")
	assert.NotContains(t, payloadKeys(t, r.Payload), "masking_inert")
}

// An explicit scope that names redact is still refused, whatever its
// value: an approver must not read as masking what is not applied.
func TestExplicitScopeRefusesRedact(t *testing.T) {
	f := newFixture(t)
	for _, redact := range []string{`{"mask":["email"]}`, `{}`, `null`} {
		req := f.request()
		req.Scope = json.RawMessage(`{"data":"type==contact","redact":` + redact + `}`)
		_, err := f.file(req)
		requireCode(t, err, steward.CodeScopeInvalid)
		assert.ErrorContains(t, err, "masking is not available", redact)
	}
}

// The freeze is the point: edits to the named policy, new grants to
// the audience and new members never widen an approved grant. The
// elevated view and its key stay as approved.
func TestLaterEditsNeverWidenAnApprovedGrant(t *testing.T) {
	f := newFixture(t)
	f.seedCorpus()
	a, rows := f.grantAudience("partners", map[string]string{"partners-policy": partnerPolicy})

	req := f.request()
	req.Scope, req.AsAudience = nil, "partners"
	g := f.approved(req)
	before, err := f.elevated(g)
	require.NoError(t, err)
	wantVisible := f.visible(before)
	require.Equal(t, want("c1", "c2", "c4"), wantVisible)

	// The policy is widened to everything, with every field.
	_, err = f.sharing.Policies().Update(t.Context(), rows["partners-policy"].ID,
		json.RawMessage(`{"data":"type!=__none__","fields":{"*":["*"]},"capabilities":{"export":true,"entities":true}}`), f.clock.now())
	require.NoError(t, err)
	// The audience gains a second, wider grant and a new member.
	wide := &storage.SharePolicy{Name: "everything", Spec: json.RawMessage(`{"data":"type!=__none__","fields":{"*":["*"]}}`)}
	require.NoError(t, f.sharing.Policies().Create(t.Context(), wide))
	require.NoError(t, f.sharing.Grants().Create(t.Context(), &storage.ShareGrant{AudienceID: a.ID, PolicyID: wide.ID, CreatedBy: storage.OwnerPrincipal}))
	require.NoError(t, f.sharing.Audiences().AddMember(t.Context(), &storage.AudienceMember{AudienceID: a.ID, PrincipalID: f.stewardP.ID, AddedBy: storage.OwnerPrincipal}))
	// A new request from the edited audience would be wider: proof the
	// edit is real.
	req2 := f.request()
	req2.Scope, req2.AsAudience = nil, "partners"
	r2 := f.mustFile(req2)
	p2, err := steward.DecodePayload(r2.Payload)
	require.NoError(t, err)
	scope2, err := sharing.ParseStewardScope(p2.Scope)
	require.NoError(t, err)
	require.Len(t, scope2.Specs, 2, "a fresh freeze sees the audience's second grant")
	for _, sp := range scope2.Specs {
		assert.Contains(t, sp.Data, "__none__", "a fresh freeze sees the widened policies")
	}

	after, err := f.elevated(g)
	require.NoError(t, err)
	assert.Equal(t, wantVisible, f.visible(after), "the approved grant did not widen")
	assert.Equal(t, before.PolicyKey(), after.PolicyKey())
	assert.Equal(t, before.Plan().FieldsFor("contact"), after.Plan().FieldsFor("contact"))
	assert.False(t, after.Allows(storage.CapabilityExport))
}

// A pending request goes stale when the audience's policies change
// before the last approval: what the approvers approved is no longer
// what "as_audience" means.
func TestAsAudienceRequestGoesStaleWhenThePoliciesChange(t *testing.T) {
	for name, change := range map[string]func(f *fixture, a *storage.Audience, rows map[string]*storage.SharePolicy){
		"the policy is edited": func(f *fixture, _ *storage.Audience, rows map[string]*storage.SharePolicy) {
			_, err := f.sharing.Policies().Update(f.t.Context(), rows["partners-policy"].ID, json.RawMessage(`{"data":"type==note"}`), f.clock.now())
			require.NoError(f.t, err)
		},
		"a grant is added": func(f *fixture, a *storage.Audience, _ map[string]*storage.SharePolicy) {
			row := &storage.SharePolicy{Name: "extra", Spec: json.RawMessage(`{"data":"type==note"}`)}
			require.NoError(f.t, f.sharing.Policies().Create(f.t.Context(), row))
			require.NoError(f.t, f.sharing.Grants().Create(f.t.Context(), &storage.ShareGrant{AudienceID: a.ID, PolicyID: row.ID, CreatedBy: storage.OwnerPrincipal}))
		},
		"the grant expires": func(f *fixture, a *storage.Audience, _ map[string]*storage.SharePolicy) {
			grants, err := f.sharing.Grants().List(f.t.Context(), storage.GrantFilter{AudienceID: a.ID})
			require.NoError(f.t, err)
			f.exec(`UPDATE share_grants SET expires_at = ? WHERE id = ?`, f.timeValue(f.clock.now().Add(time.Minute)), grants[0].ID)
			f.clock.advance(time.Hour)
		},
		"the grant is revoked": func(f *fixture, a *storage.Audience, _ map[string]*storage.SharePolicy) {
			grants, err := f.sharing.Grants().List(f.t.Context(), storage.GrantFilter{AudienceID: a.ID})
			require.NoError(f.t, err)
			require.NoError(f.t, f.sharing.Grants().Revoke(f.t.Context(), grants[0].ID, f.clock.now()))
		},
	} {
		t.Run(name, func(t *testing.T) {
			f := newFixture(t)
			a, rows := f.grantAudience("partners", map[string]string{"partners-policy": partnerPolicy})
			req := f.request()
			req.Scope, req.AsAudience = nil, "partners"
			r := f.mustFile(req)
			change(f, a, rows)
			_, err := f.approve(r, f.alice)
			requireCode(t, err, approval.CodeStale)
			_, gerr := f.sharing.StewardGrants().GetByRequest(t.Context(), r.ID)
			assert.ErrorIs(t, gerr, storage.ErrNotFound)
		})
	}
}

// A request with an explicit scope does not depend on any audience.
func TestExplicitScopeIgnoresAudienceChanges(t *testing.T) {
	f := newFixture(t)
	_, rows := f.grantAudience("partners", map[string]string{"partners-policy": partnerPolicy})
	r := f.mustFile(f.request())
	_, err := f.sharing.Policies().Update(t.Context(), rows["partners-policy"].ID, json.RawMessage(`{"data":"type==note"}`), f.clock.now())
	require.NoError(t, err)
	_, err = f.approve(r, f.alice)
	require.NoError(t, err)
}

// An audience that does not exist, or grants nothing, freezes to
// nothing and is refused.
func TestAsAudienceRefusals(t *testing.T) {
	f := newFixture(t)
	f.grantAudience("empty", nil)
	for name, aud := range map[string]string{"unknown": "nobody", "no grants": "empty"} {
		req := f.request()
		req.Scope, req.AsAudience = nil, aud
		_, err := f.file(req)
		assert.Equal(t, steward.CodeScopeInvalid, approval.CodeOf(err), name+": %v", err)
	}
	// A grant that has expired does not count.
	old, _ := f.grantAudience("lapsed", map[string]string{"lapsed-policy": partnerPolicy})
	expired := f.clock.now().Add(-time.Hour)
	grants, err := f.sharing.Grants().List(t.Context(), storage.GrantFilter{AudienceID: old.ID})
	require.NoError(t, err)
	f.exec(`UPDATE share_grants SET expires_at = ? WHERE id = ?`, f.timeValue(expired), grants[0].ID)
	lapsed := f.request()
	lapsed.Scope, lapsed.AsAudience = nil, "lapsed"
	_, err = f.file(lapsed)
	assert.Equal(t, steward.CodeScopeInvalid, approval.CodeOf(err))

	// A revoked grant does not count.
	a, _ := f.grantAudience("closed", map[string]string{"closed-policy": partnerPolicy})
	grants, err = f.sharing.Grants().List(t.Context(), storage.GrantFilter{AudienceID: a.ID})
	require.NoError(t, err)
	require.NoError(t, f.sharing.Grants().Revoke(t.Context(), grants[0].ID, f.clock.now()))
	req := f.request()
	req.Scope, req.AsAudience = nil, "closed"
	_, err = f.file(req)
	assert.Equal(t, steward.CodeScopeInvalid, approval.CodeOf(err))
}
