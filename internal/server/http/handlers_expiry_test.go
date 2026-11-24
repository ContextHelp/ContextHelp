package http_test

import (
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/ideacrafterslabs/ctxt/internal/dpkmstest"
	"github.com/ideacrafterslabs/ctxt/internal/storage"
	"github.com/ideacrafterslabs/ctxt/internal/storageutil"
)

func TestObjectExpiryAndWatchExpiryHTTP(t *testing.T) {
	driver := storageutil.NewTestDriver(t)
	srv := dpkmstest.Start(t, driver, dpkmstest.WithKeys())

	require.NoError(t, driver.Objects().Create(ownerCtx(), &storage.KnowledgeObject{
		ID: "o-exp-1", Type: "note", Status: "active",
		RawContent: "ephemeral text", TextContent: "ephemeral text",
		CreatedAt: writeSeedTime, UpdatedAt: writeSeedTime,
	}))

	// 1. PUT /api/v1/objects/{id}/expiry with TTL + natural-language scope.
	resp, body := sendJSON(t, srv, http.MethodPut, "/api/v1/objects/o-exp-1/expiry", dpkmstest.RoleAdmin, map[string]any{
		"ttl": "7d", "expiry_scope": "emails and phone numbers",
	})
	require.Equal(t, http.StatusOK, resp.StatusCode, string(body))

	got, err := driver.Objects().Get(ownerCtx(), "o-exp-1")
	require.NoError(t, err)
	require.NotNil(t, got.ExpiresAt)
	require.Equal(t, "emails and phone numbers", got.ExpiryScope)

	// 2. Place hold -> DELETE /api/v1/objects/{id}/expiry returns 409 Conflict.
	resp, body = sendJSON(t, srv, http.MethodPost, "/api/v1/holds", dpkmstest.RoleAdmin, map[string]any{
		"object_id": "o-exp-1", "hold_id": "h-exp-1", "reason": "legal hold",
	})
	require.Equal(t, http.StatusCreated, resp.StatusCode, string(body))

	resp, body = sendJSON(t, srv, http.MethodDelete, "/api/v1/objects/o-exp-1/expiry", dpkmstest.RoleAdmin, nil)
	require.Equal(t, http.StatusConflict, resp.StatusCode, string(body))

	// 3. Release hold -> DELETE clears expiry.
	resp, body = sendJSON(t, srv, http.MethodDelete, "/api/v1/holds/h-exp-1?object_id=o-exp-1", dpkmstest.RoleAdmin, nil)
	require.Equal(t, http.StatusOK, resp.StatusCode, string(body))

	resp, body = sendJSON(t, srv, http.MethodDelete, "/api/v1/objects/o-exp-1/expiry", dpkmstest.RoleAdmin, nil)
	require.Equal(t, http.StatusOK, resp.StatusCode, string(body))

	got2, err := driver.Objects().Get(ownerCtx(), "o-exp-1")
	require.NoError(t, err)
	require.Nil(t, got2.ExpiresAt)
	require.Empty(t, got2.ExpiryScope)

	// 4. POST /api/v1/inbox with expires_at + expiry_scope.
	exp := time.Now().UTC().Add(24 * time.Hour).Truncate(time.Second)
	resp, body = sendJSON(t, srv, http.MethodPost, "/api/v1/inbox", dpkmstest.RoleAdmin, map[string]any{
		"content": "temporary inbox", "type": "text",
		"expires_at": exp.Format(time.RFC3339), "expiry_scope": "raw content, keep summary",
	})
	require.Equal(t, http.StatusCreated, resp.StatusCode, string(body))
	var inboxResp struct {
		ID          string     `json:"id"`
		ExpiresAt   *time.Time `json:"expires_at"`
		ExpiryScope string     `json:"expiry_scope"`
	}
	require.NoError(t, json.Unmarshal(body, &inboxResp))
	require.NotEmpty(t, inboxResp.ID)
	require.NotNil(t, inboxResp.ExpiresAt)
	require.Equal(t, "raw content, keep summary", inboxResp.ExpiryScope)

	// 5. POST /api/v1/watches with expiry_ttl + expiry_scope.
	resp, body = sendJSON(t, srv, http.MethodPost, "/api/v1/watches", dpkmstest.RoleAdmin, map[string]any{
		"path": t.TempDir(), "expiry_ttl": "14d", "expiry_scope": "PII", "paused": true,
	})
	require.Equal(t, http.StatusCreated, resp.StatusCode, string(body))
	var watchResp struct {
		ID          string `json:"id"`
		ExpiryTTL   string `json:"expiry_ttl"`
		ExpiryScope string `json:"expiry_scope"`
	}
	require.NoError(t, json.Unmarshal(body, &watchResp))
	require.Equal(t, "14d", watchResp.ExpiryTTL)
	require.Equal(t, "PII", watchResp.ExpiryScope)
}
