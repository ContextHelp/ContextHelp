package http_test

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"slices"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ideacrafterslabs/ctxt/internal/dpkmstest"
	"github.com/ideacrafterslabs/ctxt/internal/storage"
	"github.com/ideacrafterslabs/ctxt/internal/storageutil"
)

// The inbox surface: GET /api/v1/inbox/queue and POST /api/v1/inbox/clear
// next to the list, triage and discard routes. The suite runs on SQLite
// here and on Postgres under -tags integration.

func TestInboxAPI_SQLite(t *testing.T) {
	RunInboxSuite(t, storageutil.NewTestDriver)
}

// seedInboxCorpus writes two inbox items, one active and one raw object,
// and one job per status.
func seedInboxCorpus(t *testing.T, driver storage.StorageDriver) {
	t.Helper()
	ctx := context.Background()
	objs := []*storage.KnowledgeObject{
		{ID: "i-1", Type: "text", Status: "inbox", RawContent: "one", CreatedAt: day("2026-04-10T09:00:00Z"), UpdatedAt: day("2026-04-10T09:00:00Z")},
		{ID: "i-2", Type: "text", Status: "inbox", RawContent: "two", CreatedAt: day("2026-04-11T09:00:00Z"), UpdatedAt: day("2026-04-11T09:00:00Z")},
		{ID: "a-1", Type: "note", Status: "active", RawContent: "kept", CreatedAt: day("2026-04-12T09:00:00Z"), UpdatedAt: day("2026-04-12T09:00:00Z")},
		{ID: "r-1", Type: "note", Status: "raw", RawContent: "raw", CreatedAt: day("2026-04-13T09:00:00Z"), UpdatedAt: day("2026-04-13T09:00:00Z")},
	}
	for _, o := range objs {
		require.NoError(t, driver.Objects().Create(ctx, o), o.ID)
	}
	jobs := []struct {
		id     string
		status storage.JobStatus
	}{
		{"j-pending", storage.JobPending},
		{"j-running", storage.JobRunning},
		{"j-failed", storage.JobFailed},
		{"j-done", storage.JobCompleted},
	}
	for _, j := range jobs {
		require.NoError(t, driver.Jobs().Create(ctx, &storage.Job{
			ID: j.id, Type: "ingest:text", Status: j.status, Pipeline: "text.short", MaxRetries: 3,
			CreatedAt: day("2026-04-14T09:00:00Z"), UpdatedAt: day("2026-04-14T09:00:00Z"),
		}), j.id)
	}
}

type inboxEnv struct {
	srv *dpkmstest.Server
}

func (e inboxEnv) do(t *testing.T, method, path, role string) (int, []byte) {
	t.Helper()
	resp := e.srv.Request(t, method, path, role, nil)
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	return resp.StatusCode, body
}

type queueBody struct {
	Items []struct {
		ID     string `json:"id"`
		Kind   string `json:"kind"`
		Status string `json:"status"`
	} `json:"items"`
	Total int `json:"total"`
}

func (e inboxEnv) queue(t *testing.T, rawQuery string) (ids []string, total int) {
	t.Helper()
	code, body := e.do(t, http.MethodGet, "/api/v1/inbox/queue?"+rawQuery, dpkmstest.RoleReader)
	require.Equal(t, http.StatusOK, code, "GET /inbox/queue?%s: %s", rawQuery, body)
	var out queueBody
	require.NoError(t, json.Unmarshal(body, &out))
	require.NotNil(t, out.Items, "items must be a JSON array, never null: %s", body)
	for _, it := range out.Items {
		ids = append(ids, it.ID)
	}
	slices.Sort(ids)
	return ids, out.Total
}

func (e inboxEnv) status(t *testing.T, id string) string {
	t.Helper()
	obj, err := e.srv.Driver.Objects().Get(context.Background(), id)
	require.NoError(t, err, id)
	return obj.Status
}

// RunInboxSuite runs every inbox-surface case against fresh protected
// instances over the driver newDriver opens.
func RunInboxSuite(t *testing.T, newDriver func(*testing.T) storage.StorageDriver) {
	fresh := func(t *testing.T) inboxEnv {
		t.Helper()
		driver := newDriver(t)
		srv := dpkmstest.Start(t, driver, dpkmstest.WithStaticTokens())
		seedInboxCorpus(t, driver)
		return inboxEnv{srv: srv}
	}
	t.Run("Queue", func(t *testing.T) { testInboxQueue(t, fresh(t)) })
	t.Run("QueueRejectsBadParams", func(t *testing.T) { testInboxQueueBadParams(t, fresh(t)) })
	t.Run("QueueEmpty", func(t *testing.T) { testInboxQueueEmpty(t, newDriver) })
	t.Run("Clear", func(t *testing.T) { testInboxClear(t, fresh(t)) })
	t.Run("Roles", func(t *testing.T) { testInboxRoles(t, fresh(t)) })
}

func testInboxQueue(t *testing.T, env inboxEnv) {
	cases := []struct {
		query string
		want  []string
	}{
		{"", []string{"j-failed", "j-pending", "j-running", "r-1"}},
		{"pending=true", []string{"j-pending", "j-running"}},
		{"failed=true", []string{"j-failed"}},
		{"raw=true", []string{"r-1"}},
		{"pending=true&failed=true", []string{"j-failed", "j-pending", "j-running"}},
		{"pending=false&raw=true", []string{"r-1"}},
	}
	for _, c := range cases {
		ids, total := env.queue(t, c.query)
		assert.Equal(t, c.want, ids, "queue?%s", c.query)
		assert.Equal(t, len(c.want), total, "queue?%s total", c.query)
	}

	// limit pages the combined view; total counts every match.
	ids, total := env.queue(t, "limit=1&raw=true")
	assert.Equal(t, []string{"r-1"}, ids)
	assert.Equal(t, 1, total)
}

func testInboxQueueBadParams(t *testing.T, env inboxEnv) {
	for query, param := range map[string]string{
		"raw=yes":            "raw",
		"pending=":           "pending",
		"failed=1&failed=0":  "failed",
		"limit=-1":           "limit",
		"offset=x":           "offset",
		"status=inbox":       "status",
		"pending=true&bogus": "bogus",
	} {
		code, body := env.do(t, http.MethodGet, "/api/v1/inbox/queue?"+query, dpkmstest.RoleReader)
		assertInvalidParam(t, code, body, param)
	}
}

func testInboxQueueEmpty(t *testing.T, newDriver func(*testing.T) storage.StorageDriver) {
	env := inboxEnv{srv: dpkmstest.Start(t, newDriver(t), dpkmstest.WithStaticTokens())}
	ids, total := env.queue(t, "")
	assert.Empty(t, ids)
	assert.Zero(t, total)
}

// Clear discards every inbox item and nothing else, and reports the
// count; a second clear finds nothing.
func testInboxClear(t *testing.T, env inboxEnv) {
	code, body := env.do(t, http.MethodPost, "/api/v1/inbox/clear", dpkmstest.RoleAdmin)
	require.Equal(t, http.StatusOK, code, "%s", body)
	var out struct {
		Cleared *int `json:"cleared"`
	}
	require.NoError(t, json.Unmarshal(body, &out))
	require.NotNil(t, out.Cleared, "%s", body)
	assert.Equal(t, 2, *out.Cleared)

	assert.Equal(t, "discarded", env.status(t, "i-1"))
	assert.Equal(t, "discarded", env.status(t, "i-2"))
	assert.Equal(t, "active", env.status(t, "a-1"))
	assert.Equal(t, "raw", env.status(t, "r-1"))

	code, body = env.do(t, http.MethodGet, "/api/v1/inbox", dpkmstest.RoleReader)
	require.Equal(t, http.StatusOK, code, "%s", body)
	var list struct {
		Total int `json:"total"`
	}
	require.NoError(t, json.Unmarshal(body, &list))
	assert.Zero(t, list.Total)

	code, body = env.do(t, http.MethodPost, "/api/v1/inbox/clear", dpkmstest.RoleAdmin)
	require.Equal(t, http.StatusOK, code, "%s", body)
	assert.JSONEq(t, `{"cleared":0}`, string(body))
}

// Reading the inbox and its queue is read:inbox; triage, discard and
// clear are process:inbox, which only admin holds.
func testInboxRoles(t *testing.T, env inboxEnv) {
	for _, role := range dpkmstest.Roles {
		code, body := env.do(t, http.MethodGet, "/api/v1/inbox/queue", role)
		assert.Equal(t, http.StatusOK, code, "%s GET /inbox/queue: %s", role, body)
		code, body = env.do(t, http.MethodGet, "/api/v1/inbox", role)
		assert.Equal(t, http.StatusOK, code, "%s GET /inbox: %s", role, body)
	}

	for _, role := range []string{dpkmstest.RoleWriter, dpkmstest.RoleReader} {
		for _, path := range []string{"/api/v1/inbox/clear", "/api/v1/inbox/i-1/triage", "/api/v1/inbox/i-1/discard"} {
			code, body := env.do(t, http.MethodPost, path, role)
			require.Equal(t, http.StatusForbidden, code, "%s POST %s: %s", role, path, body)
			assert.Contains(t, string(body), "process:inbox", "%s POST %s", role, path)
		}
	}
	assert.Equal(t, "inbox", env.status(t, "i-1"), "a refused request changed the item")
	assert.Equal(t, "inbox", env.status(t, "i-2"), "a refused clear changed an item")

	for _, path := range []string{"/api/v1/inbox/queue", "/api/v1/inbox"} {
		code, _ := env.do(t, http.MethodGet, path, "")
		assert.Equal(t, http.StatusUnauthorized, code, "no token GET %s", path)
	}
	code, _ := env.do(t, http.MethodPost, "/api/v1/inbox/clear", "")
	assert.Equal(t, http.StatusUnauthorized, code, "no token POST /inbox/clear")
}
