package integration

import (
	"context"
	"encoding/json"
	"fmt"
	gohttp "net/http"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ideacrafterslabs/ctxt/internal/storage"
)

// auditLogResponse mirrors the GET /api/v1/audit-log JSON envelope.
type auditLogResponse struct {
	Data  []*storage.AuditEntry `json:"data"`
	Total int                   `json:"total"`
}

func getAuditLog(t *testing.T, baseURL, query string) auditLogResponse {
	t.Helper()
	url := baseURL + "/api/v1/audit-log"
	if query != "" {
		url += "?" + query
	}
	resp, err := gohttp.Get(url)
	require.NoError(t, err)
	defer resp.Body.Close()
	require.Equal(t, gohttp.StatusOK, resp.StatusCode)

	var out auditLogResponse
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&out))
	return out
}

// TestUS0405_AppendAndQuery verifies basic append+list round-trip via HTTP.
func TestUS0405_AppendAndQuery(t *testing.T) {
	env := startTestEnv(t)
	defer env.stop(t)

	ctx := context.Background()
	now := time.Now().UTC().Truncate(time.Second)
	require.NoError(t, env.svc.Store.AuditLog().Append(ctx,
		&storage.AuditEntry{
			ID: uuid.NewString(), EventType: "object.created",
			ObjectID: "obj_roundtrip", Actor: "system",
			Payload:   map[string]any{"source": "e2e"},
			CreatedAt: now,
		}))

	out := getAuditLog(t, env.URL, "")
	assert.GreaterOrEqual(t, out.Total, 1,
		"at least one audit entry after append")
	assert.Equal(t, "object.created", out.Data[0].EventType)
}

// TestUS0405_FilterByType verifies type= query param filters correctly.
func TestUS0405_FilterByType(t *testing.T) {
	env := startTestEnv(t)
	defer env.stop(t)

	ctx := context.Background()
	now := time.Now().UTC().Truncate(time.Second)
	for i, et := range []string{
		"object.created", "fanout.completed", "object.created",
	} {
		require.NoError(t, env.svc.Store.AuditLog().Append(ctx,
			&storage.AuditEntry{
				ID: uuid.NewString(), EventType: et,
				ObjectID: "obj_0405a", Actor: "system",
				Payload:   map[string]any{},
				CreatedAt: now.Add(time.Duration(i) * time.Second),
			}))
	}

	out := getAuditLog(t, env.URL, "type=object.created")
	assert.Equal(t, 2, out.Total)
	for _, e := range out.Data {
		assert.Equal(t, "object.created", e.EventType)
	}
}

// TestUS0405_FilterBySince verifies since= respects time boundary.
func TestUS0405_FilterBySince(t *testing.T) {
	env := startTestEnv(t)
	defer env.stop(t)

	ctx := context.Background()
	old := time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC)
	recent := time.Date(2026, 4, 1, 0, 0, 0, 0, time.UTC)

	require.NoError(t, env.svc.Store.AuditLog().Append(ctx,
		&storage.AuditEntry{
			ID: uuid.NewString(), EventType: "object.created",
			ObjectID: "obj_old", Actor: "system",
			Payload: map[string]any{}, CreatedAt: old,
		}))
	require.NoError(t, env.svc.Store.AuditLog().Append(ctx,
		&storage.AuditEntry{
			ID: uuid.NewString(), EventType: "object.created",
			ObjectID: "obj_recent", Actor: "system",
			Payload: map[string]any{}, CreatedAt: recent,
		}))

	out := getAuditLog(t, env.URL, "since=2025-01-01")
	assert.Equal(t, 1, out.Total)
	assert.Equal(t, "obj_recent", out.Data[0].ObjectID)
}

// TestUS0405_FilterByObject shows full history of one object.
func TestUS0405_FilterByObject(t *testing.T) {
	env := startTestEnv(t)
	defer env.stop(t)

	ctx := context.Background()
	now := time.Now().UTC().Truncate(time.Second)

	target := "obj_0405_hist"
	for i, et := range []string{"object.created", "fanout.completed"} {
		require.NoError(t, env.svc.Store.AuditLog().Append(ctx,
			&storage.AuditEntry{
				ID: uuid.NewString(), EventType: et,
				ObjectID: target, Actor: "system",
				Payload:   map[string]any{},
				CreatedAt: now.Add(time.Duration(i) * time.Second),
			}))
	}
	// Unrelated object.
	require.NoError(t, env.svc.Store.AuditLog().Append(ctx,
		&storage.AuditEntry{
			ID: uuid.NewString(), EventType: "object.created",
			ObjectID: "obj_other", Actor: "system",
			Payload: map[string]any{}, CreatedAt: now,
		}))

	out := getAuditLog(t, env.URL,
		fmt.Sprintf("object_id=%s", target))
	assert.Equal(t, 2, out.Total)
	for _, e := range out.Data {
		assert.Equal(t, target, e.ObjectID)
	}
}

// TestUS0405_Immutable verifies no update/delete API exists for audit log.
func TestUS0405_Immutable(t *testing.T) {
	env := startTestEnv(t)
	defer env.stop(t)

	// No PUT/PATCH/DELETE on /api/v1/audit-log.
	for _, method := range []string{
		gohttp.MethodPut, gohttp.MethodPatch, gohttp.MethodDelete,
	} {
		req, _ := gohttp.NewRequest(method,
			env.URL+"/api/v1/audit-log", nil)
		resp, err := gohttp.DefaultClient.Do(req)
		require.NoError(t, err)
		resp.Body.Close()
		assert.Equal(t, gohttp.StatusMethodNotAllowed, resp.StatusCode,
			"%s should be 405", method)
	}
}
