package config

import (
	"fmt"
	"time"

	"github.com/ideacrafterslabs/ctxt/internal/storage"
)

// SharingConfig is the `sharing:` section (ADR-080).
type SharingConfig struct {
	// Classes configures the content classes assigned at ingest.
	Classes ClassesConfig `mapstructure:"classes" yaml:"classes"`
	// Audit configures the read audit of non-owner requests (§9).
	Audit AuditConfig `mapstructure:"audit" yaml:"audit"`
}

// DefaultAuditRetention is how long the read audit keeps the object id
// lists of its entries; the entries and their counts stay.
const DefaultAuditRetention = 90 * 24 * time.Hour

// AuditConfig configures the read audit (ADR-080 §9, decision D8).
type AuditConfig struct {
	// Retention is how long the object id lists of read audit entries
	// are kept before housekeeping prunes them; the entries and their
	// counts stay. A duration in hours (2160h is 90 days, the default).
	Retention time.Duration `mapstructure:"retention" yaml:"retention"`
}

// EffectiveRetention is Retention, or the default when unset.
func (a AuditConfig) EffectiveRetention() time.Duration {
	if a.Retention <= 0 {
		return DefaultAuditRetention
	}
	return a.Retention
}

// ClassesConfig configures the class rules (ADR-080 §3): evaluated by
// the content_classes step on protected and public instances, after
// structured metadata is extracted, and by `ctxt ingest`.
type ClassesConfig struct {
	// Rules are evaluated after the built-in ones; every rule that
	// matches adds its class.
	Rules []ClassRuleConfig `mapstructure:"rules" yaml:"rules,omitempty"`
	// NoDefaults drops the built-in rules (a person_note and the personal
	// profile are classed personal) and keeps only Rules.
	NoDefaults bool `mapstructure:"no_defaults" yaml:"no_defaults,omitempty"`
}

// ClassRuleConfig is one rule: every set matcher must hold for the
// rule's class to be added.
type ClassRuleConfig struct {
	// Class is the class added when the rule matches.
	Class string `mapstructure:"class" yaml:"class"`
	// Type, Subtype, Source, Pipeline, Tag and Profile match the object's
	// own fields; Pipeline matches with or without its @vN suffix, Tag
	// any of the object's tag labels.
	Type     string `mapstructure:"type" yaml:"type,omitempty"`
	Subtype  string `mapstructure:"subtype" yaml:"subtype,omitempty"`
	Source   string `mapstructure:"source" yaml:"source,omitempty"`
	Pipeline string `mapstructure:"pipeline" yaml:"pipeline,omitempty"`
	Tag      string `mapstructure:"tag" yaml:"tag,omitempty"`
	Profile  string `mapstructure:"profile" yaml:"profile,omitempty"`
	// StructuredType matches the type the structured_metadata step
	// assigned (person_note, idea, ...).
	StructuredType string `mapstructure:"structured_type" yaml:"structured_type,omitempty"`
}

// hasMatcher reports whether the rule constrains anything.
func (r ClassRuleConfig) hasMatcher() bool {
	return r.Type != "" || r.Subtype != "" || r.Source != "" || r.Pipeline != "" ||
		r.Tag != "" || r.Profile != "" || r.StructuredType != ""
}

// Validate checks every rule names a valid class and at least one
// matcher.
func (c SharingConfig) Validate() []ValidationError {
	var errs []ValidationError
	if c.Audit.Retention < 0 {
		errs = append(errs, ValidationError{Field: "sharing.audit.retention", Message: fmt.Sprintf("must be a positive duration or 0 for the default (%s); got %s", DefaultAuditRetention, c.Audit.Retention)})
	}
	for i, r := range c.Classes.Rules {
		field := fmt.Sprintf("sharing.classes.rules[%d]", i)
		if err := storage.ValidateClassName(r.Class); err != nil {
			errs = append(errs, ValidationError{Field: field + ".class", Message: err.Error()})
		}
		if !r.hasMatcher() {
			errs = append(errs, ValidationError{Field: field, Message: "set at least one of type, subtype, source, pipeline, tag, profile, structured_type"})
		}
	}
	return errs
}
