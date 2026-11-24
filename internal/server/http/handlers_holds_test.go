package http_test

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/ideacrafterslabs/ctxt/internal/dpkmstest"
	"github.com/ideacrafterslabs/ctxt/internal/storage"
	"github.com/ideacrafterslabs/ctxt/internal/storageutil"
)

func TestHoldsHTTPAPIAndEnforcement(t *testing.T) {
	driver := storageutil.NewTestDriver(t)
	srv := dpkmstest.Start(t, driver, dpkmstest.WithKeys())

	require.NoError(t, driver.Objects().Create(ownerCtx(), &storage.KnowledgeObject{
		ID: "o-hold-1", Type: "note", Status: "active",
		RawContent: "preserved text", TextContent: "preserved text",
		CreatedAt: writeSeedTime, UpdatedAt: writeSeedTime,
	}))

	// Reject empty reason.
	resp, body := sendJSON(t, srv, http.MethodPost, "/api/v1/holds", dpkmstest.RoleAdmin, map[string]any{
		"object_id": "o-hold-1", "hold_id": "h-1", "reason": "",
	})
	require.Equal(t, http.StatusBadRequest, resp.StatusCode, string(body))

	// Place bos-owned hold.
	resp, body = sendJSON(t, srv, http.MethodPost, "/api/v1/holds", dpkmstest.RoleAdmin, map[string]any{
		"object_id": "o-hold-1", "hold_id": "h-bos-1", "source": "bos", "reason": "litigation hold",
	})
	require.Equal(t, http.StatusCreated, resp.StatusCode, string(body))

	// List active holds (admin-only).
	resp, body = sendJSON(t, srv, http.MethodGet, "/api/v1/holds?object_id=o-hold-1", dpkmstest.RoleAdmin, nil)
	require.Equal(t, http.StatusOK, resp.StatusCode, string(body))
	var listResp struct {
		Total int `json:"total"`
	}
	require.NoError(t, json.Unmarshal(body, &listResp))
	require.Equal(t, 1, listResp.Total)

	// PATCH and DELETE are refused with 409 hold_in_force.
	resp, body = sendJSON(t, srv, http.MethodPatch, "/api/v1/objects/o-hold-1", dpkmstest.RoleAdmin, map[string]any{"type": "edited"})
	require.Equal(t, http.StatusConflict, resp.StatusCode, string(body))
	require.Contains(t, string(body), "hold_in_force")

	resp, body = sendJSON(t, srv, http.MethodDelete, "/api/v1/objects/o-hold-1", dpkmstest.RoleAdmin, nil)
	require.Equal(t, http.StatusConflict, resp.StatusCode, string(body))
	require.Contains(t, string(body), "hold_in_force")

	// Operator cannot release a bos-owned hold (403 FORBIDDEN).
	resp, body = sendJSON(t, srv, http.MethodDelete, "/api/v1/holds/h-bos-1?object_id=o-hold-1&source=operator&reason=try", dpkmstest.RoleAdmin, nil)
	require.Equal(t, http.StatusForbidden, resp.StatusCode, string(body))

	// Authority ("bos") releases its hold -> PATCH succeeds.
	resp, body = sendJSON(t, srv, http.MethodDelete, "/api/v1/holds/h-bos-1?object_id=o-hold-1&source=bos&reason=closed", dpkmstest.RoleAdmin, nil)
	require.Equal(t, http.StatusOK, resp.StatusCode, string(body))

	resp, body = sendJSON(t, srv, http.MethodPatch, "/api/v1/objects/o-hold-1", dpkmstest.RoleAdmin, map[string]any{"type": "edited"})
	require.Equal(t, http.StatusOK, resp.StatusCode, string(body))
}
