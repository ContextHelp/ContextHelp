package postgres

import (
	"context"
	"errors"

	"github.com/ideacrafterslabs/ctxt/internal/storage"
)

// ErrNotImplemented is returned by Postgres stubs for features not yet ported.
var ErrNotImplemented = errors.New("not implemented for postgres")

// AttachmentStore is a stub — Postgres support not yet implemented.
type AttachmentStore struct{}

func (s *AttachmentStore) SaveAttachment(
	_ context.Context, _, _, _ string, _ []byte,
) (string, error) {
	return "", ErrNotImplemented
}

func (s *AttachmentStore) GetAttachment(_ context.Context, _ string) (*storage.Attachment, error) {
	return nil, ErrNotImplemented
}

func (s *AttachmentStore) ListAttachments(_ context.Context, _ string) ([]storage.Attachment, error) {
	return nil, ErrNotImplemented
}

func (s *AttachmentStore) DeleteAttachment(_ context.Context, _ string) error {
	return ErrNotImplemented
}
