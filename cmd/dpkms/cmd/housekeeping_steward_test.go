package cmd

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	authn "github.com/ideacrafterslabs/ctxt/internal/auth"
	"github.com/ideacrafterslabs/ctxt/internal/sharing/sharingtest"
	"github.com/ideacrafterslabs/ctxt/internal/storage"
)

// Steward audit entries (ADR-083 §5) are exempt from
// sharing.audit.retention: a pass with a one-day retention prunes the
// ordinary lists of a year-old read and keeps the steward ones, the
// chain and the grant. steward.audit_retention prunes the steward
// lists; the entries, their chain and the grant still stay.
func TestHousekeepingRunKeepsStewardAudit(t *testing.T) {
	db := setupTestDB(t)
	ctx := context.Background()
	log := db.Driver.Sharing().AccessLog()
	old := time.Now().Add(-365 * 24 * time.Hour)

	sharingtest.EnsurePrincipal(t, db.Driver, "prn_steward", storage.PrincipalKindPerson, authn.RoleReader, authn.RoleStewardRequester)
	grant := sharingtest.GrantSteward(t, db.Driver, "prn_steward", `{"policies":[{"data":"type==contact"}]}`, old.Add(-time.Hour), old.Add(time.Hour))
	require.NoError(t, log.Append(ctx, []*storage.AccessEntry{{
		At: old, PrincipalID: "prn_colleague", Route: "GET /objects", Decision: storage.AccessDecisionAllowed,
		OperationDigest: storage.DigestPrefix + "00", ObjectCount: 2, ObjectIDs: []string{"obj-a", "obj-b"},
	}}))
	require.NoError(t, log.AppendSteward(ctx, &storage.AccessEntry{
		At: old, PrincipalID: "prn_steward", Route: "GET /objects", Decision: storage.AccessDecisionAllowed,
		OperationDigest: storage.DigestPrefix + "00", ObjectCount: 2, ObjectIDs: []string{"obj-a", "obj-b"}, StewardGrantID: grant.ID,
	}))
	writeConfig := func(extra string) {
		t.Helper()
		f, err := os.OpenFile(db.ConfigPath, os.O_APPEND|os.O_WRONLY, 0o600)
		require.NoError(t, err)
		_, err = f.WriteString(extra)
		require.NoError(t, err)
		require.NoError(t, f.Close())
	}
	lists := func() (ordinary, steward []string) {
		t.Helper()
		got, err := log.Query(ctx, storage.AccessFilter{})
		require.NoError(t, err)
		require.Len(t, got, 2, "housekeeping never deletes an entry")
		for _, e := range got {
			assert.Equal(t, 2, e.ObjectCount, "counts stay")
			if e.StewardGrantID != "" {
				steward = e.ObjectIDs
			} else {
				ordinary = e.ObjectIDs
			}
		}
		_, err = db.Driver.Sharing().StewardGrants().Get(ctx, grant.ID)
		require.NoError(t, err, "housekeeping never deletes a steward grant")
		return ordinary, steward
	}

	writeConfig("sharing:\n  audit:\n    retention: 24h\n")
	_, err := db.run("housekeeping", "run")
	require.NoError(t, err)
	ordinary, steward := lists()
	assert.Empty(t, ordinary, "the ordinary list is past sharing.audit.retention")
	assert.Equal(t, []string{"obj-a", "obj-b"}, steward, "sharing.audit.retention does not govern steward lists")
	head, entries, err := log.StewardChain(ctx)
	require.NoError(t, err)
	require.NoError(t, storage.VerifyAccessChain(head, entries, time.Time{}))

	writeConfig("steward:\n  audit_retention: 720h\n")
	_, err = db.run("housekeeping", "run")
	require.NoError(t, err)
	_, steward = lists()
	assert.Empty(t, steward, "steward.audit_retention prunes the steward lists")
	head, entries, err = log.StewardChain(ctx)
	require.NoError(t, err)
	require.NoError(t, storage.VerifyAccessChain(head, entries, time.Now().Add(-720*time.Hour)), "the chain still verifies with the retention cut")
}
