package steward_test

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ideacrafterslabs/ctxt/internal/approval"
	"github.com/ideacrafterslabs/ctxt/internal/auth"
	"github.com/ideacrafterslabs/ctxt/internal/config"
	"github.com/ideacrafterslabs/ctxt/internal/sharing"
	"github.com/ideacrafterslabs/ctxt/internal/steward"
)

// internal/sharing cannot import internal/approval, so it carries its
// own canonical JSON; this holds the two to one digest.
func TestScopeDigestMatchesTheApprovalPayloadDigest(t *testing.T) {
	for _, raw := range []string{
		`{"data":"type==contact"}`,
		"{ \"fields\" : {\"contact\":[\"title\",\"org\"]},\n \"data\":\"tag==a&b<c>\" }",
		`{"data":"x","n":1.0,"m":1e3,"s":"éé \"quoted\"","nested":{"b":[3,2,1],"a":null}}`,
	} {
		canon, err := approval.Canonicalize(json.RawMessage(raw))
		require.NoError(t, err)
		got, err := sharing.StewardScopeDigest(json.RawMessage(raw))
		require.NoError(t, err)
		assert.Equal(t, approval.Digest(canon), got, raw)
	}
}

// The config maps to the settings the server reads, and the resolver
// wiring expands roles into the request:steward scope.
func TestSettingsFromConfig(t *testing.T) {
	cfg := config.StewardConfig{Enabled: true, WithheldClasses: []string{"personal", "financial"}, AllowWithheld: true, AllowExport: true}
	st := steward.Settings(cfg)
	assert.True(t, st.Enabled && st.AllowWithheld && st.AllowExport)
	assert.Equal(t, config.DefaultStewardMaxDuration, st.MaxDuration, "an unset cap is the default")
	assert.Equal(t, []string{"personal", "financial"}, st.WithheldClasses)

	opts := steward.ResolverOptions(func() sharing.StewardSettings { return steward.Settings(cfg) })
	assert.Equal(t, st, opts.Settings())
	assert.True(t, opts.HoldsRequestSteward([]string{auth.RoleReader, auth.RoleStewardRequester}))
	assert.True(t, opts.HoldsRequestSteward(auth.OwnerRoles), "the owner holds it explicitly")
	assert.False(t, opts.HoldsRequestSteward([]string{auth.RoleAdmin}), "admin does not imply it")
	assert.False(t, opts.HoldsRequestSteward([]string{auth.RoleStewardApprover, auth.RoleWriter, auth.RoleReader}))
	assert.False(t, opts.HoldsRequestSteward(nil))
}
