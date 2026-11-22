package sharechange_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ideacrafterslabs/ctxt/internal/sharing"
)

// The approver sees why a change waits: the filing's reason is the
// classifier's verdict and its stable reason code.
func TestSubmit_FilingCarriesWhyTheChangeWaits(t *testing.T) {
	f := newFx(t, sqliteDriver)
	r := f.pending(f.admin, sharing.MemberPut{AudienceID: f.team.ID, PrincipalID: f.dana.ID})
	assert.Equal(t, "widening: "+sharing.ReasonMembershipNew, r.Reason)
	filed := f.eventsOf("approval.request.filed")
	require.Len(t, filed, 1)
	assert.Equal(t, "widening: "+sharing.ReasonMembershipNew, filed[0].Payload["reason"])
}

// A non-owner change applied at once records why it did not wait; the
// owner's change, which is not classified, records no class.
func TestSubmit_AppliedAuditEntryCarriesTheClass(t *testing.T) {
	f := newFx(t, sqliteDriver)
	f.grant(f.base)
	f.join(f.erin.ID, nil)
	out := f.submit(f.admin, sharing.PolicyUpdate{PolicyID: f.base.ID, Policy: narrowerBase()})
	require.NotNil(t, out.Applied)
	events := f.eventsOf(out.Applied.Event)
	require.Len(t, events, 1)
	assert.Equal(t, "narrowing", events[0].Payload["class"])
	assert.Equal(t, sharing.ReasonSpecNarrower, events[0].Payload["class_reason"])

	f.submit(f.owner, sharing.AudienceCreate{Name: "partners"})
	owned := f.eventsOf("share.audience.created")
	require.Len(t, owned, 1)
	assert.NotContains(t, owned[0].Payload, "class")
}
