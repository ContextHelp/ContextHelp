package cmd

import (
	"context"
	"errors"
	"path/filepath"
	"slices"
	"testing"
	"time"

	"github.com/ideacrafterslabs/ctxt/internal/config"
	"github.com/ideacrafterslabs/ctxt/internal/pipeline"
	"github.com/ideacrafterslabs/ctxt/internal/pipeline/builtins"
	"github.com/ideacrafterslabs/ctxt/internal/providers"
	"github.com/ideacrafterslabs/ctxt/internal/storage"
	"github.com/ideacrafterslabs/ctxt/internal/storage/indexsig"
	"github.com/ideacrafterslabs/ctxt/internal/storage/sqlite"
	"github.com/ideacrafterslabs/ctxt/internal/storageutil"
)

// The baseline is stamped once: the first start signs the existing rows of
// each registered pipeline (bare and "@v0" names), a later start reads the
// flag and signs nothing, not even a row written unsigned since.
func TestStampDerivationBaseline_RunsOnce(t *testing.T) {
	ctx := context.Background()
	d, err := sqlite.New(filepath.Join(t.TempDir(), "d.db"), storageutil.NewTestBlobStore(t))
	if err != nil {
		t.Fatal(err)
	}
	if err := d.Init(ctx); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { d.Close(ctx) })

	reg := derivationRegistry(t, "m1")
	p, err := reg.Get("text.short")
	if err != nil {
		t.Fatal(err)
	}
	if p.BaselineSignature == "" {
		t.Fatal("text.short has no baseline signature")
	}

	now := time.Now().UTC()
	add := func(id, pipe string) {
		t.Helper()
		err := d.Objects().Create(ctx, &storage.KnowledgeObject{
			ID: id, Type: "note", Pipeline: pipe, RawContent: id, TextContent: id,
			Status: storage.ObjectStatusActive, CreatedAt: now, UpdatedAt: now,
		})
		if err != nil {
			t.Fatal(err)
		}
	}
	sigOf := func(id string) string {
		t.Helper()
		var sig string
		if err := d.DB().QueryRowContext(ctx, `SELECT derivation_sig FROM objects WHERE id = ?`, id).Scan(&sig); err != nil {
			t.Fatal(err)
		}
		return sig
	}
	add("bare", "text.short")
	add("v0", "text.short@v0")
	// A pipeline left out for a missing provider is stamped too.
	add("down", "audio.transcribe")
	add("down-v0", "audio.transcribe@v0")
	add("unknown", "no.such.pipeline")
	add("none", "")

	store := d.Objects().(storage.DerivationStore)
	n, err := stampDerivationBaseline(ctx, d.DB(), indexsig.DialectSQLite, store, reg)
	if err != nil {
		t.Fatal(err)
	}
	if n != 4 {
		t.Errorf("first start stamped %d rows, want 4", n)
	}
	for _, id := range []string{"bare", "v0"} {
		if got := sigOf(id); got != p.BaselineSignature {
			t.Errorf("%s signature %q, want the baseline %q", id, got, p.BaselineSignature)
		}
	}
	down := pipeline.BaselineSignatureOf("audio.transcribe", builtins.Defs()["audio.transcribe"].Steps, "llm=ollama/m1")
	if _, err := reg.Get("audio.transcribe"); err == nil {
		t.Fatal("precondition: audio.transcribe is available")
	}
	for _, id := range []string{"down", "down-v0"} {
		if got := sigOf(id); got != down {
			t.Errorf("%s signature %q, want the unavailable pipeline's baseline %q", id, got, down)
		}
	}
	for _, id := range []string{"unknown", "none"} {
		if got := sigOf(id); got != "" {
			t.Errorf("%s signature %q, want none", id, got)
		}
	}

	add("late", "text.short")
	n, err = stampDerivationBaseline(ctx, d.DB(), indexsig.DialectSQLite, store, reg)
	if err != nil {
		t.Fatal(err)
	}
	if n != 0 || sigOf("late") != "" {
		t.Errorf("second start stamped %d rows, late = %q; want the flag to stop it", n, sigOf("late"))
	}
}

func derivationRegistry(t *testing.T, model string) pipeline.Registry {
	t.Helper()
	cfg := config.ProvidersConfig{LLM: config.ProviderBackendConfig{Backend: "ollama", Model: model}}
	// No tool is found, so the same pipelines are unavailable whatever
	// the machine has installed.
	ctx := providers.WithToolLookup(context.Background(), func(string) (string, error) { return "", errors.New("not found") })
	opts := builtins.BuildOpts{Factory: providers.NewFactory(cfg, nil).WithContext(ctx)}
	reg, err := builtins.ConfiguredRegistryWithPipelineOverrides(opts, cfg, config.PipelinesConfig{})
	if err != nil {
		t.Fatal(err)
	}
	return reg
}

// After the baseline, nothing is stale against the current signatures:
// not even rows of a pipeline left out for a missing provider. Switching
// the llm model then makes exactly the rows of pipelines with an llm step
// stale.
func TestDerivationBaseline_ModelChangeMakesLLMPipelineRowsStale(t *testing.T) {
	ctx := context.Background()
	d, err := sqlite.New(filepath.Join(t.TempDir(), "d.db"), storageutil.NewTestBlobStore(t))
	if err != nil {
		t.Fatal(err)
	}
	if err := d.Init(ctx); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { d.Close(ctx) })
	now := time.Now().UTC()
	for id, pipe := range map[string]string{
		"short-1": "text.short", "short-2": "text.short@v0",
		"audio":  "audio.transcribe", // unavailable: llm steps, no transcription tool
		"csv":    "batch.csv",        // no llm step
		"orphan": "no.such.pipeline",
	} {
		err := d.Objects().Create(ctx, &storage.KnowledgeObject{
			ID: id, Type: "note", Pipeline: pipe, RawContent: id, TextContent: id,
			Status: storage.ObjectStatusActive, CreatedAt: now, UpdatedAt: now,
		})
		if err != nil {
			t.Fatal(err)
		}
	}
	store := d.Objects().(storage.DerivationStore)
	before := derivationRegistry(t, "m1")
	if _, err := stampDerivationBaseline(ctx, d.DB(), indexsig.DialectSQLite, store, before); err != nil {
		t.Fatal(err)
	}
	if _, err := before.Get("audio.transcribe"); err == nil {
		t.Fatal("precondition: audio.transcribe is available")
	}

	n, err := store.CountStaleDerivations(ctx, pipeline.CurrentSignatures(before))
	if err != nil || n != 0 {
		t.Fatalf("stale after the baseline = %d, %v; want 0 (unavailable pipeline included)", n, err)
	}
	after := derivationRegistry(t, "m2")
	ids, err := store.ListStaleDerivations(ctx, pipeline.CurrentSignatures(after), "", 100)
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{"audio", "short-1", "short-2"}; !slices.Equal(ids, want) {
		t.Errorf("stale after switching the llm model = %v, want %v", ids, want)
	}
}
