package http_test

import (
	"encoding/json"
	"net/http"
	"net/url"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/ideacrafterslabs/ctxt/internal/dpkmstest"
	"github.com/ideacrafterslabs/ctxt/internal/storage"
	"github.com/ideacrafterslabs/ctxt/internal/storageutil"
)

func TestDeleteBySourceAndFilter(t *testing.T) {
	driver := storageutil.NewTestDriver(t)
	srv := dpkmstest.Start(t, driver, dpkmstest.WithKeys())

	const srcKey = "bos://acct/records/http-1"
	require.NoError(t, driver.Objects().Create(ownerCtx(), &storage.KnowledgeObject{
		ID: "o-src-1", Type: "note", Status: "active",
		RawContent: "body", TextContent: "body", Source: "bos", SourceKey: srcKey,
		CreatedAt: writeSeedTime, UpdatedAt: writeSeedTime,
	}))

	// List filtered by source_key.
	resp, body := sendJSON(t, srv, http.MethodGet, "/api/v1/objects?source_key="+url.QueryEscape(srcKey), dpkmstest.RoleReader, nil)
	require.Equal(t, http.StatusOK, resp.StatusCode, string(body))
	var listResp struct {
		Data  []storage.KnowledgeObject `json:"data"`
		Total int                       `json:"total"`
	}
	require.NoError(t, json.Unmarshal(body, &listResp))
	require.Equal(t, 1, listResp.Total)
	require.Equal(t, "o-src-1", listResp.Data[0].ID)

	// Writer role is refused on DELETE /api/v1/objects/by-source (requires delete:objects).
	resp, body = sendJSON(t, srv, http.MethodDelete, "/api/v1/objects/by-source?source_key="+url.QueryEscape(srcKey), dpkmstest.RoleWriter, nil)
	require.Equal(t, http.StatusForbidden, resp.StatusCode, string(body))

	// Active hold returns 409 hold_in_force.
	err := driver.Holds().Place(ownerCtx(), &storage.ObjectHold{
		ObjectID: "o-src-1", HoldID: "h-http-1", Source: "bos", Reason: "litigation", PlacedBy: "bos", PlacedAt: writeSeedTime,
	})
	require.NoError(t, err)
	resp, body = sendJSON(t, srv, http.MethodDelete, "/api/v1/objects/by-source?source_key="+url.QueryEscape(srcKey)+"&tombstone=true", dpkmstest.RoleAdmin, nil)
	require.Equal(t, http.StatusConflict, resp.StatusCode, string(body))
	require.Contains(t, string(body), "hold_in_force")

	// Release hold -> DELETE succeeds and writes tombstone.
	_, err = driver.Holds().Release(ownerCtx(), "o-src-1", "h-http-1", "bos", "done", writeSeedTime)
	require.NoError(t, err)
	resp, body = sendJSON(t, srv, http.MethodDelete, "/api/v1/objects/by-source?source_key="+url.QueryEscape(srcKey)+"&tombstone=true", dpkmstest.RoleAdmin, nil)
	require.Equal(t, http.StatusOK, resp.StatusCode, string(body))
	var eraseResp struct {
		DeletedIDs       []string `json:"deleted_ids"`
		TombstoneWritten bool     `json:"tombstone_written"`
	}
	require.NoError(t, json.Unmarshal(body, &eraseResp))
	require.Equal(t, []string{"o-src-1"}, eraseResp.DeletedIDs)
	require.True(t, eraseResp.TombstoneWritten)

	exists, err := driver.Tombstones().Exists(ownerCtx(), srcKey)
	require.NoError(t, err)
	require.True(t, exists)
}
