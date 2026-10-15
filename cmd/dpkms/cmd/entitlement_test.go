package cmd

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	kitcli "hop.top/kit/go/console/cli"

	"github.com/ideacrafterslabs/ctxt/internal/storage"
)

type grantDoc struct {
	Action string `json:"action"`
	DryRun bool   `json:"dry_run"`
	NoOp   bool   `json:"no_op"`
	Result struct {
		Principal   string   `json:"principal"`
		Changed     bool     `json:"changed"`
		Created     bool     `json:"created"`
		Added       []string `json:"added"`
		Removed     []string `json:"removed"`
		Unchanged   []string `json:"unchanged"`
		Entitlement *struct {
			Kind       string    `json:"kind"`
			Namespaces []string  `json:"namespaces"`
			ExpiresAt  time.Time `json:"expires_at"`
		} `json:"entitlement"`
	} `json:"result"`
}

func runGrantJSON(t *testing.T, db *testDB, args ...string) grantDoc {
	t.Helper()
	out, err := db.run(append(append([]string{"entitlement"}, args...), "--format", "json")...)
	require.NoError(t, err, out)
	var doc grantDoc
	require.NoError(t, json.Unmarshal(jsonDoc(t, out), &doc), out)
	return doc
}

func inboundGrant(t *testing.T, db *testDB, principal string) *storage.RegistryEntitlement {
	t.Helper()
	rows, err := db.Driver.Entitlements().List(context.Background(), storage.EntitlementInbound)
	require.NoError(t, err)
	for _, r := range rows {
		if r.RegistryName == principal {
			return r
		}
	}
	return nil
}

func TestEntitlementGrantRevokeUngate(t *testing.T) {
	db := setupTestDB(t)

	doc := runGrantJSON(t, db, "grant", "phone", "ai.*", "devops.k8s", "--expires", "2030-01-31")
	assert.True(t, doc.Result.Created)
	assert.False(t, doc.NoOp)
	assert.Equal(t, []string{"ai.*", "devops.k8s"}, doc.Result.Added)
	require.NotNil(t, doc.Result.Entitlement)
	assert.Equal(t, "inbound", doc.Result.Entitlement.Kind)
	assert.Equal(t, time.Date(2030, 2, 1, 0, 0, 0, 0, time.UTC), doc.Result.Entitlement.ExpiresAt.UTC(),
		"a date expires at the end of that UTC day")

	doc = runGrantJSON(t, db, "grant", "phone", "ai.*")
	assert.True(t, doc.NoOp, "re-grant of a held namespace is a no-op")

	doc = runGrantJSON(t, db, "grant", "phone", "public.*", "--expires", "never")
	assert.Equal(t, []string{"public.*"}, doc.Result.Added)
	g := inboundGrant(t, db, "phone")
	require.NotNil(t, g)
	assert.Equal(t, []string{"ai.*", "devops.k8s", "public.*"}, g.Namespaces)
	assert.True(t, g.ExpiresAt.IsZero(), "--expires never clears the expiry")

	doc = runGrantJSON(t, db, "revoke", "phone", "devops.k8s", "med.*")
	assert.Equal(t, []string{"devops.k8s"}, doc.Result.Removed)
	assert.Equal(t, []string{"med.*"}, doc.Result.Unchanged)

	doc = runGrantJSON(t, db, "revoke", "phone", "ai.*", "public.*")
	assert.False(t, doc.NoOp)
	g = inboundGrant(t, db, "phone")
	require.NotNil(t, g, "revoking the last namespace keeps the grant")
	assert.Empty(t, g.Namespaces)

	out, err := db.run("entitlement", "list")
	require.NoError(t, err, out)
	assert.Contains(t, out, "every gated read denied")

	doc = runGrantJSON(t, db, "revoke", "nobody", "ai.*")
	assert.True(t, doc.NoOp, "revoke without a grant is a no-op")
	assert.Nil(t, inboundGrant(t, db, "nobody"))

	out, err = db.run("entitlement", "ungate", "phone", "--confirm=yes")
	require.NoError(t, err, out)
	assert.Contains(t, out, "unrestricted")
	assert.Nil(t, inboundGrant(t, db, "phone"))

	doc = runGrantJSON(t, db, "ungate", "phone", "--confirm=yes")
	assert.True(t, doc.NoOp, "ungate without a grant is a no-op")
}

func TestEntitlementDryRunWritesNothing(t *testing.T) {
	db := setupTestDB(t)

	doc := runGrantJSON(t, db, "grant", "phone", "ai.*", "--dry-run")
	assert.True(t, doc.DryRun)
	assert.True(t, doc.Result.Created)
	assert.Nil(t, inboundGrant(t, db, "phone"))

	runGrantJSON(t, db, "grant", "phone", "ai.*")
	runGrantJSON(t, db, "revoke", "phone", "ai.*", "--dry-run")
	out, err := db.run("entitlement", "ungate", "phone", "--dry-run", "--confirm=yes")
	require.NoError(t, err, out)
	assert.Contains(t, out, "[dry-run]")
	g := inboundGrant(t, db, "phone")
	require.NotNil(t, g)
	assert.Equal(t, []string{"ai.*"}, g.Namespaces)
}

// The verbs see inbound grants only: an outbound row (a remote
// registry's entitlement) named like a principal is neither listed nor
// touched.
func TestEntitlementIgnoresOutboundRows(t *testing.T) {
	db := setupTestDB(t)
	ctx := context.Background()
	require.NoError(t, db.Driver.Entitlements().Upsert(ctx, &storage.RegistryEntitlement{
		Kind: storage.EntitlementOutbound, RegistryName: "acme", Plan: "pro", Namespaces: []string{"*"},
	}))

	out, err := db.run("entitlement", "list", "--format", "json")
	require.NoError(t, err, out)
	assert.Contains(t, out, `"total": 0`)

	doc := runGrantJSON(t, db, "ungate", "acme", "--confirm=yes")
	assert.True(t, doc.NoOp)
	runGrantJSON(t, db, "grant", "acme", "ai.*")
	out2, err := db.Driver.Entitlements().Get(ctx, storage.EntitlementOutbound, "acme")
	require.NoError(t, err)
	assert.Equal(t, []string{"*"}, out2.Namespaces, "outbound row untouched by an inbound grant")
}

func TestEntitlementInputErrors(t *testing.T) {
	db := setupTestDB(t)
	for _, args := range [][]string{
		{"entitlement", "grant", "phone"},
		{"entitlement", "grant", "phone", "ai.["},
		{"entitlement", "grant", "phone", "ai.*", "--expires", "soonish"},
		{"entitlement", "revoke", "phone"},
		{"entitlement", "ungate"},
	} {
		_, err := db.run(args...)
		assert.Error(t, err, strings.Join(args, " "))
	}
}

// Ungate widens a principal's access to everything, so it sits behind
// kit's confirmation gate (destructive); the other verbs are writes or
// reads. The gate itself is kit's, wired by prepareTree.
func TestEntitlementSideEffects(t *testing.T) {
	for cmd, want := range map[*cobra.Command]kitcli.SideEffect{
		entitlementListCmd:   kitcli.SideEffectRead,
		entitlementGrantCmd:  kitcli.SideEffectWrite,
		entitlementRevokeCmd: kitcli.SideEffectWrite,
		entitlementUngateCmd: kitcli.SideEffectDestructive,
	} {
		got, ok := kitcli.GetSideEffect(cmd)
		assert.True(t, ok, cmd.Name())
		assert.Equal(t, want, got, cmd.Name())
	}
	assert.Contains(t, entitlementUngateCmd.Long, "UNRESTRICTED")
}
