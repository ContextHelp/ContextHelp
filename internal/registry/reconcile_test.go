package registry

import (
	"errors"
	"testing"
	"time"

	"github.com/ideacrafterslabs/ctxt/internal/storage"
)

// ─── helpers ─────────────────────────────────────────────────────────────────

func makeEntity(slug, title, description, namespace string) *storage.Entity {
	return &storage.Entity{
		Slug:        slug,
		Title:       title,
		Description: description,
		Namespace:   namespace,
	}
}

func candidate(e *storage.Entity, url string, updatedAt time.Time, trust float64) RegistryEntity {
	return RegistryEntity{
		Entity:      e,
		RegistryURL: url,
		UpdatedAt:   updatedAt,
		TrustScore:  trust,
	}
}

// ─── Reconcile — single candidate ────────────────────────────────────────────

func TestReconcile_SingleCandidate_NoConflicts(t *testing.T) {
	e := makeEntity("ai.bert", "BERT", "Bidirectional Encoder", "ai")
	c := candidate(e, "https://reg.example.com", time.Now(), 0.8)

	result := Reconcile("ai.bert", []RegistryEntity{c}, MergeLastWriteWins)

	if result.Merged == nil {
		t.Fatal("expected Merged to be non-nil")
	}
	if result.Merged.Slug != "ai.bert" {
		t.Errorf("Merged.Slug: got %q, want %q", result.Merged.Slug, "ai.bert")
	}
	if len(result.Conflicts) != 0 {
		t.Errorf("expected no conflicts, got %d", len(result.Conflicts))
	}
	if len(result.Provenance) == 0 {
		t.Error("expected non-empty provenance for single candidate")
	}
}

// ─── Reconcile — last-write-wins ─────────────────────────────────────────────

func TestReconcile_LastWriteWins_PicksNewer(t *testing.T) {
	now := time.Now()
	old := now.Add(-24 * time.Hour)

	e1 := makeEntity("ai.bert", "BERT v1", "Old description", "ai")
	e2 := makeEntity("ai.bert", "BERT v2", "New description", "ai")

	c1 := candidate(e1, "https://reg-a.example.com", old, 0.5)
	c2 := candidate(e2, "https://reg-b.example.com", now, 0.3)

	result := Reconcile("ai.bert", []RegistryEntity{c1, c2}, MergeLastWriteWins)

	if result.Merged.Title != "BERT v2" {
		t.Errorf("expected newer title; got %q", result.Merged.Title)
	}
	// Provenance for title must point to reg-b.
	prov := result.Provenance["title"]
	if prov.RegistryURL != "https://reg-b.example.com" {
		t.Errorf("provenance.title.RegistryURL: got %q, want reg-b", prov.RegistryURL)
	}
}

func TestReconcile_LastWriteWins_Conflicts_Reported(t *testing.T) {
	now := time.Now()

	e1 := makeEntity("ai.bert", "BERT", "Description A", "ai")
	e2 := makeEntity("ai.bert", "BERT", "Description B", "ai") // title same, description differs

	c1 := candidate(e1, "https://reg-a.example.com", now.Add(-time.Hour), 0.5)
	c2 := candidate(e2, "https://reg-b.example.com", now, 0.5)

	result := Reconcile("ai.bert", []RegistryEntity{c1, c2}, MergeLastWriteWins)

	if len(result.Conflicts) == 0 {
		t.Fatal("expected at least one conflict for differing description")
	}
	var found bool
	for _, c := range result.Conflicts {
		if c.Field == "description" {
			found = true
			if c.Strategy != MergeLastWriteWins {
				t.Errorf("conflict strategy: got %q, want %q", c.Strategy, MergeLastWriteWins)
			}
			if c.Winner != "https://reg-b.example.com" {
				t.Errorf("conflict winner: got %q, want reg-b", c.Winner)
			}
		}
	}
	if !found {
		t.Error("expected conflict for field=description, not found")
	}
}

func TestReconcile_LastWriteWins_NoConflict_WhenIdentical(t *testing.T) {
	now := time.Now()

	e1 := makeEntity("ai.bert", "BERT", "Same description", "ai")
	e2 := makeEntity("ai.bert", "BERT", "Same description", "ai")

	c1 := candidate(e1, "https://reg-a.example.com", now.Add(-time.Hour), 0.5)
	c2 := candidate(e2, "https://reg-b.example.com", now, 0.5)

	result := Reconcile("ai.bert", []RegistryEntity{c1, c2}, MergeLastWriteWins)

	if len(result.Conflicts) != 0 {
		t.Errorf("expected no conflicts for identical entities, got %d: %+v",
			len(result.Conflicts), result.Conflicts)
	}
}

// ─── Reconcile — trust-score ──────────────────────────────────────────────────

func TestReconcile_TrustScore_PicksHigherTrust(t *testing.T) {
	now := time.Now()

	// reg-a is older but higher trust
	e1 := makeEntity("ai.bert", "BERT Official", "Official description", "ai")
	e2 := makeEntity("ai.bert", "BERT Community", "Community description", "ai")

	c1 := candidate(e1, "https://reg-a.example.com", now.Add(-24*time.Hour), 0.9)
	c2 := candidate(e2, "https://reg-b.example.com", now, 0.3)

	result := Reconcile("ai.bert", []RegistryEntity{c1, c2}, MergeTrustScore)

	if result.Merged.Title != "BERT Official" {
		t.Errorf("expected high-trust title; got %q", result.Merged.Title)
	}
	prov := result.Provenance["title"]
	if prov.RegistryURL != "https://reg-a.example.com" {
		t.Errorf("provenance.title.RegistryURL: got %q, want reg-a", prov.RegistryURL)
	}
}

func TestReconcile_TrustScore_TieFallsBackToLWW(t *testing.T) {
	now := time.Now()

	// equal trust; reg-b is newer → LWW tiebreak
	e1 := makeEntity("ai.bert", "BERT v1", "Old description", "ai")
	e2 := makeEntity("ai.bert", "BERT v2", "New description", "ai")

	c1 := candidate(e1, "https://reg-a.example.com", now.Add(-time.Hour), 0.7)
	c2 := candidate(e2, "https://reg-b.example.com", now, 0.7)

	result := Reconcile("ai.bert", []RegistryEntity{c1, c2}, MergeTrustScore)

	if result.Merged.Title != "BERT v2" {
		t.Errorf("tiebreak should pick newer; got %q", result.Merged.Title)
	}
}

// ─── ResolveOffline ───────────────────────────────────────────────────────────

func TestResolveOffline_NilLocal_ReturnsError(t *testing.T) {
	_, err := ResolveOffline("ai.bert", nil, errors.New("connection refused"))
	if err == nil {
		t.Fatal("expected error for nil local cache, got nil")
	}
}

func TestResolveOffline_WithLocal_ReturnsCached(t *testing.T) {
	local := makeEntity("ai.bert", "BERT", "cached", "ai")
	local.UpdatedAt = time.Now().Add(-time.Hour)

	res, err := ResolveOffline("ai.bert", local, errors.New("timeout"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !res.Cached {
		t.Error("expected Cached=true")
	}
	if res.Entity.Slug != "ai.bert" {
		t.Errorf("entity slug: got %q", res.Entity.Slug)
	}
	if res.Warning != "" {
		t.Errorf("expected no staleness warning for fresh cache, got %q", res.Warning)
	}
}

func TestResolveOffline_StaleCache_IncludesWarning(t *testing.T) {
	local := makeEntity("ai.bert", "BERT", "stale", "ai")
	local.UpdatedAt = time.Now().Add(-8 * 24 * time.Hour) // 8 days old

	res, err := ResolveOffline("ai.bert", local, errors.New("timeout"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res.Warning == "" {
		t.Error("expected staleness warning for 8-day-old cache")
	}
}

// ─── Provenance helpers ───────────────────────────────────────────────────────

func TestProvenanceFromSingle_AllFields(t *testing.T) {
	now := time.Now()
	e := makeEntity("ai.bert", "BERT", "desc", "ai")
	c := candidate(e, "https://reg.example.com", now, 0.8)

	prov := provenanceFromSingle(c)

	for _, field := range entityScalarFields() {
		if p, ok := prov[field]; !ok {
			t.Errorf("provenance missing field %q", field)
		} else if p.RegistryURL != "https://reg.example.com" {
			t.Errorf("provenance[%q].RegistryURL: got %q", field, p.RegistryURL)
		}
	}
}

// ─── uniqueValues helper ──────────────────────────────────────────────────────

func TestUniqueValues(t *testing.T) {
	vals := []RegistryValue{
		{Value: "foo"},
		{Value: "bar"},
		{Value: "foo"},
	}
	got := uniqueValues(vals)
	if len(got) != 2 {
		t.Errorf("uniqueValues: got %d unique, want 2: %v", len(got), got)
	}
}
