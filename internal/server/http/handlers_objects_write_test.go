package http_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	authn "github.com/ideacrafterslabs/ctxt/internal/auth"
	"github.com/ideacrafterslabs/ctxt/internal/dpkmstest"
	"github.com/ideacrafterslabs/ctxt/internal/jobs"
	"github.com/ideacrafterslabs/ctxt/internal/storage"
	"github.com/ideacrafterslabs/ctxt/internal/storageutil"
)

// The object write surface: PATCH /api/v1/objects/{id} over every field
// `ctxt edit` changes, DELETE /api/v1/objects/{id}, and POST
// /api/v1/objects/{id}/reprocess, which enqueues a dpkms job. The suite
// runs on SQLite here and on Postgres under -tags integration.

func TestObjectWrites_SQLite(t *testing.T) {
	RunObjectWriteSuite(t, storageutil.NewTestDriver)
}

// RunObjectWriteSuite runs the write-surface cases against a fresh
// instance over newDriver's storage.
func RunObjectWriteSuite(t *testing.T, newDriver func(*testing.T) storage.StorageDriver) {
	fresh := func(t *testing.T) (*dpkmstest.Server, storage.StorageDriver) {
		t.Helper()
		driver := newDriver(t)
		srv := dpkmstest.Start(t, driver, dpkmstest.WithStaticTokens())
		seedWriteObject(t, driver)
		return srv, driver
	}

	t.Run("PatchAppliesEachField", func(t *testing.T) { testPatchAppliesEachField(t, fresh) })
	t.Run("PatchRejects", func(t *testing.T) { testPatchRejects(t, fresh) })
	t.Run("Delete", func(t *testing.T) { testDelete(t, fresh) })
	t.Run("Reprocess", func(t *testing.T) { testReprocess(t, fresh) })
	t.Run("RoleScopes", func(t *testing.T) { testWriteRoleScopes(t, fresh) })
	t.Run("DriverNotFound", func(t *testing.T) { testDriverNotFound(t, newDriver(t)) })
}

type freshFn func(*testing.T) (*dpkmstest.Server, storage.StorageDriver)

var writeSeedTime = time.Date(2026, 1, 10, 9, 0, 0, 0, time.UTC)

// seedWriteObject stores o-w, the object every write case edits.
func seedWriteObject(t *testing.T, driver storage.StorageDriver) {
	t.Helper()
	require.NoError(t, driver.Objects().Create(context.Background(), &storage.KnowledgeObject{
		ID: "o-w", Type: "note", Subtype: "note.short", Status: "active",
		RawContent: "raw body", TextContent: "text body",
		Summaries: []string{"first summary", "second summary"},
		Tags:      []storage.Tag{{Label: "old", Source: "auto"}},
		Mentions:  mentionURIs(t, "@acme.api"),
		CreatedAt: writeSeedTime, UpdatedAt: writeSeedTime,
	}))
}

func sendJSON(t *testing.T, srv *dpkmstest.Server, method, path, role string, body any) (*http.Response, []byte) {
	t.Helper()
	var rd io.Reader
	if body != nil {
		switch b := body.(type) {
		case string:
			rd = bytes.NewReader([]byte(b))
		default:
			raw, err := json.Marshal(b)
			require.NoError(t, err)
			rd = bytes.NewReader(raw)
		}
	}
	resp := srv.Request(t, method, path, role, rd)
	defer resp.Body.Close()
	out, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	return resp, out
}

func storedObject(t *testing.T, driver storage.StorageDriver, id string) *storage.KnowledgeObject {
	t.Helper()
	obj, err := driver.Objects().Get(context.Background(), id)
	require.NoError(t, err)
	return obj
}

func tagLabels(tags []storage.Tag) []string {
	out := make([]string, 0, len(tags))
	for _, tg := range tags {
		out = append(out, tg.Label)
	}
	return out
}

func mentionStrings(obj *storage.KnowledgeObject) []string {
	out := make([]string, 0, len(obj.Mentions))
	for i := range obj.Mentions {
		out = append(out, obj.Mentions[i].String())
	}
	return out
}

// testPatchAppliesEachField sends one field per request and checks that
// exactly that field changed, in the response and in storage, and that
// updated_at moved.
func testPatchAppliesEachField(t *testing.T, fresh freshFn) {
	cases := []struct {
		field string
		value any
		check func(t *testing.T, obj *storage.KnowledgeObject)
	}{
		{"type", "article", func(t *testing.T, obj *storage.KnowledgeObject) {
			assert.Equal(t, "article", obj.Type)
		}},
		{"subtype", "note.long", func(t *testing.T, obj *storage.KnowledgeObject) {
			assert.Equal(t, "note.long", obj.Subtype)
		}},
		{"title", "New title", func(t *testing.T, obj *storage.KnowledgeObject) {
			// The title is the first summary; setting it replaces the list.
			assert.Equal(t, []string{"New title"}, obj.Summaries)
		}},
		{"summary", "New summary", func(t *testing.T, obj *storage.KnowledgeObject) {
			// The summary replaces the first summary and keeps the rest.
			assert.Equal(t, []string{"New summary", "second summary"}, obj.Summaries)
		}},
		{"tags", []string{" ux ", "design", ""}, func(t *testing.T, obj *storage.KnowledgeObject) {
			assert.Equal(t, []string{"ux", "design"}, tagLabels(obj.Tags))
			for _, tg := range obj.Tags {
				assert.Equal(t, "manual", tg.Source, tg.Label)
			}
		}},
		{"mentions", []string{"@ui.best-practice", "ctxt://entity/ux/onboarding"}, func(t *testing.T, obj *storage.KnowledgeObject) {
			assert.Equal(t, []string{"ctxt://entity/ui/best-practice", "ctxt://entity/ux/onboarding"}, mentionStrings(obj))
		}},
	}
	for _, tc := range cases {
		t.Run(tc.field, func(t *testing.T) {
			srv, driver := fresh(t)
			before := storedObject(t, driver, "o-w")

			resp, body := sendJSON(t, srv, http.MethodPatch, "/api/v1/objects/o-w", dpkmstest.RoleWriter,
				map[string]any{tc.field: tc.value})
			require.Equal(t, http.StatusOK, resp.StatusCode, string(body))

			var got storage.KnowledgeObject
			require.NoError(t, json.Unmarshal(body, &got))
			tc.check(t, &got)

			after := storedObject(t, driver, "o-w")
			tc.check(t, after)
			assert.True(t, after.UpdatedAt.After(before.UpdatedAt), "updated_at: %s -> %s", before.UpdatedAt, after.UpdatedAt)

			// Every other field is untouched.
			if tc.field != "type" {
				assert.Equal(t, before.Type, after.Type)
			}
			if tc.field != "subtype" {
				assert.Equal(t, before.Subtype, after.Subtype)
			}
			if tc.field != "title" && tc.field != "summary" {
				assert.Equal(t, before.Summaries, after.Summaries)
			}
			if tc.field != "tags" {
				assert.Equal(t, tagLabels(before.Tags), tagLabels(after.Tags))
			}
			if tc.field != "mentions" {
				assert.Equal(t, mentionStrings(before), mentionStrings(after))
			}
			assert.Equal(t, before.RawContent, after.RawContent)
			assert.Equal(t, before.CreatedAt.UTC(), after.CreatedAt.UTC())
		})
	}

	t.Run("empty lists clear", func(t *testing.T) {
		srv, driver := fresh(t)
		resp, body := sendJSON(t, srv, http.MethodPatch, "/api/v1/objects/o-w", dpkmstest.RoleWriter,
			map[string]any{"tags": []string{}, "mentions": []string{}})
		require.Equal(t, http.StatusOK, resp.StatusCode, string(body))
		after := storedObject(t, driver, "o-w")
		assert.Empty(t, after.Tags)
		assert.Empty(t, after.Mentions)
	})
}

func testPatchRejects(t *testing.T, fresh freshFn) {
	srv, driver := fresh(t)
	before := storedObject(t, driver, "o-w")

	cases := []struct {
		name   string
		path   string
		body   any
		status int
		code   string
	}{
		{"unknown field", "/api/v1/objects/o-w", map[string]any{"hints": "x"}, http.StatusBadRequest, "INVALID_REQUEST"},
		{"wrong field type", "/api/v1/objects/o-w", map[string]any{"tags": "a,b"}, http.StatusBadRequest, "INVALID_REQUEST"},
		{"not JSON", "/api/v1/objects/o-w", "{", http.StatusBadRequest, "INVALID_REQUEST"},
		{"no field", "/api/v1/objects/o-w", map[string]any{}, http.StatusBadRequest, "INVALID_REQUEST"},
		{"title and summary", "/api/v1/objects/o-w", map[string]any{"title": "a", "summary": "b"}, http.StatusBadRequest, "INVALID_REQUEST"},
		{"bad mention", "/api/v1/objects/o-w", map[string]any{"mentions": []string{"@"}}, http.StatusBadRequest, "INVALID_MENTION"},
		{"missing object", "/api/v1/objects/o-missing", map[string]any{"type": "x"}, http.StatusNotFound, "NOT_FOUND"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			resp, body := sendJSON(t, srv, http.MethodPatch, tc.path, dpkmstest.RoleWriter, tc.body)
			require.Equal(t, tc.status, resp.StatusCode, string(body))
			assert.Equal(t, tc.code, apiErrorCode(t, body))
		})
	}

	after := storedObject(t, driver, "o-w")
	assert.Equal(t, before.UpdatedAt.UTC(), after.UpdatedAt.UTC(), "a rejected patch must not write")
	assert.Equal(t, before.Summaries, after.Summaries)
}

func testDelete(t *testing.T, fresh freshFn) {
	srv, driver := fresh(t)

	resp, body := sendJSON(t, srv, http.MethodDelete, "/api/v1/objects/o-w", dpkmstest.RoleAdmin, nil)
	require.Equal(t, http.StatusNoContent, resp.StatusCode, string(body))
	_, err := driver.Objects().Get(context.Background(), "o-w")
	assert.ErrorIs(t, err, storage.ErrNotFound)

	resp, body = sendJSON(t, srv, http.MethodDelete, "/api/v1/objects/o-w", dpkmstest.RoleAdmin, nil)
	require.Equal(t, http.StatusNotFound, resp.StatusCode, string(body))
	assert.Equal(t, "NOT_FOUND", apiErrorCode(t, body))
}

func testReprocess(t *testing.T, fresh freshFn) {
	srv, driver := fresh(t)

	for _, step := range jobs.ReprocessSteps {
		t.Run(step, func(t *testing.T) {
			resp, body := sendJSON(t, srv, http.MethodPost, "/api/v1/objects/o-w/reprocess", dpkmstest.RoleWriter,
				map[string]any{"step": step})
			require.Equal(t, http.StatusAccepted, resp.StatusCode, string(body))
			var out struct {
				JobID    string `json:"job_id"`
				ObjectID string `json:"object_id"`
				Step     string `json:"step"`
			}
			require.NoError(t, json.Unmarshal(body, &out))
			require.NotEmpty(t, out.JobID)
			assert.Equal(t, "o-w", out.ObjectID)
			assert.Equal(t, step, out.Step)

			job, err := driver.Jobs().Get(context.Background(), out.JobID)
			require.NoError(t, err)
			assert.Equal(t, jobs.ReprocessJobType, job.Type)
			assert.Equal(t, storage.JobPending, job.Status)
			var payload jobs.ReprocessPayload
			require.NoError(t, json.Unmarshal([]byte(job.Payload), &payload))
			assert.Equal(t, jobs.ReprocessPayload{ObjectID: "o-w", Step: step}, payload)
		})
	}

	rejects := []struct {
		name   string
		path   string
		body   any
		status int
		code   string
	}{
		{"unknown step", "/api/v1/objects/o-w/reprocess", map[string]any{"step": "summarizer"}, http.StatusBadRequest, "INVALID_REQUEST"},
		{"hyphenated step", "/api/v1/objects/o-w/reprocess", map[string]any{"step": "structured-metadata"}, http.StatusBadRequest, "INVALID_REQUEST"},
		{"no step", "/api/v1/objects/o-w/reprocess", map[string]any{}, http.StatusBadRequest, "INVALID_REQUEST"},
		{"unknown field", "/api/v1/objects/o-w/reprocess", map[string]any{"step": "tagger", "force": true}, http.StatusBadRequest, "INVALID_REQUEST"},
		{"missing object", "/api/v1/objects/o-missing/reprocess", map[string]any{"step": "tagger"}, http.StatusNotFound, "NOT_FOUND"},
	}
	for _, tc := range rejects {
		t.Run(tc.name, func(t *testing.T) {
			_, total, err := driver.Jobs().List(context.Background(), storage.JobFilter{Type: jobs.ReprocessJobType})
			require.NoError(t, err)
			resp, body := sendJSON(t, srv, http.MethodPost, tc.path, dpkmstest.RoleWriter, tc.body)
			require.Equal(t, tc.status, resp.StatusCode, string(body))
			assert.Equal(t, tc.code, apiErrorCode(t, body))
			_, after, err := driver.Jobs().List(context.Background(), storage.JobFilter{Type: jobs.ReprocessJobType})
			require.NoError(t, err)
			assert.Equal(t, total, after, "a rejected reprocess must enqueue nothing")
		})
	}
}

// testWriteRoleScopes pins each route's scope through the role bundles:
// a writer edits and reprocesses but cannot delete, a reader does
// neither, a request without a token is 401, and an admin deletes.
func testWriteRoleScopes(t *testing.T, fresh freshFn) {
	srv, driver := fresh(t)
	patch := map[string]any{"subtype": "note.long"}
	reprocess := map[string]any{"step": "tagger"}

	resp, body := sendJSON(t, srv, http.MethodPatch, "/api/v1/objects/o-w", dpkmstest.RoleReader, patch)
	assertInsufficientScope(t, resp, body, authn.ScopeWriteObjects)
	resp, body = sendJSON(t, srv, http.MethodPost, "/api/v1/objects/o-w/reprocess", dpkmstest.RoleReader, reprocess)
	assertInsufficientScope(t, resp, body, authn.ScopeWriteObjects)
	resp, body = sendJSON(t, srv, http.MethodDelete, "/api/v1/objects/o-w", dpkmstest.RoleReader, nil)
	assertInsufficientScope(t, resp, body, authn.ScopeDeleteObjects)

	resp, body = sendJSON(t, srv, http.MethodPatch, "/api/v1/objects/o-w", dpkmstest.RoleWriter, patch)
	assert.Equal(t, http.StatusOK, resp.StatusCode, string(body))
	resp, body = sendJSON(t, srv, http.MethodPost, "/api/v1/objects/o-w/reprocess", dpkmstest.RoleWriter, reprocess)
	assert.Equal(t, http.StatusAccepted, resp.StatusCode, string(body))
	resp, body = sendJSON(t, srv, http.MethodDelete, "/api/v1/objects/o-w", dpkmstest.RoleWriter, nil)
	assertInsufficientScope(t, resp, body, authn.ScopeDeleteObjects)
	storedObject(t, driver, "o-w") // still there

	for _, rq := range []struct {
		method, path string
		body         any
	}{
		{http.MethodPatch, "/api/v1/objects/o-w", patch},
		{http.MethodPost, "/api/v1/objects/o-w/reprocess", reprocess},
		{http.MethodDelete, "/api/v1/objects/o-w", nil},
	} {
		resp, body := sendJSON(t, srv, rq.method, rq.path, "no-such-role", rq.body)
		assert.Equal(t, http.StatusUnauthorized, resp.StatusCode, "%s %s: %s", rq.method, rq.path, body)
	}
	storedObject(t, driver, "o-w")

	resp, body = sendJSON(t, srv, http.MethodDelete, "/api/v1/objects/o-w", dpkmstest.RoleAdmin, nil)
	assert.Equal(t, http.StatusNoContent, resp.StatusCode, string(body))
}

func assertInsufficientScope(t *testing.T, resp *http.Response, body []byte, scope authn.Scope) {
	t.Helper()
	require.Equal(t, http.StatusForbidden, resp.StatusCode, string(body))
	var env struct {
		Error struct {
			Code    string         `json:"code"`
			Details map[string]any `json:"details"`
		} `json:"error"`
	}
	require.NoError(t, json.Unmarshal(body, &env), string(body))
	assert.Equal(t, "INSUFFICIENT_SCOPE", env.Error.Code)
	assert.Equal(t, string(scope), env.Error.Details["required_scope"])
}

// testDriverNotFound: a missing row on update or delete is
// storage.ErrNotFound on every driver, so the API can tell a missing
// object (404) from a storage failure (500).
func testDriverNotFound(t *testing.T, driver storage.StorageDriver) {
	ctx := context.Background()
	err := driver.Objects().Delete(ctx, "o-none")
	assert.True(t, errors.Is(err, storage.ErrNotFound), "Delete: %v", err)
	err = driver.Objects().Update(ctx, &storage.KnowledgeObject{ID: "o-none", Type: "note", UpdatedAt: writeSeedTime})
	assert.True(t, errors.Is(err, storage.ErrNotFound), "Update: %v", err)
}

func apiErrorCode(t *testing.T, body []byte) string {
	t.Helper()
	var env struct {
		Error struct {
			Code string `json:"code"`
		} `json:"error"`
	}
	require.NoError(t, json.Unmarshal(body, &env), string(body))
	return env.Error.Code
}
