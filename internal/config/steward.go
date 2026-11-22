package config

import (
	"fmt"
	"time"

	"github.com/ideacrafterslabs/ctxt/internal/storage"
)

// Defaults of the `steward:` section (ADR-083 §2).
const (
	// DefaultStewardMaxDuration is the hard cap on a steward grant's
	// window when steward.max_duration is unset: 14 days.
	DefaultStewardMaxDuration = 14 * 24 * time.Hour
	// DefaultStewardMinReasonChars is the shortest reason a steward
	// grant request may give when steward.min_reason_chars is unset.
	DefaultStewardMinReasonChars = 30
	// DefaultStewardElevationIdle ends a browser session's steward
	// elevation this long after its last request when
	// steward.elevation_idle is unset: 15 minutes.
	DefaultStewardElevationIdle = 15 * time.Minute
)

// DefaultStewardWithheldClasses are the classes withheld from every
// steward scope when steward.withheld_classes is unset.
var DefaultStewardWithheldClasses = []string{"personal"}

// StewardConfig is the `steward:` section: steward access for business
// instances (ADR-083). The approver count lives with the approval
// mechanism's settings (approvals.steward.required).
type StewardConfig struct {
	// Enabled turns the feature on. Off (the default): no steward.grant
	// request can be filed and no grant is used for reading.
	Enabled bool `mapstructure:"enabled" yaml:"enabled"`
	// MaxDuration is the hard cap on a grant's window, checked when a
	// request is filed, when it is approved and on every use: lowering
	// it shortens the grants already live. A duration (14 days by
	// default).
	MaxDuration time.Duration `mapstructure:"max_duration" yaml:"max_duration"`
	// MinReasonChars is the shortest reason a request may give, in
	// characters (30 by default).
	MinReasonChars int `mapstructure:"min_reason_chars" yaml:"min_reason_chars"`
	// ElevationIdle ends a browser session's elevation into a grant this
	// long after the elevation's last request; the steward elevates
	// again. The session itself lives on. A duration (15 minutes by
	// default).
	ElevationIdle time.Duration `mapstructure:"elevation_idle" yaml:"elevation_idle"`
	// WithheldClasses are removed from every steward scope at compile
	// time (personal by default). An explicit empty list withholds
	// nothing.
	WithheldClasses []string `mapstructure:"withheld_classes" yaml:"withheld_classes"`
	// AllowWithheld lets a grant name a withheld class in
	// include_classes and so receive it. Off by default.
	AllowWithheld bool `mapstructure:"allow_withheld" yaml:"allow_withheld"`
	// AllowExport lets a scope open the export capability. Off by
	// default.
	AllowExport bool `mapstructure:"allow_export" yaml:"allow_export"`
}

// EffectiveMaxDuration is MaxDuration, or the default when unset.
func (s StewardConfig) EffectiveMaxDuration() time.Duration {
	if s.MaxDuration <= 0 {
		return DefaultStewardMaxDuration
	}
	return s.MaxDuration
}

// EffectiveMinReasonChars is MinReasonChars, or the default when unset.
func (s StewardConfig) EffectiveMinReasonChars() int {
	if s.MinReasonChars <= 0 {
		return DefaultStewardMinReasonChars
	}
	return s.MinReasonChars
}

// EffectiveElevationIdle is ElevationIdle, or the default when unset.
func (s StewardConfig) EffectiveElevationIdle() time.Duration {
	if s.ElevationIdle <= 0 {
		return DefaultStewardElevationIdle
	}
	return s.ElevationIdle
}

// Validate refuses a negative duration or length and a withheld class
// that is not a class name.
func (s StewardConfig) Validate() []ValidationError {
	var errs []ValidationError
	if s.MaxDuration < 0 {
		errs = append(errs, ValidationError{Field: "steward.max_duration", Message: fmt.Sprintf("must be a positive duration or 0 for the default (%s); got %s", DefaultStewardMaxDuration, s.MaxDuration)})
	}
	if s.ElevationIdle < 0 {
		errs = append(errs, ValidationError{Field: "steward.elevation_idle", Message: fmt.Sprintf("must be a positive duration or 0 for the default (%s); got %s", DefaultStewardElevationIdle, s.ElevationIdle)})
	}
	if s.MinReasonChars < 0 {
		errs = append(errs, ValidationError{Field: "steward.min_reason_chars", Message: fmt.Sprintf("must be a positive count or 0 for the default (%d); got %d", DefaultStewardMinReasonChars, s.MinReasonChars)})
	}
	for i, c := range s.WithheldClasses {
		if err := storage.ValidateClassName(c); err != nil {
			errs = append(errs, ValidationError{Field: fmt.Sprintf("steward.withheld_classes[%d]", i), Message: err.Error()})
		}
	}
	return errs
}
