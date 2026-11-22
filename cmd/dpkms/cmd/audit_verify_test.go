package cmd

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	authn "github.com/ideacrafterslabs/ctxt/internal/auth"
	"github.com/ideacrafterslabs/ctxt/internal/sharing/sharingtest"
	"github.com/ideacrafterslabs/ctxt/internal/storage"
	"github.com/ideacrafterslabs/ctxt/internal/storage/sqlite"
)

// `dpkms audit verify` passes a chain that holds and fails, naming the
// first position, on one that was edited or lost an entry in the middle.
func TestAuditVerify(t *testing.T) {
	db := setupTestDB(t)
	ctx := context.Background()
	log := db.Driver.Sharing().AccessLog()
	now := time.Now()
	sharingtest.EnsurePrincipal(t, db.Driver, "prn_steward", storage.PrincipalKindPerson, authn.RoleReader, authn.RoleStewardRequester)
	grant := sharingtest.GrantSteward(t, db.Driver, "prn_steward", `{"policies":[{"data":"type==contact"}]}`, now.Add(-time.Hour), now.Add(time.Hour))

	out, err := db.run("audit", "verify")
	require.NoError(t, err)
	assert.Contains(t, out, "0 entries", "an empty chain holds")

	for i := 0; i < 4; i++ {
		require.NoError(t, log.AppendSteward(ctx, &storage.AccessEntry{
			PrincipalID: "prn_steward", Route: "GET /objects", Decision: storage.AccessDecisionAllowed,
			OperationDigest: storage.DigestPrefix + "00", ObjectCount: 1, ObjectIDs: []string{"obj"}, StewardGrantID: grant.ID,
		}))
	}
	out, err = db.run("audit", "verify")
	require.NoError(t, err)
	assert.Contains(t, out, "4 entries")

	raw := db.Driver.(*sqlite.Driver).DB()
	_, err = raw.ExecContext(ctx, `UPDATE access_log SET route = 'GET /entities' WHERE chain_seq = 3`)
	require.NoError(t, err)
	_, err = db.run("audit", "verify")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "broken at position 3")

	_, err = raw.ExecContext(ctx, `UPDATE access_log SET route = 'GET /objects' WHERE chain_seq = 3`)
	require.NoError(t, err)
	_, err = raw.ExecContext(ctx, `DELETE FROM access_log WHERE chain_seq = 2`)
	require.NoError(t, err)
	_, err = db.run("audit", "verify")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "broken at position 2")
}
