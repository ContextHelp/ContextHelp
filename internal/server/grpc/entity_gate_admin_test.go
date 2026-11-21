package grpc_test

import (
	"context"
	"strings"
	"testing"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	pb "github.com/ideacrafterslabs/ctxt/internal/server/grpc/pb"
	"github.com/ideacrafterslabs/ctxt/internal/storage"
)

// The non-owner administrators of the gated gRPC fixture: a device and
// a person holding the admin role, each granted every object and "ai.*".
const (
	grpcAdminDeviceID = "prn_grpc_admin_device"
	grpcAdminPersonID = "prn_grpc_admin_person"
)

// grpcAdminTokens maps each non-owner administrator to its bearer token.
var grpcAdminTokens = map[string]string{
	grpcAdminDeviceID: "tok-admin-device-fixture-0123456789abcdef0123",
	grpcAdminPersonID: "tok-admin-person-fixture-0123456789abcdef0123",
}

// Administering an instance is not access to its data (ADR-082 §10, ADR-083 §10):
// over gRPC a non-owner administrator is gated like any other consumer.
// The entity outside its ai.* grant is refused and left out of the
// listing, and the reads inside it are metered and held to its quota.
func TestGRPCEntityGateHoldsNonOwnerAdmins(t *testing.T) {
	h := startGatedGRPC(t)
	client := pb.NewEntityServiceClient(h.conn)
	for _, id := range []string{grpcAdminDeviceID, grpcAdminPersonID} {
		h.gate.SetQuota(id, storage.MeteringEventEntityResolve, storage.QuotaConfig{Limit: 1})
		ctx := tokenCtx(grpcAdminTokens[id])

		_, err := client.GetEntity(ctx, &pb.GetEntityRequest{Slug: "med.claims"}, grpc.WaitForReady(true))
		assertEntitlementRequired(t, id+" GetEntity outside grant", err)
		_, err = client.GetEntityBacklinks(ctx, &pb.GetEntityBacklinksRequest{Slug: "med.claims"}, grpc.WaitForReady(true))
		assertEntitlementRequired(t, id+" backlinks outside grant", err)

		list, err := client.ListEntities(ctx, &pb.ListEntitiesRequest{}, grpc.WaitForReady(true))
		if err != nil {
			t.Fatalf("%s ListEntities: %v", id, err)
		}
		if len(list.Entities) != 1 || list.Entities[0].Slug != "ai.bert" {
			t.Errorf("%s listing = %v, want only ai.bert", id, list.Entities)
		}

		if _, err := client.GetEntity(ctx, &pb.GetEntityRequest{Slug: "ai.bert"}, grpc.WaitForReady(true)); err != nil {
			t.Fatalf("%s GetEntity inside grant: %v", id, err)
		}
		_, err = client.GetEntity(ctx, &pb.GetEntityRequest{Slug: "ai.bert"}, grpc.WaitForReady(true))
		if status.Code(err) != codes.ResourceExhausted {
			t.Errorf("%s GetEntity past quota: code = %v, want ResourceExhausted (err: %v)", id, status.Code(err), err)
		}

		events, err := h.driver.Metering().List(context.Background(), storage.MeteringFilter{RegistryName: id})
		if err != nil {
			t.Fatalf("metering list: %v", err)
		}
		if len(events) != 1 || events[0].EventType != storage.MeteringEventEntityResolve || events[0].Count != 1 {
			t.Errorf("%s metering = %+v, want one entity_resolve", id, events)
		}
	}
}

func assertEntitlementRequired(t *testing.T, what string, err error) {
	t.Helper()
	if status.Code(err) != codes.PermissionDenied || !strings.Contains(err.Error(), "ENTITLEMENT_REQUIRED") {
		t.Errorf("%s: err = %v, want PermissionDenied ENTITLEMENT_REQUIRED", what, err)
	}
}
