package cmd

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/ideacrafterslabs/ctxt/internal/storage"
)

// TestRegistryEntitlementsListsOutboundOnly: the listing shows this
// instance's entitlements on remote registries, never the inbound
// grants operators give principals (dpkms entitlement).
func TestRegistryEntitlementsListsOutboundOnly(t *testing.T) {
	db := setupTestDB(t)
	ctx := context.Background()
	for _, e := range []*storage.RegistryEntitlement{
		{Kind: storage.EntitlementOutbound, RegistryName: "acme", Plan: "pro", Namespaces: []string{"ai.*"}},
		{Kind: storage.EntitlementInbound, RegistryName: "phone", Plan: "", Namespaces: []string{"public.*"}},
	} {
		if err := db.Driver.Entitlements().Upsert(ctx, e); err != nil {
			t.Fatalf("seed %s: %v", e.RegistryName, err)
		}
	}

	out, err := db.exec("registry", "entitlements", "--format", "json")
	if err != nil {
		t.Fatalf("registry entitlements: %v\n%s", err, out)
	}
	var doc struct {
		Entitlements []struct {
			Kind         string `json:"kind"`
			RegistryName string `json:"registry_name"`
		} `json:"entitlements"`
	}
	if err := json.Unmarshal([]byte(out), &doc); err != nil {
		t.Fatalf("entitlements JSON: %v\n%s", err, out)
	}
	if len(doc.Entitlements) != 1 || doc.Entitlements[0].RegistryName != "acme" || doc.Entitlements[0].Kind != "outbound" {
		t.Errorf("entitlements = %+v, want only outbound acme", doc.Entitlements)
	}
}
