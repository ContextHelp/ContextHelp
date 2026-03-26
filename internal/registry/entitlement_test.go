package registry_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/ideacrafterslabs/ctxt/internal/registry"
	"github.com/ideacrafterslabs/ctxt/internal/storage"
)

// stubEntitlementStore is an in-memory EntitlementStore for testing.
type stubEntitlementStore struct {
	records map[string]*storage.RegistryEntitlement
}

func newStubEntitlementStore() *stubEntitlementStore {
	return &stubEntitlementStore{records: make(map[string]*storage.RegistryEntitlement)}
}

func (s *stubEntitlementStore) Upsert(_ context.Context, e *storage.RegistryEntitlement) error {
	s.records[e.RegistryName] = e
	return nil
}

func (s *stubEntitlementStore) Get(_ context.Context, name string) (*storage.RegistryEntitlement, error) {
	e, ok := s.records[name]
	if !ok {
		return nil, errors.New("not found")
	}
	return e, nil
}

func (s *stubEntitlementStore) List(_ context.Context) ([]*storage.RegistryEntitlement, error) {
	out := make([]*storage.RegistryEntitlement, 0, len(s.records))
	for _, e := range s.records {
		out = append(out, e)
	}
	return out, nil
}

func makeEntitlementServer(plan string, namespaces []string, expiresAt *time.Time) *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		payload := map[string]any{
			"plan":       plan,
			"namespaces": namespaces,
		}
		if expiresAt != nil {
			payload["expires_at"] = expiresAt.Format(time.RFC3339)
		}
		json.NewEncoder(w).Encode(payload)
	}))
}

func TestEntitlementChecker_FetchAndStore(t *testing.T) {
	expiry := time.Now().Add(24 * time.Hour).UTC().Truncate(time.Second)
	srv := makeEntitlementServer("pro", []string{"ai.*", "devops.*"}, &expiry)
	defer srv.Close()

	store := newStubEntitlementStore()
	checker := registry.NewEntitlementChecker(store)

	ent, err := checker.FetchAndStore(context.Background(), "my-reg", srv.URL, "")
	if err != nil {
		t.Fatalf("FetchAndStore: %v", err)
	}
	if ent.Plan != "pro" {
		t.Errorf("plan: got %q, want %q", ent.Plan, "pro")
	}
	if len(ent.Namespaces) != 2 {
		t.Errorf("namespaces count: got %d, want 2", len(ent.Namespaces))
	}
	if ent.RegistryName != "my-reg" {
		t.Errorf("registry_name: got %q, want %q", ent.RegistryName, "my-reg")
	}

	// Verify persisted in store.
	persisted, err := store.Get(context.Background(), "my-reg")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if persisted.Plan != "pro" {
		t.Errorf("persisted plan: got %q, want %q", persisted.Plan, "pro")
	}
}

func TestEntitlementChecker_FetchAndStore_Unauthorized(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
	}))
	defer srv.Close()

	store := newStubEntitlementStore()
	checker := registry.NewEntitlementChecker(store)

	_, err := checker.FetchAndStore(context.Background(), "paid-reg", srv.URL, "")
	if err == nil {
		t.Fatal("expected error for 401, got nil")
	}
	if !registry.IsEntitlementRequired(err) {
		t.Errorf("expected ErrEntitlementRequired, got: %v", err)
	}
}

func TestEntitlementChecker_CheckNamespace(t *testing.T) {
	store := newStubEntitlementStore()
	expiry := time.Now().Add(24 * time.Hour)
	store.records["paid-reg"] = &storage.RegistryEntitlement{
		RegistryName: "paid-reg",
		Plan:         "pro",
		Namespaces:   []string{"ai.*", "devops.*"},
		ExpiresAt:    expiry,
		FetchedAt:    time.Now(),
	}

	checker := registry.NewEntitlementChecker(store)
	ctx := context.Background()

	if err := checker.CheckNamespace(ctx, "paid-reg", "ai.bert", ""); err != nil {
		t.Errorf("ai.bert should be allowed: %v", err)
	}
	if err := checker.CheckNamespace(ctx, "paid-reg", "devops.k8s", ""); err != nil {
		t.Errorf("devops.k8s should be allowed: %v", err)
	}
	if err := checker.CheckNamespace(ctx, "paid-reg", "finance.stocks", ""); err == nil {
		t.Error("finance.stocks should be restricted")
	} else if !registry.IsEntitlementRequired(err) {
		t.Errorf("expected ErrEntitlementRequired, got: %v", err)
	}
}

func TestEntitlementChecker_CheckNamespace_Expired(t *testing.T) {
	store := newStubEntitlementStore()
	store.records["paid-reg"] = &storage.RegistryEntitlement{
		RegistryName: "paid-reg",
		Plan:         "pro",
		Namespaces:   []string{"ai.*"},
		ExpiresAt:    time.Now().Add(-time.Hour), // expired
		FetchedAt:    time.Now(),
	}

	checker := registry.NewEntitlementChecker(store)
	err := checker.CheckNamespace(context.Background(), "paid-reg", "ai.bert", "")
	if err == nil {
		t.Error("expired entitlement should be restricted")
	}
	if !registry.IsEntitlementRequired(err) {
		t.Errorf("expected ErrEntitlementRequired, got: %v", err)
	}
}

func TestEntitlementChecker_CheckNamespace_NoRecord(t *testing.T) {
	store := newStubEntitlementStore()
	checker := registry.NewEntitlementChecker(store)

	// No record → unrestricted.
	if err := checker.CheckNamespace(context.Background(), "open-reg", "any.ns", ""); err != nil {
		t.Errorf("no entitlement record should be unrestricted: %v", err)
	}
}

func TestMatchNamespace(t *testing.T) {
	// Test via CheckNamespace with a store.
	cases := []struct {
		pattern   string
		namespace string
		wantMatch bool
	}{
		{"ai.*", "ai.bert", true},
		{"ai.*", "ai", false},
		{"ai.*", "ai.bert.sub", true},
		{"devops.*", "devops.k8s", true},
		{"devops.*", "finance.x", false},
		{"*", "anything", true},
	}

	store := newStubEntitlementStore()
	checker := registry.NewEntitlementChecker(store)
	ctx := context.Background()

	for _, tc := range cases {
		store.records["reg"] = &storage.RegistryEntitlement{
			RegistryName: "reg",
			Plan:         "test",
			Namespaces:   []string{tc.pattern},
			FetchedAt:    time.Now(),
		}
		err := checker.CheckNamespace(ctx, "reg", tc.namespace, "")
		if tc.wantMatch && err != nil {
			t.Errorf("pattern=%q ns=%q: expected match, got err: %v", tc.pattern, tc.namespace, err)
		}
		if !tc.wantMatch && err == nil {
			t.Errorf("pattern=%q ns=%q: expected no match, got nil error", tc.pattern, tc.namespace)
		}
	}
}
