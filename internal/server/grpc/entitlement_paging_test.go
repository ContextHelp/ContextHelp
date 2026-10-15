package grpc_test

import (
	"context"
	"fmt"
	"reflect"
	"testing"
	"time"

	"google.golang.org/grpc"

	pb "github.com/ideacrafterslabs/ctxt/internal/server/grpc/pb"
	"github.com/ideacrafterslabs/ctxt/internal/storage"
)

// seedGRPCPagingEntities adds five ungranted entities that sort ahead of
// every granted one, and five more granted ones, to the gated fixture
// (which already holds ai.bert in ai.models and med.claims in
// med.records). "ops" is entitled to ai.bert plus ai.m1..ai.m5.
func seedGRPCPagingEntities(t *testing.T, driver storage.StorageDriver) {
	t.Helper()
	now := time.Now().Truncate(time.Second)
	for i := 1; i <= 5; i++ {
		for _, e := range []*storage.Entity{
			{Slug: fmt.Sprintf("aa.x%d", i), Namespace: "aa.restricted"},
			{Slug: fmt.Sprintf("ai.m%d", i), Namespace: "ai.models"},
		} {
			e.Title, e.CreatedAt, e.UpdatedAt = e.Slug, now, now
			if err := driver.Entities().Upsert(context.Background(), e); err != nil {
				t.Fatalf("seed %s: %v", e.Slug, err)
			}
		}
	}
}

// walkGRPCEntityPages lists entities as token, page by page until a short
// page, returning every page's slugs and checking Total matches each page.
func walkGRPCEntityPages(t *testing.T, client pb.EntityServiceClient, token string, limit int32) [][]string {
	t.Helper()
	var pages [][]string
	for offset := int32(0); ; offset += limit {
		if offset >= 100 {
			t.Fatal("paging never ended")
		}
		resp, err := client.ListEntities(tokenCtx(token), &pb.ListEntitiesRequest{Limit: limit, Offset: offset}, grpc.WaitForReady(true))
		if err != nil {
			t.Fatalf("ListEntities(offset %d): %v", offset, err)
		}
		page := make([]string, 0, len(resp.Entities))
		for _, e := range resp.Entities {
			page = append(page, e.Slug)
		}
		if int(resp.Total) != len(page) {
			t.Errorf("offset %d: Total = %d, want %d", offset, resp.Total, len(page))
		}
		pages = append(pages, page)
		if int32(len(page)) < limit {
			return pages
		}
	}
}

// A gated principal's page holds limit entitled entities whenever that
// many exist past the offset, and walking the offsets visits every
// entitled entity exactly once — mirroring the HTTP listing.
func TestGRPCEntityGateListingPagesOverEntitledSet(t *testing.T) {
	h := startGatedGRPC(t)
	seedGRPCPagingEntities(t, h.driver)

	got := walkGRPCEntityPages(t, pb.NewEntityServiceClient(h.conn), "tok-valid-fixture-0123456789abcdef0123456789", 2)
	want := [][]string{
		{"ai.bert", "ai.m1"},
		{"ai.m2", "ai.m3"},
		{"ai.m4", "ai.m5"},
		{},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("pages = %v, want %v", got, want)
	}
}

// The admin owner bypasses the gate: the same walk visits every entity.
func TestGRPCEntityGateListingAdminPagesOverEverything(t *testing.T) {
	h := startGatedGRPC(t)
	seedGRPCPagingEntities(t, h.driver)

	got := walkGRPCEntityPages(t, pb.NewEntityServiceClient(h.conn),
		"tok-admin-fixture-0123456789abcdef0123456789", 5)
	want := [][]string{
		{"aa.x1", "aa.x2", "aa.x3", "aa.x4", "aa.x5"},
		{"ai.bert", "ai.m1", "ai.m2", "ai.m3", "ai.m4"},
		{"ai.m5", "med.claims"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("pages = %v, want %v", got, want)
	}
}
