package podexec

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ideacrafterslabs/ctxt/internal/config"
	"github.com/ideacrafterslabs/ctxt/internal/jobs"
	"github.com/ideacrafterslabs/ctxt/internal/pipeline"
	"github.com/ideacrafterslabs/ctxt/internal/pipeline/builtins"
	"github.com/ideacrafterslabs/ctxt/internal/podrun"
	"github.com/ideacrafterslabs/ctxt/internal/storage"
	"github.com/ideacrafterslabs/ctxt/internal/storage/sqlite"
	"github.com/ideacrafterslabs/ctxt/internal/storageutil"
	"github.com/ideacrafterslabs/ctxt/internal/testguard"
)

func TestMain(m *testing.M) {
	testguard.Main(m, nil, "ctxt", "dpkms")
}

const fullJobCapsJSON = `{"tool":"pod","schema_version":"1.0","leaves":[{"path":"pod create"},{"path":"pod upload"},{"path":"pod exec"},{"path":"pod destroy"}]}`

type scriptRunner struct {
	run func(ctx context.Context, argv []string, opts podrun.RunOpts) (podrun.Result, error)
}

func (s *scriptRunner) Run(ctx context.Context, argv []string, opts podrun.RunOpts) (podrun.Result, error) {
	return s.run(ctx, argv, opts)
}

func (s *scriptRunner) Start(_ context.Context, _ []string, _ podrun.RunOpts) (podrun.Proc, error) {
	return nil, errors.New("not used")
}

func TestRouterTopicAllowlistAndNofanout(t *testing.T) {
	var localHits, podHits int
	local := &funcExec{fn: func(_ context.Context, j *storage.Job) (*storage.KnowledgeObject, error) {
		localHits++
		return &storage.KnowledgeObject{ID: "local-" + j.ID}, nil
	}}
	podE := &funcExec{fn: func(_ context.Context, j *storage.Job) (*storage.KnowledgeObject, error) {
		podHits++
		return &storage.KnowledgeObject{ID: "pod-" + j.ID}, nil
	}}
	r := NewRouter(local, podE, []string{"ingest:document", "ingest:url"})

	cases := []struct {
		jobType string
		wantPod bool
	}{
		{"ingest:document", true},
		{"ingest:url:nofanout", true},
		{"ingest:text", false},
		{"object:reprocess", false},
	}
	for _, tc := range cases {
		obj, err := r.Execute(context.Background(), &storage.Job{ID: "1", Type: tc.jobType})
		if err != nil {
			t.Fatalf("%s: %v", tc.jobType, err)
		}
		if tc.wantPod && !strings.HasPrefix(obj.ID, "pod-") {
			t.Errorf("%s routed locally, want pod", tc.jobType)
		}
		if !tc.wantPod && !strings.HasPrefix(obj.ID, "local-") {
			t.Errorf("%s routed to pod, want local", tc.jobType)
		}
	}
	if localHits != 2 || podHits != 2 {
		t.Fatalf("hits local=%d pod=%d, want 2/2", localHits, podHits)
	}
}

type funcExec struct {
	fn func(ctx context.Context, job *storage.Job) (*storage.KnowledgeObject, error)
}

func (f *funcExec) Execute(ctx context.Context, job *storage.Job) (*storage.KnowledgeObject, error) {
	return f.fn(ctx, job)
}

func newTestSQLiteDriver(t *testing.T) storage.StorageDriver {
	t.Helper()
	dsn := filepath.Join(t.TempDir(), "jobs.db") + "?_pragma=busy_timeout(5000)"
	driver, err := sqlite.New(dsn, storageutil.NewTestBlobStore(t))
	if err != nil {
		t.Fatal(err)
	}
	if err := driver.Init(context.Background()); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = driver.Close(context.Background()) })
	return driver
}

func waitJobsDone(q *jobs.Queue, ids []string, timeout time.Duration) {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		allDone := true
		for _, id := range ids {
			j, err := q.Get(context.Background(), id)
			if err != nil || j == nil || j.Status != storage.JobCompleted {
				allDone = false
				break
			}
		}
		if allDone {
			return
		}
		time.Sleep(25 * time.Millisecond)
	}
}

func newRunDraftFakeRunner(events *[]string, mu *sync.Mutex) *scriptRunner {
	var uploadedReq jobs.DraftJobRequest
	return &scriptRunner{
		run: func(ctx context.Context, argv []string, _ podrun.RunOpts) (podrun.Result, error) {
			verb := argv[1]
			mu.Lock()
			*events = append(*events, verb)
			mu.Unlock()
			switch verb {
			case "capabilities":
				return podrun.Result{Stdout: []byte(fullJobCapsJSON)}, nil
			case "create":
				return podrun.Result{Stdout: []byte(`{"name":"` + argv[2] + `","provider":"docker","status":"running"}`)}, nil
			case "upload":
				b, err := os.ReadFile(argv[3])
				if err != nil {
					return podrun.Result{ExitCode: 1}, err
				}
				_ = json.Unmarshal(b, &uploadedReq)
				return podrun.Result{}, nil
			case "exec":
				draft, err := jobs.RunDraft(ctx, builtins.Registry(), &uploadedReq.Job, uploadedReq.MaxHops)
				var res jobs.DraftJobResult
				if err != nil {
					res.Error = err.Error()
					res.Permanent = pipeline.IsPermanent(err)
				} else {
					res.Draft = draft
				}
				b, _ := json.Marshal(res)
				return podrun.Result{Stdout: b}, nil
			case "destroy":
				return podrun.Result{}, nil
			default:
				return podrun.Result{}, fmt.Errorf("unexpected verb %s", verb)
			}
		},
	}
}

func TestExecutorWorkerPoolEndToEndAndTeardown(t *testing.T) {
	var mu sync.Mutex
	var events []string
	sr := newRunDraftFakeRunner(&events, &mu)

	client := podrun.NewClient("/bin/pod", sr, podrun.DefaultTimeouts())
	exec := NewExecutor(client, Options{
		Provider:       "docker",
		Image:          "ctxt-worker:local",
		MaxInflight:    2,
		MaxPodsPerHour: 10,
		MaxPodDuration: 30 * time.Second,
	})

	driver := newTestSQLiteDriver(t)
	q := jobs.NewQueue(driver.Jobs())
	reg := builtins.Registry()
	router := NewRouter(&jobs.LocalExecutor{Pipelines: reg, MaxHops: 5}, exec, []string{"ingest:code"})

	pool := jobs.NewWorkerPool(q, reg, driver, 1, nil, config.JobsConfig{
		PollInterval: 20 * time.Millisecond,
		StaleTimeout: 5 * time.Minute,
		MaxRetries:   2,
		MaxHops:      5,
	})
	pool.SetExecutor(router)

	now := time.Now().Truncate(time.Second)
	for _, j := range []*storage.Job{
		{ID: "job-pod-1", Type: "ingest:code", Status: storage.JobPending, Payload: "package main\nfunc main() {}\n", Pipeline: "text.short", Source: "test", MaxRetries: 2, CreatedAt: now, UpdatedAt: now},
		{ID: "job-local-1", Type: "ingest:text", Status: storage.JobPending, Payload: "plain local text note", Pipeline: "text.short", Source: "test", MaxRetries: 2, CreatedAt: now.Add(time.Second), UpdatedAt: now.Add(time.Second)},
	} {
		if err := q.Enqueue(context.Background(), j); err != nil {
			t.Fatal(err)
		}
	}

	runCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	done := make(chan struct{})
	go func() {
		_ = pool.Start(runCtx)
		close(done)
	}()

	ids := []string{"job-pod-1", "job-local-1"}
	waitJobsDone(q, ids, 4*time.Second)
	cancel()
	<-done

	for _, id := range ids {
		j, err := q.Get(context.Background(), id)
		if err != nil || j.Status != storage.JobCompleted || j.ResultID == "" {
			t.Fatalf("job %s not completed cleanly: %+v err=%v", id, j, err)
		}
		if _, err := driver.Objects().Get(context.Background(), j.ResultID); err != nil {
			t.Fatalf("stored object for %s missing: %v", id, err)
		}
	}

	wantOrder := "capabilities,create,upload,exec,destroy"
	if got := strings.Join(events, ","); got != wantOrder {
		t.Fatalf("pod calls = %q, want %q", got, wantOrder)
	}
}

func TestExecutorConcurrencyCapAndTeardownOnFailure(t *testing.T) {
	var current, peak, destroys atomic.Int32
	sr := &scriptRunner{
		run: func(_ context.Context, argv []string, _ podrun.RunOpts) (podrun.Result, error) {
			switch argv[1] {
			case "capabilities":
				return podrun.Result{Stdout: []byte(fullJobCapsJSON)}, nil
			case "create":
				n := current.Add(1)
				for {
					p := peak.Load()
					if n <= p || peak.CompareAndSwap(p, n) {
						break
					}
				}
				return podrun.Result{Stdout: []byte(`{"name":"` + argv[2] + `","provider":"docker","status":"running"}`)}, nil
			case "upload":
				return podrun.Result{}, nil
			case "exec":
				time.Sleep(40 * time.Millisecond)
				return podrun.Result{ExitCode: 1, Stderr: []byte("boom")}, errors.New("exit 1")
			case "destroy":
				current.Add(-1)
				destroys.Add(1)
				return podrun.Result{}, nil
			default:
				return podrun.Result{}, nil
			}
		},
	}

	client := podrun.NewClient("/bin/pod", sr, podrun.DefaultTimeouts())
	exec := NewExecutor(client, Options{
		MaxInflight:    2,
		MaxPodsPerHour: 100,
		MaxPodDuration: 5 * time.Second,
	})

	var wg sync.WaitGroup
	for i := 0; i < 6; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			_, err := exec.Execute(context.Background(), &storage.Job{
				ID:       fmt.Sprintf("job-%d", idx),
				Type:     "ingest:url",
				Pipeline: "text.short",
			})
			if err == nil || pipeline.IsPermanent(err) {
				t.Errorf("job-%d want transient error, got %v", idx, err)
			}
		}(i)
	}
	wg.Wait()

	if peak.Load() > 2 {
		t.Fatalf("peak concurrent pods = %d, exceeded max_inflight=2", peak.Load())
	}
	if destroys.Load() != 6 {
		t.Fatalf("destroys = %d, want 6 (every created pod torn down on failure)", destroys.Load())
	}
}

func TestExecutorMissingBinaryAndHourlyBudget(t *testing.T) {
	var destroys atomic.Int32
	sr := &scriptRunner{
		run: func(_ context.Context, argv []string, _ podrun.RunOpts) (podrun.Result, error) {
			switch argv[1] {
			case "capabilities":
				return podrun.Result{Stdout: []byte(fullJobCapsJSON)}, nil
			case "create":
				return podrun.Result{Stdout: []byte(`{"name":"` + argv[2] + `","provider":"docker","status":"running"}`)}, nil
			case "upload":
				return podrun.Result{}, nil
			case "exec":
				return podrun.Result{ExitCode: 127, Stderr: []byte("dpkms: not found")}, errors.New("exit 127")
			case "destroy":
				destroys.Add(1)
				return podrun.Result{}, nil
			default:
				return podrun.Result{}, nil
			}
		},
	}
	now := time.Date(2026, 11, 22, 12, 0, 0, 0, time.UTC)
	client := podrun.NewClient("/bin/pod", sr, podrun.DefaultTimeouts())
	exec := NewExecutor(client, Options{
		MaxInflight:    2,
		MaxPodsPerHour: 1,
		MaxPodDuration: 5 * time.Second,
		Now:            func() time.Time { return now },
	})

	// First job hits exit 127 -> PermanentError + teardown.
	_, err := exec.Execute(context.Background(), &storage.Job{ID: "j1", Type: "ingest:url", Pipeline: "text.short"})
	if !pipeline.IsPermanent(err) {
		t.Fatalf("exit 127 want PermanentError, got %v", err)
	}
	if destroys.Load() != 1 {
		t.Fatalf("expected teardown after exit 127, destroys=%d", destroys.Load())
	}

	// Second job within same hour hits hourly ceiling.
	_, err = exec.Execute(context.Background(), &storage.Job{ID: "j2", Type: "ingest:url", Pipeline: "text.short"})
	if err == nil || !strings.Contains(err.Error(), "hourly pod ceiling") {
		t.Fatalf("want hourly pod ceiling error, got %v", err)
	}
}
