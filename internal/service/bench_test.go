package service_test

import (
	"context"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"testing"
	"time"

	"github.com/ideacrafterslabs/ctxt/internal/retrieval"
	"github.com/ideacrafterslabs/ctxt/internal/search"
	"github.com/ideacrafterslabs/ctxt/internal/service"
	"github.com/ideacrafterslabs/ctxt/internal/storage"
	"github.com/ideacrafterslabs/ctxt/internal/storage/sqlite"
	"github.com/ideacrafterslabs/ctxt/internal/storage/storagetest/corpus"
	"github.com/ideacrafterslabs/ctxt/internal/storageutil"
)

const (
	// benchObjectsEnv sizes the benchmark corpus.
	benchObjectsEnv = "CTXT_BENCH_OBJECTS"
	// benchSeed is the corpus seed: fixed, so every run measures one
	// corpus.
	benchSeed = 1
	// benchPage is the list page size: the default of GET /api/v1/objects.
	benchPage = 20
)

// benchObjects is how many objects BenchmarkOwner builds: benchObjectsEnv
// when set, else corpus.DefaultObjects. It is read when the package
// initialises, before TestMain runs, because testguard.Main clears every
// CTXT_* variable: a read inside the benchmark would always find it
// unset.
var benchObjects, benchObjectsErr = benchObjectsFromEnv()

func benchObjectsFromEnv() (int, error) {
	v := os.Getenv(benchObjectsEnv)
	if v == "" {
		return corpus.DefaultObjects, nil
	}
	n, err := strconv.Atoi(v)
	if err != nil || n < 1 {
		return 0, fmt.Errorf("%s=%q: want a positive object count", benchObjectsEnv, v)
	}
	return n, nil
}

// BenchmarkOwner is the owner's find and list over the generated corpus:
// the baseline the sharing phases compare a restricted viewer against.
// The corpus (corpus.Spec{Objects: benchObjects, Seed: benchSeed}) is
// built once, in b.TempDir(), and shared by the sub-benchmarks. Each
// reports p95-ms, the 95th percentile of its iteration latencies in
// milliseconds, next to the harness's mean ns/op, and build-s, how long
// the corpus took to build.
//
// find runs in full-text mode and cycles through corpus.Queries. Vector
// and hybrid modes embed the query with a provider, which the benchmark
// has none of, so they are not measured. list reads the first page at
// the API's default size, total included.
//
// Skipped under -short. Sized by CTXT_BENCH_OBJECTS; the baseline run is
//
//	CTXT_BENCH_OBJECTS=100000 go test -tags fts5 -run '^$' -bench BenchmarkOwner -benchtime 10s ./internal/service/
func BenchmarkOwner(b *testing.B) {
	if testing.Short() {
		b.Skip("corpus benchmark skipped under -short")
	}
	if benchObjectsErr != nil {
		b.Fatal(benchObjectsErr)
	}
	ctx := ownerCtx()
	svc, c := newBenchService(ctx, b)
	queries := c.Queries()
	find := func(q string) error {
		res, err := svc.Find(ctx, service.FindRequest{Query: q, Mode: service.FindModeFTS}, retrieval.SemanticSource{})
		if err != nil {
			return err
		}
		if res.Total == 0 {
			return fmt.Errorf("find %q: no results", q)
		}
		return nil
	}

	b.Run("find", func(b *testing.B) {
		for _, q := range queries {
			if err := find(q); err != nil {
				b.Fatal(err)
			}
		}
		i := 0
		benchP95(b, c, func() error {
			q := queries[i%len(queries)]
			i++
			return find(q)
		})
	})
	b.Run("list", func(b *testing.B) {
		benchP95(b, c, func() error {
			objs, total, err := svc.ListObjects(ctx, storage.ObjectFilter{Limit: benchPage})
			if err != nil {
				return err
			}
			if len(objs) != benchPage || total != c.Spec.Objects {
				return fmt.Errorf("list: %d objects of %d, want %d of %d", len(objs), total, benchPage, c.Spec.Objects)
			}
			return nil
		})
	})
}

// newBenchService opens a SQLite store in b.TempDir(), builds the corpus
// into it and returns a service over it.
func newBenchService(ctx context.Context, b *testing.B) (*service.Service, *corpus.Corpus) {
	b.Helper()
	drv, err := sqlite.New(filepath.Join(b.TempDir(), "corpus.db"), storageutil.NewTestBlobStore(b))
	if err != nil {
		b.Fatalf("open store: %v", err)
	}
	if err := drv.Init(ctx); err != nil {
		b.Fatalf("init store: %v", err)
	}
	b.Cleanup(func() { _ = drv.Close(ownerCtx()) })

	c, err := corpus.Build(ctx, drv, corpus.Spec{
		Objects: benchObjects, Seed: benchSeed,
		Progress: func(done, total int) { b.Logf("corpus: %d/%d objects", done, total) },
	})
	if err != nil {
		b.Fatalf("build corpus: %v", err)
	}
	b.Logf("corpus: %d objects (%d contacts, %d notes), %d edges, built in %s",
		c.Spec.Objects, len(c.Contacts), len(c.NoteIDs), c.Edges, c.BuildTime.Round(time.Millisecond))
	return service.New(drv, nil, nil, search.NewEngine(drv), "", nil), c
}

// benchP95 runs op for b's iterations over c and reports the 95th
// percentile of their latencies as p95-ms, next to the mean the harness
// reports, and c's build time as build-s.
func benchP95(b *testing.B, c *corpus.Corpus, op func() error) {
	b.Helper()
	b.ReportAllocs()
	var samples []time.Duration
	for b.Loop() {
		start := time.Now()
		if err := op(); err != nil {
			b.Fatal(err)
		}
		samples = append(samples, time.Since(start))
	}
	b.ReportMetric(float64(percentile(samples, 0.95))/float64(time.Millisecond), "p95-ms")
	b.ReportMetric(c.BuildTime.Seconds(), "build-s")
}

// percentile is the nearest-rank p-th percentile of samples, which it
// sorts in place.
func percentile(samples []time.Duration, p float64) time.Duration {
	if len(samples) == 0 {
		return 0
	}
	slices.Sort(samples)
	rank := max(1, int(math.Ceil(p*float64(len(samples)))))
	return samples[rank-1]
}
