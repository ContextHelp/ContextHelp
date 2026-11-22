package sharing_test

import (
	"context"
	"database/sql"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ideacrafterslabs/ctxt/internal/sharing"
	"github.com/ideacrafterslabs/ctxt/internal/storage"
)

// refusedAs resolves the steward's elevation under g through identity
// and asserts the refusal: its reason, whether it is deferred, and the
// zero viewer in its place.
func (f *stewardFix) refusedAs(g *storage.StewardGrant, identity, wantReason string, deferred bool) *sharing.StewardRefusal {
	f.t.Helper()
	v, err := f.resolve(f.person.ID, g, identity)
	var refusal *sharing.StewardRefusal
	require.True(f.t, errors.As(err, &refusal), "want a steward refusal, got viewer %q and error %v", v.PolicyKey(), err)
	assert.Equal(f.t, wantReason, refusal.Reason)
	assert.Equal(f.t, deferred, refusal.Deferred, "deferred only when the cause is unavailability")
	assert.True(f.t, v.MatchesNothing(), "a refused elevation never falls back to a view")
	return refusal
}

// An elevated request through an identity the registry does not know
// is an answer, not unavailability: denied, as the ordinary path's
// unknown identity sees nothing.
func TestStewardUnknownIdentityIsDenied(t *testing.T) {
	f := newStewardFix(t)
	g := f.open(f.person.ID, familyScope)
	f.refusedAs(g, "idn_nothing", sharing.StewardReasonIdentityUnknown, false)
	_, err := f.resolve(f.person.ID, g, f.key.ID)
	require.NoError(t, err, "a known identity passes")
}

// A key policy that cannot be used (its stored spec no longer parses as
// a policy) is denied with its own code, not taken for a scope fault or
// for unavailability.
func TestStewardInvalidKeyPolicyIsDenied(t *testing.T) {
	f := newStewardFix(t)
	g := f.open(f.person.ID, familyScope)
	row := f.policies[sharing.TemplateColleagues]
	require.NoError(t, f.st.BindIdentityPolicy(context.Background(), f.key.ID, row.ID))
	_, err := f.resolve(f.person.ID, g, f.key.ID)
	require.NoError(t, err)

	db := f.drv.(interface{ DB() *sql.DB }).DB()
	_, err = db.ExecContext(context.Background(), `UPDATE share_policies SET spec = ? WHERE id = ?`, `{"data":"mention==x"}`, row.ID)
	require.NoError(t, err)
	refusal := f.refusedAs(g, f.key.ID, sharing.StewardReasonKeyPolicyInvalid, false)
	assert.Equal(t, "STEWARD_KEY_POLICY_INVALID", refusal.Code())
	_, err = f.resolve(f.person.ID, g, "")
	require.NoError(t, err, "without the credential no key policy applies")
}
