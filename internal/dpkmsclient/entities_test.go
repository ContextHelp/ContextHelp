package dpkmsclient_test

import (
	"context"
	"errors"
	"net/http"
	"slices"
	"testing"
	"time"

	"github.com/ideacrafterslabs/ctxt/internal/dpkmsclient"
	"github.com/ideacrafterslabs/ctxt/internal/dpkmstest"
	"github.com/ideacrafterslabs/ctxt/internal/storage"
	"github.com/ideacrafterslabs/ctxt/internal/storageutil"
)

// entityFixture seeds two "checkout" entities (one in namespace zz), one
// with an alias, and an object mentioning ui.cart.
func entityFixture(t *testing.T) storage.StorageDriver {
	t.Helper()
	ctx := context.Background()
	drv := storageutil.NewTestDriver(t)
	now := time.Now().Truncate(time.Second)
	for _, e := range []*storage.Entity{
		{Slug: "ui.checkout-flow", Title: "Checkout Flow", Namespace: "ui"},
		{Slug: "ui.cart", Title: "Shopping Cart", Namespace: "ui", Aliases: []string{"basket"}, Description: "Where items wait."},
		{Slug: "zz.late", Title: "Late CHECKOUT page", Namespace: "zz"},
	} {
		e.CreatedAt, e.UpdatedAt = now, now
		if err := drv.Entities().Upsert(ctx, e); err != nil {
			t.Fatal(err)
		}
	}
	if err := drv.Objects().Create(ctx, &storage.KnowledgeObject{
		ID: "o-cart", Type: "note", TextContent: "the cart page", CreatedAt: now, UpdatedAt: now,
	}); err != nil {
		t.Fatal(err)
	}
	if err := drv.Edges().Create(ctx, &storage.Edge{
		ID: "e-cart", FromType: "object", FromID: "o-cart", ToType: "entity", ToID: "ctxt://entity/ui/cart",
		EdgeType: "mentions", Weight: 1, CreatedAt: now,
	}); err != nil {
		t.Fatal(err)
	}
	return drv
}

func entitySlugs(es []*storage.Entity) []string {
	out := make([]string, 0, len(es))
	for _, e := range es {
		out = append(out, e.Slug)
	}
	return out
}

// Each entity read against a reader token on a protected instance.
func TestEntities_Reader(t *testing.T) {
	srv := dpkmstest.Start(t, entityFixture(t), dpkmstest.WithStaticTokens())
	c := newClient(t, dpkmsclient.Endpoint{URL: srv.URL, Token: srv.Token(dpkmstest.RoleReader)})
	ctx := context.Background()

	for _, tc := range []struct {
		q    dpkmsclient.EntityQuery
		want []string
	}{
		{dpkmsclient.EntityQuery{}, []string{"ui.cart", "ui.checkout-flow", "zz.late"}},
		{dpkmsclient.EntityQuery{Query: "checkout"}, []string{"ui.checkout-flow", "zz.late"}},
		{dpkmsclient.EntityQuery{Query: "BASKET"}, []string{"ui.cart"}},
		{dpkmsclient.EntityQuery{Query: "checkout", Namespace: "zz"}, []string{"zz.late"}},
		{dpkmsclient.EntityQuery{Limit: 1, Offset: 1}, []string{"ui.checkout-flow"}},
		{dpkmsclient.EntityQuery{Query: "nothing"}, []string{}},
	} {
		got, err := c.ListEntities(ctx, tc.q)
		if err != nil {
			t.Fatalf("ListEntities(%+v): %v", tc.q, err)
		}
		if g := entitySlugs(got); !slices.Equal(g, tc.want) {
			t.Errorf("ListEntities(%+v) = %v; want %v", tc.q, g, tc.want)
		}
	}

	e, err := c.GetEntity(ctx, "ui.cart")
	if err != nil || e.Slug != "ui.cart" || e.Description != "Where items wait." {
		t.Fatalf("GetEntity: %+v, %v", e, err)
	}

	for mention, want := range map[string]string{"ui.cart": "ui.cart", "basket": "ui.cart"} {
		e, err := c.ResolveEntity(ctx, mention)
		if err != nil || e.Slug != want {
			t.Fatalf("ResolveEntity(%q): %+v, %v", mention, e, err)
		}
	}

	objs, err := c.EntityBacklinks(ctx, "ui.cart")
	if err != nil || len(objs) != 1 || objs[0].ID != "o-cart" {
		t.Fatalf("EntityBacklinks: %+v, %v", objs, err)
	}

	obj, err := c.GetObject(ctx, "o-cart")
	if err != nil || obj.ID != "o-cart" || obj.TextContent != "the cart page" {
		t.Fatalf("GetObject: %+v, %v", obj, err)
	}
}

// A miss is NOT_FOUND (exit 3), a missing token UNAUTHORIZED (exit 5) and
// an empty mention USAGE (exit 2).
func TestEntities_Errors(t *testing.T) {
	srv := dpkmstest.Start(t, entityFixture(t), dpkmstest.WithStaticTokens())
	reader := newClient(t, dpkmsclient.Endpoint{URL: srv.URL, Token: srv.Token(dpkmstest.RoleReader)})
	anon := newClient(t, dpkmsclient.Endpoint{URL: srv.URL})
	ctx := context.Background()

	var re *dpkmsclient.RemoteError
	misses := map[string]func() error{
		"GetEntity":     func() error { _, err := reader.GetEntity(ctx, "no.such"); return err },
		"ResolveEntity": func() error { _, err := reader.ResolveEntity(ctx, "no.such"); return err },
		"GetObject":     func() error { _, err := reader.GetObject(ctx, "o-none"); return err },
	}
	for name, call := range misses {
		err := call()
		if e := envelope(t, err); e.ExitCode != 3 || !errors.As(err, &re) || re.StatusCode != http.StatusNotFound {
			t.Errorf("%s miss: exit=%d err=%v", name, e.ExitCode, err)
		}
	}

	unauth := map[string]func() error{
		"ListEntities":    func() error { _, err := anon.ListEntities(ctx, dpkmsclient.EntityQuery{Query: "cart"}); return err },
		"GetEntity":       func() error { _, err := anon.GetEntity(ctx, "ui.cart"); return err },
		"ResolveEntity":   func() error { _, err := anon.ResolveEntity(ctx, "basket"); return err },
		"EntityBacklinks": func() error { _, err := anon.EntityBacklinks(ctx, "ui.cart"); return err },
		"GetObject":       func() error { _, err := anon.GetObject(ctx, "o-cart"); return err },
	}
	for name, call := range unauth {
		err := call()
		if e := envelope(t, err); e.ExitCode != 5 || !errors.As(err, &re) || re.StatusCode != http.StatusUnauthorized {
			t.Errorf("%s without a token: exit=%d err=%v", name, e.ExitCode, err)
		}
	}

	_, err := reader.ResolveEntity(ctx, "")
	if e := envelope(t, err); e.ExitCode != 2 || !errors.As(err, &re) || re.Code != "INVALID_PARAM" {
		t.Errorf("empty mention: exit=%d err=%v", e.ExitCode, err)
	}
}
