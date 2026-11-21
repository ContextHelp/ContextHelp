package leaktest

import (
	"context"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"

	pb "github.com/ideacrafterslabs/ctxt/internal/server/grpc/pb"
	"github.com/ideacrafterslabs/ctxt/internal/sharing/sharingtest"
	"github.com/ideacrafterslabs/ctxt/internal/storage"
	"github.com/ideacrafterslabs/ctxt/internal/storageutil"
)

// A grant opens the entity routes to an administrator; it does not
// carry it past the namespace entitlement gate. With every object
// shared and an inbound entitlement to "org" only, the key and the
// session administrator are answered like any other consumer: the
// person entity is refused on every entity read, over HTTP and gRPC,
// and left out of the listing; the org entity is served and metered.
// Only the owner bypasses the gate.
func TestAdminEntityGateHoldsGrantedAdmins(t *testing.T) {
	adminEntityGateHoldsGrantedAdmins(t, storageutil.NewTestDriver)
}

func adminEntityGateHoldsGrantedAdmins(t *testing.T, newDriver driverFunc) {
	f := Start(t, newDriver(t))
	ctx := context.Background()
	driver := f.Server.Driver
	sharingtest.GrantEverything(t, driver, AdminKeyID, AdminDeviceID, AdminSessionID)
	for _, id := range []string{AdminKeyID, AdminDeviceID, AdminSessionID} {
		require.NoError(t, driver.Entitlements().Upsert(ctx, &storage.RegistryEntitlement{
			Kind: storage.EntitlementInbound, RegistryName: id, Plan: "inbound", Namespaces: []string{"org"}, FetchedAt: time.Now(),
		}))
	}

	for _, p := range f.Admins() {
		for _, path := range []string{
			"/entities/" + EntitySarah,
			"/entities/resolve?mention=" + EntitySarah,
			"/entities/" + EntitySarah + "/backlinks",
		} {
			r := fetch(t, f, p, path)
			assert.Equal(t, http.StatusForbidden, r.Status, "%s GET %s: %s", p.Name, path, r.Body)
			assert.Contains(t, string(r.Body), "ENTITLEMENT_REQUIRED", "%s GET %s", p.Name, path)
		}

		list := fetch(t, f, p, "/entities?limit=100")
		require.Equal(t, http.StatusOK, list.Status, "%s: %s", p.Name, list.Body)
		assert.Contains(t, string(list.Body), EntityAcme, "%s lists the entity it is entitled to", p.Name)
		assert.NotContains(t, string(list.Body), EntitySarah, "%s lists an entity outside its entitlement", p.Name)

		acme := fetch(t, f, p, "/entities/"+EntityAcme)
		assert.Equal(t, http.StatusOK, acme.Status, "%s GET %s: %s", p.Name, EntityAcme, acme.Body)
		events, err := driver.Metering().List(ctx, storage.MeteringFilter{RegistryName: p.ID})
		require.NoError(t, err)
		assert.Len(t, events, 1, "%s entity read is metered", p.Name)
	}

	for _, name := range []string{NameAdminKey, NameAdminDevice} {
		key := f.Admin(name)
		_, err := pb.NewEntityServiceClient(f.Conn).GetEntity(f.RPCContext(ctx, key), &pb.GetEntityRequest{Slug: EntitySarah})
		assert.Equal(t, codes.PermissionDenied, codeOf(err), "%s: %v", name, err)
		assert.True(t, err != nil && strings.Contains(err.Error(), "ENTITLEMENT_REQUIRED"), "%s: %v", name, err)
	}

	// The owner holds no entitlement row and bypasses the gate anyway.
	assert.Equal(t, http.StatusOK, fetch(t, f, f.Owner(), "/entities/"+EntitySarah).Status)
}
