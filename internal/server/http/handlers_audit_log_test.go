package http

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/ideacrafterslabs/ctxt/internal/storage"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestListAuditLog_Empty(t *testing.T) {
	ts := newTestServerBundle(t)
	defer ts.Close()

	resp, err := http.Get(ts.URL + "/api/v1/audit-log")
	require.NoError(t, err)
	defer resp.Body.Close()
	assert.Equal(t, http.StatusOK, resp.StatusCode)

	var out struct {
		Data  []*storage.AuditEntry `json:"data"`
		Total int                   `json:"total"`
	}
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&out))
	assert.Empty(t, out.Data)
	assert.Equal(t, 0, out.Total)
}

func TestListAuditLog_WithEntries(t *testing.T) {
	ts := newTestServerBundle(t)
	defer ts.Close()

	ctx := context.Background()
	now := time.Now().UTC().Truncate(time.Second)
	for _, et := range []string{"object.created", "fanout.completed", "object.deleted"} {
		require.NoError(t, ts.svc.Store.AuditLog().Append(ctx,
			&storage.AuditEntry{
				ID: uuid.NewString(), EventType: et,
				ObjectID: "obj_a", Actor: "system",
				Payload: map[string]any{}, CreatedAt: now,
			}))
		now = now.Add(time.Second)
	}

	resp, err := http.Get(ts.URL + "/api/v1/audit-log")
	require.NoError(t, err)
	defer resp.Body.Close()
	assert.Equal(t, http.StatusOK, resp.StatusCode)

	var out struct {
		Data  []*storage.AuditEntry `json:"data"`
		Total int                   `json:"total"`
	}
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&out))
	assert.Len(t, out.Data, 3)
	assert.Equal(t, 3, out.Total)
}

func TestListAuditLog_FilterByType(t *testing.T) {
	ts := newTestServerBundle(t)
	defer ts.Close()

	ctx := context.Background()
	now := time.Now().UTC().Truncate(time.Second)
	for _, et := range []string{"object.created", "fanout.completed", "object.created"} {
		require.NoError(t, ts.svc.Store.AuditLog().Append(ctx,
			&storage.AuditEntry{
				ID: uuid.NewString(), EventType: et,
				ObjectID: "obj_b", Actor: "system",
				Payload: map[string]any{}, CreatedAt: now,
			}))
		now = now.Add(time.Second)
	}

	resp, err := http.Get(ts.URL + "/api/v1/audit-log?type=object.created")
	require.NoError(t, err)
	defer resp.Body.Close()

	var out struct {
		Data  []*storage.AuditEntry `json:"data"`
		Total int                   `json:"total"`
	}
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&out))
	assert.Equal(t, 2, out.Total)
	for _, e := range out.Data {
		assert.Equal(t, "object.created", e.EventType)
	}
}

func TestListAuditLog_FilterByMultipleTypes(t *testing.T) {
	ts := newTestServerBundle(t)
	defer ts.Close()

	ctx := context.Background()
	now := time.Now().UTC().Truncate(time.Second)
	for _, et := range []string{"object.created", "fanout.completed", "object.deleted"} {
		require.NoError(t, ts.svc.Store.AuditLog().Append(ctx,
			&storage.AuditEntry{
				ID: uuid.NewString(), EventType: et,
				ObjectID: "obj_mt", Actor: "system",
				Payload: map[string]any{}, CreatedAt: now,
			}))
		now = now.Add(time.Second)
	}

	resp, err := http.Get(ts.URL + "/api/v1/audit-log?type=object.created,object.deleted")
	require.NoError(t, err)
	defer resp.Body.Close()

	var out struct {
		Data  []*storage.AuditEntry `json:"data"`
		Total int                   `json:"total"`
	}
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&out))
	assert.Equal(t, 2, out.Total)
}

func TestListAuditLog_FilterByObjectID(t *testing.T) {
	ts := newTestServerBundle(t)
	defer ts.Close()

	ctx := context.Background()
	now := time.Now().UTC().Truncate(time.Second)
	for _, oid := range []string{"obj_x", "obj_y", "obj_x"} {
		require.NoError(t, ts.svc.Store.AuditLog().Append(ctx,
			&storage.AuditEntry{
				ID: uuid.NewString(), EventType: "object.created",
				ObjectID: oid, Actor: "system",
				Payload: map[string]any{}, CreatedAt: now,
			}))
		now = now.Add(time.Second)
	}

	resp, err := http.Get(ts.URL + "/api/v1/audit-log?object_id=obj_x")
	require.NoError(t, err)
	defer resp.Body.Close()

	var out struct {
		Data  []*storage.AuditEntry `json:"data"`
		Total int                   `json:"total"`
	}
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&out))
	assert.Equal(t, 2, out.Total)
}

func TestListAuditLog_FilterByActor(t *testing.T) {
	ts := newTestServerBundle(t)
	defer ts.Close()

	ctx := context.Background()
	now := time.Now().UTC().Truncate(time.Second)
	for _, actor := range []string{"system", "user:alice", "system"} {
		require.NoError(t, ts.svc.Store.AuditLog().Append(ctx,
			&storage.AuditEntry{
				ID: uuid.NewString(), EventType: "object.created",
				ObjectID: "obj_act", Actor: actor,
				Payload: map[string]any{}, CreatedAt: now,
			}))
		now = now.Add(time.Second)
	}

	resp, err := http.Get(ts.URL + "/api/v1/audit-log?actor=user:alice")
	require.NoError(t, err)
	defer resp.Body.Close()

	var out struct {
		Data  []*storage.AuditEntry `json:"data"`
		Total int                   `json:"total"`
	}
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&out))
	assert.Equal(t, 1, out.Total)
	assert.Equal(t, "user:alice", out.Data[0].Actor)
}

func TestListAuditLog_InvalidSince(t *testing.T) {
	ts := newTestServerBundle(t)
	defer ts.Close()

	resp, err := http.Get(ts.URL + "/api/v1/audit-log?since=not-a-date")
	require.NoError(t, err)
	defer resp.Body.Close()
	assert.Equal(t, http.StatusBadRequest, resp.StatusCode)
}

func TestListAuditLog_InvalidUntil(t *testing.T) {
	ts := newTestServerBundle(t)
	defer ts.Close()

	resp, err := http.Get(ts.URL + "/api/v1/audit-log?until=xyz")
	require.NoError(t, err)
	defer resp.Body.Close()
	assert.Equal(t, http.StatusBadRequest, resp.StatusCode)
}

func TestListAuditLog_Limit(t *testing.T) {
	ts := newTestServerBundle(t)
	defer ts.Close()

	ctx := context.Background()
	now := time.Now().UTC().Truncate(time.Second)
	for i := 0; i < 5; i++ {
		require.NoError(t, ts.svc.Store.AuditLog().Append(ctx,
			&storage.AuditEntry{
				ID: uuid.NewString(), EventType: "object.created",
				ObjectID: "obj_lim", Actor: "system",
				Payload: map[string]any{}, CreatedAt: now,
			}))
		now = now.Add(time.Second)
	}

	resp, err := http.Get(ts.URL + "/api/v1/audit-log?limit=2")
	require.NoError(t, err)
	defer resp.Body.Close()

	var out struct {
		Data  []*storage.AuditEntry `json:"data"`
		Total int                   `json:"total"`
	}
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&out))
	assert.Len(t, out.Data, 2)
	assert.Equal(t, 5, out.Total)
}
