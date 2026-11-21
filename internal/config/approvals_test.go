package config

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// approvals.pending_ttl defaults to 72h, takes a duration from the
// config file, and refuses a negative one.
func TestApprovalsConfig_PendingTTL(t *testing.T) {
	cfg, err := Load("ctxt", "")
	require.NoError(t, err)
	assert.Equal(t, DefaultApprovalPendingTTL, cfg.Approvals.PendingTTL)
	assert.Equal(t, 72*time.Hour, cfg.Approvals.EffectivePendingTTL())

	cfg, err = loadFromYAML(t, "approvals:\n  pending_ttl: 24h\n")
	require.NoError(t, err)
	assert.Equal(t, 24*time.Hour, cfg.Approvals.EffectivePendingTTL())
	assert.Empty(t, cfg.Approvals.Validate())

	assert.Equal(t, DefaultApprovalPendingTTL, ApprovalsConfig{}.EffectivePendingTTL(), "0 is the default")
	errs := ApprovalsConfig{PendingTTL: -time.Hour}.Validate()
	require.Len(t, errs, 1)
	assert.Equal(t, "approvals.pending_ttl", errs[0].Field)
	all := Validate(&Config{Approvals: ApprovalsConfig{PendingTTL: -time.Hour}})
	found := false
	for _, e := range all {
		found = found || e.Field == "approvals.pending_ttl"
	}
	assert.True(t, found, "Config.Validate runs the approvals section: %v", all)
}
