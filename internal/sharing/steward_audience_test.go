package sharing_test

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ideacrafterslabs/ctxt/internal/sharing"
	"github.com/ideacrafterslabs/ctxt/internal/storage"
)

// auditorPolicies are an audience's two policies, at version 1, chosen
// so that no object renders, or masks, the same through both:
//
//	p1 contacts, not financial; contact titles only; entities and fts;
//	   masks email and phone
//	p2 work-tagged objects of any class; every field; rsql; masks email
//	   and address
//
// Over the corpus: c1 is covered by both, c2 and c3 by p1 alone, c4
// (financial) and n1 by p2 alone.
func auditorPolicies() []sharing.Policy {
	return []sharing.Policy{
		{
			Name: "p1-contacts", Version: 1, Data: "type==contact", ExcludeClasses: []string{"financial"},
			Fields:       map[string][]string{"contact": {sharing.FieldTitle}},
			Redact:       sharing.Redact{Mask: []string{"email", "phone"}},
			Capabilities: sharing.Capabilities{Entities: true, Search: []string{sharing.SearchFTS}},
		},
		{
			Name: "p2-work", Version: 1, Data: "tag==work",
			Fields:       map[string][]string{"*": {sharing.FieldAll}},
			Redact:       sharing.Redact{Mask: []string{"email", "address"}, Style: sharing.RedactPartial},
			Capabilities: sharing.Capabilities{Search: []string{sharing.SearchRSQL}},
		},
	}
}

// objectPlan is the plan object id renders under for v, as a scoped
// read attributes it.
func objectPlan(t *testing.T, drv storage.StorageDriver, v sharing.Viewer, id string) sharing.RenderPlan {
	t.Helper()
	plan, err := sharing.ObjectPlanOf(context.Background(), v, drv, id)
	require.NoError(t, err, id)
	return plan
}

// memberViewer is what a member of the audience sees: one grant per
// policy.
func memberViewer(t *testing.T, principal string, policies []sharing.Policy) sharing.Viewer {
	t.Helper()
	in := sharing.Input{PrincipalID: principal}
	for _, p := range policies {
		in.Grants = append(in.Grants, sharing.Grant{ID: "grt_" + p.Name, Audience: "auditors", Policy: p})
	}
	v, err := sharing.Compile(in)
	require.NoError(t, err)
	return v
}

// rendered reads object id through the scoped driver as v sees it: the
// rendered object, or nil when v does not see it.
func rendered(t *testing.T, drv storage.StorageDriver, v sharing.Viewer, id string) *storage.KnowledgeObject {
	t.Helper()
	ctx := sharing.WithViewer(context.Background(), v)
	obj, err := sharing.Scoped(ctx, drv, nil).Objects().Get(ctx, id)
	if errors.Is(err, storage.ErrNotFound) {
		return nil
	}
	require.NoError(t, err, id)
	return obj
}

// as_audience freezes "what the audience's members see": per object,
// the elevated view renders exactly what a member's does (the rows, the
// fields of each row through the policies covering it, the masks, and
// the surfaces each capability opens), never the union of the policies.
func TestAsAudienceElevationSeesWhatAMemberSees(t *testing.T) {
	f := newStewardFix(t)
	f.settings.WithheldClasses = nil
	policies := auditorPolicies()
	scope, dropped, err := sharing.FreezeAudienceScope(policies, *f.settings)
	require.NoError(t, err)
	assert.Empty(t, dropped)
	require.Len(t, scope.Specs, 2, "one spec per policy, never a merge")
	raw, err := scope.JSON()
	require.NoError(t, err)
	g := f.open(f.person.ID, string(raw))
	steward, err := f.resolve(f.person.ID, g, "")
	require.NoError(t, err)
	member := memberViewer(t, f.person.ID, policies)

	for _, name := range []string{"c1", "c2", "c3", "c4", "n1"} {
		id := "obj-" + name
		want, got := rendered(t, f.drv, member, id), rendered(t, f.drv, steward, id)
		require.Equal(t, want == nil, got == nil, "%s: a member sees it %v, the steward %v", name, want != nil, got != nil)
		assert.Equal(t, want, got, "%s renders as it does for a member", name)
	}
	assert.Nil(t, rendered(t, f.drv, member, "obj-nothing"))

	// Masking is inert today, but the plan carries it. Per object the
	// steward's mask is a member's, what every policy covering the
	// object masks, so once masking lands the elevated view never shows
	// more than a member sees.
	for name, mask := range map[string][]string{
		"c1": {"email"}, "c2": {"email", "phone"}, "c3": {"email", "phone"},
		"c4": {"address", "email"}, "n1": {"address", "email"},
	} {
		id := "obj-" + name
		assert.Equal(t, mask, objectPlan(t, f.drv, member, id).Masked, "%s: what a member's plan masks", name)
		assert.Equal(t, objectPlan(t, f.drv, member, id).Masked, objectPlan(t, f.drv, steward, id).Masked, "%s: the steward's mask is the member's", name)
	}
	assert.Equal(t, member.Plan().Masked, steward.Plan().Masked)

	for _, c := range []string{
		storage.CapabilityEntities, storage.CapabilityExport, storage.CapabilityMCP, storage.CapabilityEvents,
		"search:" + sharing.SearchFTS, "search:" + sharing.SearchRSQL, "search:" + sharing.SearchVector,
	} {
		assert.Equal(t, member.Allows(c), steward.Allows(c), "%s: the gate", c)
		assert.Equal(t, visible(t, f.drv, member.ForCapability(c)), visible(t, f.drv, steward.ForCapability(c)), "%s: the rows the surface answers", c)
	}

	// The withheld classes apply to every spec as to an explicit scope:
	// c3 (personal) leaves the elevated view, nothing else changes.
	f.settings.WithheldClasses = []string{"personal"}
	withheld, err := f.resolve(f.person.ID, g, "")
	require.NoError(t, err)
	assert.Nil(t, rendered(t, f.drv, withheld, "obj-c3"))
	for _, name := range []string{"c1", "c2", "c4", "n1"} {
		id := "obj-" + name
		assert.Equal(t, rendered(t, f.drv, member, id), rendered(t, f.drv, withheld, id), name)
	}
}
