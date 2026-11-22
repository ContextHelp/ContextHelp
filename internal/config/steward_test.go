package config

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The steward settings (ADR-083 §2) default to feature off, a 14-day
// cap, a 30-character reason, a 15-minute elevation idle, `personal`
// withheld, nothing exportable and one approver.
func TestStewardConfigDefaults(t *testing.T) {
	cfg, err := Load("ctxt", "")
	require.NoError(t, err)
	s := cfg.Steward
	assert.False(t, s.Enabled)
	assert.Equal(t, 14*24*time.Hour, s.EffectiveMaxDuration())
	assert.Equal(t, 30, s.EffectiveMinReasonChars())
	assert.Equal(t, 15*time.Minute, s.EffectiveElevationIdle())
	assert.Equal(t, []string{"personal"}, s.WithheldClasses)
	assert.False(t, s.AllowWithheld)
	assert.False(t, s.AllowExport)
	assert.Equal(t, 1, cfg.Approvals.Steward.EffectiveRequired())
	assert.Empty(t, Validate(cfg), "the defaults validate")
}

// Every setting reads from the config file; an explicit empty
// withheld list withholds nothing, which is not the default.
func TestStewardConfigFromFile(t *testing.T) {
	cfg, err := loadFromYAML(t, `
steward:
  enabled: true
  max_duration: 72h
  min_reason_chars: 12
  elevation_idle: 5m
  withheld_classes: [personal, financial]
  allow_withheld: true
  allow_export: true
approvals:
  steward:
    required: 2
`)
	require.NoError(t, err)
	s := cfg.Steward
	assert.True(t, s.Enabled)
	assert.Equal(t, 72*time.Hour, s.EffectiveMaxDuration())
	assert.Equal(t, 12, s.EffectiveMinReasonChars())
	assert.Equal(t, 5*time.Minute, s.EffectiveElevationIdle())
	assert.Equal(t, []string{"personal", "financial"}, s.WithheldClasses)
	assert.True(t, s.AllowWithheld)
	assert.True(t, s.AllowExport)
	assert.Equal(t, 2, cfg.Approvals.Steward.EffectiveRequired())
	assert.Empty(t, Validate(cfg))

	none, err := loadFromYAML(t, "steward:\n  withheld_classes: []\n")
	require.NoError(t, err)
	assert.Empty(t, none.Steward.WithheldClasses, "an explicit empty list withholds nothing")
	// A pending TTL set beside the steward settings keeps its own.
	assert.Equal(t, DefaultApprovalPendingTTL, none.Approvals.PendingTTL)
}

// Unset and zero take the defaults; a negative duration, length or
// approver count, or a bad class name, is a validation error naming
// the key.
func TestStewardConfigValidate(t *testing.T) {
	assert.Equal(t, DefaultStewardMaxDuration, StewardConfig{}.EffectiveMaxDuration())
	assert.Equal(t, DefaultStewardMinReasonChars, StewardConfig{}.EffectiveMinReasonChars())
	assert.Equal(t, DefaultStewardElevationIdle, StewardConfig{}.EffectiveElevationIdle())
	assert.Equal(t, 1, ApprovalKindConfig{}.EffectiveRequired())
	assert.Equal(t, 3, ApprovalKindConfig{Required: 3}.EffectiveRequired())

	fields := func(c *Config) []string {
		var out []string
		for _, e := range Validate(c) {
			out = append(out, e.Field)
		}
		return out
	}
	for name, c := range map[string]struct {
		cfg   Config
		field string
	}{
		"negative max duration":   {Config{Steward: StewardConfig{MaxDuration: -time.Hour}}, "steward.max_duration"},
		"negative reason length":  {Config{Steward: StewardConfig{MinReasonChars: -1}}, "steward.min_reason_chars"},
		"negative elevation idle": {Config{Steward: StewardConfig{ElevationIdle: -time.Minute}}, "steward.elevation_idle"},
		"bad withheld class":      {Config{Steward: StewardConfig{WithheldClasses: []string{"Not A Class"}}}, "steward.withheld_classes[0]"},
		"negative approvers":      {Config{Approvals: ApprovalsConfig{Steward: ApprovalKindConfig{Required: -1}}}, "approvals.steward.required"},
	} {
		cfg := c.cfg
		assert.Contains(t, fields(&cfg), c.field, name)
	}
	ok := Config{Steward: StewardConfig{Enabled: true, MaxDuration: time.Hour, WithheldClasses: []string{"personal"}}}
	assert.NotContains(t, fields(&ok), "steward.max_duration")
}
