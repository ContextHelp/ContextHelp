// Package sharechange wires the sharing registry to the approval
// mechanism (ADR-083 §10 B): the share.change kind, the payload a
// widening change by a non-owner administrator is frozen as, and the
// one Apply that writes a change, whether a route applies it at once
// or the last approval applies it from a request.
package sharechange

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"

	"github.com/ideacrafterslabs/ctxt/internal/sharing"
)

// Actions lists every action the share.change kind files: one per
// mutating sharing route of ADR-083 §10 B. A change the classifier
// cannot show to be neutral or narrowing is filed whatever its action,
// so the kind accepts all of them.
var Actions = []string{
	sharing.ActionAudienceCreate,
	sharing.ActionMemberPut,
	sharing.ActionMemberRemove,
	sharing.ActionPolicyCreate,
	sharing.ActionTemplateInstall,
	sharing.ActionPolicyUpdate,
	sharing.ActionGrantCreate,
	sharing.ActionGrantRevoke,
	sharing.ActionCeilingSet,
	sharing.ActionKeyPolicyBind,
}

// newChange returns the zero change of action.
func newChange(action string) (sharing.Change, error) {
	switch action {
	case sharing.ActionAudienceCreate:
		return &sharing.AudienceCreate{}, nil
	case sharing.ActionMemberPut:
		return &sharing.MemberPut{}, nil
	case sharing.ActionMemberRemove:
		return &sharing.MemberRemove{}, nil
	case sharing.ActionPolicyCreate:
		return &sharing.PolicyCreate{}, nil
	case sharing.ActionTemplateInstall:
		return &sharing.TemplateInstall{}, nil
	case sharing.ActionPolicyUpdate:
		return &sharing.PolicyUpdate{}, nil
	case sharing.ActionGrantCreate:
		return &sharing.GrantCreate{}, nil
	case sharing.ActionGrantRevoke:
		return &sharing.GrantRevoke{}, nil
	case sharing.ActionCeilingSet:
		return &sharing.CeilingSet{}, nil
	case sharing.ActionKeyPolicyBind:
		return &sharing.KeyPolicyBind{}, nil
	}
	return nil, fmt.Errorf("share change: unknown action %q", action)
}

// Encode returns the payload of c: its JSON form, the route's request
// body resolved to ids.
func Encode(c sharing.Change) (json.RawMessage, error) {
	if c == nil {
		return nil, errors.New("share change: nil change")
	}
	b, err := json.Marshal(c)
	if err != nil {
		return nil, fmt.Errorf("share change %s: encode: %w", c.Action(), err)
	}
	return b, nil
}

// Decode returns the change of action that payload encodes. Unknown
// fields and trailing data are refused: a frozen payload says exactly
// what it will do.
func Decode(action string, payload json.RawMessage) (sharing.Change, error) {
	ptr, err := newChange(action)
	if err != nil {
		return nil, err
	}
	dec := json.NewDecoder(bytes.NewReader(payload))
	dec.DisallowUnknownFields()
	if err := dec.Decode(ptr); err != nil {
		return nil, fmt.Errorf("share change %s: payload: %w", action, err)
	}
	if _, err := dec.Token(); !errors.Is(err, io.EOF) {
		return nil, fmt.Errorf("share change %s: payload: trailing data after the change", action)
	}
	return deref(ptr), nil
}

// deref returns the change a pointer from newChange points to: the
// classifier switches on value types.
func deref(c sharing.Change) sharing.Change {
	switch c := c.(type) {
	case *sharing.AudienceCreate:
		return *c
	case *sharing.MemberPut:
		return *c
	case *sharing.MemberRemove:
		return *c
	case *sharing.PolicyCreate:
		return *c
	case *sharing.TemplateInstall:
		return *c
	case *sharing.PolicyUpdate:
		return *c
	case *sharing.GrantCreate:
		return *c
	case *sharing.GrantRevoke:
		return *c
	case *sharing.CeilingSet:
		return *c
	case *sharing.KeyPolicyBind:
		return *c
	}
	return c
}

// SubjectOf returns the principal a change is about, when it is about
// one: the member an audience gains or keeps, the principal whose
// ceiling moves, the principal whose key is rebound. The approval
// request names it as its subject, so it sees the request and may not
// decide it. "" for a change about no single principal.
func SubjectOf(c sharing.Change) string {
	switch c := c.(type) {
	case sharing.MemberPut:
		return c.PrincipalID
	case sharing.CeilingSet:
		return c.PrincipalID
	case sharing.KeyPolicyBind:
		return c.PrincipalID
	}
	return ""
}
