package builtins

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"maps"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ideacrafterslabs/ctxt/internal/config"
	"github.com/ideacrafterslabs/ctxt/internal/embeddings"
	"github.com/ideacrafterslabs/ctxt/internal/embeddings/embeddingtest"
	"github.com/ideacrafterslabs/ctxt/internal/embeddings/registry"
	"github.com/ideacrafterslabs/ctxt/internal/jobs"
	"github.com/ideacrafterslabs/ctxt/internal/pipeline"
	"github.com/ideacrafterslabs/ctxt/internal/search"
	"github.com/ideacrafterslabs/ctxt/internal/service"
	"github.com/ideacrafterslabs/ctxt/internal/storage"
	"github.com/ideacrafterslabs/ctxt/internal/storageutil"
)

// Dedup runs end to end: service.Analyze enqueues, a worker runs the
// registry's text.long pipeline (embedding through recorded Ollama, then
// dedup) against a real SQLite driver whose per-model index holds the
// earlier objects' vectors. Re-record:
//
//	XRR_MODE=record go test -tags fts5 -count=1 -run Dedup ./internal/pipeline/builtins/
const (
	dedupBase = "numbat sightings near the dryandra woodland"
	// dedupNear rewords dedupBase: similarity 0.965 under
	// snowflake-arctic-embed2.
	dedupNear = "numbat sighted near the dryandra woodlands"
	// dedupFar shares nothing with dedupBase: similarity 0.19.
	dedupFar = "quarterly payroll tax deadlines for small businesses"

	// Thresholds bracketing the base/near similarity; the lower one is
	// the configured default.
	dedupBelowNear = 0.95
	dedupAboveNear = 0.98
)

var dedupSnowflake = registry.Model{
	ModelID: "snowflake-arctic-embed2@default", Provider: "ollama", Dimension: 1024,
	ConfigJSON: `{"backend":"ollama","model":"snowflake-arctic-embed2","endpoint":"http://127.0.0.1:11434"}`,
}

type dedupEnv struct {
	drv storage.StorageDriver
	svc *service.Service
	q   *jobs.Queue
}

// newDedupEnv wires a real SQLite driver, its model registry (with
// defaultModel as default when set) and per-model index into the
// production registry builder and service, with dup as the duplicates
// config on both.
func newDedupEnv(t *testing.T, dup config.DuplicatesConfig, defaultModel bool) *dedupEnv {
	t.Helper()
	ctx := context.Background()
	drv := storageutil.NewTestDriver(t)
	reg, err := registry.ForDriver(drv)
	if err != nil {
		t.Fatal(err)
	}
	if err := reg.Register(ctx, dedupSnowflake, defaultModel); err != nil {
		t.Fatal(err)
	}
	if err := drv.Embeddings().EnsureIndex(ctx, embeddings.SpecFor(dedupSnowflake)); err != nil {
		t.Fatal(err)
	}
	pipes := ConfiguredRegistryWithOpts(BuildOpts{
		Models:     reg,
		Resolver:   wiringResolver(t),
		Embeddings: drv.Embeddings(),
		Audit:      drv.AuditLog(),
		Duplicates: dup,
	})
	q := jobs.NewQueue(drv.Jobs())
	svc := service.New(drv, q, pipes, search.NewEngine(drv), "", nil, config.Config{Duplicates: dup})
	return &dedupEnv{drv: drv, svc: svc, q: q}
}

// ingest analyzes body through text.long and, when a job was enqueued,
// runs a worker until it finishes. It returns the stored (or matched)
// object's ID.
func (e *dedupEnv) ingest(t *testing.T, body string) string {
	t.Helper()
	ctx := context.Background()
	id, err := e.svc.Analyze(ctx, service.AnalyzeRequest{Content: body, Type: "text", Pipeline: "text.long", Source: "cli"})
	if err != nil {
		t.Fatalf("analyze %q: %v", body, err)
	}
	job, err := e.q.Get(ctx, id)
	if err != nil || job == nil {
		return id // analyze answered with an existing object
	}

	runCtx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	pool := jobs.NewWorkerPool(e.q, e.svc.Pipes, e.drv, 1, nil, config.JobsConfig{
		PollInterval: 20 * time.Millisecond, StaleTimeout: time.Minute, MaxRetries: 1, MaxHops: 5,
	})
	go func() {
		for runCtx.Err() == nil {
			if j, _ := e.q.Get(ctx, id); j != nil && (j.Status == storage.JobCompleted || j.Status == storage.JobFailed) {
				cancel()
				return
			}
			time.Sleep(20 * time.Millisecond)
		}
	}()
	_ = pool.Start(runCtx)

	job, err = e.q.Get(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	if job.Status != storage.JobCompleted {
		t.Fatalf("job for %q: status %q (err=%q), want completed", body, job.Status, job.Error)
	}
	return job.ResultID
}

func (e *dedupEnv) object(t *testing.T, id string) *storage.KnowledgeObject {
	t.Helper()
	obj, err := e.drv.Objects().Get(context.Background(), id)
	if err != nil || obj == nil {
		t.Fatalf("get object %s: %v", id, err)
	}
	return obj
}

type dedupLog struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (l *dedupLog) Write(p []byte) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.b.Write(p)
}

func (l *dedupLog) String() string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.b.String()
}

// captureDedupLog routes slog, debug level included, into a buffer.
func captureDedupLog(t *testing.T) *dedupLog {
	t.Helper()
	buf := &dedupLog{}
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(buf, &slog.HandlerOptions{Level: slog.LevelDebug})))
	t.Cleanup(func() { slog.SetDefault(prev) })
	return buf
}

func dupCfg(policy string, threshold float64) config.DuplicatesConfig {
	return config.DuplicatesConfig{Policy: policy, SimilarityThreshold: threshold, CheckExact: true, CheckSimilar: true}
}

func (e *dedupEnv) objectCount(t *testing.T) int {
	t.Helper()
	_, n, err := e.drv.Objects().List(context.Background(), storage.ObjectFilter{Limit: 100})
	if err != nil {
		t.Fatal(err)
	}
	return n
}

func requireDuplicateOf(t *testing.T, obj *storage.KnowledgeObject, want string) float64 {
	t.Helper()
	if got, _ := obj.Metadata["duplicate_of"].(string); got != want {
		t.Fatalf("duplicate_of = %q, want %q (metadata %v)", got, want, obj.Metadata)
	}
	if kind, _ := obj.Metadata["duplicate_kind"].(string); kind != "similar" {
		t.Errorf("duplicate_kind = %q, want similar", kind)
	}
	sim, ok := obj.Metadata["duplicate_similarity"].(float64)
	if !ok {
		t.Fatalf("duplicate_similarity missing: %v", obj.Metadata)
	}
	return sim
}

// similarAudits returns the dedup.similar rows the driver's audit log holds.
func (e *dedupEnv) similarAudits(t *testing.T) []*storage.AuditEntry {
	t.Helper()
	rows, _, err := e.drv.AuditLog().List(context.Background(), storage.AuditFilter{EventType: "dedup.similar", Limit: 100})
	if err != nil {
		t.Fatal(err)
	}
	return rows
}

// requireSimilarAudit asserts rows hold exactly one dedup.similar entry
// for objectID as a duplicate of dupOf at sim under policy, carrying IDs
// and scores only.
func requireSimilarAudit(t *testing.T, rows []*storage.AuditEntry, objectID, dupOf string, sim float64, policy string) {
	t.Helper()
	if len(rows) != 1 {
		t.Fatalf("%d dedup.similar audit rows, want 1: %v", len(rows), rows)
	}
	row := rows[0]
	if row.ObjectID != objectID || row.Actor != "system" {
		t.Errorf("audit row object=%q actor=%q, want %q system", row.ObjectID, row.Actor, objectID)
	}
	want := map[string]any{
		"duplicate_of": dupOf,
		"similarity":   sim,
		"kind":         "similar",
		"policy":       policy,
		"model_id":     dedupSnowflake.ModelID,
	}
	if !maps.Equal(row.Payload, want) {
		t.Errorf("audit payload = %v, want %v", row.Payload, want)
	}
	raw, err := json.Marshal(row)
	if err != nil {
		t.Fatal(err)
	}
	for _, word := range strings.Fields(dedupBase + " " + dedupNear) {
		if strings.Contains(string(raw), word) {
			t.Errorf("audit row carries content word %q: %s", word, raw)
		}
	}
}

// requireExactAudit asserts the audit log holds exactly one dedup.exact
// entry: the hash match against base under policy, without content.
func requireExactAudit(t *testing.T, e *dedupEnv, base, policy string) {
	t.Helper()
	rows, _, err := e.drv.AuditLog().List(context.Background(), storage.AuditFilter{EventType: "dedup.exact", Limit: 100})
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 {
		t.Fatalf("%d dedup.exact audit rows, want 1: %v", len(rows), rows)
	}
	want := map[string]any{"duplicate_of": base, "similarity": 1.0, "kind": "exact", "policy": policy}
	if row := rows[0]; row.ObjectID != base || row.Actor != "system" || !maps.Equal(row.Payload, want) {
		t.Errorf("audit row object=%q actor=%q payload=%v, want %q system %v", row.ObjectID, row.Actor, row.Payload, base, want)
	}
}

func requireNoDuplicate(t *testing.T, obj *storage.KnowledgeObject) {
	t.Helper()
	if dup, ok := obj.Metadata["duplicate_of"]; ok {
		t.Fatalf("object %s flagged as duplicate of %v (metadata %v)", obj.ID, dup, obj.Metadata)
	}
}

// A reworded note scores above the default 0.95 threshold: it is stored,
// recorded as a near-duplicate of the first, and warned about.
func TestDedupE2E_NearDuplicateAboveThresholdIsRecorded(t *testing.T) {
	logs := captureDedupLog(t)
	e := newDedupEnv(t, dupCfg("warn", dedupBelowNear), true)
	base := e.ingest(t, dedupBase)
	near := e.ingest(t, dedupNear)

	if near == base {
		t.Fatal("near-duplicate collapsed onto the first object; warn keeps both")
	}
	sim := requireDuplicateOf(t, e.object(t, near), base)
	if sim < dedupBelowNear || sim >= dedupAboveNear {
		t.Errorf("similarity %v outside the measured bracket [%v, %v)", sim, dedupBelowNear, dedupAboveNear)
	}
	requireNoDuplicate(t, e.object(t, base))
	if !strings.Contains(logs.String(), `msg="dedup: near-duplicate detected"`) ||
		!strings.Contains(logs.String(), "duplicate_of="+base) {
		t.Errorf("warn policy logged no near-duplicate warning; logs:\n%s", logs.String())
	}
}

// The near-duplicate decision reaches the audit log, not just slog: one
// dedup.similar row naming the new object, the one it duplicates, the
// score, policy and model, and no content.
func TestDedupE2E_NearDuplicateIsAudited(t *testing.T) {
	for _, policy := range []string{"warn", "keep"} {
		t.Run(policy, func(t *testing.T) {
			e := newDedupEnv(t, dupCfg(policy, dedupBelowNear), true)
			base := e.ingest(t, dedupBase)
			if rows := e.similarAudits(t); len(rows) != 0 {
				t.Fatalf("first ingest audited as a duplicate: %v", rows)
			}
			near := e.ingest(t, dedupNear)
			sim := requireDuplicateOf(t, e.object(t, near), base)
			requireSimilarAudit(t, e.similarAudits(t), near, base, sim, policy)
		})
	}
}

// drop audits the decision for the draft it suppresses.
func TestDedupE2E_DropIsAudited(t *testing.T) {
	e := newDedupEnv(t, dupCfg("drop", dedupBelowNear), true)
	base := e.ingest(t, dedupBase)
	p, err := e.svc.Pipes.Get("text.long")
	if err != nil {
		t.Fatal(err)
	}
	out := runSteps(t, p, &storage.KnowledgeObject{ID: "draft", RawContent: dedupNear, Source: "cli"})
	requireSimilarAudit(t, e.similarAudits(t), "draft", base, requireDuplicateOf(t, out, base), "drop")
}

// The same pair under a threshold above its similarity, and an unrelated
// note under the default, are not duplicates, and nothing is audited.
func TestDedupE2E_BelowThresholdIsNotADuplicate(t *testing.T) {
	e := newDedupEnv(t, dupCfg("warn", dedupAboveNear), true)
	e.ingest(t, dedupBase)
	requireNoDuplicate(t, e.object(t, e.ingest(t, dedupNear)))
	if rows := e.similarAudits(t); len(rows) != 0 {
		t.Errorf("below-threshold pair audited: %v", rows)
	}

	e = newDedupEnv(t, dupCfg("warn", dedupBelowNear), true)
	e.ingest(t, dedupBase)
	requireNoDuplicate(t, e.object(t, e.ingest(t, dedupFar)))
	if rows := e.similarAudits(t); len(rows) != 0 {
		t.Errorf("unrelated note audited: %v", rows)
	}
}

// check_similar=false leaves dedup out of every pipeline.
func TestDedupE2E_CheckSimilarDisabled(t *testing.T) {
	cfg := dupCfg("warn", dedupBelowNear)
	cfg.CheckSimilar = false
	e := newDedupEnv(t, cfg, true)
	e.ingest(t, dedupBase)
	requireNoDuplicate(t, e.object(t, e.ingest(t, dedupNear)))

	p, err := e.svc.Pipes.Get("text.long")
	if err != nil {
		t.Fatal(err)
	}
	for _, s := range p.Steps {
		if s.Name() == "dedup" {
			t.Fatal("dedup injected with check_similar=false")
		}
	}
}

// With check_similar on, every pipeline that embeds runs dedup directly
// after its embedding step, and no other pipeline runs it.
func TestDedupE2E_InjectedAfterEmbeddingInEveryEmbeddingPipeline(t *testing.T) {
	e := newDedupEnv(t, dupCfg("warn", dedupBelowNear), true)
	checked := map[string]bool{}
	for name, d := range Defs() {
		p, err := e.svc.Pipes.Get(name)
		if err != nil {
			continue // pipeline needs providers this registry lacks
		}
		names := make([]string, len(p.Steps))
		for i, s := range p.Steps {
			names[i] = s.Name()
		}
		embeds := slices.Contains(d.Steps, "embedding")
		di := slices.Index(names, "dedup")
		if !embeds {
			if di >= 0 {
				t.Errorf("%s: dedup without an embedding step: %v", name, names)
			}
			continue
		}
		if di < 1 || names[di-1] != "embedding_generator" {
			t.Errorf("%s: dedup not directly after embedding: %v", name, names)
		}
		checked[name] = true
	}
	for _, name := range []string{"text.short", "text.long", "doc.markdown", "url.generic"} {
		if !checked[name] {
			t.Errorf("%s not checked; checked %v", name, checked)
		}
	}
}

func stepNames(t *testing.T, reg interface {
	Get(string) (*pipeline.Pipeline, error)
}, name string,
) []string {
	t.Helper()
	p, err := reg.Get(name)
	if err != nil {
		t.Fatal(err)
	}
	out := make([]string, len(p.Steps))
	for i, s := range p.Steps {
		out[i] = s.Name()
	}
	return out
}

// The per-pipeline override registry (dpkms serve) injects dedup too, and
// skip_steps: [dedup] opts one pipeline out.
func TestPipelineOverridesInjectDedupAndHonourSkip(t *testing.T) {
	opts := BuildOpts{
		Models:     embeddingtest.Models{registry.Model{ModelID: "m@1", Dimension: 3, IsDefault: true}},
		Embeddings: embeddingtest.NewMemStore(),
		Duplicates: dupCfg("warn", dedupBelowNear),
	}
	reg := ConfiguredRegistryWithPipelineOverrides(opts, config.ProvidersConfig{}, config.PipelinesConfig{
		Overrides: map[string]config.PipelineOverride{"text.short": {SkipSteps: []string{"dedup"}}},
	})
	if names := stepNames(t, reg, "text.long"); !slices.Contains(names, "dedup") {
		t.Errorf("text.long: dedup not injected: %v", names)
	}
	if names := stepNames(t, reg, "text.short"); slices.Contains(names, "dedup") {
		t.Errorf("text.short: skip_steps [dedup] ignored: %v", names)
	}
}

// An exact re-ingest is caught by content hash, not by dedup: under warn
// the worker reinforces the first object, which is never marked a
// duplicate of itself; under drop analyze answers with the first object.
func TestDedupE2E_ExactDuplicate(t *testing.T) {
	for _, policy := range []string{"warn", "keep", "drop"} {
		t.Run(policy, func(t *testing.T) {
			e := newDedupEnv(t, dupCfg(policy, dedupBelowNear), true)
			base := e.ingest(t, dedupBase)
			if again := e.ingest(t, dedupBase); again != base {
				t.Fatalf("re-ingest stored %s, want the first object %s", again, base)
			}
			if n := e.objectCount(t); n != 1 {
				t.Errorf("%d objects after an exact re-ingest, want 1", n)
			}
			requireNoDuplicate(t, e.object(t, base))
			requireExactAudit(t, e, base, policy)
		})
	}
}

// keep records the near-duplicate on the object without a warning.
func TestDedupE2E_KeepPolicyRecordsSilently(t *testing.T) {
	logs := captureDedupLog(t)
	e := newDedupEnv(t, dupCfg("keep", dedupBelowNear), true)
	base := e.ingest(t, dedupBase)
	requireDuplicateOf(t, e.object(t, e.ingest(t, dedupNear)), base)
	if strings.Contains(logs.String(), "near-duplicate detected") {
		t.Errorf("keep policy warned; logs:\n%s", logs.String())
	}
}

// drop marks the pipeline's output suppress_output on top of the record.
func TestDedupE2E_DropPolicyMarksOutputSuppressed(t *testing.T) {
	e := newDedupEnv(t, dupCfg("drop", dedupBelowNear), true)
	base := e.ingest(t, dedupBase)
	p, err := e.svc.Pipes.Get("text.long")
	if err != nil {
		t.Fatal(err)
	}
	out := runSteps(t, p, &storage.KnowledgeObject{ID: "draft", RawContent: dedupNear, Source: "cli"})
	requireDuplicateOf(t, out, base)
	if out.Metadata["suppress_output"] != true {
		t.Errorf("drop left suppress_output unset: %v", out.Metadata)
	}
}

// With no default model, ingest still succeeds and dedup is skipped with a
// debug line, never an error.
func TestDedupE2E_NoDefaultModelSkips(t *testing.T) {
	logs := captureDedupLog(t)
	e := newDedupEnv(t, dupCfg("warn", dedupBelowNear), false)
	e.ingest(t, dedupBase)
	requireNoDuplicate(t, e.object(t, e.ingest(t, dedupNear)))
	if !strings.Contains(logs.String(), `level=DEBUG msg="dedup: skipped" reason="no default embedding model"`) {
		t.Errorf("no debug line for the skipped dedup; logs:\n%s", logs.String())
	}
}
