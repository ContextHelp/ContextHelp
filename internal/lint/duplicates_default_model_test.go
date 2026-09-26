package lint_test

import (
	"context"
	"database/sql"
	"strings"
	"testing"
	"time"

	"github.com/ideacrafterslabs/ctxt/internal/embeddings/registry"
	"github.com/ideacrafterslabs/ctxt/internal/lint"
	"github.com/ideacrafterslabs/ctxt/internal/storage"
	"github.com/ideacrafterslabs/ctxt/internal/storage/storagetest"
	"github.com/ideacrafterslabs/ctxt/internal/storageutil"
)

// dupFixture is a driver whose embeddings live in a MemEmbeddingStore, with
// "dup-default" (the default) and "dup-other" registered at dimension 3.
type dupFixture struct {
	driver storage.StorageDriver
	emb    *storagetest.MemEmbeddingStore
	reg    *registry.Store
}

func newDupFixture(t *testing.T) *dupFixture {
	t.Helper()
	ctx := context.Background()
	emb := storagetest.NewMemEmbeddingStore()
	drv := storagetest.WithEmbeddings(storageutil.NewTestDriver(t), emb)
	reg, err := registry.ForDriver(drv)
	if err != nil {
		t.Fatal(err)
	}
	for _, m := range []struct {
		id  string
		def bool
	}{{"dup-default", true}, {"dup-other", false}} {
		if err := reg.Register(ctx, registry.Model{ModelID: m.id, Provider: "ollama", Dimension: 3}, m.def); err != nil {
			t.Fatal(err)
		}
		if err := emb.EnsureIndex(ctx, storage.EmbeddingModelSpec{ModelID: m.id, Provider: "ollama", Dimension: 3}); err != nil {
			t.Fatal(err)
		}
	}
	return &dupFixture{driver: drv, emb: emb, reg: reg}
}

func (f *dupFixture) object(t *testing.T, id string, vectors map[string][]float32) {
	t.Helper()
	ctx := context.Background()
	now := time.Now()
	if err := f.driver.Objects().Create(ctx, &storage.KnowledgeObject{
		ID: id, Type: "text", RawContent: id, CreatedAt: now, UpdatedAt: now,
	}); err != nil {
		t.Fatal(err)
	}
	var vs []storage.ObjectVector
	for model, v := range vectors {
		vs = append(vs, storage.ObjectVector{ModelID: model, Vector: v})
	}
	if err := f.emb.Put(ctx, id, vs); err != nil {
		t.Fatal(err)
	}
}

func runDuplicates(t *testing.T, drv storage.StorageDriver) []lint.Issue {
	t.Helper()
	cfg := lint.DefaultConfig()
	cfg.Checks = []string{"duplicates"}
	report, err := lint.New(drv, cfg).Run(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	return report.Issues
}

// The threshold is a similarity (1 - cosine distance): a near-identical
// pair is flagged and an orthogonal pair is not. Comparing the raw distance
// against the threshold inverts both outcomes.
func TestCheckDuplicates_ThresholdComparesSimilarity(t *testing.T) {
	f := newDupFixture(t)
	f.object(t, "dup-a", map[string][]float32{"dup-default": {1, 0, 0}})
	f.object(t, "dup-b", map[string][]float32{"dup-default": {0.99, 0.14, 0}}) // cos ~0.99 to a
	f.object(t, "dup-c", map[string][]float32{"dup-default": {0, 0, 1}})       // cos 0 to a and b

	issues := runDuplicates(t, f.driver)
	if len(issues) != 1 {
		t.Fatalf("issues = %+v, want exactly the dup-a/dup-b pair", issues)
	}
	iss := issues[0]
	pair := iss.ObjectID + " " + iss.Message
	if !strings.Contains(pair, "dup-a") || !strings.Contains(pair, "dup-b") {
		t.Fatalf("flagged %q, want the dup-a/dup-b pair", pair)
	}
	if iss.Severity != lint.SeverityWarning || !strings.Contains(iss.Message, "similarity 0.99") {
		t.Fatalf("issue = %+v, want a warning reporting similarity ~0.99", iss)
	}
}

// Only the default model's vectors are compared: a pair that is identical
// under another registered model is not a duplicate.
func TestCheckDuplicates_ReadsDefaultModelOnly(t *testing.T) {
	f := newDupFixture(t)
	f.object(t, "dup-x", map[string][]float32{"dup-default": {1, 0, 0}, "dup-other": {0, 1, 0}})
	f.object(t, "dup-y", map[string][]float32{"dup-default": {0, 0, 1}, "dup-other": {0, 1, 0}})

	if issues := runDuplicates(t, f.driver); len(issues) != 0 {
		t.Fatalf("issues = %+v, want none (identical only under the non-default model)", issues)
	}
	for _, q := range f.emb.Searches() {
		if q.ModelID != "dup-default" {
			t.Fatalf("searched model %q, want only the default", q.ModelID)
		}
	}

	if err := f.reg.SetDefault(context.Background(), "dup-other"); err != nil {
		t.Fatal(err)
	}
	if issues := runDuplicates(t, f.driver); len(issues) != 1 {
		t.Fatalf("after the default flip issues = %+v, want the dup-x/dup-y pair", issues)
	}
}

// Without a default model the check reports that it was skipped instead of
// passing silently.
func TestCheckDuplicates_NoDefaultModelIsReported(t *testing.T) {
	f := newDupFixture(t)
	f.object(t, "dup-a", map[string][]float32{"dup-default": {1, 0, 0}})
	f.object(t, "dup-b", map[string][]float32{"dup-default": {1, 0, 0}})
	db := f.driver.(interface{ DB() *sql.DB }).DB()
	if _, err := db.Exec(`UPDATE embedding_models SET is_default = 0`); err != nil {
		t.Fatal(err)
	}

	issues := runDuplicates(t, f.driver)
	if len(issues) != 1 || issues[0].Severity != lint.SeverityInfo || !strings.Contains(issues[0].Message, "no default embedding model") {
		t.Fatalf("issues = %+v, want one info issue naming the missing default", issues)
	}
}
