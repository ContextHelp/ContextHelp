package http_test

import (
	"context"

	"github.com/ideacrafterslabs/ctxt/internal/sharing"
)

// ownerCtx is the context the external tests act under when they call
// the service or its store directly: the owner's, unrestricted.
func ownerCtx() context.Context {
	return sharing.WithViewer(context.Background(), sharing.Unrestricted)
}
