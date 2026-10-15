package storagetest

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/ideacrafterslabs/ctxt/internal/storage"
)

// EntitlementConformance pins the EntitlementStore contract every
// driver implements: rows keyed by (kind, name), kind-scoped get, list
// and delete, a refused invalid kind, and an empty namespace list kept
// as a row rather than dropped.
func EntitlementConformance(t *testing.T, drv storage.StorageDriver) {
	t.Helper()
	st := drv.Entitlements()
	if st == nil {
		t.Fatal("Entitlements() returned nil")
	}

	for _, c := range []struct {
		name string
		fn   func(*testing.T, storage.EntitlementStore)
	}{
		{"round trip keeps kind and fields", entConfRoundTrip},
		{"kinds with the same name are separate rows", entConfKindsAreSeparate},
		{"list returns one kind ordered by name", entConfListByKind},
		{"delete removes one kind's row", entConfDelete},
		{"delete of a missing row is not found", entConfDeleteMissing},
		{"invalid kind is refused", entConfInvalidKind},
		{"empty namespace list is kept", entConfEmptyNamespaces},
	} {
		t.Run(c.name, func(t *testing.T) {
			c.fn(t, st)
		})
	}
}

func entConfUpsert(t *testing.T, st storage.EntitlementStore, e *storage.RegistryEntitlement) {
	t.Helper()
	if err := st.Upsert(context.Background(), e); err != nil {
		t.Fatalf("Upsert(%s %s): %v", e.Kind, e.RegistryName, err)
	}
}

func entConfRoundTrip(t *testing.T, st storage.EntitlementStore) {
	ctx := context.Background()
	expires := time.Date(2030, 1, 2, 3, 4, 5, 0, time.UTC)
	entConfUpsert(t, st, &storage.RegistryEntitlement{
		Kind: storage.EntitlementInbound, RegistryName: "rt-principal", Plan: "pro",
		Namespaces: []string{"ai.*", "devops.k8s"}, ExpiresAt: expires,
	})
	got, err := st.Get(ctx, storage.EntitlementInbound, "rt-principal")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.Kind != storage.EntitlementInbound || got.RegistryName != "rt-principal" || got.Plan != "pro" {
		t.Errorf("got kind=%q name=%q plan=%q", got.Kind, got.RegistryName, got.Plan)
	}
	if len(got.Namespaces) != 2 || got.Namespaces[0] != "ai.*" || got.Namespaces[1] != "devops.k8s" {
		t.Errorf("namespaces = %v", got.Namespaces)
	}
	if !got.ExpiresAt.Equal(expires) {
		t.Errorf("expires = %v, want %v", got.ExpiresAt, expires)
	}
	if got.FetchedAt.IsZero() {
		t.Error("fetched_at not stamped")
	}
}

func entConfKindsAreSeparate(t *testing.T, st storage.EntitlementStore) {
	ctx := context.Background()
	entConfUpsert(t, st, &storage.RegistryEntitlement{
		Kind: storage.EntitlementInbound, RegistryName: "shared-name", Namespaces: []string{"inbound.only"},
	})
	// An outbound sync for a registry of the same name must not touch
	// the inbound grant.
	entConfUpsert(t, st, &storage.RegistryEntitlement{
		Kind: storage.EntitlementOutbound, RegistryName: "shared-name", Plan: "remote", Namespaces: []string{"*"},
	})

	in, err := st.Get(ctx, storage.EntitlementInbound, "shared-name")
	if err != nil {
		t.Fatalf("Get inbound: %v", err)
	}
	if len(in.Namespaces) != 1 || in.Namespaces[0] != "inbound.only" {
		t.Errorf("inbound namespaces after outbound upsert = %v, want [inbound.only]", in.Namespaces)
	}
	out, err := st.Get(ctx, storage.EntitlementOutbound, "shared-name")
	if err != nil {
		t.Fatalf("Get outbound: %v", err)
	}
	if out.Kind != storage.EntitlementOutbound || len(out.Namespaces) != 1 || out.Namespaces[0] != "*" {
		t.Errorf("outbound = %+v", out)
	}
	if _, err := st.Get(ctx, storage.EntitlementOutbound, "rt-only-inbound-missing"); err == nil {
		t.Error("Get of a missing outbound row succeeded")
	}
}

func entConfListByKind(t *testing.T, st storage.EntitlementStore) {
	ctx := context.Background()
	for _, n := range []string{"list-c", "list-a", "list-b"} {
		entConfUpsert(t, st, &storage.RegistryEntitlement{
			Kind: storage.EntitlementInbound, RegistryName: n, Namespaces: []string{"x"},
		})
	}
	entConfUpsert(t, st, &storage.RegistryEntitlement{
		Kind: storage.EntitlementOutbound, RegistryName: "list-out", Namespaces: []string{"x"},
	})

	in, err := st.List(ctx, storage.EntitlementInbound)
	if err != nil {
		t.Fatalf("List inbound: %v", err)
	}
	var names []string
	for _, e := range in {
		if e.Kind != storage.EntitlementInbound {
			t.Errorf("inbound list carries %s row %q", e.Kind, e.RegistryName)
		}
		if len(e.RegistryName) >= 5 && e.RegistryName[:5] == "list-" {
			names = append(names, e.RegistryName)
		}
	}
	if len(names) != 3 || names[0] != "list-a" || names[1] != "list-b" || names[2] != "list-c" {
		t.Errorf("inbound list- rows = %v, want [list-a list-b list-c]", names)
	}

	out, err := st.List(ctx, storage.EntitlementOutbound)
	if err != nil {
		t.Fatalf("List outbound: %v", err)
	}
	found := false
	for _, e := range out {
		if e.Kind != storage.EntitlementOutbound {
			t.Errorf("outbound list carries %s row %q", e.Kind, e.RegistryName)
		}
		if e.RegistryName == "list-a" {
			t.Error("outbound list carries the inbound row list-a")
		}
		found = found || e.RegistryName == "list-out"
	}
	if !found {
		t.Error("outbound list misses list-out")
	}
}

func entConfDelete(t *testing.T, st storage.EntitlementStore) {
	ctx := context.Background()
	for _, k := range []storage.EntitlementKind{storage.EntitlementInbound, storage.EntitlementOutbound} {
		entConfUpsert(t, st, &storage.RegistryEntitlement{Kind: k, RegistryName: "del-me", Namespaces: []string{"x"}})
	}
	if err := st.Delete(ctx, storage.EntitlementInbound, "del-me"); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if _, err := st.Get(ctx, storage.EntitlementInbound, "del-me"); err == nil {
		t.Error("inbound row still readable after Delete")
	}
	if _, err := st.Get(ctx, storage.EntitlementOutbound, "del-me"); err != nil {
		t.Errorf("outbound row of the same name gone after inbound Delete: %v", err)
	}
}

func entConfDeleteMissing(t *testing.T, st storage.EntitlementStore) {
	err := st.Delete(context.Background(), storage.EntitlementInbound, "never-granted")
	if !errors.Is(err, storage.ErrNotFound) {
		t.Errorf("Delete(missing) = %v, want ErrNotFound", err)
	}
}

func entConfInvalidKind(t *testing.T, st storage.EntitlementStore) {
	for _, k := range []storage.EntitlementKind{"", "sideways"} {
		err := st.Upsert(context.Background(), &storage.RegistryEntitlement{Kind: k, RegistryName: "bad-kind", Namespaces: []string{"x"}})
		if err == nil {
			t.Errorf("Upsert with kind %q succeeded", k)
		}
	}
}

func entConfEmptyNamespaces(t *testing.T, st storage.EntitlementStore) {
	ctx := context.Background()
	for _, ns := range [][]string{{}, nil} {
		entConfUpsert(t, st, &storage.RegistryEntitlement{
			Kind: storage.EntitlementInbound, RegistryName: "deny-all", Namespaces: ns,
		})
		got, err := st.Get(ctx, storage.EntitlementInbound, "deny-all")
		if err != nil {
			t.Fatalf("Get(deny-all) after upsert of %#v: %v", ns, err)
		}
		if len(got.Namespaces) != 0 {
			t.Errorf("namespaces = %v, want none", got.Namespaces)
		}
	}
}
