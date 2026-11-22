package steward

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/ideacrafterslabs/ctxt/internal/sharing"
	"github.com/ideacrafterslabs/ctxt/internal/storage"
)

// audienceState is what an as_audience scope depends on: which grants
// give the audience which policy at which version, in force now. Any
// edit, new grant, revocation or expiry changes it, so a pending
// request compiled from the audience goes stale rather than be
// approved against a different audience than the approvers saw.
type audienceState struct {
	Name string `json:"name"`
	// Missing is true when no audience of the name exists.
	Missing bool            `json:"missing,omitempty"`
	Grants  []audienceGrant `json:"grants"`
}

// audienceGrant is one share grant of the audience, with its policy's
// version.
type audienceGrant struct {
	Grant   string `json:"grant"`
	Policy  string `json:"policy"`
	Version int    `json:"version"`
}

// audienceView is an audience's state and the policies it grants.
type audienceView struct {
	audienceState
	policies []sharing.Policy
}

// audience reads the audience named name as of now: the policies its
// grants in force give it. An audience that does not exist is a view
// with Missing set.
func (k *kind) audience(ctx context.Context, name string, now time.Time) (audienceView, error) {
	view := audienceView{audienceState: audienceState{Name: name, Grants: []audienceGrant{}}}
	a, err := k.o.Sharing.Audiences().GetByName(ctx, name)
	if errors.Is(err, storage.ErrNotFound) {
		view.Missing = true
		return view, nil
	}
	if err != nil {
		return view, fmt.Errorf("audience %q: %w", name, err)
	}
	grants, err := k.o.Sharing.Grants().List(ctx, storage.GrantFilter{AudienceID: a.ID})
	if err != nil {
		return view, fmt.Errorf("audience %q grants: %w", name, err)
	}
	for _, g := range grants {
		if !g.ActiveAt(now) {
			continue
		}
		row, err := k.o.Sharing.Policies().Get(ctx, g.PolicyID)
		if err != nil {
			return view, fmt.Errorf("audience %q policy %s: %w", name, g.PolicyID, err)
		}
		view.Grants = append(view.Grants, audienceGrant{Grant: g.ID, Policy: row.ID, Version: row.Version})
		pol, err := sharing.PolicyFromSpec(row)
		if err != nil {
			return view, refuse(CodeScopeInvalid, "audience %q: %v", name, err)
		}
		view.policies = append(view.policies, pol)
	}
	slices.SortFunc(view.Grants, func(a, b audienceGrant) int { return strings.Compare(a.Grant, b.Grant) })
	return view, nil
}
