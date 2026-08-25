package grpc_test

import (
	"context"
	"net"
	"testing"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"

	authn "github.com/ideacrafterslabs/ctxt/internal/auth"
	"github.com/ideacrafterslabs/ctxt/internal/jobs"
	"github.com/ideacrafterslabs/ctxt/internal/pipeline/builtins"
	"github.com/ideacrafterslabs/ctxt/internal/registry"
	"github.com/ideacrafterslabs/ctxt/internal/search"
	"github.com/ideacrafterslabs/ctxt/internal/security"
	grpcserver "github.com/ideacrafterslabs/ctxt/internal/server/grpc"
	pb "github.com/ideacrafterslabs/ctxt/internal/server/grpc/pb"
	"github.com/ideacrafterslabs/ctxt/internal/service"
	"github.com/ideacrafterslabs/ctxt/internal/storage"
	"github.com/ideacrafterslabs/ctxt/internal/storageutil"
)

// newGatedGRPCServer mirrors the HTTP entitlement harness on the gRPC
// transport: an authenticated EntityService with the inbound gate and a
// threshold-1 security emitter, seeded with one entity in a granted
// namespace and one outside it. The test principal "ops" (token
// tok-valid) is granted "ai.*".
func newGatedGRPCServer(t *testing.T) (pb.EntityServiceClient, *registry.InboundGate, <-chan security.Alert) {
	t.Helper()
	driver := storageutil.NewTestDriver(t)
	q := jobs.NewQueue(driver.Jobs())
	svc := service.New(driver, q, builtins.Registry(), search.NewEngine(driver), "", nil)

	ctx := context.Background()
	now := time.Now().Truncate(time.Second)
	for slug, ns := range map[string]string{
		"ai.bert":    "ai.models",
		"med.claims": "med.records",
	} {
		if err := driver.Entities().Upsert(ctx, &storage.Entity{
			Slug: slug, Title: slug, Namespace: ns, CreatedAt: now, UpdatedAt: now,
		}); err != nil {
			t.Fatalf("seed entity %s: %v", slug, err)
		}
	}
	if err := driver.Entitlements().Upsert(ctx, &storage.RegistryEntitlement{
		RegistryName: "ops",
		Plan:         "inbound",
		Namespaces:   []string{"ai.*"},
		FetchedAt:    now,
	}); err != nil {
		t.Fatalf("seed entitlement: %v", err)
	}

	gate := registry.NewInboundGate(driver.Entitlements(), driver.Metering())
	em := security.New(security.Config{
		AuthFailureThreshold: 1,
		ACLDenialThreshold:   1,
		WindowDuration:       time.Minute,
	}, nil)
	alerts := make(chan security.Alert, 8)
	em.AddHandler(func(_ context.Context, a security.Alert) { alerts <- a })

	provider, err := authn.NewStatic([]authn.StaticToken{
		{Token: "tok-valid", Principal: "ops", Roles: []string{"admin"}},
	})
	if err != nil {
		t.Fatalf("NewStatic: %v", err)
	}

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	addr := ln.Addr().String()
	ln.Close() // release so the server can rebind

	srv := grpcserver.New(addr, svc,
		grpcserver.WithAuth(provider),
		grpcserver.WithSecurity(em),
		grpcserver.WithEntitlements(gate),
	)
	srvCtx, cancel := context.WithCancel(context.Background())
	go func() {
		if err := srv.Start(srvCtx); err != nil && srvCtx.Err() == nil {
			t.Logf("grpc server error: %v", err)
		}
	}()
	t.Cleanup(cancel)

	conn, err := grpc.NewClient(addr,
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	t.Cleanup(func() { conn.Close() })

	return pb.NewEntityServiceClient(conn), gate, alerts
}

// opsCtx returns an outgoing context authenticated as principal "ops".
func opsCtx() context.Context {
	return metadata.NewOutgoingContext(context.Background(),
		metadata.Pairs("authorization", "Bearer tok-valid"))
}

func waitGRPCAlert(t *testing.T, ch <-chan security.Alert) security.Alert {
	t.Helper()
	select {
	case a := <-ch:
		return a
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for security alert")
		return security.Alert{}
	}
}

func TestGRPCEntityGateAllowsGrantedNamespace(t *testing.T) {
	client, _, _ := newGatedGRPCServer(t)

	e, err := client.GetEntity(opsCtx(), &pb.GetEntityRequest{Slug: "ai.bert"}, grpc.WaitForReady(true))
	if err != nil {
		t.Fatalf("GetEntity granted: %v", err)
	}
	if e.Slug != "ai.bert" {
		t.Errorf("slug = %q, want ai.bert", e.Slug)
	}
}

func TestGRPCEntityGateDeniesUngrantedNamespace(t *testing.T) {
	client, _, alerts := newGatedGRPCServer(t)

	_, err := client.GetEntity(opsCtx(), &pb.GetEntityRequest{Slug: "med.claims"}, grpc.WaitForReady(true))
	if status.Code(err) != codes.PermissionDenied {
		t.Fatalf("code = %v, want PermissionDenied (err: %v)", status.Code(err), err)
	}

	a := waitGRPCAlert(t, alerts)
	if a.Kind != security.EventACLDenial {
		t.Errorf("alert kind = %q, want %q", a.Kind, security.EventACLDenial)
	}
	if a.Principal != "ops" {
		t.Errorf("alert principal = %q, want ops", a.Principal)
	}
}

func TestGRPCEntityGateFiltersListing(t *testing.T) {
	client, _, _ := newGatedGRPCServer(t)

	resp, err := client.ListEntities(opsCtx(), &pb.ListEntitiesRequest{}, grpc.WaitForReady(true))
	if err != nil {
		t.Fatalf("ListEntities: %v", err)
	}
	if len(resp.Entities) != 1 {
		t.Fatalf("listing has %d entities, want 1 (ungranted namespaces excluded)", len(resp.Entities))
	}
	if resp.Entities[0].Slug != "ai.bert" {
		t.Errorf("slug = %q, want ai.bert", resp.Entities[0].Slug)
	}
}

func TestGRPCEntityGateQuotaExhausted(t *testing.T) {
	client, gate, alerts := newGatedGRPCServer(t)
	gate.SetQuota("ops", storage.MeteringEventEntityResolve, storage.QuotaConfig{Limit: 1})

	if _, err := client.GetEntity(opsCtx(), &pb.GetEntityRequest{Slug: "ai.bert"}, grpc.WaitForReady(true)); err != nil {
		t.Fatalf("first resolve within quota: %v", err)
	}

	_, err := client.GetEntity(opsCtx(), &pb.GetEntityRequest{Slug: "ai.bert"}, grpc.WaitForReady(true))
	if status.Code(err) != codes.ResourceExhausted {
		t.Fatalf("code = %v, want ResourceExhausted (err: %v)", status.Code(err), err)
	}

	a := waitGRPCAlert(t, alerts)
	if a.Kind != security.EventQuotaExhausted {
		t.Errorf("alert kind = %q, want %q", a.Kind, security.EventQuotaExhausted)
	}
	if a.Principal != "ops" {
		t.Errorf("alert principal = %q, want ops", a.Principal)
	}
}

func TestGRPCEntityGateBacklinksDenied(t *testing.T) {
	client, _, _ := newGatedGRPCServer(t)

	_, err := client.GetEntityBacklinks(opsCtx(), &pb.GetEntityBacklinksRequest{Slug: "med.claims"}, grpc.WaitForReady(true))
	if status.Code(err) != codes.PermissionDenied {
		t.Fatalf("code = %v, want PermissionDenied (err: %v)", status.Code(err), err)
	}
}

// An entity the store cannot resolve must fail closed: the gate is
// never skipped, so backlinks are refused rather than served ungated.
func TestGRPCEntityGateBacklinksUnknownEntityFailsClosed(t *testing.T) {
	client, _, _ := newGatedGRPCServer(t)

	_, err := client.GetEntityBacklinks(opsCtx(), &pb.GetEntityBacklinksRequest{Slug: "ghost"}, grpc.WaitForReady(true))
	if status.Code(err) != codes.NotFound {
		t.Fatalf("code = %v, want NotFound (err: %v)", status.Code(err), err)
	}
}

// Backlinks in a granted namespace stay served (empty set is fine).
func TestGRPCEntityGateBacklinksAllowed(t *testing.T) {
	client, _, _ := newGatedGRPCServer(t)

	resp, err := client.GetEntityBacklinks(opsCtx(), &pb.GetEntityBacklinksRequest{Slug: "ai.bert"}, grpc.WaitForReady(true))
	if err != nil {
		t.Fatalf("GetEntityBacklinks granted: %v", err)
	}
	if resp == nil {
		t.Fatal("nil response")
	}
}
