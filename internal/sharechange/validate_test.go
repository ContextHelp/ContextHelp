package sharechange_test

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ideacrafterslabs/ctxt/internal/approval"
	"github.com/ideacrafterslabs/ctxt/internal/sharechange"
	"github.com/ideacrafterslabs/ctxt/internal/sharing"
	"github.com/ideacrafterslabs/ctxt/internal/storage"
)

// A filing the registry would refuse is refused before it is filed:
// nothing pending can name a row that does not exist, a principal that
// cannot join, a key that is not the principal's, an expiry already
// over, or a payload that says more than the change.
func TestFile_RefusesWhatTheRegistryWouldRefuse(t *testing.T) {
	f := newFx(t, sqliteDriver)
	raw := func(v any) json.RawMessage {
		b, err := json.Marshal(v)
		require.NoError(t, err)
		return b
	}
	member := func(c sharing.MemberPut) approval.Filing {
		fl, err := sharechange.Filing(f.admin, c, "")
		require.NoError(t, err)
		return fl
	}
	cases := []struct {
		name string
		f    approval.Filing
	}{
		{"unknown audience", member(sharing.MemberPut{AudienceID: "aud_nope", PrincipalID: f.dana.ID})},
		{"unknown principal", member(sharing.MemberPut{AudienceID: f.team.ID, PrincipalID: "prn_nope"})},
		{"the owner joins no audience", member(sharing.MemberPut{AudienceID: f.team.ID, PrincipalID: storage.OwnerPrincipal})},
		{"expiry already over", member(sharing.MemberPut{AudienceID: f.team.ID, PrincipalID: f.dana.ID, ExpiresAt: f.clock.at(-time.Hour)})},
		{"a field the change does not have", approval.Filing{
			Kind: sharechange.KindName, Action: sharing.ActionMemberPut, Requester: f.admin, SubjectID: f.dana.ID,
			Payload: raw(map[string]any{"audience_id": f.team.ID, "principal_id": f.dana.ID, "role": "admin"}),
		}},
		{"a subject that is not the member", approval.Filing{
			Kind: sharechange.KindName, Action: sharing.ActionMemberPut, Requester: f.admin, SubjectID: f.alice.ID,
			Payload: raw(sharing.MemberPut{AudienceID: f.team.ID, PrincipalID: f.dana.ID}),
		}},
		{"an action the kind does not have", approval.Filing{
			Kind: sharechange.KindName, Action: "share.everything", Requester: f.admin, Payload: raw(map[string]any{}),
		}},
		{"unknown policy for a grant", func() approval.Filing {
			fl, err := sharechange.Filing(f.admin, sharing.GrantCreate{AudienceID: f.team.ID, PolicyID: "pol_nope"}, "")
			require.NoError(t, err)
			return fl
		}()},
		{"a key that is not the principal's", func() approval.Filing {
			fl, err := sharechange.Filing(f.admin, sharing.KeyPolicyBind{KeyID: "key_x", IdentityID: f.key.ID, PrincipalID: f.alice.ID}, "")
			require.NoError(t, err)
			return fl
		}()},
		{"the owner's key", func() approval.Filing {
			fl, err := sharechange.Filing(f.admin, sharing.KeyPolicyBind{KeyID: "key_x", IdentityID: f.key.ID, PrincipalID: storage.OwnerPrincipal}, "")
			require.NoError(t, err)
			return fl
		}()},
		{"a class that is not a class", func() approval.Filing {
			fl, err := sharechange.Filing(f.admin, sharing.CeilingSet{PrincipalID: f.dana.ID, DenyClasses: []string{"Bad!"}}, "")
			require.NoError(t, err)
			return fl
		}()},
	}
	before := f.snapshot()
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			_, err := f.appr.File(context.Background(), c.f)
			requireCode(t, err, approval.CodeInvalid)
		})
	}
	assert.Equal(t, before, f.snapshot())
	reqs, err := f.drv.Approvals().List(context.Background(), storage.ApprovalFilter{})
	require.NoError(t, err)
	assert.Empty(t, reqs, "nothing refused was filed")

	t.Run("a suspended principal", func(t *testing.T) {
		require.NoError(t, f.drv.Auth().Principals().Suspend(context.Background(), f.dana.ID))
		_, err := f.appr.File(context.Background(), member(sharing.MemberPut{AudienceID: f.team.ID, PrincipalID: f.dana.ID}))
		requireCode(t, err, approval.CodeInvalid)
	})
}

// The owner files nothing on the mechanism: its changes apply at once.
func TestFile_OwnerFilesNothing(t *testing.T) {
	f := newFx(t, sqliteDriver)
	fl, err := sharechange.Filing(f.owner, sharing.MemberPut{AudienceID: f.team.ID, PrincipalID: f.dana.ID}, "")
	require.NoError(t, err)
	_, err = f.appr.File(context.Background(), fl)
	requireCode(t, err, approval.CodeOwnerCannotRequest)
}

// The payload round-trips: every action decodes to the change that
// encoded it, and nothing but the change decodes.
func TestPayload_RoundTripsEveryAction(t *testing.T) {
	exp := time.Date(2026, 12, 1, 0, 0, 0, 0, time.UTC)
	changes := []sharing.Change{
		sharing.AudienceCreate{Name: "partners"},
		sharing.MemberPut{AudienceID: "aud_1", PrincipalID: "prn_1", ExpiresAt: &exp},
		sharing.MemberRemove{AudienceID: "aud_1", PrincipalID: "prn_1"},
		sharing.PolicyCreate{Policy: widerBase()},
		sharing.TemplateInstall{Name: sharing.TemplatePartners},
		sharing.PolicyUpdate{PolicyID: "pol_1", Policy: narrowerBase()},
		sharing.GrantCreate{AudienceID: "aud_1", PolicyID: "pol_1", ExpiresAt: &exp},
		sharing.GrantRevoke{GrantID: "grt_1"},
		sharing.CeilingSet{PrincipalID: "prn_1", DenyClasses: []string{"personal"}},
		sharing.KeyPolicyBind{KeyID: "key_1", IdentityID: "idn_1", PrincipalID: "prn_1", PolicyID: "pol_1"},
	}
	require.Len(t, changes, len(sharechange.Actions), "a change per action")
	for _, c := range changes {
		raw, err := sharechange.Encode(c)
		require.NoError(t, err)
		got, err := sharechange.Decode(c.Action(), raw)
		require.NoError(t, err, c.Action())
		assert.Equal(t, c, got)
		assert.Contains(t, sharechange.Actions, c.Action())

		_, err = sharechange.Decode(c.Action(), append(raw[:len(raw):len(raw)], []byte(` {}`)...))
		assert.Error(t, err, "trailing data after the change")
	}
	_, err := sharechange.Decode("share.nope", json.RawMessage(`{}`))
	assert.Error(t, err)
}
