package cmd

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"
	kitcli "hop.top/kit/go/console/cli"
	"hop.top/kit/go/console/output"
	"hop.top/kit/go/runtime/bus"

	"github.com/ideacrafterslabs/ctxt/internal/config"
	"github.com/ideacrafterslabs/ctxt/internal/embeddings"
	"github.com/ideacrafterslabs/ctxt/internal/embeddings/registry"
	"github.com/ideacrafterslabs/ctxt/internal/events"
	"github.com/ideacrafterslabs/ctxt/internal/pidfile"
	"github.com/ideacrafterslabs/ctxt/internal/providers/providertest"
	"github.com/ideacrafterslabs/ctxt/internal/storage"
	"github.com/ideacrafterslabs/ctxt/internal/storage/indexsig"
	"github.com/ideacrafterslabs/ctxt/internal/storage/sqlite"
)

// Lifecycle fixture models: 4-dimension vectors written straight into the
// store. set-default, deprecate and purge never call a provider.
const (
	lcA = "lifecycle-a@2026-09-26"
	lcB = "lifecycle-b@2026-09-26"
	lcC = "lifecycle-c@2026-09-26"
)

type setDefaultDoc struct {
	ModelID         string  `json:"model_id"`
	PreviousDefault *string `json:"previous_default"`
	Coverage        float64 `json:"coverage"`
	MinCoverage     float64 `json:"min_coverage"`
	Changed         bool    `json:"changed"`
	DryRun          bool    `json:"dry_run"`
}

type deprecateDoc struct {
	ModelID         string `json:"model_id"`
	DeprecatedAt    string `json:"deprecated_at"`
	PurgeEligibleAt string `json:"purge_eligible_at"`
	GracePeriod     string `json:"grace_period"`
	Changed         bool   `json:"changed"`
	DryRun          bool   `json:"dry_run"`
}

type purgeDoc struct {
	ModelID         string `json:"model_id"`
	DeprecatedAt    string `json:"deprecated_at"`
	PurgeEligibleAt string `json:"purge_eligible_at"`
	Rows            int64  `json:"rows"`
	DryRun          bool   `json:"dry_run"`
}

// lifecycleModel registers id (dimension 4) in db and builds its index.
func lifecycleModel(t *testing.T, db *testDB, id string, makeDefault bool) {
	t.Helper()
	ctx := context.Background()
	if err := registryOf(t, db).Register(ctx, registry.Model{ModelID: id, Provider: "ollama", Dimension: 4}, makeDefault); err != nil {
		t.Fatal(err)
	}
	if err := db.Driver.Embeddings().EnsureIndex(ctx, storage.EmbeddingModelSpec{ModelID: id, Provider: "ollama", Dimension: 4}); err != nil {
		t.Fatal(err)
	}
}

// lifecycleObjects creates n objects and returns their IDs.
func lifecycleObjects(t *testing.T, db *testDB, n int) []string {
	t.Helper()
	now := time.Now().Truncate(time.Second)
	ids := make([]string, 0, n)
	for i := 0; i < n; i++ {
		id := fmt.Sprintf("obj_lifecycle_%d", i)
		if err := db.Driver.Objects().Create(context.Background(), &storage.KnowledgeObject{
			ID: id, Type: "note", RawContent: "lifecycle " + id, CreatedAt: now, UpdatedAt: now,
		}); err != nil {
			t.Fatal(err)
		}
		ids = append(ids, id)
	}
	return ids
}

// lifecycleEmbed writes a one-hot vector for each object under modelID.
func lifecycleEmbed(t *testing.T, db *testDB, modelID string, ids ...string) {
	t.Helper()
	for i, id := range ids {
		v := make([]float32, 4)
		v[i%4] = 1
		if err := db.Driver.Embeddings().Put(context.Background(), id, []storage.ObjectVector{{ModelID: modelID, Vector: v}}); err != nil {
			t.Fatal(err)
		}
	}
}

// newLifecycleDB: A is the default covering 4 objects, B covers 3 (0.75),
// C covers all 4.
func newLifecycleDB(t *testing.T) *testDB {
	t.Helper()
	db := setupTestDB(t)
	lifecycleModel(t, db, lcA, true)
	lifecycleModel(t, db, lcB, false)
	lifecycleModel(t, db, lcC, false)
	ids := lifecycleObjects(t, db, 4)
	lifecycleEmbed(t, db, lcA, ids...)
	lifecycleEmbed(t, db, lcB, ids[:3]...)
	lifecycleEmbed(t, db, lcC, ids...)
	return db
}

func defaultModelID(t *testing.T, db *testDB) string {
	t.Helper()
	m, err := registryOf(t, db).Default(context.Background())
	if errors.Is(err, registry.ErrNoDefaultModel) {
		return ""
	}
	if err != nil {
		t.Fatal(err)
	}
	return m.ModelID
}

func decodeDoc[T any](t *testing.T, out string) T {
	t.Helper()
	var doc T
	if err := json.Unmarshal([]byte(out), &doc); err != nil {
		t.Fatalf("decode: %v\n%s", err, out)
	}
	return doc
}

// appendConfig adds YAML to the test config file.
func appendConfig(t *testing.T, db *testDB, yaml string) {
	t.Helper()
	f, err := os.OpenFile(db.ConfigPath, os.O_APPEND|os.O_WRONLY, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if _, err := f.WriteString(yaml); err != nil {
		t.Fatal(err)
	}
}

// captureLifecycleEvents routes the commands' bus events into a slice for
// the rest of the test.
func captureLifecycleEvents(t *testing.T) func() []bus.Event {
	t.Helper()
	var mu sync.Mutex
	var got []bus.Event
	prev := embeddingsEventBus
	embeddingsEventBus = func() bus.Bus {
		b := bus.New()
		b.Subscribe("#", func(_ context.Context, e bus.Event) error {
			mu.Lock()
			defer mu.Unlock()
			got = append(got, e)
			return nil
		})
		return b
	}
	t.Cleanup(func() { embeddingsEventBus = prev })
	return func() []bus.Event {
		mu.Lock()
		defer mu.Unlock()
		return append([]bus.Event(nil), got...)
	}
}

// useLifecycleClock pins the lifecycle commands' clock.
func useLifecycleClock(t *testing.T, now time.Time) {
	t.Helper()
	prev := embeddingsNow
	embeddingsNow = func() time.Time { return now }
	t.Cleanup(func() { embeddingsNow = prev })
}

func lifecyclePayload(t *testing.T, e bus.Event) events.EmbeddingModelLifecyclePayload {
	t.Helper()
	p, ok := e.Payload.(events.EmbeddingModelLifecyclePayload)
	if !ok {
		t.Fatalf("payload of %s is %T", e.Topic, e.Payload)
	}
	return p
}

// --- set-default -------------------------------------------------------------

func TestEmbeddingsSetDefault_CoverageGuard(t *testing.T) {
	db := newLifecycleDB(t)
	evs := captureLifecycleEvents(t)

	out, err := db.exec("embeddings", "set-default", lcB, "--format", "json")
	assertClass(t, err, output.CodeConflict)
	if !strings.Contains(err.Error(), "0.75") || !strings.Contains(err.Error(), "0.99") {
		t.Errorf("refusal must name coverage and threshold: %v\n%s", err, out)
	}
	if got := defaultModelID(t, db); got != lcA {
		t.Fatalf("default after a refused flip = %s, want %s", got, lcA)
	}
	if n := len(evs()); n != 0 {
		t.Errorf("a refused flip emitted %d events", n)
	}

	out, err = db.exec("embeddings", "set-default", lcB, "--min-coverage", "0.75", "--format", "json")
	if err != nil {
		t.Fatalf("set-default at the threshold: %v\n%s", err, out)
	}
	doc := decodeDoc[setDefaultDoc](t, out)
	if doc.ModelID != lcB || doc.PreviousDefault == nil || *doc.PreviousDefault != lcA ||
		doc.Coverage != 0.75 || doc.MinCoverage != 0.75 || !doc.Changed || doc.DryRun {
		t.Errorf("set-default = %+v", doc)
	}
	if got := defaultModelID(t, db); got != lcB {
		t.Fatalf("default = %s, want %s", got, lcB)
	}

	got := evs()
	if len(got) != 1 || got[0].Topic != events.TopicCtxtUpgradeEmbeddingModelPromoted {
		t.Fatalf("events = %+v, want one %s", got, events.TopicCtxtUpgradeEmbeddingModelPromoted)
	}
	p := lifecyclePayload(t, got[0])
	if p.ModelID != lcB || p.PreviousDefault != lcA || p.Coverage == nil || *p.Coverage != 0.75 ||
		p.MinCoverage == nil || *p.MinCoverage != 0.75 {
		t.Errorf("promoted payload = %+v", p)
	}

	// Promoting the default again changes nothing and emits nothing.
	out, err = db.exec("embeddings", "set-default", lcB, "--format", "json")
	if err != nil {
		t.Fatalf("re-promote: %v\n%s", err, out)
	}
	if doc := decodeDoc[setDefaultDoc](t, out); doc.Changed {
		t.Errorf("re-promote = %+v, want changed=false", doc)
	}
	if n := len(evs()); n != 1 {
		t.Errorf("re-promote emitted an event (%d total)", n)
	}
}

func TestEmbeddingsSetDefault_ThresholdFromConfigAndFlag(t *testing.T) {
	for name, tc := range map[string]struct {
		config string
		flag   []string
		ok     bool
	}{
		"default threshold refuses 0.75":   {"", nil, false},
		"config 0.7 admits 0.75":           {"embeddings:\n  min_coverage: 0.7\n", nil, true},
		"config 0.8 refuses 0.75":          {"embeddings:\n  min_coverage: 0.8\n", nil, false},
		"flag lowers the config threshold": {"embeddings:\n  min_coverage: 0.8\n", []string{"--min-coverage", "0.7"}, true},
		"flag raises the config threshold": {"embeddings:\n  min_coverage: 0.7\n", []string{"--min-coverage", "0.9"}, false},
		"flag zero admits anything":        {"", []string{"--min-coverage", "0"}, true},
	} {
		t.Run(name, func(t *testing.T) {
			db := newLifecycleDB(t)
			appendConfig(t, db, tc.config)
			out, err := db.exec(append([]string{"embeddings", "set-default", lcB, "--format", "json"}, tc.flag...)...)
			if tc.ok {
				if err != nil {
					t.Fatalf("set-default: %v\n%s", err, out)
				}
				if got := defaultModelID(t, db); got != lcB {
					t.Errorf("default = %s, want %s", got, lcB)
				}
				return
			}
			assertClass(t, err, output.CodeConflict)
			if got := defaultModelID(t, db); got != lcA {
				t.Errorf("default = %s, want %s unchanged", got, lcA)
			}
		})
	}
}

func TestEmbeddingsSetDefault_Refusals(t *testing.T) {
	db := newLifecycleDB(t)
	if err := registryOf(t, db).Deprecate(context.Background(), lcC, time.Now().Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	for name, tc := range map[string]struct {
		args []string
		code string
	}{
		"deprecated":         {[]string{lcC}, output.CodeConflict},
		"unregistered":       {[]string{"lifecycle-missing@1"}, output.CodeNotFound},
		"invalid id":         {[]string{"bad id"}, output.CodeUsage},
		"threshold above 1":  {[]string{lcC, "--min-coverage", "1.5"}, output.CodeUsage},
		"negative threshold": {[]string{lcC, "--min-coverage", "-0.1"}, output.CodeUsage},
	} {
		t.Run(name, func(t *testing.T) {
			_, err := db.exec(append([]string{"embeddings", "set-default"}, tc.args...)...)
			assertClass(t, err, tc.code)
		})
	}
	appendConfig(t, db, "embeddings:\n  min_coverage: 2\n")
	_, err := db.exec("embeddings", "set-default", lcA)
	assertClass(t, err, output.CodeUsage)
	if got := defaultModelID(t, db); got != lcA {
		t.Errorf("default = %s after refusals, want %s", got, lcA)
	}
}

func TestEmbeddingsSetDefault_DryRun(t *testing.T) {
	db := newLifecycleDB(t)
	evs := captureLifecycleEvents(t)
	out, err := db.exec("embeddings", "set-default", lcC, "--dry-run", "--format", "json")
	if err != nil {
		t.Fatalf("dry run: %v\n%s", err, out)
	}
	doc := decodeDoc[setDefaultDoc](t, out)
	if !doc.DryRun || !doc.Changed || doc.Coverage != 1 {
		t.Errorf("dry run = %+v", doc)
	}
	if got := defaultModelID(t, db); got != lcA {
		t.Errorf("dry run flipped the default to %s", got)
	}
	if n := len(evs()); n != 0 {
		t.Errorf("dry run emitted %d events", n)
	}
	_, err = db.exec("embeddings", "set-default", lcB, "--dry-run")
	assertClass(t, err, output.CodeConflict)

	out, err = db.exec("embeddings", "set-default", lcC)
	if err != nil || !strings.Contains(out, lcC) || !strings.Contains(out, lcA) {
		t.Errorf("human set-default: %v\n%s", err, out)
	}
}

// The flip reaches another handle's next read: the query path reads the
// default per query, so there is nothing to invalidate.
func TestEmbeddingsSetDefault_VisibleToOtherHandle(t *testing.T) {
	db := newLifecycleDB(t)
	other, err := sqlite.New(dbFile(db))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { other.Close(context.Background()) })
	reader := registry.New(other.DB())
	if m, err := reader.Default(context.Background()); err != nil || m.ModelID != lcA {
		t.Fatalf("reader default before = %+v, %v", m, err)
	}
	if out, err := db.exec("embeddings", "set-default", lcC); err != nil {
		t.Fatalf("set-default: %v\n%s", err, out)
	}
	if m, err := reader.Default(context.Background()); err != nil || m.ModelID != lcC {
		t.Fatalf("reader default after = %+v, %v; want %s", m, err, lcC)
	}
}

// --- deprecate -----------------------------------------------------------------

func TestEmbeddingsDeprecate(t *testing.T) {
	db := newLifecycleDB(t)
	evs := captureLifecycleEvents(t)
	now := time.Date(2026, 9, 26, 15, 0, 0, 0, time.UTC)
	useLifecycleClock(t, now)

	_, err := db.exec("embeddings", "deprecate", lcA)
	assertClass(t, err, output.CodeConflict)
	if m, _ := registryOf(t, db).Get(context.Background(), lcA); m == nil || m.DeprecatedAt != nil {
		t.Fatalf("refused deprecation stamped the default: %+v", m)
	}

	for name, on := range map[string]string{"relative span": "3d", "past date": "2020-01-01", "garbage": "soon"} {
		_, err := db.exec("embeddings", "deprecate", lcB, "--on", on)
		if err == nil {
			t.Errorf("%s: --on %s accepted", name, on)
			continue
		}
		assertClass(t, err, output.CodeUsage)
	}

	out, err := db.exec("embeddings", "deprecate", lcB, "--on", "2026-10-01T00:00:00Z", "--format", "json")
	if err != nil {
		t.Fatalf("deprecate --on: %v\n%s", err, out)
	}
	doc := decodeDoc[deprecateDoc](t, out)
	if doc.DeprecatedAt != "2026-10-01T00:00:00Z" || doc.PurgeEligibleAt != "2026-10-31T00:00:00Z" ||
		doc.GracePeriod != "720h0m0s" || !doc.Changed {
		t.Errorf("deprecate = %+v", doc)
	}
	m, err := registryOf(t, db).Get(context.Background(), lcB)
	if err != nil || m.DeprecatedAt == nil || !m.DeprecatedAt.Equal(time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)) {
		t.Fatalf("stored deprecation = %+v, %v", m, err)
	}

	// Without --on: now. An already-deprecated model keeps its date.
	out, err = db.exec("embeddings", "deprecate", lcC, "--format", "json")
	if err != nil {
		t.Fatalf("deprecate: %v\n%s", err, out)
	}
	if doc := decodeDoc[deprecateDoc](t, out); doc.DeprecatedAt != now.Format(time.RFC3339) {
		t.Errorf("deprecate without --on = %+v, want now", doc)
	}
	out, err = db.exec("embeddings", "deprecate", lcB, "--format", "json")
	if err != nil {
		t.Fatalf("re-deprecate: %v\n%s", err, out)
	}
	if doc := decodeDoc[deprecateDoc](t, out); doc.Changed || doc.DeprecatedAt != "2026-10-01T00:00:00Z" {
		t.Errorf("re-deprecate without --on = %+v, want the existing date unchanged", doc)
	}

	got := evs()
	if len(got) != 2 {
		t.Fatalf("events = %+v, want two deprecations", got)
	}
	for i, id := range []string{lcB, lcC} {
		if got[i].Topic != events.TopicCtxtUpgradeEmbeddingModelDeprecated || lifecyclePayload(t, got[i]).ModelID != id {
			t.Errorf("event %d = %+v", i, got[i])
		}
	}
	if p := lifecyclePayload(t, got[0]); p.DeprecatedAt != "2026-10-01T00:00:00Z" || p.PurgeEligibleAt != "2026-10-31T00:00:00Z" {
		t.Errorf("deprecated payload = %+v", p)
	}

	// A deprecated model cannot become the default.
	_, err = db.exec("embeddings", "set-default", lcC, "--min-coverage", "0")
	assertClass(t, err, output.CodeConflict)
}

func TestEmbeddingsDeprecate_DryRun(t *testing.T) {
	db := newLifecycleDB(t)
	out, err := db.exec("embeddings", "deprecate", lcB, "--dry-run", "--format", "json")
	if err != nil {
		t.Fatalf("dry run: %v\n%s", err, out)
	}
	if doc := decodeDoc[deprecateDoc](t, out); !doc.DryRun || !doc.Changed {
		t.Errorf("dry run = %+v", doc)
	}
	if m, _ := registryOf(t, db).Get(context.Background(), lcB); m == nil || m.DeprecatedAt != nil {
		t.Errorf("dry run stamped a deprecation: %+v", m)
	}
	_, err = db.exec("embeddings", "deprecate", lcA, "--dry-run")
	assertClass(t, err, output.CodeConflict)
}

// --- purge ---------------------------------------------------------------------

func TestEmbeddingsPurge(t *testing.T) {
	db := newLifecycleDB(t)
	evs := captureLifecycleEvents(t)
	ctx := context.Background()
	deprecated := time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)
	if err := registryOf(t, db).Deprecate(ctx, lcB, deprecated); err != nil {
		t.Fatal(err)
	}

	for name, tc := range map[string]struct {
		id   string
		code string
	}{
		"default":        {lcA, output.CodeConflict},
		"not deprecated": {lcC, output.CodeConflict},
		"unregistered":   {"lifecycle-missing@1", output.CodeNotFound},
		"invalid id":     {"bad id", output.CodeUsage},
	} {
		t.Run(name, func(t *testing.T) {
			_, err := db.exec("embeddings", "purge", tc.id)
			assertClass(t, err, tc.code)
		})
	}

	useLifecycleClock(t, deprecated.Add(30*24*time.Hour-time.Minute))
	_, err := db.exec("embeddings", "purge", lcB)
	assertClass(t, err, output.CodeConflict)
	if err == nil || !strings.Contains(err.Error(), "2026-10-01T00:00:00Z") {
		t.Errorf("grace refusal must name the eligible date: %v", err)
	}
	assertModelIntact(t, db, lcB, 3)

	appendConfig(t, db, "embeddings:\n  grace_period: 24h\n")
	useLifecycleClock(t, deprecated.Add(25*time.Hour))
	out, err := db.exec("embeddings", "purge", lcB, "--dry-run", "--format", "json")
	if err != nil {
		t.Fatalf("purge dry run: %v\n%s", err, out)
	}
	if doc := decodeDoc[purgeDoc](t, out); !doc.DryRun || doc.Rows != 3 {
		t.Errorf("purge dry run = %+v", doc)
	}
	assertModelIntact(t, db, lcB, 3)

	out, err = db.exec("embeddings", "purge", lcB, "--format", "json")
	if err != nil {
		t.Fatalf("purge: %v\n%s", err, out)
	}
	doc := decodeDoc[purgeDoc](t, out)
	if doc.ModelID != lcB || doc.Rows != 3 || doc.DeprecatedAt != "2026-09-01T00:00:00Z" ||
		doc.PurgeEligibleAt != "2026-09-02T00:00:00Z" || doc.DryRun {
		t.Errorf("purge = %+v", doc)
	}
	assertModelPurged(t, db, lcB)
	assertModelIntact(t, db, lcA, 4)
	assertModelIntact(t, db, lcC, 4)
	if got := defaultModelID(t, db); got != lcA {
		t.Errorf("default = %s, want %s", got, lcA)
	}

	got := evs()
	if len(got) != 1 || got[0].Topic != events.TopicCtxtUpgradeEmbeddingModelPurged {
		t.Fatalf("events = %+v, want one purge", got)
	}
	if p := lifecyclePayload(t, got[0]); p.ModelID != lcB || p.Rows == nil || *p.Rows != 3 {
		t.Errorf("purged payload = %+v", p)
	}

	out, err = db.exec("embeddings", "list", "--format", "json")
	if err != nil || strings.Contains(out, lcB) {
		t.Errorf("purged model still listed: %v\n%s", err, out)
	}
}

func assertModelIntact(t *testing.T, db *testDB, modelID string, rows int) {
	t.Helper()
	if n := embeddingRows(t, db, modelID); n != rows {
		t.Errorf("%s has %d rows, want %d", modelID, n, rows)
	}
	if _, err := registryOf(t, db).Get(context.Background(), modelID); err != nil {
		t.Errorf("%s registry row: %v", modelID, err)
	}
	if sig := modelSignature(t, db, modelID); sig == nil {
		t.Errorf("%s lost its index signature", modelID)
	}
	if _, err := db.Driver.Embeddings().Search(context.Background(),
		storage.VectorQuery{ModelID: modelID, Vector: []float32{1, 0, 0, 0}}); err != nil {
		t.Errorf("%s index: %v", modelID, err)
	}
}

func assertModelPurged(t *testing.T, db *testDB, modelID string) {
	t.Helper()
	if n := embeddingRows(t, db, modelID); n != 0 {
		t.Errorf("%s kept %d rows", modelID, n)
	}
	if _, err := registryOf(t, db).Get(context.Background(), modelID); !errors.Is(err, registry.ErrModelNotFound) {
		t.Errorf("%s registry row after purge: %v", modelID, err)
	}
	if sig := modelSignature(t, db, modelID); sig != nil {
		t.Errorf("%s kept its index signature: %+v", modelID, sig)
	}
	if _, err := db.Driver.Embeddings().Search(context.Background(),
		storage.VectorQuery{ModelID: modelID, Vector: []float32{1, 0, 0, 0}}); !errors.Is(err, storage.ErrEmbeddingIndexMissing) {
		t.Errorf("%s index after purge: %v, want ErrEmbeddingIndexMissing", modelID, err)
	}
}

func embeddingRows(t *testing.T, db *testDB, modelID string) int {
	t.Helper()
	var n int
	if err := sqliteDB(t, db).DB().QueryRow(`SELECT COUNT(*) FROM embeddings WHERE model_id = ?`, modelID).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

func modelSignature(t *testing.T, db *testDB, modelID string) *indexsig.Row {
	t.Helper()
	row, err := indexsig.Load(context.Background(), sqliteDB(t, db).DB(), indexsig.DialectSQLite, indexsig.EmbeddingSignatureID(modelID))
	if err != nil {
		t.Fatal(err)
	}
	return row
}

// --- confirmation, targeting, signature ------------------------------------------

// purge and deprecate carry kit's typed-token annotations. The package
// tree's gate is not installed under test (see confirm_gate_test.go), so
// the annotations are replayed on an isolated tree whose command path
// matches production: without --confirm-token the body never runs, and
// the token kit prints lets it run.
func TestEmbeddingsLifecycle_ConfirmToken(t *testing.T) {
	for _, real := range []*cobra.Command{embeddingsPurgeCmd, embeddingsDeprecateCmd} {
		t.Run(real.Name(), func(t *testing.T) {
			run := func(args ...string) (string, bool, error) {
				ran := false
				leaf := &cobra.Command{
					Use:         real.Use,
					Annotations: map[string]string{},
					RunE: func(*cobra.Command, []string) error {
						ran = true
						return nil
					},
				}
				for k, v := range real.Annotations {
					leaf.Annotations[k] = v
				}
				parent := &cobra.Command{Use: "embeddings"}
				parent.AddCommand(leaf)
				r := kitcli.New(kitcli.Config{Name: "ctxt", Version: "test"})
				r.Cmd.AddCommand(parent)
				r.WrapRunE()
				var buf bytes.Buffer
				r.Cmd.SetOut(&buf)
				r.Cmd.SetErr(&buf)
				r.Cmd.SetArgs(append([]string{"embeddings", real.Name(), lcB}, args...))
				err := r.Cmd.Execute()
				return buf.String(), ran, err
			}

			out, ran, _ := run("--confirm=yes")
			if ran {
				t.Fatalf("%s ran without --confirm-token", real.Name())
			}
			token := tokenFrom(out)
			if token == "" {
				t.Fatalf("no token in refusal:\n%s", out)
			}
			if _, ran, _ := run("--confirm-token=000000000000"); ran {
				t.Fatalf("%s ran with a wrong token", real.Name())
			}
			if _, ran, err := run("--confirm-token=" + token); !ran {
				t.Fatalf("%s refused the printed token: %v", real.Name(), err)
			}
		})
	}
	if _, ok := embeddingsSetDefaultCmd.Annotations["kit/destructive-token"]; ok {
		t.Error("set-default is not destructive and must not demand a token")
	}
}

// --instance routes every lifecycle command at that instance's database,
// not the configured one.
func TestEmbeddingsLifecycle_InstanceTargeting(t *testing.T) {
	db := setupTestDB(t) // the configured database: stays empty
	inst := newLifecycleDB(t)
	runDir, err := config.RunDir()
	if err != nil {
		t.Fatal(err)
	}
	if err := pidfile.Write(runDir, pidfile.Info{
		PID: os.Getpid(), Name: "lifecycle", Port: 59123, DBPath: dbFile(inst), StartedAt: time.Now(),
	}); err != nil {
		t.Fatal(err)
	}
	if err := registryOf(t, inst).Deprecate(context.Background(), lcB, time.Now().Add(-48*time.Hour)); err != nil {
		t.Fatal(err)
	}
	appendConfig(t, db, "embeddings:\n  grace_period: 24h\n")

	for _, args := range [][]string{
		{"embeddings", "set-default", lcC},
		{"embeddings", "deprecate", lcA},
		{"embeddings", "purge", lcB},
	} {
		if out, err := execInstance(db, "lifecycle", args...); err != nil {
			t.Fatalf("%v: %v\n%s", args, err, out)
		}
	}
	if got := defaultModelID(t, inst); got != lcC {
		t.Errorf("instance default = %s, want %s", got, lcC)
	}
	if m, err := registryOf(t, inst).Get(context.Background(), lcA); err != nil || m.DeprecatedAt == nil {
		t.Errorf("instance %s not deprecated: %+v, %v", lcA, m, err)
	}
	assertModelPurged(t, inst, lcB)
	if ids := registeredIDs(t, db); len(ids) != 0 {
		t.Errorf("configured database was touched: %v", ids)
	}
}

func TestEmbeddingsLifecycle_Signature(t *testing.T) {
	for _, path := range []string{"embeddings set-default", "embeddings deprecate", "embeddings purge"} {
		target, _, err := rootCmd.Find(strings.Fields(path))
		if err != nil {
			t.Fatal(err)
		}
		if target.RunE == nil {
			t.Errorf("%s has no RunE", path)
		}
		for _, v := range root.ValidateSignature().Violations {
			if strings.Contains(v.Path, path) {
				t.Errorf("signature violation: %s [%s/%s] %s", v.Path, v.Check, v.Severity, v.Detail)
			}
		}
	}
	for flag, cmd := range map[string]*cobra.Command{"min-coverage": embeddingsSetDefaultCmd, "on": embeddingsDeprecateCmd} {
		if cmd.LocalFlags().Lookup(flag) == nil {
			t.Errorf("%s: missing local --%s", cmd.CommandPath(), flag)
		}
	}
}

// execInstance runs ctxt with --instance name against db's config.
// executeCommand pins viper's "instance" key to "" to keep tests off the
// developer's instances, and a viper.Set outranks the flag binding, so the
// key is set to what the parsed --instance flag would bind; the flag is
// passed too.
func execInstance(db *testDB, name string, args ...string) (string, error) {
	resetAllFlags(rootCmd)
	for _, k := range []string{"format", "output", "output.format", "verbose", "quiet", "no-color", "no-hints", "profile", "profile.default", "offline", "offline.enabled"} {
		viper.Set(k, "")
	}
	viper.Set("instance", name)
	defer viper.Set("instance", "")

	var buf bytes.Buffer
	rootCmd.SetArgs(append([]string{"--config", db.ConfigPath, "--instance", name}, args...))
	rootCmd.SetOut(&buf)
	rootCmd.SetErr(&buf)
	defer func() {
		rootCmd.SetOut(nil)
		rootCmd.SetErr(nil)
	}()
	err := rootCmd.Execute()
	return buf.String(), err
}

// dbFile is the SQLite file setupTestDB created for db.
func dbFile(db *testDB) string {
	return filepath.Join(filepath.Dir(db.ConfigPath), "test.db")
}

// --- provider-backed flows (recorded Ollama) ------------------------------------

// `ctxt embeddings set-default` changes what the very next `find` searches:
// the query path reads the default per query, with no restart and no cache
// to invalidate. Replays the find-query-path cassettes.
func TestEmbeddingsSetDefault_NextQueryFollows(t *testing.T) {
	db := setupTestDB(t)
	ctx := context.Background()
	d := sqliteDB(t, db)
	client, calls := providertest.OllamaClient(t, findCassettes)
	findEmbeddingHTTPClient = client
	t.Cleanup(func() { findEmbeddingHTTPClient = nil })

	query := "rotating signing keys without downtime"
	arctic := func(id, endpoint string) registry.Model {
		return registry.Model{
			ModelID: id, Provider: "ollama", Dimension: 1024,
			ConfigJSON: `{"backend":"ollama","model":"snowflake-arctic-embed2","endpoint":"` + endpoint + `"}`,
		}
	}
	a, b := arctic("arctic-e2e-a", "http://127.0.0.1:11434"), arctic("arctic-e2e-b", "http://127.0.0.1:11556")
	p, err := embeddings.NewProviderResolver(&embeddings.Resolver{
		LookupEnv: func(string) (string, bool) { return "", false }, HTTPClient: client,
	}).ForModel(ctx, a)
	if err != nil {
		t.Fatal(err)
	}
	vec, err := p.Embed(ctx, query)
	if err != nil {
		t.Fatal(err)
	}
	reg := registry.New(d.DB())
	for _, m := range []registry.Model{a, b} {
		if err := reg.Register(ctx, m, m.ModelID == a.ModelID); err != nil {
			t.Fatal(err)
		}
		if err := d.Embeddings().EnsureIndex(ctx, storage.EmbeddingModelSpec{ModelID: m.ModelID, Provider: m.Provider, Dimension: m.Dimension}); err != nil {
			t.Fatal(err)
		}
	}
	// The same vector under one model each: a hit names the index searched.
	for id, model := range map[string]string{"note-a": a.ModelID, "note-b": b.ModelID} {
		now := time.Now()
		if err := d.Objects().Create(ctx, &storage.KnowledgeObject{ID: id, Type: "note", RawContent: "unrelated wording", CreatedAt: now, UpdatedAt: now}); err != nil {
			t.Fatal(err)
		}
		if err := d.Embeddings().Put(ctx, id, []storage.ObjectVector{{ModelID: model, Vector: vec}}); err != nil {
			t.Fatal(err)
		}
	}

	find := func() (ids []string, model string) {
		t.Helper()
		stdout, stderr, err := execFind(t, db, "find", query, "--semantic", "--format", "json")
		if err != nil {
			t.Fatalf("find: %v\n%s", err, stderr)
		}
		var out struct {
			Objects []struct {
				ID string `json:"id"`
			} `json:"objects"`
			Diagnostics struct {
				Semantic struct {
					Status  string `json:"status"`
					ModelID string `json:"model_id"`
				} `json:"semantic"`
			} `json:"diagnostics"`
		}
		if err := json.Unmarshal([]byte(stdout), &out); err != nil {
			t.Fatalf("stdout is not JSON: %v\n%s", err, stdout)
		}
		if out.Diagnostics.Semantic.Status != "ok" {
			t.Fatalf("semantic status = %q, stderr:\n%s", out.Diagnostics.Semantic.Status, stderr)
		}
		for _, o := range out.Objects {
			ids = append(ids, o.ID)
		}
		return ids, out.Diagnostics.Semantic.ModelID
	}

	if ids, model := find(); model != a.ModelID || len(ids) != 1 || ids[0] != "note-a" {
		t.Fatalf("before the flip got %v under %s, want [note-a] under %s", ids, model, a.ModelID)
	}
	// b covers one object of two.
	if out, err := db.exec("embeddings", "set-default", b.ModelID, "--min-coverage", "0.5"); err != nil {
		t.Fatalf("set-default: %v\n%s", err, out)
	}
	before := len(calls.URLs())
	if ids, model := find(); model != b.ModelID || len(ids) != 1 || ids[0] != "note-b" {
		t.Fatalf("after the flip got %v under %s, want [note-b] under %s", ids, model, b.ModelID)
	}
	if n := len(calls.URLs()) - before; n != 1 {
		t.Errorf("post-flip query made %d provider calls, want 1", n)
	}
}

// Deprecating a model ends its dual-write: ingest before the deprecation
// writes vectors for both registered models, ingest after it writes only
// the remaining one. Replays the embeddings-migrate cassettes.
func TestEmbeddingsDeprecate_StopsDualWrite(t *testing.T) {
	e := newMigrateEnv(t)
	rows := func(modelID string) int { return embeddingRows(t, e.db, modelID) }

	before := len(e.calls.URLs())
	e.ingest(t, migrateBodies[0])
	if rows(migrateFromID) != 1 || rows(migrateToID) != 1 {
		t.Fatalf("dual-write before deprecation: %s=%d %s=%d, want 1 each",
			migrateFromID, rows(migrateFromID), migrateToID, rows(migrateToID))
	}
	if n := len(e.calls.URLs()) - before; n != 2 {
		t.Errorf("ingest before deprecation made %d provider calls, want 2", n)
	}

	if out, err := e.db.exec("embeddings", "deprecate", migrateToID); err != nil {
		t.Fatalf("deprecate: %v\n%s", err, out)
	}
	before = len(e.calls.URLs())
	e.ingest(t, migrateBodies[1])
	if rows(migrateFromID) != 2 || rows(migrateToID) != 1 {
		t.Errorf("after deprecation: %s=%d %s=%d, want 2 and 1 (no new row for the deprecated model)",
			migrateFromID, rows(migrateFromID), migrateToID, rows(migrateToID))
	}
	if n := len(e.calls.URLs()) - before; n != 1 {
		t.Errorf("ingest after deprecation made %d provider calls, want 1", n)
	}
}
