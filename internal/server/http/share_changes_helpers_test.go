package http

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/ideacrafterslabs/ctxt/internal/approval"
	"github.com/ideacrafterslabs/ctxt/internal/sharechange"
	"github.com/ideacrafterslabs/ctxt/internal/storage"
)

// newShareApprovals builds the approval mechanism of a test router over
// store: the share.change kind registered, requests pending for 72
// hours of now.
func newShareApprovals(t *testing.T, store storage.StorageDriver, now func() time.Time) *approval.Service {
	t.Helper()
	reg := approval.NewRegistry()
	require.NoError(t, sharechange.Register(reg, sharechange.Deps{
		Sharing: store.Sharing(), Auth: store.Auth(), Audit: store.AuditLog(), Now: now,
	}))
	svc, err := approval.NewService(store.Approvals(), store.AuditLog(), reg, approval.Options{PendingTTL: 72 * time.Hour, Now: now})
	require.NoError(t, err)
	return svc
}
