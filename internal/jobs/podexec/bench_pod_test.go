//go:build podbench

package podexec

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/ideacrafterslabs/ctxt/internal/config"
	"github.com/ideacrafterslabs/ctxt/internal/jobs"
	"github.com/ideacrafterslabs/ctxt/internal/pipeline/builtins"
	"github.com/ideacrafterslabs/ctxt/internal/podrun"
	"github.com/ideacrafterslabs/ctxt/internal/storage"
	"github.com/ideacrafterslabs/ctxt/internal/storage/sqlite"
	"github.com/ideacrafterslabs/ctxt/internal/storageutil"
)

func TestPodExecutorBenchmark(t *testing.T) {
	podBin := os.Getenv("PODRUN_POD_BIN")
	if podBin == "" {
		t.Skip("set PODRUN_POD_BIN to run live pod benchmark")
	}
	image := os.Getenv("PODBENCH_IMAGE")
	if image == "" {
		image = "ctxt-pod-worker:local"
	}

	podHome := t.TempDir()
	env := []string{
		"HOME=" + podHome,
		"XDG_CONFIG_HOME=" + filepath.Join(podHome, "config"),
		"XDG_DATA_HOME=" + filepath.Join(podHome, "data"),
		"XDG_STATE_HOME=" + filepath.Join(podHome, "state"),
		"XDG_CACHE_HOME=" + filepath.Join(podHome, "cache"),
	}

	client := podrun.NewClient(podBin, podrun.NewExecRunner(), podrun.DefaultTimeouts())
	client.Env = env

	// 1. Phase breakdown across 8 sequential ephemeral pods.
	t.Log("=== PHASE 1: Ephemeral Pod Overhead Breakdown (N=8 sequential, docker) ===")
	var createD, uploadD, execD, destroyD, totalD []time.Duration
	reqPath, err := writeRequestFile(jobs.DraftJobRequest{
		Job: storage.Job{
			ID:       "bench-phase",
			Type:     "ingest:code",
			Pipeline: "text.short",
			Payload:  "package bench\nfunc Hello() string { return \"world\" }\n",
			Source:   "bench",
		},
		MaxHops: 5,
	})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = os.Remove(reqPath) }()

	for i := 0; i < 8; i++ {
		name := fmt.Sprintf("ctxt-bench-phase-%d", i)
		t0 := time.Now()
		if _, err := client.CreatePod(context.Background(), podrun.CreateOpts{
			Name:     name,
			Provider: "docker",
			Image:    image,
			Note:     "bench phase",
		}); err != nil {
			t.Fatalf("create %s: %v", name, err)
		}
		t1 := time.Now()
		if err := client.Upload(context.Background(), name, reqPath, "/tmp/ctxt-job-req.json"); err != nil {
			_ = client.DestroyPod(context.Background(), name, "cleanup")
			t.Fatalf("upload %s: %v", name, err)
		}
		t2 := time.Now()
		res, err := client.Exec(context.Background(), name, []string{"dpkms", "worker", "run-draft", "--input", "/tmp/ctxt-job-req.json"}, podrun.RunOpts{})
		if err != nil || res.ExitCode != 0 {
			_ = client.DestroyPod(context.Background(), name, "cleanup")
			t.Fatalf("exec %s: err=%v exit=%d stderr=%s", name, err, res.ExitCode, string(res.Stderr))
		}
		t3 := time.Now()
		if err := client.DestroyPod(context.Background(), name, "bench cleanup"); err != nil {
			t.Fatalf("destroy %s: %v", name, err)
		}
		t4 := time.Now()
		createD = append(createD, t1.Sub(t0))
		uploadD = append(uploadD, t2.Sub(t1))
		execD = append(execD, t3.Sub(t2))
		destroyD = append(destroyD, t4.Sub(t3))
		totalD = append(totalD, t4.Sub(t0))
	}
	t.Logf("create : %s", summarizeDurations(createD))
	t.Logf("upload : %s", summarizeDurations(uploadD))
	t.Logf("exec   : %s", summarizeDurations(execD))
	t.Logf("destroy: %s", summarizeDurations(destroyD))
	t.Logf("TOTAL  : %s", summarizeDurations(totalD))

	// 2. End-to-end WorkerPool throughput & wall-clock matrix.
	shortPayload := "package bench\n// Short text note for throughput benchmark.\nfunc F() int { return 42 }\n"
	largePayload := strings.Repeat("# Heading Section\n\nParagraph of technical documentation covering ingestion pipelines, indexing, embeddings, and worker queues.\n\n", 800) // ~100KB

	type scenario struct {
		name      string
		jobsCount int
		pipeline  string
		payload   string
		delay     time.Duration
		mode      string
		workers   int
		inflight  int
	}
	scenarios := []scenario{
		// Workload A: fast text.short (~1ms compute)
		{"fast-text local w=1", 16, "text.short", shortPayload, 0, "local", 1, 0},
		{"fast-text local w=4", 16, "text.short", shortPayload, 0, "local", 4, 0},
		{"fast-text pod w=1", 16, "text.short", shortPayload, 0, "pod", 1, 1},
		{"fast-text pod w=2", 16, "text.short", shortPayload, 0, "pod", 2, 2},
		{"fast-text pod w=4", 16, "text.short", shortPayload, 0, "pod", 4, 4},
		{"fast-text pod w=8", 16, "text.short", shortPayload, 0, "pod", 8, 8},
		// Workload B: 100KB text.long parsing
		{"100KB-doc local w=1", 12, "text.long", largePayload, 0, "local", 1, 0},
		{"100KB-doc local w=4", 12, "text.long", largePayload, 0, "local", 4, 0},
		{"100KB-doc pod w=4", 12, "text.long", largePayload, 0, "pod", 4, 4},
		{"100KB-doc pod w=8", 12, "text.long", largePayload, 0, "pod", 8, 8},
		// Workload C: 1.5s simulated LLM/OCR/fetch step
		{"1.5s-heavy local w=1", 12, "text.short", shortPayload, 1500 * time.Millisecond, "local", 1, 0},
		{"1.5s-heavy local w=2", 12, "text.short", shortPayload, 1500 * time.Millisecond, "local", 2, 0},
		{"1.5s-heavy local w=4", 12, "text.short", shortPayload, 1500 * time.Millisecond, "local", 4, 0},
		{"1.5s-heavy pod w=4", 12, "text.short", shortPayload, 1500 * time.Millisecond, "pod", 4, 4},
		{"1.5s-heavy pod w=8", 12, "text.short", shortPayload, 1500 * time.Millisecond, "pod", 8, 8},
		// Workload D: 3.0s simulated transcription/vision step
		{"3.0s-media local w=2", 12, "text.short", shortPayload, 3000 * time.Millisecond, "local", 2, 0},
		{"3.0s-media local w=4", 12, "text.short", shortPayload, 3000 * time.Millisecond, "local", 4, 0},
		{"3.0s-media pod w=8", 12, "text.short", shortPayload, 3000 * time.Millisecond, "pod", 8, 8},
	}

	t.Log("=== PHASE 2: WorkerPool End-to-End Matrix ===")
	for _, sc := range scenarios {
		wall, err := runBenchScenario(t, client, image, sc.jobsCount, sc.pipeline, sc.payload, sc.delay, sc.mode, sc.workers, sc.inflight)
		if err != nil {
			t.Fatalf("scenario %s failed: %v", sc.name, err)
		}
		flux := float64(sc.jobsCount) / wall.Seconds()
		t.Logf("%-22s | N=%2d | wall=%7.2fs | flux=%6.2f jobs/s | avg/job=%6.0fms",
			sc.name, sc.jobsCount, wall.Seconds(), flux, float64(wall.Milliseconds())/float64(sc.jobsCount))
	}
}

func runBenchScenario(
	t *testing.T,
	client *podrun.Client,
	image string,
	n int,
	pipeName string,
	payload string,
	delay time.Duration,
	mode string,
	workers int,
	inflight int,
) (time.Duration, error) {
	t.Helper()
	dsn := filepath.Join(t.TempDir(), "bench.db") + "?_pragma=busy_timeout(10000)"
	driver, err := sqlite.New(dsn, storageutil.NewTestBlobStore(t))
	if err != nil {
		return 0, err
	}
	if err := driver.Init(context.Background()); err != nil {
		return 0, err
	}
	defer func() { _ = driver.Close(context.Background()) }()

	q := jobs.NewQueue(driver.Jobs())
	reg := builtins.Registry()
	pool := jobs.NewWorkerPool(q, reg, driver, workers, nil, config.JobsConfig{
		PollInterval: 10 * time.Millisecond,
		StaleTimeout: 10 * time.Minute,
		MaxRetries:   1,
		MaxHops:      5,
	})

	localExec := &delayedExec{
		inner: &jobs.LocalExecutor{Pipelines: reg, MaxHops: 5},
		delay: delay,
	}
	if mode == "pod" {
		podExec := &delayedExec{
			inner: NewExecutor(client, Options{
				Provider:       "docker",
				Image:          image,
				DpkmsBin:       "dpkms",
				MaxInflight:    inflight,
				MaxPodsPerHour: 0,
				MaxPodDuration: 2 * time.Minute,
				MaxHops:        5,
			}),
			delay: delay,
		}
		pool.SetExecutor(NewRouter(localExec, podExec, []string{"ingest:code"}))
	} else if delay > 0 {
		pool.SetExecutor(localExec)
	}

	ids := make([]string, n)
	now := time.Now().Truncate(time.Second)
	for i := 0; i < n; i++ {
		id := fmt.Sprintf("b-%d", i)
		ids[i] = id
		if err := q.Enqueue(context.Background(), &storage.Job{
			ID:         id,
			Type:       "ingest:code",
			Status:     storage.JobPending,
			Payload:    fmt.Sprintf("item-%d\n%s", i, payload),
			Pipeline:   pipeName,
			Source:     "bench",
			MaxRetries: 1,
			CreatedAt:  now,
			UpdatedAt:  now,
		}); err != nil {
			return 0, err
		}
	}

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	start := time.Now()
	done := make(chan struct{})
	go func() {
		_ = pool.Start(ctx)
		close(done)
	}()

	for {
		completed := 0
		for _, id := range ids {
			j, err := q.Get(context.Background(), id)
			if err != nil {
				continue
			}
			if j.Status == storage.JobFailed {
				cancel()
				<-done
				return 0, fmt.Errorf("job %s failed: %s", id, j.Error)
			}
			if j.Status == storage.JobCompleted {
				completed++
			}
		}
		if completed == n {
			break
		}
		if ctx.Err() != nil {
			<-done
			return 0, ctx.Err()
		}
		time.Sleep(15 * time.Millisecond)
	}
	wall := time.Since(start)
	cancel()
	<-done
	return wall, nil
}

type delayedExec struct {
	inner jobs.PipelineExecutor
	delay time.Duration
}

func (d *delayedExec) Execute(ctx context.Context, job *storage.Job) (*storage.KnowledgeObject, error) {
	if d.delay > 0 {
		select {
		case <-time.After(d.delay):
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
	return d.inner.Execute(ctx, job)
}

func summarizeDurations(ds []time.Duration) string {
	cp := append([]time.Duration(nil), ds...)
	slices.Sort(cp)
	var sum time.Duration
	for _, d := range cp {
		sum += d
	}
	mean := sum / time.Duration(len(cp))
	p50 := cp[len(cp)/2]
	p95 := cp[(len(cp)*95)/100]
	return fmt.Sprintf("mean=%dms p50=%dms p95=%dms min=%dms max=%dms",
		mean.Milliseconds(), p50.Milliseconds(), p95.Milliseconds(), cp[0].Milliseconds(), cp[len(cp)-1].Milliseconds())
}
