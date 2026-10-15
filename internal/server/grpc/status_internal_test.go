package grpc

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/ideacrafterslabs/ctxt/internal/search"
	"github.com/ideacrafterslabs/ctxt/internal/service"
	"github.com/ideacrafterslabs/ctxt/internal/sharing"
	"github.com/ideacrafterslabs/ctxt/internal/storage"
)

func restrictedRefusal() error {
	return fmt.Errorf("Jobs.Get: %w", storage.ErrRestrictedViewer)
}

// toStatus is the one mapping from a service error to a status code;
// this table is the contract every handler answers by.
func TestToStatusMapsServiceErrors(t *testing.T) {
	none := context.Background()
	restricted := sharing.WithViewer(none, sharing.Viewer{})
	owner := sharing.WithViewer(none, sharing.Unrestricted)

	cases := []struct {
		name string
		ctx  context.Context
		err  error
		want codes.Code
	}{
		{"not found", none, fmt.Errorf("entity %w", storage.ErrNotFound), codes.NotFound},
		{"restricted viewer refused", restricted, restrictedRefusal(), codes.NotFound},
		{"owner refused", owner, restrictedRefusal(), codes.Internal},
		{"no viewer refused", none, restrictedRefusal(), codes.Internal},
		{"invalid query", none, fmt.Errorf("x: %w", search.ErrInvalidQuery), codes.InvalidArgument},
		{"unknown pipeline", none, fmt.Errorf("analyze: %w", service.ErrPipelineNotFound), codes.InvalidArgument},
		{"canceled", none, fmt.Errorf("get: %w", context.Canceled), codes.Canceled},
		{"deadline", none, fmt.Errorf("get: %w", context.DeadlineExceeded), codes.DeadlineExceeded},
		{"status passthrough", none, status.Error(codes.PermissionDenied, "nope"), codes.PermissionDenied},
		{"other", none, errors.New("disk on fire"), codes.Internal},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := toStatus(tc.ctx, "job", tc.err)
			if code := status.Code(got); code != tc.want {
				t.Fatalf("code = %v, want %v (err: %v)", code, tc.want, got)
			}
		})
	}
	if err := toStatus(none, "job", nil); err != nil {
		t.Fatalf("nil error mapped to %v", err)
	}
}

// A hidden row and a missing row answer the same bytes: a different
// message would confirm the row exists (ADR-080 §4).
func TestToStatusHiddenRowReadsAsMissing(t *testing.T) {
	viewer := sharing.WithViewer(context.Background(), sharing.Viewer{})
	missing := toStatus(viewer, "job", fmt.Errorf("job %w", storage.ErrNotFound))
	hidden := toStatus(viewer, "job", restrictedRefusal())
	if missing.Error() != hidden.Error() {
		t.Fatalf("hidden = %q, missing = %q; want identical", hidden, missing)
	}
	if hidden.Error() != notFound("job").Error() {
		t.Fatalf("hidden = %q, nil-row answer = %q; want identical", hidden, notFound("job"))
	}
}

// Without a viewer on the context the refusal is a transport bug: the
// status names it, so a forgotten attach cannot pass for a missing row.
func TestToStatusNamesRefusalWithoutViewer(t *testing.T) {
	err := toStatus(context.Background(), "job", restrictedRefusal())
	if !strings.Contains(err.Error(), storage.ErrRestrictedViewer.Error()) {
		t.Fatalf("err = %v, want the refusal named", err)
	}
}

// InvalidArgument carries the query's own text, as HTTP's 400 body does.
func TestToStatusInvalidArgumentKeepsQueryText(t *testing.T) {
	cause := fmt.Errorf("parse error: expected ')' at position 12: %w", search.ErrInvalidQuery)
	err := toStatus(context.Background(), "objects", cause)
	if status.Convert(err).Message() != cause.Error() {
		t.Fatalf("message = %q, want %q", status.Convert(err).Message(), cause.Error())
	}
}
