package grpc

import (
	"context"
	"errors"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/ideacrafterslabs/ctxt/internal/search"
	"github.com/ideacrafterslabs/ctxt/internal/service"
	"github.com/ideacrafterslabs/ctxt/internal/sharing"
	"github.com/ideacrafterslabs/ctxt/internal/storage"
)

// toStatus is the one place a service error becomes a gRPC status, so
// every handler in this package answers alike, and like HTTP answers the
// same input. what names the resource for the message: "job", "entity",
// "object", "objects".
//
//   - storage.ErrNotFound: NotFound, "<what> not found" (HTTP 404).
//   - storage.ErrRestrictedViewer under a restricted viewer: the same
//     NotFound, byte for byte. A row the viewer may not read is answered
//     as if it did not exist (ADR-080 §4, hidden means absent); another
//     code or message would confirm the row is there. The same refusal
//     with no viewer on ctx, or under the owner, is a transport or
//     storage bug, not a visibility answer: it stays Internal and names
//     the refusal, so a forgotten attach is loud instead of passing for
//     a missing row.
//   - search.ErrInvalidQuery, service.ErrPipelineNotFound:
//     InvalidArgument with the error's own text (HTTP 400 / 422).
//   - context.Canceled, context.DeadlineExceeded: Canceled,
//     DeadlineExceeded.
//   - anything else: Internal, "<what>: <err>", the text HTTP's
//     INTERNAL_ERROR body already carries.
//
// A status error passes through unchanged; nil stays nil.
func toStatus(ctx context.Context, what string, err error) error {
	switch {
	case err == nil:
		return nil
	case isStatus(err):
		return err
	case errors.Is(err, storage.ErrNotFound):
		return notFound(what)
	case errors.Is(err, storage.ErrRestrictedViewer):
		if v, ok := sharing.FromContext(ctx); ok && !v.IsUnrestricted() {
			return notFound(what)
		}
	case errors.Is(err, search.ErrInvalidQuery), errors.Is(err, service.ErrPipelineNotFound):
		return status.Error(codes.InvalidArgument, err.Error())
	case errors.Is(err, context.Canceled):
		return status.Errorf(codes.Canceled, "%s: %v", what, err)
	case errors.Is(err, context.DeadlineExceeded):
		return status.Errorf(codes.DeadlineExceeded, "%s: %v", what, err)
	}
	return status.Errorf(codes.Internal, "%s: %v", what, err)
}

// notFound is the one NotFound a handler answers, for a row that is
// missing and for a row the viewer may not see alike.
func notFound(what string) error {
	return status.Errorf(codes.NotFound, "%s not found", what)
}

// isStatus reports whether err already carries a gRPC status.
func isStatus(err error) bool {
	_, ok := status.FromError(err)
	return ok
}
