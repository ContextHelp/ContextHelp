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

// gRPC enforces the same scopes as HTTP: a reader cannot analyze; a
// writer and an admin can.
func TestGRPCAnalyzeScopes(t *testing.T) {
	h := startGatedGRPC(t)
	client := pb.NewAnalyzeServiceClient(h.conn)
	req := &pb.AnalyzeRequest{Content: "scope test", Type: "text", Source: "test"}

	_, err := client.Analyze(tokenCtx("tok-valid"), req, grpc.WaitForReady(true))
	if status.Code(err) != codes.PermissionDenied {
		t.Fatalf("reader Analyze: code = %v, want PermissionDenied (err: %v)", status.Code(err), err)
	}
	if !strings.Contains(err.Error(), "write:objects") {
		t.Errorf("reader Analyze error %q does not name write:objects", err)
	}

	for _, token := range []string{"tok-writer", "tok-admin"} {
		if _, err := client.Analyze(tokenCtx(token), req, grpc.WaitForReady(true)); err != nil {
			t.Errorf("%s Analyze: %v", token, err)
		}
	}

	_, err = client.Analyze(tokenCtx("tok-nope"), req, grpc.WaitForReady(true))
	if status.Code(err) != codes.Unauthenticated {
		t.Errorf("invalid token: code = %v, want Unauthenticated", status.Code(err))
	}
}

// The admin owner skips the entity gate over gRPC too: no entitlement
// filter, no metering, no quota. The reader stays gated and metered.
func TestGRPCAdminBypassesEntityGate(t *testing.T) {
	h := startGatedGRPC(t)
	h.gate.SetQuota("owner", storage.MeteringEventEntityResolve, storage.QuotaConfig{Limit: 1})
	client := pb.NewEntityServiceClient(h.conn)
	admin := tokenCtx("tok-admin")

	for i := 0; i < 2; i++ {
		if _, err := client.GetEntity(admin, &pb.GetEntityRequest{Slug: "med.claims"}, grpc.WaitForReady(true)); err != nil {
			t.Fatalf("admin GetEntity %d: %v", i+1, err)
		}
	}
	list, err := client.ListEntities(admin, &pb.ListEntitiesRequest{}, grpc.WaitForReady(true))
	if err != nil {
		t.Fatalf("admin ListEntities: %v", err)
	}
	if len(list.Entities) != 2 {
		t.Errorf("admin listing has %d entities, want 2", len(list.Entities))
	}
	if _, err := client.GetEntityBacklinks(admin, &pb.GetEntityBacklinksRequest{Slug: "med.claims"}, grpc.WaitForReady(true)); err != nil {
		t.Errorf("admin backlinks: %v", err)
	}

	events, err := h.driver.Metering().List(context.Background(), storage.MeteringFilter{RegistryName: "owner"})
	if err != nil {
		t.Fatalf("metering list: %v", err)
	}
	if len(events) != 0 {
		t.Errorf("admin reads recorded %d metering events, want 0", len(events))
	}

	if _, err := client.GetEntity(opsCtx(), &pb.GetEntityRequest{Slug: "ai.bert"}, grpc.WaitForReady(true)); err != nil {
		t.Fatalf("reader GetEntity: %v", err)
	}
	events, err = h.driver.Metering().List(context.Background(), storage.MeteringFilter{RegistryName: "ops"})
	if err != nil {
		t.Fatalf("metering list: %v", err)
	}
	if len(events) != 1 {
		t.Errorf("reader read recorded %d metering events, want 1", len(events))
	}
	_, err = client.GetEntity(opsCtx(), &pb.GetEntityRequest{Slug: "med.claims"}, grpc.WaitForReady(true))
	if status.Code(err) != codes.PermissionDenied {
		t.Errorf("reader outside grant: code = %v, want PermissionDenied", status.Code(err))
	}
}
