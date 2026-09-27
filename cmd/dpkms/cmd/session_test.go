package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	authn "github.com/ideacrafterslabs/ctxt/internal/auth"
	"github.com/ideacrafterslabs/ctxt/internal/storage"
)

// sessionDB is a test database with a static token table in its config.
func sessionDB(t *testing.T) *testDB {
	t.Helper()
	db := setupTestDB(t)
	f, err := os.OpenFile(db.ConfigPath, os.O_APPEND|os.O_WRONLY, 0)
	require.NoError(t, err)
	_, err = fmt.Fprint(f, "server:\n  auth:\n    provider: static\n    static:\n      tokens:\n        - token: tok-ops\n          principal: ops\n          roles: [admin]\n")
	require.NoError(t, err)
	require.NoError(t, f.Close())

	now := time.Now().UTC()
	ctx := context.Background()
	for _, s := range []*storage.UISession{
		{ID: "uis_active", PrincipalID: "ops", TokenHash: authn.HashSecret("tok-ops"), CreatedAt: now.Add(-time.Minute)},
		{ID: "uis_orphan", PrincipalID: "ops", TokenHash: authn.HashSecret("tok-gone"), CreatedAt: now.Add(-2 * time.Minute)},
		{ID: "uis_other", PrincipalID: "other", TokenHash: authn.HashSecret("tok-other"), CreatedAt: now.Add(-3 * time.Minute)},
		{ID: "uis_old", PrincipalID: "ops", TokenHash: authn.HashSecret("tok-ops"), CreatedAt: now.Add(-48 * time.Hour)},
	} {
		s.SecretHash = "secret-" + s.ID
		s.Scope = authn.ScopeUI
		s.LastSeenAt = s.CreatedAt
		s.IdleExpiresAt = s.CreatedAt.Add(12 * time.Hour)
		s.ExpiresAt = s.CreatedAt.Add(7 * 24 * time.Hour)
		require.NoError(t, db.Driver.UISessions().Create(ctx, s))
	}
	return db
}

type sessionListDoc struct {
	Sessions []struct {
		ID          string `json:"id"`
		PrincipalID string `json:"principal_id"`
		Status      string `json:"status"`
		SecretHash  string `json:"secret_hash"`
		TokenHash   string `json:"token_hash"`
	} `json:"sessions"`
	Total int `json:"total"`
}

func TestSessionList(t *testing.T) {
	db := sessionDB(t)

	out, err := db.run("session", "list", "--format", "json")
	require.NoError(t, err, out)
	var doc sessionListDoc
	require.NoError(t, json.Unmarshal(jsonDoc(t, out), &doc), out)
	got := map[string]string{}
	for _, s := range doc.Sessions {
		got[s.ID] = s.Status
		assert.Empty(t, s.SecretHash, "hashes never leave the store")
		assert.Empty(t, s.TokenHash)
	}
	assert.Equal(t, map[string]string{
		"uis_active": "active", "uis_orphan": "token removed", "uis_other": "token removed",
	}, got, "idle-expired uis_old is hidden without --all")

	out, err = db.run("session", "list", "--all", "--principal", "ops", "--format", "json")
	require.NoError(t, err, out)
	doc = sessionListDoc{}
	require.NoError(t, json.Unmarshal(jsonDoc(t, out), &doc), out)
	require.Equal(t, 3, doc.Total)
	assert.Equal(t, "uis_active", doc.Sessions[0].ID, "newest first")
	assert.Equal(t, "expired", doc.Sessions[2].Status)
}

func TestSessionRevoke(t *testing.T) {
	db := sessionDB(t)
	ctx := context.Background()

	out, err := db.run("session", "revoke", "uis_active")
	require.NoError(t, err, out)
	assert.Contains(t, out, "Revoked uis_active")
	s, err := db.Driver.UISessions().Get(ctx, "uis_active")
	require.NoError(t, err)
	require.NotNil(t, s.RevokedAt)
	assert.Equal(t, authn.RevokeReasonRevoked, s.RevokeReason)

	_, err = db.run("session", "revoke", "uis_nope")
	assert.ErrorIs(t, err, storage.ErrNotFound)

	out, err = db.run("session", "revoke", "--principal", "ops", "--format", "json")
	require.NoError(t, err, out)
	assert.Contains(t, out, `"uis_orphan"`)
	assert.NotContains(t, out, `"uis_old"`, "an ended session is not revoked again")
	other, err := db.Driver.UISessions().Get(ctx, "uis_other")
	require.NoError(t, err)
	assert.Nil(t, other.RevokedAt, "another principal's session is untouched")

	_, err = db.run("session", "revoke")
	assert.Error(t, err, "revoke needs an ID or --principal")
	_, err = db.run("session", "revoke", "uis_other", "--principal", "ops")
	assert.Error(t, err, "not both")
}

// jsonDoc returns the JSON document in out, from its first brace.
func jsonDoc(t *testing.T, out string) []byte {
	t.Helper()
	i := strings.IndexByte(out, '{')
	require.GreaterOrEqual(t, i, 0, "no JSON in output: %s", out)
	return []byte(out[i:])
}
