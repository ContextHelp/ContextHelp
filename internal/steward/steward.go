// Package steward is the steward grant kind of the shared approval
// mechanism (ADR-083 §2): a record that gives one named principal a
// frozen share-policy scope for a bounded window, after approval by
// someone other than the requester. The kind validates and freezes a
// request (Kind), writes the grant row in the transaction of the final
// approval, and the grant half of the state machine ends a grant
// (Service.Revoke). Reading under a grant is internal/sharing's: the
// resolver's steward branch compiles the viewer.
package steward

import (
	"slices"
	"time"

	"github.com/ideacrafterslabs/ctxt/internal/auth"
	"github.com/ideacrafterslabs/ctxt/internal/config"
	"github.com/ideacrafterslabs/ctxt/internal/sharing"
)

// The kind's names on the approval mechanism, and the audit events it
// writes (each in the transaction of the transition it records).
const (
	// KindName is the approval kind's discriminator.
	KindName = "steward.grant"
	// ActionRequest is the one action of the kind.
	ActionRequest = "steward.grant.request"

	// EventGrantCreated: the final approval wrote a grant. The actor is
	// the requester; the payload names the request, the grant, the
	// steward, the approvers and the scope digest.
	EventGrantCreated = "steward.grant.created"
	// EventGrantRevoked: a grant was revoked. The actor is the revoker.
	EventGrantRevoked = "steward.grant.revoked"
)

// HoldsRequestSteward reports whether roles carry request:steward, the
// scope a steward holds. A role is a scope bundle and nothing else
// decides it, so an IdP-mapped role counts as the bundle it maps to.
func HoldsRequestSteward(roles []string) bool {
	return slices.Contains(auth.ScopesForRoles(roles), auth.ScopeRequestSteward)
}

// Settings maps the `steward:` config section to the settings the
// viewer branch and the kind read.
func Settings(c config.StewardConfig) sharing.StewardSettings {
	return sharing.StewardSettings{
		Enabled:         c.Enabled,
		MaxDuration:     c.EffectiveMaxDuration(),
		WithheldClasses: slices.Clone(c.WithheldClasses),
		AllowWithheld:   c.AllowWithheld,
		AllowExport:     c.AllowExport,
	}
}

// ResolverOptions wires a sharing.Resolver for steward grants over
// settings, which it reads on every request.
func ResolverOptions(settings func() sharing.StewardSettings) sharing.StewardOptions {
	return sharing.StewardOptions{Settings: settings, HoldsRequestSteward: HoldsRequestSteward}
}

// clockMS returns t in the stores' resolution: UTC milliseconds.
func clockMS(t time.Time) time.Time { return t.UTC().Truncate(time.Millisecond) }
