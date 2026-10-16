package cmd

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	authn "github.com/ideacrafterslabs/ctxt/internal/auth"
	"github.com/ideacrafterslabs/ctxt/internal/storage"
)

// sessionDB is a test database holding the owner's sessions, minted by
// two of its keys, one of a person, and one old enough to be expired.
// One key is revoked, so the sessions it minted are ended by the cascade.
func sessionDB(t *testing.T) *testDB {
	t.Helper()
	db := setupTestDB(t)
	ctx := context.Background()
	auth := db.Driver.Auth()
	require.NoError(t, auth.Principals().Create(ctx, &storage.Principal{ID: "prn_other", Kind: storage.PrincipalKindPerson, DisplayName: "Other"}))
	identity := func(principal, subject string) string {
		i := &storage.Identity{PrincipalID: principal, Method: storage.IdentityMethodAPIKey, Subject: subject}
		require.NoError(t, auth.Identities().Create(ctx, i))
		return i.ID
	}
	laptop, phone, other := identity(storage.OwnerPrincipal, "laptop"), identity(storage.OwnerPrincipal, "phone"), identity("prn_other", "other")

	now := time.Now().UTC()
	for _, s := range []*storage.UISession{
		{ID: "uis_active", PrincipalID: storage.OwnerPrincipal, IdentityID: laptop, CreatedAt: now.Add(-time.Minute)},
		{ID: "uis_orphan", PrincipalID: storage.OwnerPrincipal, IdentityID: phone, CreatedAt: now.Add(-2 * time.Minute)},
		{ID: "uis_other", PrincipalID: "prn_other", IdentityID: other, CreatedAt: now.Add(-3 * time.Minute)},
		{ID: "uis_old", PrincipalID: storage.OwnerPrincipal, IdentityID: laptop, CreatedAt: now.Add(-48 * time.Hour)},
	} {
		s.SecretHash = "secret-" + s.ID
		s.Scope = authn.SessionKindUI
		s.LastSeenAt = s.CreatedAt
		s.IdleExpiresAt = s.CreatedAt.Add(12 * time.Hour)
		s.ExpiresAt = s.CreatedAt.Add(7 * 24 * time.Hour)
		require.NoError(t, db.Driver.UISessions().Create(ctx, s))
	}
	require.NoError(t, auth.Identities().Revoke(ctx, phone, now))
	return db
}

type sessionListDoc struct {
	Sessions []struct {
		ID          string `json:"id"`
		PrincipalID string `json:"principal_id"`
		Status      string `json:"status"`
		SecretHash  string `json:"secret_hash"`
		IdentityID  string `json:"identity_id"`
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
		assert.NotEmpty(t, s.IdentityID, "a listing names the key that signed the browser in")
	}
	assert.Equal(t, map[string]string{
		"uis_active": "active", "uis_other": "active",
	}, got, "idle-expired uis_old and the revoked key's uis_orphan are hidden without --all")

	out, err = db.run("session", "list", "--all", "--principal", storage.OwnerPrincipal, "--format", "json")
	require.NoError(t, err, out)
	doc = sessionListDoc{}
	require.NoError(t, json.Unmarshal(jsonDoc(t, out), &doc), out)
	require.Equal(t, 3, doc.Total)
	assert.Equal(t, "uis_active", doc.Sessions[0].ID, "newest first")
	assert.Equal(t, "revoked ("+storage.RevokeReasonIdentityRevoked+")", doc.Sessions[1].Status)
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

	minted := signInOwner(t, db, "uis_second")
	out, err = db.run("session", "revoke", "--principal", storage.OwnerPrincipal, "--format", "json")
	require.NoError(t, err, out)
	assert.Contains(t, out, `"`+minted+`"`)
	assert.NotContains(t, out, `"uis_orphan"`, "an ended session is not revoked again")
	assert.NotContains(t, out, `"uis_old"`, "an ended session is not revoked again")
	other, err := db.Driver.UISessions().Get(ctx, "uis_other")
	require.NoError(t, err)
	assert.Nil(t, other.RevokedAt, "another principal's session is untouched")

	_, err = db.run("session", "revoke")
	assert.Error(t, err, "revoke needs an ID or --principal")
	_, err = db.run("session", "revoke", "uis_other", "--principal", storage.OwnerPrincipal)
	assert.Error(t, err, "not both")
}

// jsonDoc returns the JSON document in out, from its first brace.
func jsonDoc(t *testing.T, out string) []byte {
	t.Helper()
	i := strings.IndexByte(out, '{')
	require.GreaterOrEqual(t, i, 0, "no JSON in output: %s", out)
	return []byte(out[i:])
}

// signInOwner stores one more active session of the owner with id.
func signInOwner(t *testing.T, db *testDB, id string) string {
	t.Helper()
	ctx := context.Background()
	idns, err := db.Driver.Auth().Identities().ListByPrincipal(ctx, storage.OwnerPrincipal)
	require.NoError(t, err)
	now := time.Now().UTC()
	require.NoError(t, db.Driver.UISessions().Create(ctx, &storage.UISession{
		ID: id, SecretHash: "secret-" + id, PrincipalID: storage.OwnerPrincipal, IdentityID: idns[0].ID, Scope: authn.SessionKindUI,
		CreatedAt: now, LastSeenAt: now, IdleExpiresAt: now.Add(12 * time.Hour), ExpiresAt: now.Add(7 * 24 * time.Hour),
	}))
	return id
}
