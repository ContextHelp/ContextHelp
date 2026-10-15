package stack

import (
	"context"
	"io"

	authn "github.com/ideacrafterslabs/ctxt/internal/auth"
	"github.com/ideacrafterslabs/ctxt/internal/config"
)

// wireAuth sets the stack's authentication pieces for access: the
// chain, keys, session manager and ticket ceremonies of a non-private
// instance (buildAuth), or, on a private one, keys alone, since its
// owner still manages keys and principals over the API ahead of
// promoting it.
func (s *Stack) wireAuth(in Inputs, access string, notes io.Writer) error {
	cfg := in.Config
	if access == config.AccessPrivate {
		s.Keys = authn.NewKeysOver(in.Driver.Auth(), nil)
		return nil
	}
	chain, keys, sessions, err := buildAuth(context.Background(), notes, in.Driver, cfg, access)
	if err != nil {
		return err
	}
	s.RouteAuth, s.Keys, s.UISessions = chain, keys, sessions
	if in.Auth != nil {
		s.RouteAuth = in.Auth
	}
	// Invite and sign-in links need a session to end in and a ceremony
	// server.auth.sign_in allows.
	if sessions != nil && len(cfg.Server.Auth.EffectiveSignIn()) > 0 {
		if s.Tickets, err = authn.NewTickets(in.Driver.Auth(), sessions, nil); err != nil {
			return err
		}
	}
	return nil
}
