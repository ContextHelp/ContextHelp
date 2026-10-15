package sharing

import (
	"context"
	"errors"

	"github.com/ideacrafterslabs/ctxt/internal/storage"
)

// The owner previews another viewer's view (ADR-080 §2, owner bypass):
// a request carrying HeaderViewAs (MetadataViewAs over gRPC) with a
// principal id or a share policy name runs through that viewer, render,
// backstop and audit included, and is audited as the owner's preview.
// Only the owner may send it; a read route only.
const (
	// HeaderViewAs is the HTTP request header.
	HeaderViewAs = "X-Ctxt-View-As"
	// MetadataViewAs is the gRPC metadata key.
	MetadataViewAs = "x-ctxt-view-as"
)

// ErrViewAsUnknown is returned by Resolver.ViewAs when target names no
// principal and no share policy.
var ErrViewAsUnknown = errors.New("view-as: no principal or share policy by that name")

// WithRegistry gives the resolver the principal registry ViewAs looks
// principals up in.
func (r *Resolver) WithRegistry(reg storage.AuthStore) *Resolver {
	if r != nil {
		r.registry = reg
	}
	return r
}

// ViewAs returns the viewer of target: the viewer of the principal
// with that id (Unrestricted for the owner), else a viewer holding the
// share policy of that name as its only grant, compiled for the owner.
// A target that is neither is ErrViewAsUnknown.
func (r *Resolver) ViewAs(ctx context.Context, target string) (Viewer, error) {
	if r == nil || r.store == nil || target == "" {
		return Viewer{}, ErrViewAsUnknown
	}
	if r.registry != nil {
		p, err := r.registry.Principals().Get(ctx, target)
		switch {
		case err == nil:
			return r.Resolve(ctx, p.ID, p.Kind, "")
		case !errors.Is(err, storage.ErrNotFound):
			return Viewer{}, err
		}
	}
	row, err := r.store.Policies().GetByName(ctx, target)
	if errors.Is(err, storage.ErrNotFound) {
		return Viewer{}, ErrViewAsUnknown
	}
	if err != nil {
		return Viewer{}, err
	}
	p, err := PolicyFromSpec(row)
	if err != nil {
		return Viewer{}, err
	}
	return Compile(Input{
		PrincipalID: storage.OwnerPrincipal,
		Grants:      []Grant{{ID: "preview:" + p.Name, Audience: "preview", Policy: p}},
	})
}
